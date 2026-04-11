# GENERATED FILE — DO NOT EDIT.
# Regenerate with `go generate ./pkg/yap/config/...`
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

    extraRuntimePaths = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages to prepend to the wrapped yap binary's PATH. Use for tools yap shells out to that are not covered by the defaults (e.g. a custom injection helper, or ffmpeg for audio inspection).";
    };

    extraRuntimeLibs = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages whose lib/ directories should be prepended to the wrapped yap binary's LD_LIBRARY_PATH. miniaudio ships Linux backends for PulseAudio, ALSA and JACK; the defaults include alsa-lib + libpulseaudio (covers PipeWire via pipewire-pulse). Add libjack here if you run bare JACK without pulse/alsa, or add any other dlopened dependency.";
    };

    settings = {
      general = {
        hotkey = lib.mkOption {
          type = lib.types.str;
          default = "KEY_RIGHTCTRL";
          description = "Evdev key name or plus-delimited combo, e.g. KEY_RIGHTCTRL or KEY_LEFTSHIFT+KEY_SPACE";
        };
        mode = lib.mkOption {
          type = lib.types.enum [ "hold" "toggle" ];
          default = "hold";
          description = "Hold-to-talk or press-to-toggle";
        };
        max_duration = lib.mkOption {
          type = lib.types.int;
          default = 60;
          description = "Maximum recording length in seconds";
        };
        audio_feedback = lib.mkOption {
          type = lib.types.bool;
          default = true;
          description = "Play start/stop chimes";
        };
        audio_device = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Capture device name; empty selects the system default";
        };
        silence_detection = lib.mkOption {
          type = lib.types.bool;
          default = false;
          description = "Auto-stop on sustained silence";
        };
        silence_threshold = lib.mkOption {
          type = lib.types.float;
          default = 0.02;
          description = "Amplitude threshold (0..1)";
        };
        silence_duration = lib.mkOption {
          type = lib.types.float;
          default = 2.0;
          description = "Seconds of silence before auto-stop";
        };
        history = lib.mkOption {
          type = lib.types.bool;
          default = false;
          description = "Append every transcription to history.jsonl";
        };
        stream_partials = lib.mkOption {
          type = lib.types.bool;
          default = true;
          description = "Inject partials into partial-safe targets while speaking";
        };
      };
      transcription = {
        backend = lib.mkOption {
          type = lib.types.enum [ "custom" "groq" "openai" "whisperlocal" ];
          default = "whisperlocal";
          description = "Transcription backend";
        };
        model = lib.mkOption {
          type = lib.types.str;
          default = "base.en";
          description = "Model name (whisperlocal: base.en; remote: backend-specific)";
        };
        model_path = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Explicit local model path (whisperlocal only); empty auto-downloads";
        };
        whisper_server_path = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Path to the whisper-server binary (whisperlocal only); empty resolves via $YAP_WHISPER_SERVER, $PATH, then a Nix profile fallback";
        };
        whisper_threads = lib.mkOption {
          type = lib.types.int;
          default = 0;
          description = "whisper.cpp thread count (whisperlocal only); 0 picks runtime.NumCPU()/2 rounded up to at least 1";
        };
        whisper_use_gpu = lib.mkOption {
          type = lib.types.bool;
          default = true;
          description = "use GPU backend for whisper.cpp when available (whisperlocal only)";
        };
        language = lib.mkOption {
          type = lib.types.str;
          default = "en";
          description = "ISO language code; empty auto-detects";
        };
        prompt = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Context hint passed to the backend when supported";
        };
        api_url = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Remote endpoint URL; required when backend is remote";
        };
        api_key = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "API key; env: YAP_API_KEY or GROQ_API_KEY";
        };
      };
      transform = {
        enabled = lib.mkOption {
          type = lib.types.bool;
          default = false;
          description = "Route transcription through a transform backend before injection";
        };
        backend = lib.mkOption {
          type = lib.types.enum [ "local" "openai" "passthrough" ];
          default = "passthrough";
          description = "Transform backend";
        };
        model = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Model name; required when enabled and backend is not passthrough";
        };
        system_prompt = lib.mkOption {
          type = lib.types.str;
          default = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text.";
          description = "System prompt for the transform backend";
        };
        api_url = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Transform endpoint; local backends default to http://localhost:11434/v1";
        };
        api_key = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "API key; env: YAP_TRANSFORM_API_KEY";
        };
      };
      injection = {
        prefer_osc52 = lib.mkOption {
          type = lib.types.bool;
          default = true;
          description = "Use OSC52 for terminals when supported";
        };
        bracketed_paste = lib.mkOption {
          type = lib.types.bool;
          default = true;
          description = "Retained for schema compatibility; the injector no longer wraps payloads, tmux paste-buffer -p and the terminal handle framing";
        };
        electron_strategy = lib.mkOption {
          type = lib.types.enum [ "clipboard" "keystroke" ];
          default = "clipboard";
          description = "How to deliver to Electron apps";
        };
        default_strategy = lib.mkOption {
          type = lib.types.str;
          default = "";
          description = "Fallback strategy name (tmux|osc52|electron|wayland|x11) forced when no app_overrides entry matches; empty disables";
        };
        app_overrides = lib.mkOption {
          type = lib.types.listOf (lib.types.attrsOf lib.types.str);
          default = [ ];
          description = "Per-app strategy overrides, first match wins";
        };
      };
      tray = {
        enabled = lib.mkOption {
          type = lib.types.bool;
          default = false;
          description = "Show system tray icon (Phase 15)";
        };
      };
    };
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
