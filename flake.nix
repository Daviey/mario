{
  description = "mario — terminal Mario-style platformer with a replay-verified online leaderboard";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # The store-friendly build identity: commit rev when built from a
      # clean checkout, "dirty<rev>" during development, "dev" outside git.
      version = self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = forAllSystems (pkgs: rec {
        mario = pkgs.buildGoModule {
          pname = "mario";
          inherit version;
          src = self;
          subPackages = [ "cmd/mario" ];
          # Stdlib-only module — no dependencies to vendor. buildGoModule
          # sets CGO_ENABLED=0 in env by default (static, like the
          # Makefile); do not shadow it with a derivation attribute.
          vendorHash = null;
          ldflags = [
            "-s"
            "-w"
            "-X github.com/Daviey/mario/render.Version=v${version}"
          ];
          meta = with pkgs.lib; {
            description = "Terminal Mario-style platformer with a replay-verified online leaderboard";
            homepage = "https://daviey.github.io/mario/";
            license = licenses.mit;
            mainProgram = "mario";
            platforms = platforms.unix;
          };
        };
        default = mario;
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.mario}/bin/mario";
        };
      });

      devShells = forAllSystems (pkgs:
        let
          # The Android SDK pieces are unfree in nixpkgs (Google's
          # terms) and pulled in as several oddly-named FODs; allow all
          # of them in this private nixpkgs instance — used only by the
          # android shell — so `nix develop .#android` needs no
          # NIXPKGS_ALLOW_UNFREE / --impure flags anywhere.
          # android_sdk.accept_license accepts Google's SDK license.
          androidPkgs = import nixpkgs {
            system = pkgs.stdenv.hostPlatform.system;
            config.allowUnfree = true;
            config.android_sdk.accept_license = true;
          };
          # Composed Android SDK for `make apk` (aapt2/d8/zipalign/
          # apksigner + android.jar). No NDK, no emulator: the APK is a
          # WebView shell, so the game stays GOOS=js with CGO off.
          androidSdk = (androidPkgs.androidenv.composeAndroidPackages {
            buildToolsVersions = [ "35.0.0" ];
            platformVersions = [ "35" ];
          }).androidsdk;
        in
        {
          default = pkgs.mkShell {
            packages = [ pkgs.go ];
          };
          # nix develop .#android -c make apk
          android = pkgs.mkShell {
            packages = [ androidSdk pkgs.openjdk17_headless ];
            ANDROID_HOME = "${androidSdk}/libexec/android-sdk";
          };
        });
    };
}
