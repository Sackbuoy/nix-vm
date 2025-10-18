# nix-vm
A version manager using nix

install like so
```
nix profile install github:sackbuoy/nix-vim

```

then create a file in cwd called `./nix-vm.yaml` with the following example
contents:
```
goreleaser: 2.9.0
kubernetes: 1.33.3
```

then just run `nix-vm`
the first run will download nixpkgs(which will take a while) and search through
the revision history for the version of the packages specified
this will also generate a flake in the pwd called `versions` that contains all
the pinned versions of your packages

## Using in another flake:
run `nix-vm` to generate `versions/` flake directory
pull into your flake like so:
```
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    versions.url = "path:./versions";
  };

  outputs = { self, nixpkgs, versions }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      pinnedPkgs = versions.packages system;
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        buildInputs = with pinnedPkgs; [
          goreleaser
          kubernetes
        ];
      };
    };
}
```
