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
      packages = forAllSystems (pkgs:
        let
          # The single-file UEFI bootable and its pieces (x86_64 only).
          efi = nixpkgs.lib.optionalAttrs (pkgs.system == "x86_64-linux") (
            let
              efiInit = pkgs.buildGoModule {
                pname = "mario-efi-init";
                inherit version;
                src = self;
                subPackages = [ "cmd/efi" ];
                # Stdlib-only module — no dependencies to vendor. The
                # default CGO_ENABLED=0 env gives the static binary the
                # initramfs needs.
                vendorHash = null;
                ldflags = [
                  "-s"
                  "-w"
                  "-X github.com/Daviey/mario/render.Version=v${version}"
                ];
                meta.description = "mario as initramfs /init — the EFI-stub boot payload";
              };
              efiInitrd = pkgs.runCommand "mario-efi-initramfs"
                { nativeBuildInputs = [ pkgs.cpio ]; }
                ''
                  d=$(mktemp -d)
                  install -m 0755 ${efiInit}/bin/efi $d/init
                  (cd $d && echo init | cpio -o -H newc -R 0:0 > $out)
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
                  EVDEV = yes;
                  KEYBOARD_ATKBD = yes;
                  SERIAL_8250_CONSOLE = yes;
                };
              };
            }
          );
        in
        rec {
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
        } // efi);

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.mario}/bin/mario";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go ];
        };
      });
    };
}
