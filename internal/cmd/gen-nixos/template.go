// Package main — gen-nixos
//
// This file holds the text/template sources for the generated
// nixosModules.nix and homeManagerModules.nix. Kept in its own file so
// the templates are reviewable without scrolling through generator
// scaffolding.
package main

// settingsOptionsTmpl is the shared fragment that emits the
// `settings = { ... }` Nix option declarations. It is embedded in both
// the NixOS and home-manager module templates — single source of truth
// for the config schema.
const settingsOptionsTmpl = `    settings = {
{{- range .Sections }}
      {{.Name}} = {
{{- range .Fields }}
        {{.Key}} = lib.mkOption {
          type = {{nixType .}};
          default = {{nixDefault .}};
          description = {{nixDescription .}};
        };
{{- end }}
      };
{{- end }}
    };`

// nixosModuleTemplate is the full NixOS module. It wraps the yap binary
// with runtime deps, generates a live TOML config from settings, and
// handles system-level concerns (input group, pipewire ALSA).
const nixosModuleTemplate = `# GENERATED FILE — DO NOT EDIT.
# Regenerate with ` + "`go generate ./pkg/yap/config/...`" + `
#
# This module is derived from the struct tags in pkg/yap/config.
# Schema changes belong in that package; this file follows.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.yap;

  # Runtime dependencies injected into the wrapped yap binary's PATH.
  # Injection tools are all included unconditionally (small packages;
  # yap detects the display server at runtime and picks the right one).
  # whisper-cpp is conditional on the transcription backend.
  runtimeDeps = with pkgs; [
    wtype
    xdotool
    xprop
    ydotool
  ] ++ lib.optional (cfg.settings.transcription.backend == "whisperlocal") whisper-cpp;

  wrappedPkg = pkgs.symlinkJoin {
    name = "yap";
    paths = [ cfg.package ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/yap \
        --prefix PATH : ${lib.makeBinPath runtimeDeps}
    '';
  };

  configFile = (pkgs.formats.toml {}).generate "yap-config.toml" cfg.settings;
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

` + settingsOptionsTmpl + `
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    {
      environment.systemPackages = [ wrappedPkg ];
      services.pipewire.alsa.enable = true;
      environment.etc."yap/config.toml".source = configFile;
    }

    (lib.mkIf (cfg.user != null) {
      users.users.${cfg.user}.extraGroups = [ "input" ];
    })
  ]);
}
`

// homeManagerModuleTemplate is the home-manager module. It installs a
// wrapped yap binary, writes user config via xdg.configFile, and
// optionally creates a systemd user service for the daemon.
//
// The daemon.enable option supports users who manage their own keybinds
// (sxhkd, WM binds, etc.) and only want the CLI tools (yap record,
// yap toggle) without the background daemon.
const homeManagerModuleTemplate = `# GENERATED FILE — DO NOT EDIT.
# Regenerate with ` + "`go generate ./pkg/yap/config/...`" + `
#
# This module is derived from the struct tags in pkg/yap/config.
# Schema changes belong in that package; this file follows.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.programs.yap;

  # Runtime dependencies injected into the wrapped yap binary's PATH.
  # Injection tools are all included unconditionally (small packages;
  # yap detects the display server at runtime and picks the right one).
  # whisper-cpp is conditional on the transcription backend.
  runtimeDeps = with pkgs; [
    wtype
    xdotool
    xprop
    ydotool
  ] ++ lib.optional (cfg.settings.transcription.backend == "whisperlocal") whisper-cpp;

  wrappedPkg = pkgs.symlinkJoin {
    name = "yap";
    paths = [ cfg.package ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/yap \
        --prefix PATH : ${lib.makeBinPath runtimeDeps}
    '';
  };
in {
  options.programs.yap = {
    enable = lib.mkEnableOption "yap";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The yap package to use.";
    };

    daemon.enable = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Start yap daemon as a systemd user service. Disable if you manage keybinds externally (sxhkd, WM binds, etc.) and only use yap record/toggle.";
    };

` + settingsOptionsTmpl + `
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ wrappedPkg ];

    xdg.configFile."yap/config.toml" = {
      source = (pkgs.formats.toml {}).generate "yap-config.toml" cfg.settings;
    };

    systemd.user.services.yap = lib.mkIf cfg.daemon.enable {
      Unit = {
        Description = "yap hold-to-talk voice dictation daemon";
        After = [ "pipewire.service" ];
      };
      Service = {
        ExecStart = "${lib.getExe wrappedPkg} listen --foreground";
        Restart = "on-failure";
        RestartSec = 3;
      };
      Install = { WantedBy = [ "default.target" ]; };
    };
  };
}
`
