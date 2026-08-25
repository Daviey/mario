# Repository Guidelines

Terminal (CLI) Mario-style platformer in Go — plus a browser WASM build — with an online high-score board on Supabase. Single module `mario`, stdlib-only, no external dependencies. Private repo: `github.com/Daviey/mario`.

## Project Overview

A fully playable SMB-style platformer rendered as half-block terminal pixels (native) or canvas pixels (browser). The engine is **fully deterministic** — no randomness, no wall-clock in simulation — so identical input sequences reproduce identical games (the tests rely on this). Scores submit to a Supabase PostgREST table; scores are client-attested (no verification layer — a deliberate design decision; the verifier lives in git history if ever needed).

## Architecture & Data Flow

```
stdin bytes ──▶ gameIO.feed() ──┬─(UI active)──▶ scoreUI.keys   (lbui.go)
                               └─(playing)───▶ input.Mapper.Feed
                                                │ Poll() each tick
60 Hz ticker ─▶ play() (main.go) ─▶ engine.Game.Update(engine.Input)
                                    │
                     render.Stream.Draw(g, ui) ─▶ worldFrame → Frame (square pixels)
                     native: Diff → ANSI half-blocks to stdout
                     wasm:   RenderPixels → RGB bytes → page's marioFrame()
```

- **Spine**: `play()` in `main.go` owns the 60 Hz `time.Ticker`; shared by native and WASM entries.
- **Input routing rule**: while any leaderboard screen holds the keyboard (`scoreUI.capturing()`), raw bytes never reach `input.Mapper` — typing `r` in name entry cannot restart the game (`gameio.go`).
- **Engine** (`engine/`): `Game.Update(Input)` advances one tick through states `StateTitle → StatePlaying ↔ StateDying/StateLevelClear → StateGameOver/StateWin`. Shared body/vec types in `entity.go`; physics in `physics.go`; levels are ASCII grids parsed by `ParseLevel`, built-ins in `levels.go`.
- **Render** (`render/`): pure function of engine state — `worldFrame()` paints a `Frame` (W×H pixel grid) using rune-art sprites (`sprites.go`) and the 3×5 pixel font (`font.go`); `blit()` packs 2 pixels per terminal cell (`▀`); `Diff()`/`Stream` emit only changed cells wrapped in synchronized-output mode. WASM uses `RenderPixels()` (RGB triplets for canvas). `Render`/`FrameANSI`/`Stream.Draw` take an optional `*ScoreUI` that swaps the world for the leaderboard screens, drawn as **real text cells** (not the pixel font); the browser build receives the same snapshot as JSON (`marioBoard`) and renders a DOM panel.
- **Leaderboard** (`lbui.go`, `board/`): `scoreUI` state machine (`UIOff/UIAsk/UIEntry/UIBoard`) driven by `tick(g)`; network runs off the tick loop via injectable `submit`/`fetch` funcs (nil → real `board.Client` from env). `board.Client` is a thin PostgREST wrapper (`Submit`, `Top`); RLS allows anon insert + public read only.
- **Platform split** by build tags: `term_unix.go` (`!windows`), `term_windows.go` (`windows`), `wasm.go` (`js`).

## Key Directories

| Path | Purpose |
|---|---|
| `engine/` | Pure game logic: states, physics, entities, levels, items, enemies |
| `render/` | Pixel frame → terminal/canvas pipeline, sprites, 3×5 font, UI screens, diffing |
| `input/` | Raw byte → `engine.Input` mapper; kitty keyboard protocol + legacy key-repeat fallback |
| `board/` | Supabase PostgREST client + `.env` loader |
| `supabase/migrations/` | SQL schema for the `scores` table (RLS + grants) — source of truth for the live DB |
| `web/` | Static browser shell: `index.html` (canvas + boot-screen loader), builds to `dist/web/` |
| root `*.go` | Entry points (`native.go`, `wasm.go`, `main.go`), input routing (`gameio.go`), leaderboard UI (`lbui.go`), player identity (`player.go`), `-scores` output (`scorescmd.go`) |

## Development Commands

```bash
make build        # native binary ./mario (CGO_ENABLED=0 forced by Makefile)
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
- **Purity split**: `engine.Update` mutates state; `render.*` is a pure function of state (no I/O, no clocks). UI/network side effects live in `lbui.go` behind injectable funcs.
- **Error handling**: gameplay never fails on leaderboard errors — submit/fetch failures degrade to `OFFLINE` status strings; `maybeSubmit`-style flows swallow config errors and stay silent.
- **Arcade-string constraint** (world/HUD overlays only — the leaderboard screens are real text now): anything drawn through the 3×5 pixel font may use `A-Z 0-9 space . - + / : ! ?` only. Uppercase everything; no `_`, no lowercase; long lines ladder down via `pickTextPx(candidates, maxW)`.
- **Names**: max 8 chars, charset `A-Z0-9 . -` (enforced by `sanitizeName` in `player.go` AND by a DB CHECK — keep them in sync).
- **Go 1.22 style**: `for i := range n` (project rule); errors wrapped with `%w`; table-driven tests.
- **Layout invariants**: title screen text positions come from `titleTextEls` (single source of truth); clouds must never paint a pixel inside a title text band (`cloudBlocked` does pixel-level suppression).
- **Player identity**: device UUID + name in `<UserConfigDir>/mario/player.json`, regenerated on corruption, never required for play.
- **Commit style**: short lowercase imperative subjects (`board: dark band covers header too`).

## Important Files

- `main.go` — package doc (controls, flags), shared `play()` loop, `runDemo` + `scriptInput` (the deterministic demo script used by tests and `-ui-preview`)
- `native.go` — CLI entry, flag inventory (`-demo -demoticks -level -width -basic -scores -ui-preview`), terminal lifecycle (raw mode, kitty push/pop, cleanup on signal), stdin pump
- `wasm.go` — browser entry; page contract: page provides `marioFrame(w,h,rgb)` and `marioBoard(json)` (leaderboard DOM text) before load; game exports `marioFeed(keys)`, `marioSize(worldPxW, worldPxH)`
- `gameio.go` — the one-keyboard-one-owner input router
- `lbui.go` — leaderboard state machine; entry keys: `ENTER` accept, `BS` delete, `ESC` back; board keys: `L`/`Q` close; title `l` opens
- `board/board.go` — `Client.Submit/Top`, `FromEnv` (`SUPABASE_URL`+`SUPABASE_KEY`, falling back to build-time `DefaultURL`/`DefaultKey` embedded by `make web` so the WASM build can reach the board), `LoadDotEnv`
- `supabase/migrations/20260825000000_scores.sql` — live table schema; apply changes here AND to the live DB
- `.env` (gitignored) — `SUPABASE_URL`, `SUPABASE_KEY` (publishable key — safe to embed), `SUPABASE_DB_PASSWORD`

## Runtime/Tooling Preferences

- Go ≥ 1.22, `CGO_ENABLED=0` always (static; NixOS host cannot run dynamic scratch builds — set `GOTMPDIR` if `/tmp` is noexec).
- No package manager, no Node tooling for the game itself; `web/` is plain HTML+JS consuming the `.wasm`.
- Terminal features are progressive enhancement: truecolor → 16-color fallback (`-basic` to force), kitty keyboard protocol → legacy key-repeat inference.
- **CI (release + Pages)**: tag push `v*` → `.github/workflows/release.yml` (tests → `make release` 5 binaries + `SHA256SUMS` → `make web` → `mario-web.zip` → GitHub Release + `web-dist` artifact) → `.github/workflows/pages.yml` (fires via `workflow_run` on release success) re-packages that artifact and deploys to https://daviey.github.io/mario/. The split exists because the `github-pages` environment's protection rules reject tag refs — Pages deploys must run from a default-branch context (`workflow_run`); the policy REST API can't change this. No PR/push CI otherwise; no linters, no go.work, no Dockerfile — `make vet`/`make fmtcheck` are the local quality gates.
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
- **UI trigger keys must be mapper no-ops**: any byte the input mapper doesn't know maps to `AnyKey`, which starts the game on the title screen before `scoreUI` can see it. `'l'` (leaderboard) is deliberately mapped to a no-op event in `mappedKey` — a new UI hotkey needs the same treatment (regression: `input.TestLeaderKeyIsNotAGameKey`).
- The pixel font's missing glyphs are a *product constraint*: `[`, `_`, lowercase etc. render wrong — the name-entry field draws its own rails instead of brackets for exactly this reason.
- `engine.Input` is a fixed bool struct; anything needing character keys (name entry) must bypass the mapper via `gameIO`'s capture routing.
- **UI input capture vs escape sequences**: any screen that captures raw bytes (like `scoreUI`) must manually filter out multi-byte terminal escape sequences (e.g. `\x1b[...` arrow keys). Otherwise, the leading `0x1b` is misinterpreted as a literal ESC keypress, instantly closing the UI.
- Viewport extremes are real: `ViewH` can be 4 tiles tall; every new screen must survive the size sweeps or `TestScoreUIAllViewportSizes` fails.
- The WASM build shares `play()` but not `render.Stream` — it rasterizes via `RenderPixels`; changes to overlay drawing must work through both `worldFrame` paths.
- `web/index.html` contains a self-contained boot loader that ports the game's art/palette/font — if sprites/font change, consider whether the loader art should follow.
- **CI runners use dash as `/bin/sh`** — Makefile recipes must stay POSIX (no `<<<`/bashisms): they pass locally on NixOS bash and die only in Actions. The runner's toolcache Go also lacks `$(go env GOROOT)/lib/wasm/wasm_exec.js` (hence the `find` in `make web`), and `dist/mario-*` already matches `mario-web.zip` — never list it twice in `gh release create`.
- **Re-running a release run re-executes `gh release create`** — delete the tag's release first or it fails on existing assets. Reruns use the tag commit's workflow file; the `pages` (`workflow_run`) job always uses the default-branch file.
- Live Supabase details: project `jdmgfpzxcdpyylhwdbkz` (direct IPv6 `db.<ref>.supabase.co` reachable from this host; pooler tenant lookup fails); schema changes go through the migration file + direct DB apply.
