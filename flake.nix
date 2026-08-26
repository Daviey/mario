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

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go ];
        };
      });
    };
}
