# Security Policy

## Supported surfaces

Only the latest `main` and the most recent release are supported.

| Surface | Location |
|---|---|
| Web build | https://mario.baby and https://daviey.github.io/mario/ (same WASM build; also bundled inside the Android APK and iOS IPA WebView shells) |
| SSH game server | `ssh mario@mario.baby` — the game only; there is no shell |
| Native binaries & packages | GitHub Releases (`mario-*`, `.deb`, `.rpm`, `.AppImage`, macOS `.app`, EFI image, BIOS ISO) |

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting:

**https://github.com/Daviey/mario/security/advisories/new**

Do not open a public issue or PR for anything security-sensitive.

Helpful details: which surface, steps to reproduce (a script, a crafted replay
JSON, or an SSH transcript is ideal), and the impact you see. For the
leaderboard, the exact HTTP requests matter.

This is a hobby project: no bounty, no SLA. Expect an acknowledgement within
a few days and a fix that depends on severity. Coordinated disclosure via the
GitHub Security Advisory, with credit if you want it.

## In scope

- **`internal/sshd` (the from-scratch SSH2 server)**: KEX/host-key
  verification, MAC/encryption, packet parsing, channel and session
  isolation — anything that breaks the "one game session per connection,
  nothing else runs" contract or yields code execution. The mosh handshake
  argv whitelist (`-mosh`) is a particularly sharp edge: escaping it into
  arbitrary command execution is a critical report.
- **Leaderboard integrity**: landing a row that does not reproduce its
  claimed score (replay-verifier bypass), forging `verified` status, or
  writing rows that bypass the PoW/rate-limit triggers in a way that scales.
- **Data exposure**: reading columns the anon role is not granted (`ip`,
  `device_id`, the operator diagnostics) through PostgREST/RPC/RLS mistakes,
  or leaking other players' data via `board_rows`.
- **Injection through peer-stored data**: any path where a stored name or
  board field reaches a player's terminal or the page DOM without the shared
  charset sanitisation (terminal escape injection / XSS). The web page's CSP
  is deliberately narrow; a CSP escape is in scope.
- **Web shell & service worker**: cache poisoning that serves a different
  build's bytes as this origin, or any way the boot loader fetches/executes
  off-origin content.
- **Release engineering**: anything that makes a published artifact execute
  code beyond the game (build-time injection, packaging tricks).

## Out of scope

- **The game being unauthenticated.** `ssh mario@mario.baby` accepting any
  username with no password is the product, not a finding. The server exposes
  exactly one service: the game.
- **Cheating at your own client.** Modifying your own WASM/binary, replay
  files, or `localStorage` is only interesting if the result survives the
  server-side replay verifier — that would be an *in-scope* leaderboard
  integrity report, otherwise it is just a tampered client.
- **Resource exhaustion against the single-host SSH/web endpoints or the
  Supabase free tier.** The SSH server has admission queueing and rate
  limits, but a small VPS is trivially saturable by design; volumetric
  flooding is not actionable.
- **Automated scanner output without a demonstrated impact**, social
  engineering, phishing, or physical access.
- **Self-XSS** or issues that require already controlling the reporter's own
  browser profile.

## Safe harbour

Good-faith research against the surfaces above — with reasonable effort to
avoid privacy violations, data destruction, and service degradation — is
welcome and will not be met with legal action.
