{
  inputs = {
    flake-parts.url = "github:hercules-ci/flake-parts";
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    devshell.url = "github:numtide/devshell";
    foundry.url = "github:shazow/foundry.nix/monthly";
    solc = {
      url = "github:hellwolf/solc.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    systems.url = "github:nix-systems/default";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [inputs.devshell.flakeModule];

      systems = import inputs.systems;
      perSystem = {
        system,
        pkgs,
        self',
        ...
      }: {
        _module.args.pkgs = import inputs.nixpkgs {
          inherit system;
          overlays = [
            inputs.foundry.overlay
            inputs.solc.overlay
          ];
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            # nix
            alejandra

            # golang
            go
            go-tools
            gopls
            gotools
            delve
            gdlv

            # solidity
            foundry
            go-ethereum
            solc
          ];
        };

        packages.op-probe = pkgs.callPackage ./. {};
        packages.default = self'.packages.op-probe;
      };
    };
}
