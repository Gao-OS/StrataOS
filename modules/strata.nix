# NixOS module scaffold for Strata.
# This defines the systemd service unit and basic options.
# Not fully implemented — intended as the integration point for NixOS deployments.
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.strata;
in
{
  options.services.strata = {
    enable = mkEnableOption "Strata distributed runtime";

    nodeId = mkOption {
      type = types.str;
      description = "Unique identifier for this Strata node.";
    };

    runtimeDir = mkOption {
      type = types.str;
      default = "/run/strata";
      description = "Directory for sockets and ephemeral state.";
    };

    package = mkOption {
      type = types.package;
      description = "The strata-supervisor package to use.";
    };

    identityPackage = mkOption {
      type = types.package;
      description = "The strata-identity package to use.";
    };

    fsPackage = mkOption {
      type = types.package;
      description = "The strata-fs package to use.";
    };
  };

  config = mkIf cfg.enable {
    systemd.services.strata-supervisor = {
      description = "Strata Supervisor";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      environment = {
        STRATA_RUNTIME_DIR = cfg.runtimeDir;
        STRATA_NODE_ID = cfg.nodeId;
        STRATA_IDENTITY_BIN = "${cfg.identityPackage}/bin/identity";
        STRATA_FS_BIN = "${cfg.fsPackage}/bin/fs";
      };

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/supervisor";
        RuntimeDirectory = "strata";
        Restart = "on-failure";
        RestartSec = 5;
        Type = "simple";
      };
    };
  };
}
