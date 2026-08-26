# Repository Guidelines

Terminal (CLI) Mario-style platformer in Go — plus a browser WASM build — with an online high-score board on Supabase. Single module `github.com/Daviey/mario`, stdlib-only, no external dependencies. Private repo: `github.com/Daviey/mario`.

## Project Overview

A fully playable SMB-style platformer rendered as half-block terminal pixels (native) or canvas pixels (browser). The engine is **fully deterministic** — no randomness, no wall-clock in simulation — so identical input sequences reproduce identical games (the tests rely on this). Scores submit to a Supabase PostgREST table; scores are client-attested (no verification layer — a deliberate design decision; the verifier lives in git history if ever needed).

## Architecture & Data Flow

```text
stdin bytes ──▶ gameIO.feed() ──┬─(UI active)──▶ io.plain ──▶ scoreUI.keys (lbui.go)
                               ├─(title 'l')──▶ io.plain ──▶ scoreUI.note
                               └─(playing)───▶ input.Mapper.Feed
                                                │ Poll() each tick
60 Hz ticker ─▶ play() (main.go) ─▶ engine.Game.Update(engine.Input)
                                    │
                     render.Stream.Draw(g, ui) ─▶ worldFrame → Frame (square pixels)
                     native: Diff → ANSI half-blocks to stdout
                     wasm:   RenderPixels → RGB bytes → page's marioFrame()
```

- **Spine**: the importable root package `mario` (library facade, `mario.go`) owns the 60 Hz loop: `App.Run(*render.Stream)` blocks for the native terminal; `App.Step()` drives one tick for clock-owning consumers (the browser build). Both entries are thin: `cmd/mario` (native CLI) and `cmd/web` (WASM).
- **Input routing rule**: while any leaderboard screen holds the keyboard (`scoreUI.capturing()`), raw bytes are decoded via `input.PlainDecoder` and passed to the UI. The mapper speaks `CSI u` natively, but the UI is strictly byte-oriented (`gameio.go`).
- **Engine** (`engine/`): `Game.Update(Input)` advances one tick (states above). Shared body/vec types in `entity.go`; physics in `physics.go`; levels are ASCII grids parsed by `ParseLevel`, built-ins in `levels.go` (four worlds: 1-1, underground 1-2, 1-3, 2-1). Power states are `PowerSmall/PowerSuper/PowerFire` (`Player.Power`; the old `Player.Super` bool is gone). Fireballs (max 2, Run-key rising edge), piranha plants (rise/sink cycle + mercy rule while the player stands near), the stomp/shell combo ladder (`stompLadder`, 1-UP past the end), HURRY at `Time ≤ 100`, and sound events (`Game.Events`, drained each tick by the host; the WASM build forwards them to the page's `marioSfx`). `daily.go` generates the deterministic daily-challenge level from a date seed (`DailyLevelFor`), with structural solvability tests guarding pit widths and plant placement.
- **Themes**: `Level.Theme` (`ThemeOverworld`/`ThemeUnderground`) re-skins the palette in `render.paletteFor` — underground = black sky, blue bricks, brick ceiling, no clouds/hills/bushes.
- **Render** (`render/`): pure function of engine state — `worldFrame()` paints a `Frame` (W×H pixel grid) using rune-art sprites (`sprites.go`) and the 3×5 pixel font (`font.go`); `blit()` packs 2 pixels per terminal cell (`▀`); `Diff()`/`Stream` emit only changed cells wrapped in synchronized-output mode. WASM uses `RenderPixels()` (RGB triplets for canvas). `Render`/`FrameANSI`/`Stream.Draw` take an optional `*ScoreUI` that swaps the world for the leaderboard screens, drawn as **real text cells** (not the pixel font); the browser build receives the same snapshot as JSON (`marioBoard`) and renders a DOM panel.
- **Leaderboard** (`internal/ui`, `board/`): `ui.UI` state machine (`UIOff/UIAsk/UIEntry/UIBoard`) driven by `Tick(g)`; network runs off the tick loop via injectable `submit`/`fetch` funcs (nil → real `board.Client` from env). `board.Client` is a thin PostgREST wrapper (`Submit` carries `mode`/`day`, `Top` = classic, `TopMode` filters daily by UTC day). After a submit the board shows `YOU ARE #N` (rank = first Mine row). Local best lives in `player.json` (`persist.SaveBest`) and shows on the title + game-over screens; the engine keeps it live in `Game.Best`.
- **Platform split** by build tags: `term_unix.go` (`!windows`), `term_windows.go` (`windows`), `wasm.go` (`js`).

## Key Directories

| Path | Purpose |
|---|---|
| `engine/` | Pure game logic: states, physics, entities, levels, items, enemies |
| `render/` | Pixel frame → terminal/canvas pipeline, sprites, 3×5 font, UI screens, diffing |
| `input/` | Raw byte → `engine.Input` mapper; kitty protocol `CSI u` decoding (`plain.go`) + legacy OS key-repeat inference with calibration persistence |
| `board/` | Supabase PostgREST client + `.env` loader |
| `supabase/migrations/` | SQL schema for the `scores` table (RLS + grants) — source of truth for the live DB |
| `web/` | Static browser shell: `index.html` (canvas + boot-screen loader), builds to `dist/web/` |
| root `*.go` | Entry points, input routing (`gameio.go`), calibration/identity persistence (`keys.go`, `player.go`), leaderboard UI (`lbui.go`) |

## Development Commands

```bash
make build        # native binary ./mario (CGO_ENABLED=0 forced by Makefile)
make check        # fmtcheck + vet + test (the CI gate)
make test         # go test ./...
make race         # tests under -race
make cover / vet / fmt / fmtcheck
make run          # build + run
make demo         # ./mario -demo (headless scripted run)
make release      # cross-compile linux/amd64+arm64, darwin/amd64+arm64, windows/amd64 → dist/
make web          # GOOS=js GOARCH=wasm → dist/web/ (embeds Supabase URL/publishable key from env or .env)
make web-serve    # serve dist/web at http://127.0.0.1:8417/

CGO_ENABLED=0 go test -run TestGameOverAutoAsksAndSubmits -v .   # single test
LIVE=1 go test -run TestLiveUISubmit -v .            # real-backend E2E (see Testing)
./mario -scores 10                                   # print leaderboard
./mario -ui-preview board                            # headless UI screens: ask|entry|board|title-board
# release: git tag vX.Y.Z && git push origin vX.Y.Z   # CI: all binaries → GH Release, dist/web → Pages
```

Go 1.22+ required (range-over-int used). On NixOS the host cannot exec dynamically-linked scratch binaries — never drop `CGO_ENABLED=0`.

## Code Conventions & Common Patterns

- **Stdlib only.** Adding a dependency needs strong justification; even the leaderboard client is plain `net/http` + `encoding/json`.
- **Determinism is load-bearing.** No `math/rand`, no `time.Now()` in engine or render logic; blink/pulse effects key off `g.Tick`.
- **Purity split**: `engine.Update` mutates state; `render.*` is a pure function of state (no I/O, no clocks). UI/network side effects live in `internal/ui` behind injectable funcs.
- **Error handling**: gameplay never fails on leaderboard errors — submit/fetch failures degrade to `OFFLINE` status strings; `maybeSubmit`-style flows swallow config errors and stay silent.
- **Arcade-string constraint** (world/HUD overlays only — the leaderboard screens are real text now): anything drawn through the 3×5 pixel font may use `A-Z 0-9 space . - + / : ! ?` only. Uppercase everything; no `_`, no lowercase; long lines ladder down via `pickTextPx(candidates, maxW)`.
- **Names**: max 8 chars, charset `A-Z0-9 . -` (enforced by `sanitizeName` in `player.go`, by a DB CHECK regex, AND by `sanitizeDisplayName` in `board` to protect the terminal/DOM from peer-stored legacy rows).
- **Go 1.22 style**: `for i := range n` (project rule); errors wrapped with `%w`; table-driven tests.
- **Layout invariants**: title screen text positions come from `titleTextEls` (single source of truth); clouds must never paint a pixel inside a title text band (`cloudBlocked` does pixel-level suppression).
- **Persistence**: Player identity (UUID/name) lives in `player.json`; terminal input learning (OS repeat delay, per-key hold habits) in `keys.json`. Both live in `<UserConfigDir>/mario/` and are best-effort (silent fallback).
- **Commit style**: short lowercase imperative subjects (`board: dark band covers header too`).

## Important Files

- `mario.go` — library facade: `Options`/`App` (`New`, `Feed`, `Step`, `Run`, `UI`, `Quit`, `SaveCalibration`, `StartDaily`); package doc shows the embed-as-easter-egg pattern
- `demo.go` — `LoadLevels`, `RunDemo` (the deterministic demo script lives in `internal/ui/script.go` and doubles as the attract-mode input)
- `cmd/mario/main.go` — CLI entry, flag inventory (`-demo -demoticks -level -width -basic -scores -ui-preview`), terminal lifecycle (raw mode, kitty push/pop, cleanup on signal), stdin pump
- `cmd/web/main.go` — WASM entry (`go build ./cmd/web` under GOOS=js)
- Repo hygiene: never commit `.env`; new worktrees need it copied in manually (leaderboard silently offline otherwise). For local dev, `chmod 0600 .env` since it holds the DB password.
- **Leaderboard rate limits** (`supabase/migrations/20260825000002_rate_limits.sql`, applied live): a BEFORE INSERT trigger caps anon submissions at 10/min per source address (from Cloudflare's `cf-connecting-ip` via the `request.headers` setting) and 2/min per device_id. Bursty probes or LIVE tests must pace themselves or they get `400 too many submissions`. The peer-filled `scores.ip` column is hidden from anon by column-scoped SELECT grants — `select=*` fails for anon by design.
- **Leaderboard hardening** (`20260825000003_pow_and_privacy.sql`): submissions require 20-bit SHA-256 proof-of-work over `<device_id>:<score>:<nonce>` (solver in `board/pow.go`, verifier in `verify_pow()` trigger — keep difficulty in sync); `device_id` is NOT readable by anon (mine-ness arrives precomputed via the `board_rows` RPC, so don't reach for `Row.DeviceID`); nightly pg_cron job keeps top-500 rows / drops >60d. Reads go through POST `/rest/v1/rpc/board_rows`, inserts must carry `pow_nonce`.
- **Daily mode** (`20260826000000_daily_mode.sql`): scores carry `mode` ('classic'|'daily') and `day`; `board_rows(p_device_id, p_limit, p_mode, p_day)` — null/`classic` reads the classic board only, `'daily'`+day reads one challenge board. Enter via title `'d'` (terminal key or the web DAILY pad button) or `mario -daily`.
- `cmd/web/main.go` — browser entry; page contract: page provides `marioFrame(w,h,rgb)`, `marioBoard(json)` (leaderboard DOM text), optional `marioSfx(name)` (WebAudio synth; the game calls it once per engine sound event) and `marioTitle(bool)` (title-screen enter/exit; the page shows the DAILY pad button only there) before load; game exports `marioFeed(keys)`, `marioSize(worldPxW, worldPxH)`
- `web/` PWA: `manifest.webmanifest`, `sw.js` (cache-first, CACHE name seds to the git VERSION at `make web` — same-commit rebuilds serve stale caches in dev; hard-reload or bump to bust), `icons/` (regenerate via `go run ./tools/genicon`)
- `internal/ui/router.go` — the one-keyboard-one-owner input router (includes `PlainDecoder` for UI text entry)
- `internal/ui/lbui.go` — leaderboard state machine; entry keys: `ENTER` accept, `BS` delete, `ESC` back; board keys: `L`/`Q` close; title `l` opens
- `internal/persist/` — loading/saving of `keys.json` input calibration to prevent legacy repeat-delay stutters across sessions
- `board/board.go` — `Client.Submit/Top`, `FromEnv` (`SUPABASE_URL`+`SUPABASE_KEY`, falling back to build-time `DefaultURL`/`DefaultKey` embedded by `make web` so the WASM build can reach the board), `LoadDotEnv`
- `supabase/migrations/20260825000000_scores.sql` — live table schema; apply changes here AND to the live DB
- `.env` (gitignored) — `SUPABASE_URL`, `SUPABASE_KEY` (publishable key — safe to embed), `SUPABASE_DB_PASSWORD`
- `icon.ico` / `icon.rc` / `mario_windows_amd64.syso` — Windows exe icon. Regenerate via `tools/icongen` (renders `render/sprites.go` art into a PNG-entry .ico) then mingw `windres` (nix shell `pkgsCross.mingwW64.buildPackages.gcc`). The **syso is committed**: every windows/amd64 `go build` links it automatically, so CI needs no mingw toolchain — only regenerate when sprite art changes. The web favicon is NOT a file: `web/index.html` draws it at runtime onto a canvas from the same palette/sprite data (CSP `img-src data:`).

## Runtime/Tooling Preferences

- Go ≥ 1.22, `CGO_ENABLED=0` always (static; NixOS host cannot run dynamic scratch builds — set `GOTMPDIR` if `/tmp` is noexec).
- No package manager, no Node tooling for the game itself; `web/` is plain HTML+JS consuming the `.wasm`.
- Terminal features are progressive enhancement: truecolor → 16-color fallback (`-basic`), kitty keyboard protocol (flags `1|2|8` for explicit press/repeat/release) → legacy key-repeat inference.
- **CI (push + release + Pages)**: push to `main` and PRs → `.github/workflows/ci.yml` on the self-hosted `mario` runner: `make check` + `make race` + native build + `make release` (all cross-targets) + a GOOS=js web compile-check (skipped if repo vars missing). Tag push `v*` → `.github/workflows/release.yml`: `make check` gate → a **build matrix** (one job per GOOS/GOARCH, `fail-fast: false`, each uploading `mario-<os>-<arch>` via `make <os>/<arch>`) + a `web` job (uploads the `web-dist` artifact) → tag-gated `release` job that merges artifacts, writes `SHA256SUMS` (+ `mario-web.zip`), and publishes; reruns are idempotent (existing release → `gh release upload --clobber`). → `.github/workflows/pages.yml` (fires via `workflow_run` on release success) re-packages the `web-dist` artifact and deploys to https://daviey.github.io/mario/. The split exists because the `github-pages` environment's protection rules reject tag refs — Pages deploys must run from a default-branch context (`workflow_run`); the policy REST API can't change this. Single self-hosted runner: matrix jobs queue serially (signal, not speed). No linters, no go.work, no Dockerfile — `make vet`/`make fmtcheck`/`make check` are the quality gates.
- Repo hygiene: never commit `.env`; new worktrees need it copied in manually (leaderboard silently offline otherwise).

## Testing & QA

- stdlib `testing` only; no assertion libraries, no golden files, no `t.Parallel()`.
- Assertions are programmatic: pixel reads via `Frame.At`/screen helpers, string-contains on ANSI output, geometry checks.
- **Determinism tests** re-run the demo script and byte-compare output — introducing randomness into engine/render breaks them.
- **Sweep/property tests** iterate viewport sizes (widths 16–60, heights 4–`LevelHeight`) and camera positions; cloud tests assert non-vacuous suppression (`suppressed > 0`).
- **Fake backends**: `fakePostgREST` (board) and injectable `submit`/`fetch` (lbui) keep unit tests offline.
- **Live E2E**: `live_test.go` — `LIVE=1 go test -run TestLiveUISubmit -v .` drives the real UI machine against real Supabase (writes a `LIVEUI` row; delete it after via direct DB if you run it).
- **Live seed**: `live_seed_test.go` — `LIVE=1 go test -run TestLiveSeedOneRow -v .` inserts one `SEEDCHK` row so the in-game board has content to display; remove it from the Supabase dashboard afterwards (the anon key cannot delete).
- Visual inspection: `render/visual_test.go` dumps frames as ASCII (`-v`, skipped in `-short`); `./mario -ui-preview <mode>` prints real ANSI frames headlessly.
- Coverage: `make cover`; no enforced threshold.

## Gotchas

- **Worktree policy (this machine)**: agent work happens in linked worktrees (`git worktree add .worktrees/<feature> -b <branch>`, or sibling `../game-<feature>`), never in the direct checkout; copy `.env` in; merge back into `main` (expect divergence — the user commits concurrently) and re-run the full suite on merged `main` before pushing. **Hook-enforced since 2026-08-25**: `.omp/hooks/pre/worktree-guard.ts` blocks `edit`/`write` calls targeting the main checkout in every omp session here (`.worktrees/**` and sibling worktrees pass; `bash` — git merge/push — is untouched; the guard fails open on unusual shapes, so it is a guardrail, not a jail).
- **Check `git status` before `git add -A` on the main checkout** — the user keeps WIP test files there; sweeping them up has shipped half-done tests before.
- Ranged edits in this repo have repeatedly misfired (especially after `gofmt` renumbering); prefer read-then-full-file writes for multi-line changes, and single-line 1:1 replacements otherwise.
- **UI trigger keys must be mapper no-ops**: any byte the input mapper doesn't know maps to `AnyKey`, which starts the game on the title screen before `scoreUI` can see it. `'l'` (leaderboard) is deliberately mapped to a no-op event in `mappedKey` — a new UI hotkey needs the same treatment (regression: `input.TestLeaderKeyIsNotAGameKey`). `'d'` (daily) CANNOT be a no-op (it is WASD-Right), so its title trigger is checked in `Router.TakeDailyAtTitle` BEFORE the engine update consumes the same press as Right — keep that ordering.
- **Plant markers are float-adjacent**: `Builder.Plant` stores `{x+0.65, topRow}` and `Builder.Rows` recomputes the marker column with `math.Round(p.X-0.65)` — a plain `int()` truncates `15.999…` and shifts the marker one column off its pipe (regression: `TestDailyLevelsSolvableShape`).
- The pixel font's missing glyphs are a *product constraint*: `[`, `_`, lowercase etc. render wrong — the name-entry field draws its own rails instead of brackets for exactly this reason.
- `engine.Input` is a fixed bool struct; anything needing character keys (name entry) must bypass the mapper via `gameIO`'s capture routing.
- **UI input capture vs escape sequences**: The UI consumes single bytes for typing/control. `input.PlainDecoder` strips out non-text escape sequences (preventing lone `0x1b` from instantly closing the UI) and translates `CSI u` text keys back into normal bytes.
- Viewport extremes are real: `ViewH` can be 4 tiles tall; every new screen must survive the size sweeps or `TestScoreUIAllViewportSizes` fails.
- The WASM build shares `play()` but not `render.Stream` — it rasterizes via `RenderPixels`; changes to overlay drawing must work through both `worldFrame` paths.
- `web/index.html` contains a self-contained boot loader that ports the game's art/palette/font — if sprites/font change, consider whether the loader art should follow.
- **Mobile Web Support**: The browser build bypasses virtual keyboard quirks by rendering its own on-screen gamepad and alphanumeric keypad (`#pad`). The page reads `st.mode` from `marioBoard(json)` to swap control clusters contextually; `marioTitle(bool)` toggles the DAILY button (hidden in play — a plain 'd' tap is a Right press). The CSS uses `100dvh`, `touch-action: none`, and `visualViewport` resize listeners to prevent iOS Safari scrolling and zooming. The page must feed every game key as a kitty press/release pair (`\e[<cp>;1:1u` / `\e[<cp>;1:3u`) — a bare text byte is a legacy press that decays after ~0.2s in the mapper, so pad holds would stall (regression: phone ◀/▶ died after a step). UI-capture paths are safe: `PlainDecoder` decodes press pairs back to plain bytes. HTML-attribute payloads can't hold raw control chars — use `&#13;` for Enter (a template-literal `"\\r"` is a literal backslash+r). Sound needs a user gesture first (autoplay policy) — the page lazily creates/resumes its AudioContext on the first pointerdown/keydown.
- **Board Restart**: Pressing `r` on the leaderboard closes the board, sets `u.restart`, and injects an `Input.Restart` edge via `gameio.poll()`. This cleans up UI state (`asked = false`) so the next game-over can prompt for submission again.
- **CI runners use dash as `/bin/sh`** — Makefile recipes must stay POSIX (no `<<<`/bashisms): they pass locally on NixOS bash and die only in Actions. The runner's toolcache Go also lacks `$(go env GOROOT)/lib/wasm/wasm_exec.js` (hence the `find` in `make web`), and `dist/mario-*` already matches `mario-web.zip` — never list it twice in `gh release create`.
- **Re-running a release run** is idempotent since the matrix rework: `gh release create` failure falls back to `gh release upload --clobber`, refreshing assets instead of dying on "already_exists". Reruns still use the tag commit's workflow file; the `pages` (`workflow_run`) job always uses the default-branch file.
- Live Supabase details: project `jdmgfpzxcdpyylhwdbkz` (direct IPv6 `db.<ref>.supabase.co` reachable from this host; pooler tenant lookup fails); schema changes go through the migration file + direct DB apply.


## Agent Maintenance & Self-Correction

<critical>
- **Continuous Documentation**: Agents MUST evaluate whether `AGENTS.md` requires an update during the Cleanup phase of any task.
- **Update Threshold**: Do NOT routinely update `AGENTS.md` for trivial bug fixes, standard feature additions, or localized refactors. ONLY update it if you have introduced a new architectural pattern, changed an interface/API contract described here, or if you need to remove deprecated/obsolete information.
</critical>