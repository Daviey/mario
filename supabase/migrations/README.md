# Migration operating rules

## Applied migrations are append-only history

Never edit a migration that has been applied to the live Supabase
project. The live database is the source of truth; the files here are a
record of what was run, not a spec the database is re-derived from. A
rewritten file also desyncs any future replay (an out-of-order or
half-matching apply leaves overloads and grants in the wrong state).

Schema changes go through a NEW timestamped file: write it, apply it
directly to the live DB (Supabase SQL editor or psql), then commit the
file here. Supabase's own migration tooling is not in the loop — the
apply is manual, so the file and the DB diverge the moment an old file
is edited instead of extended.

## Ordering hazard: the shared 20260826000000 prefix

`20260826000000_daily_mode.sql` and `20260826000000_level.sql` share a
timestamp prefix, and there is no sub-second tiebreaker. A fresh replay
MUST apply them in lexical order (`daily_mode` before `level` — the
order they were written and applied in). Sorting by anything but the
filename (mtime, discovery order) can transpose them.

Order matters because `board_rows` is redefined five times across the
history, each drop targeting a specific signature:

1. `20260825000003_pow_and_privacy.sql` — first `board_rows(uuid, int)`
2. `20260826000000_daily_mode.sql` — drop + redefine
3. `20260826000000_level.sql` — drop + redefine
4. `20260826000001_board_rows_union.sql` — drops BOTH the `(uuid, int)`
   and `(uuid, int, text, date)` signatures, then redefines
5. `20260826000002_replay_verification.sql` — drop + redefine;
   **final, authoritative** — this is the definition clients call today

Transposing steps 2–4 leaves multiple `board_rows` overloads alive (or
drops one that a later file still expects) and the API silently serving
the wrong definition. When adding a migration, take the next
timestamped prefix (never reuse one, even for a "companion" file).

## The pow difficulty constant is duplicated

`board/pow.go` (`powBits = 20`) and `verify_pow()` in
`20260825000003_pow_and_privacy.sql` encode the same difficulty twice:

- Go: the client grinds nonces until the sha256 of
  `device_id:score:nonce` has `powBits` leading zero bits.
- SQL: the `scores_pow` trigger rejects an insert unless the hex sha256
  of `device_id:score:pow_nonce` starts with `"00000"` — five hex
  zeros = 20 zero bits.

They MUST stay in sync: raise the SQL side without the Go side and
every client submission is rejected; raise the Go side first and
clients only waste cycles. If the difficulty ever changes, bump
`powBits` and ship a new migration that replaces `verify_pow()` in the
same commit.
