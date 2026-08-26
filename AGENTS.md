# Repository Guidelines

Terminal (CLI) Mario-style platformer in Go — plus a browser WASM build — with an online high-score board on Supabase. Single module `github.com/Daviey/mario`, stdlib-only, no external dependencies. Private repo: `github.com/Daviey/mario`. MIT-licensed.

## Project Overview

A fully playable SMB-style platformer rendered as half-block terminal pixels (native) or canvas pixels (browser). The engine is **fully deterministic** — no randomness, no wall-clock in simulation — so identical input sequences reproduce identical games (the tests rely on this). Scores submit to a Supabase PostgREST table carrying the run's input recording; a GitHub Action verifier replays each recording and keeps only rows that reproduce their claimed score (legacy pre-replay rows stay unmarked).

## Architecture & Data Flow

```text
stdin bytes ──▶ Router.Feed() ──┬─(UI active)──▶ Router.plain ──▶ UI.keys (internal/ui/ui.go)
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
- **Input routing rule**: while any leaderboard screen holds the keyboard (`scoreUI.capturing()`), raw bytes are decoded via `input.PlainDecoder` and passed to the UI. The mapper speaks `CSI u` natively, but the UI is strictly byte-oriented (`internal/ui/router.go`).
- **Engine** (`engine/`): `Game.Update(Input)` advances one tick through states `StateTitle → StateWorldCard → StatePlaying → (flag) StateFlagSlide → StateWalkCastle → StateScoreTick → StateWorldCard(next) … StateGameOver/StateWin` (world card skippable with AnyKey; death adds a 30-tick freeze-frame then respawns at `Level.CheckpointX` when reached). Shared body/vec types in `entity.go`; physics in `physics.go`; levels are ASCII grids parsed by `ParseLevel`, built-ins in `levels.go` (seven levels: 1-1, underground 1-2, 1-3, 2-1, underground 2-2, sky 2-3, castle 2-4). Power states are `PowerSmall/PowerSuper/PowerFire` plus star power (`Player.Star` ticks: kill-on-touch, absorbs all damage except lava/pits, palette flicker). Fireballs (max 2, Run-key rising edge), piranha plants (rise/sink cycle + mercy rule while the player stands near), paratroopas (`KindPara`: hop while walking; a stomp demotes them to walking koopas), fire bars (`FireBar`: angle is a pure function of the tick; level char 'h'), lava ('L': non-solid, kills on touch), hidden blocks ('H' coin / '1' 1-UP: invisible, non-solid, trigger only on a rising head-bump via `bumpHidden`), the stomp/shell combo ladder (`stompLadder`, 1-UP past the end), HURRY at `Time ≤ 100`, and sound events (`Game.Events`, drained each tick by the host; the WASM build forwards them to the page's `marioSfx`). `daily.go` generates the deterministic daily-challenge level from a date seed (`DailyLevelFor`), with structural solvability tests guarding pit widths and plant placement.
- **Themes**: `Level.Theme` (`ThemeOverworld`/`ThemeUnderground`/`ThemeSky`/`ThemeCastle`) re-skins the palette in `render.paletteFor` — underground = black sky, blue bricks, brick ceiling; sky = pale sky, sandstone terrain, clouds only; castle = black sky, grey stone, no sky dressing.
- **Replay verification** (`replay/`): `App.Step` records every run tick (`replay.Recorder`, RLE `{"v":1,"ticks":N,"runs":[[mask,count],...]}` wire format, 130k-tick cap); `replay.Run(levels, mode, json)` re-executes a stream and returns score/level/state. `replay.Run` starts classic runs from `Game.Reset()` and daily runs from `Game.BeginDaily()` — the recording's first tick is the first world-card tick (the title-dismiss tick is excluded; a card reached from `Dying`/`ScoreTick` continues the same recording, any other card entry starts a fresh one).
- **Leaderboard** (`internal/ui`, `board/`, `replay/`): `ui.UI` state machine (`UIOff/UIAsk/UIEntry/UIBoard`) driven by `Tick(g)`; network runs off the tick loop via injectable `submit`/`fetch` funcs (nil → real `board.Client` from env). Every submission carries the run's replay recording + `board.EngineVersion`; without a shippable recording the UI shows `UNRECORDED` and refuses to submit (the server's `require_replay` trigger enforces the same). `mario -verify-pending` (service key) fetches pending rows via `Client.Pending`, replays each with `replay.Run` (version/score/level must all match: keep → `SetVerified`, else `DeleteRow`) — the `verify-scores.yml` Action runs it every 15 min. Verified rows show a green ✓ on the terminal board and the web board (`board_rows` returns `verified`; rows sort score-desc then verified-desc). Local best lives in `player.json` (`persist.SaveBest`).
- **Platform split** by build tags: `cmd/mario/term_unix.go` (`!windows`), `cmd/mario/term_windows.go` (`windows`), `cmd/web/main.go` (`js`, the WASM entry).

## Key Directories

| Path | Purpose |
|---|---|
| `engine/` | Pure game logic: states, physics, entities, levels, items, enemies |
| `render/` | Pixel frame → terminal/canvas pipeline, sprites, 3×5 font, UI screens, diffing |
| `input/` | Raw byte → `engine.Input` mapper; kitty protocol `CSI u` decoding (`plain.go`) + legacy OS key-repeat inference with calibration persistence |
| `board/` | Supabase PostgREST client + `.env` loader + `EngineVersion` |
| `replay/` | Run recorder (RLE input log) and deterministic replay executor |
| `supabase/migrations/` | SQL schema for the `scores` table (RLS + grants) — source of truth for the live DB |
| `web/` | Static browser shell: `index.html` (canvas + boot-screen loader), builds to `dist/web/` |
| `packaging/` | Distro payload shared by .deb and AUR: `mario.6` manpage, `mario.desktop`, Debian `copyright`; `packaging/aur/` = AUR `PKGBUILD` + `.SRCINFO` |
| `internal/art/` | Standalone Mario-icon renderer (`art.IconPNG`) shared by genicon, mkdeb and the AUR build |
| `tools/mkdeb/` | Pure-stdlib .deb builder (manual ar + tar + gzip; deterministic — tests parse the archive back) |
| root `*.go` | The library facade and its thin entries only: `mario.go` (App), `demo.go` (LoadLevels/RunDemo), `preview.go` (ui-preview). Input routing lives in `internal/ui/router.go`, calibration/identity persistence in `internal/persist/` (`keys.go`, `player.go`), the leaderboard machine in `internal/ui/ui.go` |

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
make deb          # .deb packages (linux amd64+arm64) into dist/ via tools/mkdeb (no dpkg needed)
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
- **Names**: max 8 chars, charset `A-Z0-9 . -` (enforced by `sanitizeName` in `internal/persist/player.go`, by a DB CHECK regex, AND by `sanitizeDisplayName` in `board` to protect the terminal/DOM from peer-stored legacy rows).
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
- **Leaderboard hardening** (`20260825000003_pow_and_privacy.sql`): submissions require 20-bit SHA-256 proof-of-work over `<device_id>:<score>:<nonce>` (solver in `board/pow.go`, verifier in `verify_pow()` trigger — keep difficulty in sync); `device_id` is NOT readable by anon (mine-ness arrives precomputed via the `board_rows` RPC, so don't reach for `Row.DeviceID`); nightly pg_cron job keeps top-500 rows / drops >60d. Reads go through POST `/rest/v1/rpc/board_rows`, inserts must carry `pow_nonce`. Submissions also carry `level` (1-based level reached, clamped 1..99 client-side and by a DB CHECK; legacy rows read as 1) — `board_rows` returns it and every board surface renders it as an `L<n>` column (`20260826000000_level.sql`).
- **Daily mode** (`20260826000000_daily_mode.sql`, merged with the level column in `20260826000001_board_rows_union.sql`): scores carry `mode` ('classic'|'daily') and `day`; `board_rows(p_device_id, p_limit, p_mode, p_day)` — null/`classic` reads the classic board only, `'daily'`+day reads one challenge board. Enter via title `'d'` (terminal key or the web DAILY pad button) or `mario -daily`. The UI's default fetch branches to `TopMode` when the run was daily — keep that wired through `UI.dailyMode()`.
- `cmd/web/main.go` — browser entry; page contract: page provides `marioFrame(w,h,rgb)`, `marioBoard(json)` (leaderboard DOM text), optional `marioSfx(name)` (WebAudio synth; called once per engine sound event) and `marioTitle(bool)` (title-screen enter/exit; the page shows the DAILY pad button only there) before load; game exports `marioFeed(keys)`, `marioSize(worldPxW, worldPxH)`
- `web/` PWA: `manifest.webmanifest`, `sw.js` (cache-first, CACHE name seds to the git VERSION at `make web` — same-commit rebuilds serve stale caches in dev; hard-reload or bump to bust), `icons/` (regenerate via `go run ./tools/genicon`)- `internal/ui/router.go` — the one-keyboard-one-owner input router (includes `PlainDecoder` for UI text entry)
- `internal/ui/ui.go` — leaderboard state machine; entry keys: `ENTER` accept, `BS` delete, `ESC` back; board keys: `L`/`Q` close; title `l` opens
- `internal/persist/` — loading/saving of `keys.json` input calibration to prevent legacy repeat-delay stutters across sessions
- `board/board.go` — `Client.Submit/Top`, `FromEnv` (`SUPABASE_URL`+`SUPABASE_KEY`, falling back to build-time `DefaultURL`/`DefaultKey` embedded by `make web` so the WASM build can reach the board), `LoadDotEnv`
- `supabase/migrations/20260825000000_scores.sql` — live table schema; apply changes here AND to the live DB
- `.env` (gitignored) — `SUPABASE_URL`, `SUPABASE_KEY` (publishable key — safe to embed), `SUPABASE_DB_PASSWORD`, and optionally `SUPABASE_SERVICE_KEY` (the `sb_secret_` dashboard key — NEVER embed or ship; local `mario -verify-pending` and the `verify-scores.yml` Action secret only)
- `icon.ico` / `icon.rc` / `mario_windows_amd64.syso` — Windows exe icon. Regenerate via `tools/icongen` (renders `render/sprites.go` art into a PNG-entry .ico) then mingw `windres` (nix shell `pkgsCross.mingwW64.buildPackages.gcc`). The **syso is committed**: every windows/amd64 `go build` links it automatically, so CI needs no mingw toolchain — only regenerate when sprite art changes. The web favicon is NOT a file: `web/index.html` draws it at runtime onto a canvas from the same palette/sprite data (CSP `img-src data:`).

## Runtime/Tooling Preferences

- Go ≥ 1.22, `CGO_ENABLED=0` always (static; NixOS host cannot run dynamic scratch builds — set `GOTMPDIR` if `/tmp` is noexec).
- No package manager, no Node tooling for the game itself; `web/` is plain HTML+JS consuming the `.wasm`.
- Terminal features are progressive enhancement: truecolor → 16-color fallback (`-basic`), kitty keyboard protocol (flags `1|2|8` for explicit press/repeat/release) → legacy key-repeat inference.
- **CI (push + release + Pages)**: push to `main` and PRs → `.github/workflows/ci.yml` on the self-hosted `mario` runner: `make check` + native build + `make release` (all cross-targets) + a GOOS=js web compile-check (skipped if repo vars missing). The runner image has **no C toolchain** — the race detector can never run there (`make race` is a local-only gate) and every Makefile goal must force `CGO_ENABLED=0` or it dies compiling `runtime/cgo`. Tag push `v*` → `.github/workflows/release.yml`: `make check` gate → a **build matrix** (one job per GOOS/GOARCH, `fail-fast: false`, each uploading `mario-<os>-<arch>` via `make <os>/<arch>`) — the two Linux jobs also run `make deb/<arch>` (`tools/mkdeb`, no dpkg on the runner) and upload `mario_<ver>_<arch>.deb` alongside the binary + a `web` job (uploads the `web-dist` artifact) → tag-gated `release` job that merges artifacts, writes `SHA256SUMS` (+ `mario-web.zip`), and publishes; reruns are idempotent (existing release → `gh release upload --clobber`). There is NO AUR publish job: `packaging/aur` is kept for LOCAL builds only (`makepkg -p packaging/aur/PKGBUILD` on a machine with a GitHub ssh key that can clone this private repo). → `.github/workflows/pages.yml` (fires via `workflow_run` on release success) re-packages the `web-dist` artifact and deploys to https://daviey.github.io/mario/. The split exists because the `github-pages` environment's protection rules reject tag refs — Pages deploys must run from a default-branch context (`workflow_run`); the policy REST API can't change this. Single self-hosted runner: matrix jobs queue serially (signal, not speed). No linters, no go.work, no Dockerfile — `make vet`/`make fmtcheck`/`make check` are the quality gates; `nix run nixpkgs#actionlint` lints the workflows (labels config in `.github/actionlint.yaml`).
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
- **Distro packaging**: `make deb` + `packaging/aur` + `flake.nix` are versioned off `git describe`/the release tag — no `EngineVersion` bump for packaging-only changes. The flake uses `buildGoModule` (CGO off comes from its env defaults — don't shadow `CGO_ENABLED` with a derivation attr; it errors). AUR is local-build only — never wire a publish job or an `AUR_SSH_KEY` secret back in.
- **Check `git status` before `git add -A` on the main checkout** — the user keeps WIP test files there; sweeping them up has shipped half-done tests before.
- Ranged edits in this repo have repeatedly misfired (especially after `gofmt` renumbering); prefer read-then-full-file writes for multi-line changes, and single-line 1:1 replacements otherwise.
- **UI trigger keys must be mapper no-ops**: any byte the input mapper doesn't know maps to `AnyKey`, which starts the game on the title screen before `scoreUI` can see it. `'l'` (leaderboard) is deliberately mapped to a no-op event in `mappedKey` — a new UI hotkey needs the same treatment (regression: `input.TestLeaderKeyIsNotAGameKey`). `'d'` (daily) CANNOT be a no-op (it is WASD-Right), so its title trigger is checked in `Router.TakeDailyAtTitle` BEFORE the engine update consumes the same press as Right — keep that ordering.
- **Plant markers are float-adjacent**: `Builder.Plant` stores `{x+0.65, topRow}` and `Builder.Rows` recomputes the marker column with `math.Round(p.X-0.65)` — a plain `int()` truncates `15.999…` and shifts the marker one column off its pipe (regression: `TestDailyLevelsSolvableShape`).
- **Bump `board.EngineVersion` on ANY gameplay/level change** (engine, levels, daily generator): the verifier rejects every pending row whose recording predates the current build — that is the safety direction, but forgetting to bump means live submissions get deleted at the next verification pass.
- **Recording alignment is load-bearing**: `replay.Run` must start from exactly the state `App.Step` records from (first world-card tick; `Reset()`/`BeginDaily()` equivalence). If run-start flow ever changes (new states, a skip-intro path), update `Recorder` arming in `mario.go` and `replay.Run` together — the run-vs-live equality tests in `replay/replay_test.go` are the tripwire.
- The pixel font's missing glyphs are a *product constraint*: `[`, `_`, lowercase etc. render wrong — the name-entry field draws its own rails instead of brackets for exactly this reason.
- `engine.Input` is a fixed bool struct; anything needing character keys (name entry) must bypass the mapper via the Router's capture routing.
- **UI input capture vs escape sequences**: The UI consumes single bytes for typing/control. `input.PlainDecoder` strips out non-text escape sequences (preventing lone `0x1b` from instantly closing the UI) and translates `CSI u` text keys back into normal bytes.
 - **Single-key-repeat demotion (legacy regime)**: Wayland compositors (and anything behind tmux, which strips the kitty negotiation) autorepeat only the newest pressed key — holding right and pressing jump silences right's repeat stream forever. The mapper's demotion model (`input/input.go`: `demotedHeld`, resurrection, `upExtendTicks` cap for jump) exists to keep proven holds alive through that; don't "simplify" those arrays away. Regression tests: `input/demote_test.go`.
- Viewport extremes are real: `ViewH` can be 4 tiles tall; every new screen must survive the size sweeps or `TestScoreUIAllViewportSizes` fails.
- The WASM build shares `play()` but not `render.Stream` — it rasterizes via `RenderPixels`; changes to overlay drawing must work through both `worldFrame` paths.
- `web/index.html` contains a self-contained boot loader that ports the game's art/palette/font — if sprites/font change, consider whether the loader art should follow.
- **Mobile Web Support**: The browser build bypasses virtual keyboard quirks by rendering its own on-screen gamepad and alphanumeric keypad (`#pad`). The page reads `st.mode` from `marioBoard(json)` to swap control clusters contextually; `marioTitle(bool)` toggles the DAILY button (hidden in play — a plain 'd' tap is a Right press). The CSS uses `100dvh`, `touch-action: none`, and `visualViewport` resize listeners to prevent iOS Safari scrolling and zooming. The page must feed every game key as a kitty press/release pair (`\e[<cp>;1:1u` / `\e[<cp>;1:3u`) — a bare text byte is a legacy press that decays after ~0.2s in the mapper, so pad holds would stall (regression: phone ◀/▶ died after a step). UI-capture paths are safe: `PlainDecoder` decodes press pairs back to plain bytes. HTML-attribute payloads can't hold raw control chars — use `&#13;` for Enter (a template-literal `"\\r"` is a literal backslash+r). Sound needs a user gesture first (autoplay policy) — the page lazily creates/resumes its AudioContext on the first pointerdown/keydown.
- **Board Restart**: Pressing `r` on the leaderboard closes the board, sets `u.restart`, and injects an `Input.Restart` edge via `router.poll()`. This cleans up UI state (`asked = false`) so the next game-over can prompt for submission again.- **CI runners use dash as `/bin/sh`** — Makefile recipes must stay POSIX (no `<<<`/bashisms): they pass locally on NixOS bash and die only in Actions. The runner's toolcache Go also lacks `$(go env GOROOT)/lib/wasm/wasm_exec.js` (hence the `find` in `make web`), and `dist/mario-*` already matches `mario-web.zip` — never list it twice in `gh release create`.
- **Re-running a release run** is idempotent since the matrix rework: `gh release create` failure falls back to `gh release upload --clobber`, refreshing assets instead of dying on "already_exists". Reruns still use the tag commit's workflow file; the `pages` (`workflow_run`) job always uses the default-branch file.
- Live Supabase details: project `jdmgfpzxcdpyylhwdbkz` (direct IPv6 `db.<ref>.supabase.co` reachable from this host; pooler tenant lookup fails); schema changes go through the migration file + direct DB apply.


## Agent Maintenance & Self-Correction

<critical>
- **Continuous Documentation**: Agents MUST evaluate whether `AGENTS.md` requires an update during the Cleanup phase of any task.
- **Update Threshold**: Do NOT routinely update `AGENTS.md` for trivial bug fixes, standard feature additions, or localized refactors. ONLY update it if you have introduced a new architectural pattern, changed an interface/API contract described here, or if you need to remove deprecated/obsolete information.
</critical>