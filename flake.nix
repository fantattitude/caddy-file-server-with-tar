{
  description = "Caddy with streamed .tar downloads for directories";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      makeCaddy = pkgs: pkgs.callPackage ./nix/caddy.nix { };
    in {
      packages = lib.genAttrs systems (system: {
        default = makeCaddy nixpkgs.legacyPackages.${system} { };
      });

      # Use the caller's package set when composing with other plugins.
      lib.buildCaddy = pkgs: makeCaddy pkgs;

      nixosModules.default = { lib, pkgs, ... }: {
        services.caddy.package = lib.mkDefault self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      };
    };
}
