// Package main — gen-nixos
//
// This file holds the text/template sources for the generated
// nixosModules.nix and homeManagerModules.nix. Kept in its own file so
// the templates are reviewable without scrolling through generator
// scaffolding.
package main

import "strings"

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

// commonOptionsTmpl is the shared fragment that emits the
// `daemon.enable`, `extraRuntimePaths`, and `extraRuntimeLibs` option
// declarations. These three options have identical semantics on both
// the NixOS and home-manager modules, so they live in one place —
// single source of truth for daemon toggling and runtime extension.
// The indentation (four leading spaces per line) matches the
// `options.<scope>.yap = { ... }` block in both module templates.
const commonOptionsTmpl = `    daemon.enable = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Start yap daemon as a systemd user service. Disable if you manage keybinds externally (sxhkd, WM binds, etc.) and only use yap record/toggle.";
    };

    extraRuntimePaths = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages to prepend to the wrapped yap binary's PATH. Use for tools yap shells out to that are not covered by the defaults (e.g. a custom injection helper, or ffmpeg for audio inspection).";
    };

    extraRuntimeLibs = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages whose lib/ directories should be prepended to the wrapped yap binary's LD_LIBRARY_PATH. miniaudio ships Linux backends for PulseAudio, ALSA and JACK; the defaults include alsa-lib + libpulseaudio (covers PipeWire via pipewire-pulse). Add libjack here if you run bare JACK without pulse/alsa, or add any other dlopened dependency.";
    };`

// systemdUserServiceBodyTmpl is the shared fragment for the Unit,
// Service, and Install attribute sets of the yap systemd user service.
// Both modules emit byte-identical unit definitions; only the
// surrounding conditional wrapping differs (NixOS nests the block
// inside a `lib.mkMerge` branch, home-manager attaches `lib.mkIf`
// directly to the attribute), which changes the indentation level of
// the inner attributes. This fragment is defined at the shallower
// (home-manager) indentation; the NixOS template re-indents it by two
// extra spaces via `nestedSystemdUserServiceBodyNixOS` below.
//
// The fragment starts with a newline so every unit attribute line
// (including the first) is subject to the re-indentation rewrite in
// `nestedSystemdUserServiceBodyNixOS`.
const systemdUserServiceBodyTmpl = `
      Unit = {
        Description = "yap hold-to-talk voice dictation daemon";
        After = [ "pipewire.service" ];
      };
      Service = {
        ExecStart = "${lib.getExe wrappedPkg} listen --foreground";
        Restart = "on-failure";
        RestartSec = 3;
      };
      Install = { WantedBy = [ "default.target" ]; };`

// nestedSystemdUserServiceBodyNixOS is the shared systemd service body
// re-indented to sit inside the NixOS module's extra `lib.mkMerge`
// nesting level. Two-space shift because the NixOS embedding adds one
// extra brace level (`(lib.mkIf cfg.daemon.enable {
// systemd.user.services.yap = { ... }; })`) compared to home-manager's
// flat attribute. Defined as a package-level `var` initialized at
// program start so the shared body remains the single source of truth
// and the NixOS embedding is a mechanical derivation.
//
// The `\n      ` → `\n        ` rewrite universally shifts every line
// in the body by +2 spaces because every line has at least 6 leading
// spaces in the home-manager indentation; replacing the leading 6
// spaces with 8 preserves the relative nesting of Unit/Service/Install
// attributes while re-anchoring the block two columns to the right.
var nestedSystemdUserServiceBodyNixOS = strings.ReplaceAll(
	systemdUserServiceBodyTmpl, "\n      ", "\n        ")

// runtimeHelperTmpl is the shared fragment that declares `runtimeDeps`,
// `runtimeLibs`, and the `wrappedPkg` symlinkJoin which wraps the yap
// binary with the right PATH and LD_LIBRARY_PATH for runtime tooling
// and miniaudio's dlopened audio backends. It is embedded in both the
// NixOS and home-manager module templates — single source of truth for
// runtime dependency wiring.
//
// Runtime deps and libs are gated on `pkgs.stdenv.isLinux`. yap's audio
// stack (malgo → miniaudio) only dlopens libpulse/libasound/libjack on
// Linux; on macOS it uses CoreAudio (system framework) and on Windows
// it uses WASAPI (system DLL), neither of which needs anything on
// LD_LIBRARY_PATH. The injection tools (wtype, xdotool, xprop, ydotool)
// are also Linux-specific. Guarding on isLinux means importing
// homeManagerModules from nix-darwin does not error out evaluating
// pkgs.alsa-lib (which has no darwin build). When yap gains darwin /
// windows platform implementations, their respective runtime wiring
// can be added to additional branches without touching the Linux path.
//
// On Linux, runtimeDeps go on PATH and runtimeLibs go on LD_LIBRARY_PATH.
// miniaudio tries PulseAudio → ALSA → JACK in that priority order, so
// shipping libpulse + libasound covers: pure PulseAudio, pure ALSA, all
// PipeWire systems (via pipewire-pulse shim — the best-maintained of
// PipeWire's compatibility shims), and any mix. JACK is intentionally
// omitted from the default set because pipewire-jack exposes a single
// generic capture device that does not handle 16kHz mono correctly,
// which was the original bug that prompted this wiring. Users who
// genuinely want JACK (or any other library) can extend both sets via
// the `extraRuntimeLibs` and `extraRuntimePaths` options on the module.
//
// Without runtimeLibs, `yap record` fails with "miniaudio: Invalid
// argument" on every PipeWire NixOS install because only libjack
// happens to resolve (via pipewire-jack's session-level
// LD_LIBRARY_PATH), and miniaudio then picks JACK as the backend.
const runtimeHelperTmpl = `  # Runtime dependencies injected into the wrapped yap binary's PATH.
  # Injection tools are all included unconditionally (small packages;
  # yap detects the display server at runtime and picks the right one).
  # whisper-cpp is conditional on the transcription backend.
  #
  # Guarded on stdenv.isLinux because wtype/xdotool/ydotool are X11 and
  # Wayland tools — they do not build on darwin. Users on nix-darwin
  # importing this module get an empty runtimeDeps, and wrappedPkg
  # below becomes a pure passthrough symlinkJoin.
  runtimeDeps = lib.optionals pkgs.stdenv.isLinux (with pkgs; [
    wtype
    xdotool
    xprop
    ydotool
  ] ++ lib.optional (cfg.settings.transcription.backend == "whisperlocal") whisper-cpp)
  ++ cfg.extraRuntimePaths;

  # Runtime shared libraries miniaudio dlopens at audio-backend init on
  # Linux. These are not linked at build time, so they must be on the
  # wrapped binary's LD_LIBRARY_PATH or 'yap record' fails on PipeWire
  # NixOS. macOS uses CoreAudio and Windows uses WASAPI — neither needs
  # anything here. See the runtimeHelperTmpl doc comment in
  # internal/cmd/gen-nixos/template.go for the full rationale on why
  # libpulseaudio + alsa-lib is the right set and why libjack is not.
  runtimeLibs = lib.optionals pkgs.stdenv.isLinux (with pkgs; [
    alsa-lib
    libpulseaudio
  ]) ++ cfg.extraRuntimeLibs;

  wrappedPkg = pkgs.symlinkJoin {
    name = "yap";
    paths = [ cfg.package ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    # On non-Linux hosts both runtimeDeps and runtimeLibs are empty by
    # default, so wrapProgram reduces to a no-op wrapper that just
    # forwards to the unwrapped binary. Users who need runtime wiring
    # on a future darwin port can populate extraRuntimePaths /
    # extraRuntimeLibs from their own config and it flows through here.
    postBuild = ''
      wrapProgram $out/bin/yap \
        --prefix PATH : ${lib.makeBinPath runtimeDeps} \
        --prefix LD_LIBRARY_PATH : ${lib.makeLibraryPath runtimeLibs}
    '';
  };`

// nixosModuleTemplate is the full NixOS module. It wraps the yap binary
// with runtime deps, generates a live TOML config from settings, and
// handles system-level concerns (input group, pipewire ALSA, ydotoold,
// systemd user service).
//
// The daemon.enable option mirrors the home-manager module: when true
// (default) yap auto-starts as a systemd user service; users who manage
// keybinds externally (sxhkd, WM binds) can set it to false and only
// use the CLI tools (yap record, yap toggle).
//
// Declared as `var` rather than `const` because it embeds
// `nestedSystemdUserServiceBodyNixOS`, a re-indented derivation of the
// shared systemd body that is computed at package init via
// `strings.ReplaceAll`. The value never changes at runtime — it is
// effectively constant, but cannot be typed as `const` in Go because
// the initializer is not a compile-time constant expression.
var nixosModuleTemplate = `# GENERATED FILE — DO NOT EDIT.
# Regenerate with ` + "`go generate ./pkg/yap/config/...`" + `
#
# This module is derived from the struct tags in pkg/yap/config.
# Schema changes belong in that package; this file follows.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.yap;

` + runtimeHelperTmpl + `

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

` + commonOptionsTmpl + `

` + settingsOptionsTmpl + `
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    {
      environment.systemPackages = [ wrappedPkg ];
      services.pipewire.alsa.enable = lib.mkDefault true;
      environment.etc."yap/config.toml".source = configFile;
      # ydotool's wayland injection strategies require ydotoold running.
      programs.ydotool.enable = true;
    }

    (lib.mkIf (cfg.user != null) {
      users.users.${cfg.user}.extraGroups = [ "input" ];
    })

    (lib.mkIf cfg.daemon.enable {
      systemd.user.services.yap = {` + nestedSystemdUserServiceBodyNixOS + `
      };
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

` + runtimeHelperTmpl + `
in {
  options.programs.yap = {
    enable = lib.mkEnableOption "yap";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The yap package to use.";
    };

` + commonOptionsTmpl + `

` + settingsOptionsTmpl + `
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ wrappedPkg ];

    xdg.configFile."yap/config.toml" = {
      source = (pkgs.formats.toml {}).generate "yap-config.toml" cfg.settings;
    };

    systemd.user.services.yap = lib.mkIf cfg.daemon.enable {` + systemdUserServiceBodyTmpl + `
    };
  };
}
`
