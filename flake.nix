{
  description = "StrataOS – capability-oriented distributed runtime substrate";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      systemOutputs = flake-utils.lib.eachDefaultSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          version = "0.3.0-mvp1";
          src = ./.;
        in {
          packages = {
            supervisor = pkgs.buildGoModule {
              pname = "strata-supervisor";
              inherit version src;
              subPackages = [ "cmd/supervisor" ];
              vendorHash = null;
            };

            identity = pkgs.buildGoModule {
              pname = "strata-identity";
              inherit version src;
              subPackages = [ "cmd/identity" ];
              vendorHash = null;
            };

            fs = pkgs.buildGoModule {
              pname = "strata-fs";
              inherit version src;
              subPackages = [ "cmd/fs" ];
              vendorHash = null;
            };

            strata-ctl = pkgs.buildGoModule {
              pname = "strata-ctl";
              inherit version src;
              subPackages = [ "cmd/strata-ctl" ];
              vendorHash = null;
            };

            default = self.packages.${system}.supervisor;
          };

          devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [ go gopls ];
          };
        }
      );
    in
    systemOutputs // {
      nixosModules.strata = import ./modules/strata.nix;
    };
}
