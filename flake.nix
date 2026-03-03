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
          version = "0.3.2";
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

            registry = pkgs.buildGoModule {
              pname = "strata-registry";
              inherit version src;
              subPackages = [ "cmd/registry" ];
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

      mkNixosConfig = system: nixpkgs.lib.nixosSystem {
        inherit system;
        modules = [
          self.nixosModules.strata
          ({ config, pkgs, ... }: {
            # Basic system configuration
            boot.loader.grub.enable = true;
            boot.loader.grub.device = "/dev/sda";

            networking.hostName = "strata";
            time.timeZone = "UTC";

            environment.systemPackages = with pkgs; [
              vim
              curl
              git
            ];

            # Enable Strata runtime
            services.strata = {
              enable = true;
              nodeId = "local-0";
              package = self.packages.${system}.supervisor;
              identityPackage = self.packages.${system}.identity;
              fsPackage = self.packages.${system}.fs;
              registryPackage = self.packages.${system}.registry;
            };

            # Minimal configuration for live images/VMs
            users.users.root.password = "root";
            services.getty.autologinUser = "root";

            system.stateVersion = "24.05";
          })
        ];
      };
    in
    systemOutputs // {
      nixosModules.strata = import ./modules/strata.nix;

      nixosConfigurations = {
        strata-iso-x86_64 = mkNixosConfig "x86_64-linux";
        strata-vm-x86_64 = mkNixosConfig "x86_64-linux";
      };
    };
}
