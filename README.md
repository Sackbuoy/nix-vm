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
