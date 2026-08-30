{
  description = "mario — terminal Mario-style platformer with a replay-verified online leaderboard";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs, ... }@flake:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # The store-friendly build identity: commit rev when built from a
      # clean checkout, "dirty<rev>" during development, "dev" outside git.
      version = self.shortRev or self.dirtyShortRev or "dev";

      # mkMario builds the game server package. versionStamp replaces the
      # default rev stamp in render.Version — deploy tooling that has the
      # tagged git repo passes `git describe --tags` here so the title
      # screen shows the release string instead of a bare hash (the flake
      # sandbox has no history; tags cannot be resolved at eval time).
      mkMario = { system, versionStamp ? version }: (nixpkgs.legacyPackages.${system}).buildGoModule {
        pname = "mario";
        version = if versionStamp == "" then version else versionStamp;
        src = self;
        subPackages = [ "cmd/mario" ];
        # Stdlib-only module — no dependencies to vendor. buildGoModule
        # sets CGO_ENABLED=0 in env by default (static, like the
        # Makefile); do not shadow it with a derivation attribute.
        vendorHash = null;
        ldflags = [
          "-s"
          "-w"
          "-X github.com/Daviey/mario/render.Version=${if versionStamp == "" then "v${version}" else versionStamp}"
        ];
        meta = with (nixpkgs.legacyPackages.${system}).lib; {
          description = "Terminal Mario-style platformer with a replay-verified online leaderboard";
          homepage = "https://daviey.github.io/mario/";
          license = licenses.mit;
          mainProgram = "mario";
          platforms = platforms.unix;
        };
      };
    in
    {
      packages = forAllSystems (pkgs@{ system, ... }:
        let
          # The single-file UEFI bootable and its pieces (x86_64 only).
          efi = nixpkgs.lib.optionalAttrs (pkgs.system == "x86_64-linux") (
            let
              efiInit = pkgs.buildGoModule {
                pname = "mario-efi-init";
                inherit version;
                src = self;
                subPackages = [ "cmd/efi" ];
                # Force a fully static binary: the initramfs has no
                # dynamic loader, and nixpkgs' Go otherwise links
                # against a store glibc (execve in the boot image would
                # fail with ENOENT on the interpreter path).
                env.CGO_ENABLED = 0;
                buildFlags = [ "-buildmode=exe" ];
                ldflags = [
                  "-linkmode"
                  "internal"
                  "-s"
                  "-w"
                  "-X github.com/Daviey/mario/render.Version=v${version}"
                ];
                # Stdlib-only module — no dependencies to vendor.
                vendorHash = null;
                meta.description = "mario as initramfs /init — the EFI-stub boot payload";
              };
              # tools/mkcpio builds the archive (same tool as the make
              # dev loop): /init plus the /dev/console + /dev/null nodes
              # PID 1 needs for stdio — cpio(1) can't mknod in the build
              # sandbox. The .cpio filename suffix matters too: the
              # kernel's gen_initramfs.sh only consumes INITRAMFS_SOURCE
              # verbatim (vs. parsing it as a text description) when the
              # name ends in .cpio.
              mkcpio = pkgs.buildGoModule {
                pname = "mkcpio";
                inherit version;
                src = self;
                subPackages = [ "tools/mkcpio" ];
                vendorHash = null;
              };
              efiInitrd = pkgs.runCommand "mario-efi-initramfs.cpio"
                { nativeBuildInputs = [ mkcpio ]; }
                ''
                  mkdir -p $out && rmdir $out
                  mkcpio $out ${efiInit}/bin/efi
                '';
            in
            {
              mario-efi-init = efiInit;
              mario-efi-initrd = efiInitrd;
              # dist/mario.efi: an EFI-stub Linux kernel with the game's
              # initramfs embedded — boots straight into the game from a
              # UEFI boot menu, no OS. Drivers the boot image relies on
              # (efifb/vesafb framebuffer, PS/2 keyboard, serial console)
              # are forced built-in so no modules are ever needed.
              mario-efi = pkgs.linuxKernel.packages.linux_6_12.kernel.override {
                structuredExtraConfig = with nixpkgs.lib.kernel; {
                  INITRAMFS_SOURCE = freeform "${efiInitrd}";
                  CMDLINE_BOOL = yes;
                  CMDLINE = freeform "console=tty0 console=ttyS0";
                  EFI_STUB = yes;
                  DEVTMPFS = yes;
                  FB_EFI = yes;
                  FB_VESA = yes;
                  INPUT_EVDEV = yes;
                  KEYBOARD_ATKBD = yes;
                  SERIAL_8250_CONSOLE = yes;
                };
              };
            }
          );
        in
        rec {
          mario = mkMario { inherit system; };
          default = mario;
        } // efi);

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

          # nix develop .#ios -c make ipa
          # clang MUST be the unwrapped multi-target one: the nixpkgs
          # cc-wrapper rewrites header search for its linux libc and
          # breaks -isysroot framework lookup for apple triples (the
          # wrapped build cannot even find Foundation/Foundation.h).
          # lld provides ld64.lld (Mach-O), ldid the ad-hoc signature.
          ios = pkgs.mkShell {
            packages = [ pkgs.llvmPackages.clang-unwrapped pkgs.llvmPackages.lld pkgs.ldid ];
          };
        });
    };
}
