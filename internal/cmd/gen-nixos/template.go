// Package main — gen-nixos
//
// This file holds the text/template source for the generated
// nixosModules.nix. Kept in its own file so the template is reviewable
// without scrolling through the generator scaffolding.
package main

// nixTemplate is the source text rendered by main.go into
// nixosModules.nix. It uses text/template with two data passes:
//
//  1. A flat list of field descriptors for the `options.services.yap.settings`
//     declaration.
//  2. The same list grouped by section for the `configFile` TOML
//     rendering.
//
// The envelope (enable, package, user, environment.systemPackages,
// services.pipewire.alsa.enable, users.users extraGroups) matches the
// pre-Phase-2 hand-written module so existing NixOS users see no
// surface breakage when upgrading.
const nixTemplate = `# GENERATED FILE — DO NOT EDIT.
# Regenerate with ` + "`go generate ./pkg/yap/config/...`" + `
#
# This module is derived from the struct tags in pkg/yap/config.
# Schema changes belong in that package; this file follows.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.yap;
  configFile = pkgs.writeText "yap-config.toml" ''
{{- range $i, $s := .Sections }}
{{ if $i }}
{{ end -}}
[{{$s.Name}}]
{{- range $s.Fields }}
{{.Key}} = {{tomlRender .}}
{{- end }}
{{- end }}
'';
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

    settings = {
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
    };
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    {
      environment.systemPackages = [ cfg.package ];
      services.pipewire.alsa.enable = true;
      environment.etc."yap/config.toml".source = configFile;
    }

    (lib.mkIf (cfg.user != null) {
      users.users.${cfg.user}.extraGroups = [ "input" ];
    })
  ]);
}
`
