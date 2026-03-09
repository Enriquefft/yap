self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.yap;
in {
  options.services.yap = {
    enable = lib.mkEnableOption "yap hold-to-talk voice dictation daemon";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The yap package to use.";
    };

    user = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "User account under which yap runs. When set, adds the user to the input group for evdev access.";
    };
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    {
      environment.systemPackages = [ cfg.package ];
      services.pipewire.alsa.enable = true;
    }

    (lib.mkIf (cfg.user != null) {
      users.users.${cfg.user}.extraGroups = [ "input" ];
    })
  ]);
}
