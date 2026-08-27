# Changelog

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
