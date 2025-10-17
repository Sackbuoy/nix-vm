{
  description = "Nix VM - Version-pinned package manager for Nix";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname = "nix-vm";
          version = "0.1.0";

          src = ./.;

          vendorHash = "sha256-mcazBBUvRN7bZ/HheOdlNl3F3nfVoiFyVFRtPZ1MxGE=";

          ldflags = [
            "-s"
            "-w"
          ];

          meta = with pkgs.lib; {
            description = "Version-pinned package manager for Nix";
            homepage = "https://github.com/yourusername/nix-vm";
            license = licenses.mit;
            maintainers = [];
            mainProgram = "nix-vm";
          };
        };

        # Development shell
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
            git
          ];

          shellHook = ''
            echo "nix-vm development environment"
            echo "Run 'go build' to build the project"
          '';
        };

        # App for easy running
        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/nix-vm";
        };
      }
    );
}
