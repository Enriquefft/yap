{ config, lib, pkgs, ... }:
with lib;
let
  cfg = config.services.yap;
in {
  options.services.yap = {
    enable = mkEnableOption "yap hold-to-talk voice dictation daemon";
    package = mkOption {
      type = types.package;
      default = pkgs.yap;
      description = "The yap package to use.";
    };
    user = mkOption {
      type = types.str;
      default = "$USER";
      description = "User account under which yap runs.";
    };
  };

  config = mkIf cfg.enable {
    # Add user to input group for evdev access
    users.users.${cfg.user}.extraGroups = [ "input" ];

    # Enable PipeWire ALSA for modern audio
    services.pipewire.alsa.enable = true;

    # Optional: systemd user service (could be future enhancement)
    # systemd.user.services.yap = {
    #   Unit = { Description = "yap hold-to-talk daemon"; };
    #   Service = {
    #     ExecStart = "${cfg.package}/bin/yap start";
    #     Restart = "on-failure";
    #   };
    #   Install = { WantedBy = [ "default.target" ]; };
    # };
  };
}
