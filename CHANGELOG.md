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


