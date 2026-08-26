# iOS packaging (unsigned .ipa, built on Linux)

`make ipa` produces `dist/mario_<ver>_ios_unsigned.ipa` — the WASM web
build wrapped in a thin WKWebView shell (`main.m`), compiled on plain
Linux with no Mac and no Xcode:

- **clang** `-target arm64-apple-ios15.0 -isysroot <iPhoneOS SDK>` compiles
  the ObjC wrapper; the SDK ships `.tbd` link stubs, so no Apple binaries
  are copied into the build.
- **lld** (`ld64.lld`) links the Mach-O (see `tools/mkipa/main.go`).
- **ldid** applies the ad-hoc signature.
- `tools/mkipa` assembles `Payload/mario.app` (binary, `Info.plist`,
  app icons rendered from `internal/art`, `www/` = `make web` output)
  into a deterministic zip.

The web payload is served through a `WKURLSchemeHandler` (`marioapp://`)
because WebKit gives `file://` URLs neither `fetch` (the page fetches
`mario.wasm` for honest load progress) nor sane origin isolation — the
same role Android's asset interception plays.

## The SDK (not in this repo)

Apple licenses the iPhoneOS SDK for use on Apple hardware only, so it
cannot be shipped here. The build needs an extracted SDK at `$IOS_SDK`
(default `~/dev/ios-sdks/sdks/iPhoneOS16.5.sdk`), e.g. via a sparse
checkout of [theos/sdks](https://github.com/theos/sdks) — the standard
source for Linux iOS toolchains (jailbreak scene); understand its
provenance before redistributing anything built against it.

```sh
mkdir -p ~/dev/ios-sdks && cd ~/dev/ios-sdks
git clone --filter=blob:none --sparse --depth 1 https://github.com/theos/sdks
cd sdks && git sparse-checkout set iPhoneOS16.5.sdk
```

On NixOS use the repo flake shell (multi-target clang must be the
**unwrapped** one — the nixpkgs cc-wrapper breaks `-isysroot` framework
lookup for apple triples):

```sh
nix develop .#ios -c make ipa
```

## Installing on an iPhone

iOS has no APK-style sideloading. The `.ipa` is unsigned; re-sign it
with your own Apple ID using [Sideloadly](https://sideloadly.io/)
(Mac/Windows) or AltStore:

1. Open the `.ipa` in Sideloadly, enter your Apple ID, install.
2. On the phone: Settings → General → VPN & Device Management → trust
   your developer certificate.
3. Free Apple IDs: the signature lasts 7 days (re-install to refresh),
   3 apps max. App Store/TestFlight distribution would need a paid
   Apple Developer account and macOS signing — deliberately not wired.

For a zero-signing iPhone experience use the PWA instead: open the
GitHub Pages site in Safari → Share → Add to Home Screen (offline,
auto-updating, full touch pad).
