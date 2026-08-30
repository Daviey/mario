# Changelog

## v0.5.0 — 2026-08-30

Big SSH-server release: mosh roaming, auto truecolor detection, an admission queue for full servers, plus a 256-color tier, a bell, cheats, and several live input/recording bugfixes.

## Highlights
- **Mosh roaming for anonymous SSH players** (`-mosh`): survive roaming and flaky links, with truecolor preserved via COLORTERM forwarding.
- **Automatic truecolor detection**: the server probes terminals (DA2/DA3) to tell VTE/kitty/iTerm2-class terminals apart and picks the right palette.
- **Admission queue with ETA**: joining a full server now shows your position instead of failing.
- **256-color tier**: terminals that aren't truecolor get the fixed xterm cube instead of washed-out base-16 colors.

## Added
- Mosh handshake support with port-range and process-group handling (`-mosh`, `-mosh-ports`).
- Server capacity harness and FIFO admission queue with Little's-law ETA.
- Truecolor detection via DA2/DA3 terminal probe at shell start; iTerm2 and kitty/alacritty recognized.
- Per-client-host input-calibration warm-start on reconnect.
- Terminal bell sound feedback for CLI, SSH and mosh.
- Cheats mode (`-cheats`): unlimited fireballs; runs are unrecorded and can't reach the leaderboard.
- Play-context logging with scores (surface, user agent, term, input regime) and write-only play-session telemetry.
- About screen (`i` on title) noting this is an unofficial fan game; title banner.
- Status line now shows the full key set: s duck, k die, r restart.
- 256-color cube rendering tier with OKLab-nearest quantization.
- Direct-Postgres fallback for `-verify-pending`; `-version` flag.
- Social preview banner image.

## Changed
- Web service worker is network-first — a release goes live on the next page load.
- Board fetch retries transient failures; one board-open path in the UI.
- Verifier alerts on systematic replay drops; release CI slimmed for PRs; atomic web deploy swap.
- Hot-path render caches, shared HUD/status content builder, 16-color SGR dedupe.
- README rewritten: mario.baby link, trademark disclaimer, `-scores`/`-daily` docs.

## Fixed
- SSH terminal damage: kitty keyboard flags now pop before leaving the alt screen (no more CSI-u garbage in your shell).
- SSH input lag/run-on: reader never writes, window adjusts coalesce, bell never blocks ticks.
- Recorder now survives death respawns — death-containing runs replay correctly and no longer get deleted from the leaderboard.
- Hidden 1-up block no longer double-spawns; flag tiers scaled to the drawn pole; lava boundary probes fixed.
- BrickDark restored in the base palette.
- Truncated escape sequences age out per tick; release-all settles in-flight hold habits.

## Packaging
- Engine version bumped: leaderboard recordings from before this release may fail re-verification.

## Note for players
- Older leaderboard replays recorded with a previous engine version may be re-verified or dropped after this update.


## v0.4.1 — 2026-08-29

This release adds a from-scratch SSH game server and live terminal resizing, while cutting terminal render bandwidth by more than half. Packaging and CI also grew an AppImage target and auto-deployed web hosting.

## Highlights
- `mario -serve ADDR` runs an unauthenticated SSH server that presents the game — from-scratch SSH2 stack (transport, KEX, session channels), one game per connection.
- Terminal output reworked with a differential SGR encoder, solid spaces and gap bridging, cutting bandwidth ~56% per tick.
- The viewport now follows terminal resizes live while playing.

## Added
- SSH game server (`mario -serve`), stdlib-only SSH2 implementation with E2E and unit tests.
- Live terminal resize support (`App.Resize`), with race tests for concurrent resize vs step and viewport sweeps.
- AppImage packaging for linux/amd64, built via nix with a CI smoke guard.
- Auto-deploy of the web build to mario.baby on every push to main (connection config in repo secrets).
- Changelog backfill for v0.1.0..v0.3.4; release notes now written into the release body and committed to CHANGELOG.md (no separate asset).
- Touch web control additions: restart pill and wrapped control row on narrow phones.

## Changed
- Wire-encoding contract documented for the differential renderer; round-trip and bandwidth tests added.
- Slow SSH links no longer stall gameplay: ticks and input flow independently of rendering backpressure.

## Fixed
- AppImage CI job failure root-caused (bwrap needs CAP_SYS_ADMIN under root) and fixed; documented.
- Restored Snapshot+Flush vs Draw equivalence assertion and the WEBLDFLAGS credential injection for the web build.

## Packaging
- New linux/amd64 AppImage artifact.
- Web build continuously deployed at mario.baby.
- Manpage and README updated for the new serve mode and features.



## v0.4.1 — 2026-08-29

This release adds an SSH arcade mode, cuts terminal bandwidth roughly in half, makes the viewport follow terminal resizes live, and auto-deploys the web build to mario.baby.

## Highlights
- `mario -serve ADDR` runs an unauthenticated SSH server that presents the game — `ssh mario.baby -p 1985` and play.
- Differential renderer cuts terminal bandwidth ~56% while keeping output byte-exact.
- The viewport now follows terminal resizes live.

## Added
- SSH game server (`internal/sshd`, `cmd/mario/serve.go`): stdlib-only SSH2 transport, curve25519/ed25519 KEX, one session per connection; input keeps flowing over slow links via per-packet window adjusts and a latest-frame writer goroutine.
- CI auto-deploy of the web build to mario.baby on every push to main.
- AppImage CI build + smoke guard, with the bwrap `CAP_SYS_ADMIN` root cause documented.
- Release notes job writes the release body and commits CHANGELOG.md.
- Race test for concurrent Resize vs Step, SSH E2E tests, bandwidth and diff round-trip tests.

## Changed
- Renderer emits differential SGR, solid-pixel spaces, and bridges clean gaps with rewritten cells instead of cursor addresses; wire-encoding contract documented.
- Touch pad: restart pill added; mid row wraps on narrow phones.
- Release body changelog link points at main, not the tag; RELEASE_NOTES.md asset dropped.

## Fixed
- WEBLDFLAGS credential injection and deploy-mode fix for the web build.
- Restored the Snapshot+Flush vs Draw equivalence assertion.

## Packaging
- .deb and .rpm packages ship alongside binaries for all five Linux architectures; AppImage built in CI (linux/amd64).


## v0.4.0 — 2026-08-27

This release spreads mario across every platform: iOS and macOS packages built entirely on Linux, RPM and AppImage formats, a BIOS-bootable ISO, touch controls for mobile play, and a proper README with screenshots.

## Highlights
- Touch controls for the browser and Android builds: virtual joystick, A/B buttons, arrange mode with draggable persisted layouts.
- iOS unsigned .ipa built on Linux (clang + lld + ldid), macOS universal .app via pure-Go tools/mkapp — no Xcode needed.
- Cross-compile matrix expanded to 17 targets; .deb and .rpm packages for all five Linux architectures; AppImage for linux/amd64.
- New README with regenerated screenshots from a real game capture tool.

## Added
- On-screen touch overlay for web/Android (joystick, A/B buttons, pill row, draggable arrange mode).
- iOS .ipa packaging (`tools/mkipa`) and macOS .app bundler (`tools/mkapp`), both deterministic with parse-back tests.
- RPM packaging via `tools/mkrpm` for all Linux architectures.
- AppImage target (linux/amd64) using pinned appimagetool under appimage-run.
- BIOS-bootable hybrid ISO of the EFI payload (`make iso`).
- Apple touch icon for iOS home screens.
- omp-generated release notes attached to each release.
- README with screenshots (title, overworld, underground, sky, castle, board, web desktop/mobile) and demo GIF.

## Changed
- Cross-compile release matrix expanded to 17 OS/arch targets, .debs for all five Linux arches.
- Android APK goes immersive and neuters the service worker so app updates don't serve stale caches.
- Android signing keystore rotated; old key purged from history (uninstall before updating if you held an old install).
- Release artifacts retained 1 day (private-repo storage quota).
- Touch pad now also enables on coarse pointers with touch points (fixes Samsung/WebView fine-pointer misreports).

## Fixed
- EFI: OVMF resolution for efi-qemu-ovmf (OVMF.fd output, FV/, chmod vars); serial drained before poweroff so the quit line survives; hermetic flake source for make targets.
- mkapp normalizes zip modes past the CI runner's umask; deb targets renamed per-GOARCH with an awk-free versionCode.
- Release workflow: typoed checkout pin in the notes job; restored workflow name so the Pages `workflow_run` filter matches.


## v0.3.4 — 2026-08-26

Small player-facing fixes: a suicide key, landscape-locked Android, and audible web sound.

## Added
- 'k' suicide key — die on demand when trapped.

## Fixed
- Android locked to landscape with the touch pad forced on; WebView debugging gated.
- Web sound actually audible: synth master gain connected to the audio destination.
- EFI: device nodes baked into the nix initramfs via the new tools/mkcpio.

## Packaging
- tools/mkcpio added for deterministic initramfs assembly.

## v0.3.3 — 2026-08-26

EFI image fixes and a privacy change: nothing is stored on the player's machine anymore.

## Changed
- EFI initramfs is named with a .cpio suffix so the kernel consumes it verbatim; forced static linking (nixpkgs Go pulled a store glibc); device nodes created in the initramfs.
- No storage on the user's machine: player identity is per-run, input-calibration files removed; browser localStorage stays.

## v0.3.2 — 2026-08-26

New platforms and hardening: Android APK WebView wrapper, first EFI boot-image work, and a full security audit.

## Highlights
- Android APK: thin WebView wrapper around the web build, with landscape lock and forced touch pad (best-effort in CI).
- EFI: initial bootable-image work (flake build visibility).
- Security audit hardening across backend, web, native and CI.

## Changed
- Web on-screen pad hides when a physical key is pressed, with a manual toggle.
- AUR is local-build only (publish job dropped); replay decoding gained bounds tests.
- APK CI gets a 40-minute timeout for first SDK materialisation and is best-effort — never gates release or Pages.

## v0.3.1 — 2026-08-26

Distribution pass: MIT license, pure-Go .deb packaging, a Nix flake, and an AUR package.

## Added
- MIT license.
- Debian packaging via the pure-Go mkdeb tool (no dpkg needed), make deb targets for release assets.
- Nix flake for the game.
- AUR PKGBUILD (local builds).
- Web build persists player identity via localStorage.

## v0.3.0 — 2026-08-26

World 2 lands (three new levels, paratroopas, fire bars, lava, sky + castle themes) alongside star power, hidden blocks, and a replay-verified leaderboard.

## Highlights
- World 2: levels 2-2, 2-3 and 2-4 with paratroopas, fire bars, lava, sky and castle themes.
- Star power-up (kill-on-touch, palette flicker), hidden coin/1-UP blocks, secrets in every level.
- Replay-verified leaderboard: every submission carries a per-tick input recording; a server-side replay gate + GitHub Action verifier keep only scores that reproduce.

## Note for players
- Older leaderboard entries predate replay verification and appear unmarked.

## v0.2.0 — 2026-08-26

Feature drop: importable library split, richer animation — and a 16-feature gameplay wave (fire flower, flag sequence, combos, plants, world cards, sound, daily challenge).

## Highlights
- Engine reorganized into an importable root library (`github.com/Daviey/mario`) with thin cmd entries; internals moved to internal/ui + internal/persist.
- Gameplay wave: flag-slide sequence, stomp/shell combo ladder, checkpoints, fire flower + fireballs, piranha plants, HURRY at time 100, world cards, sound events + PWA, daily challenge, local best, a 4th level, underground theme.
- Mario favicon on web + embedded icon in the Windows exe.

## Added
- Full `-help` usage text with controls and examples.
- CI: matrix release builds, push/PR workflow.
- Board shows the level a score was reached on.

## Changed
- Touch pad shown only on devices with no fine pointer; pad buttons feed kitty press/release pairs so holds stick.

## Fixed
- Held direction now survives jump on single-key-repeat terminals.
- The 'm' glyph finally reads as 'm'.

## v0.1.1 — 2026-08-26

Polish and hardening pass: mobile touch controls, kitty-protocol input restoration, leaderboard anti-abuse, and a security audit — plus a big art upgrade for Mario and the title.

## Highlights
- Responsive touch controls for phone browsers.
- Leaderboard hardening: rate limits (10/min/IP, 2/min/device), proof-of-work gate, private device_id via the board_rows RPC, nightly retention.
- Mario walk cycle with jump and skid poses; enemy waddle, dust, death pose, squash/stretch.

## Added
- Builds on the trucker self-hosted runner; CGO forced off everywhere (static toolchain only).
- Board display-name sanitizer tests; UI tests pinned offline.
- Build version shown under the title.

## Changed
- Leaderboard screens use real text, not pixel blocks; 'L' opens the board as well as 'l'.

## Fixed
- Restored kitty-protocol support with persisted input calibration.
- Board screens no longer dismissed instantly by arrow-key escape sequences.
- Security audit fixes: terminal/DOM injection, file permissions, CI workflows.

## v0.1.0 — 2026-08-25

First release: a fully playable terminal Mario-style platformer in Go with a browser WASM build and an online high-score board on Supabase.

## Highlights
- SMB-style platformer rendered as half-block terminal pixels (native) or canvas pixels (browser), fully deterministic engine.
- Online leaderboard: game-over submit prompt, pixel-font name entry, board screen from the title or game-over.
- Deterministic web boot screen: the loader is a level of the game — Mario reaches the flagpole at 100% download.

## Added
- In-progress leaderboard client: run recording, submit, scores command, replay verifier GitHub Action (verification later dropped in this range).
- Web canvas renderer with integer scaling, HUD/title polish.
- In-game leaderboard UI with name entry; native single-owner stdin pump so post-game keystrokes reach the submit prompt.
- Tag-triggered release CI: all platform binaries + web to GitHub Pages.

## Fixed
- Input: key overrun, eaten jumps, mushy left-right; latched press edges with hold windows calibrated against OS repeat cadence.
- Render: pipe rim gaps, cloud occlusion, ? block legibility; HUD/status text ladders to fit every viewport width; title clouds keep clear of title text bands at pixel level.
- Render: Pix 4→6 — 2.25x pixel density with a redrawn art set.

## Packaging
- POSIX-sh release recipe; Pages deploy split into a workflow_run job (environment protection rejects tag refs).
