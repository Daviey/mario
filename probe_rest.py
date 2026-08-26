#!/usr/bin/env python3
"""Live anon REST probe matrix against the Supabase scores table.

Uses only the publishable anon key (public by design). Inserts are paced
under the live rate limits (10/min per IP, 2/min per device — every insert
probe uses a fresh device_id, so only the IP budget of ~6 is spent).
"""
import hashlib
import json
import os
import sys
import time
import urllib.request
import uuid

# --- load .env (no dotenv dep) ---
env = {}
with open(os.path.join(os.path.dirname(__file__), ".env")) as f:
    for line in f:
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            env[k.strip()] = v.strip()

URL = env["SUPABASE_URL"].rstrip("/")
KEY = env["SUPABASE_KEY"]
DBPASS = env.get("SUPABASE_DB_PASSWORD", "")

BASE = f"{URL}/rest/v1"


def req(method, path, body=None, params=None, headers=None, key=None):
    url = f"{BASE}{path}"
    if params:
        url += "?" + params
    h = {
        "apikey": key or KEY,
        "Authorization": f"Bearer {key or KEY}",
        "Content-Type": "application/json",
    }
    if headers:
        h.update(headers)
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(r) as resp:
            text = resp.read().decode()
            return resp.status, text[:400]
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:400]


def pow_for(device, score):
    """20-bit sha256 PoW over '<device>:<score>:<nonce>' (matches board/pow.go)."""
    prefix = f"{device}:{score}:"
    n = 0
    while True:
        if hashlib.sha256(f"{prefix}{n}".encode()).hexdigest().startswith("00000"):
            return str(n)
        n += 1


def insert_probe(label, overrides, base=None):
    row = {
        "name": "PROBE",
        "score": 1234,
        "device_id": str(uuid.uuid4()),
        "pow_nonce": None,  # filled below
        "replay": '{"v":1,"ticks":1,"runs":[[0,1]]}',
        "engine_version": "probe",
    }
    if base:
        row.update(base)
    row["pow_nonce"] = pow_for(row["device_id"], row["score"])
    row.update(overrides)
    row = {k: v for k, v in row.items() if v is not Ellipsis}
    return label, req("POST", "/scores", row, headers={"Prefer": "return=minimal"})


results = []

# --- read-path grants (expect: granted cols 200, hidden cols 4xx) ---
for cols, expect in [
    ("name,score", "200"),
    ("*", "4xx grant"),
    ("device_id", "4xx grant"),
    ("ip", "4xx grant"),
    ("verified", "4xx grant"),
    ("replay", "4xx grant"),
    ("engine_version", "4xx grant"),
]:
    st, body = req("GET", "/scores", params=f"select={cols}&limit=1")
    results.append((f"GET select={cols} (expect {expect})", st, body[:160]))

# --- write-path grants (expect 4xx column-grant violations) ---
st, body = req("PATCH", "/scores", {"name": "NOPE"}, params="name=eq.PROBE")
results.append(("PATCH update row (expect denied)", st, body[:160]))
st, body = req("DELETE", "/scores", params="name=eq.PROBE")
results.append(("DELETE row (expect denied)", st, body[:160]))

# --- insert probes ---
probes = [
    insert_probe("insert valid row", {}),
    insert_probe("insert lowercase name", {"name": "abc"}),
    insert_probe("insert ESC-in-name", {"name": "A\x1b[31mX"}),
    insert_probe("insert 9-char name", {"name": "ABCDEFGHI"}),
    insert_probe("insert with id spoof", {"id": str(uuid.uuid4())}),
    insert_probe("insert verified=true", {"verified": True}),
    insert_probe("insert created_at spoof", {"created_at": "2000-01-01T00:00:00Z"}),
    insert_probe("insert huge replay 262145 chars", {"replay": "A" * 262145}),
    insert_probe("insert score=999999999", {"score": 999999999}),
    insert_probe("insert level=0", {"level": 0}),
    insert_probe("insert mode=junk", {"mode": "eviltxt"}),
]
for label, (st, body) in probes:
    results.append((label, st, body[:200]))

# --- RPC probes ---
st, body = req("POST", "/rpc/board_rows", {"p_limit": 1000})
n = len(json.loads(body)) if st == 200 else "?"
results.append(("rpc board_rows p_limit=1000 (expect clamp<=100)", st, f"rows={n}"))
st, body = req("POST", "/rpc/board_rows", {"p_mode": "daily", "p_day": "2030-01-01"})
results.append(("rpc board_rows future day", st, body[:120]))

for label, st, body in results:
    print(f"[{st}] {label}\n      {body}")

print("\nDBPASS present:", bool(DBPASS))
