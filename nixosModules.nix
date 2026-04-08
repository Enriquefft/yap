# GENERATED FILE — DO NOT EDIT.
# Regenerate with `go generate ./pkg/yap/config/...`
#
# This module is derived from the struct tags in pkg/yap/config.
# Schema changes belong in that package; this file follows.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.yap;
  configFile = pkgs.writeText "yap-config.toml" ''
[general]
hotkey = "KEY_RIGHTCTRL"
mode = "hold"
max_duration = 60
audio_feedback = true
audio_device = ""
silence_detection = false
silence_threshold = 0.02
silence_duration = 2.0
history = false
stream_partials = true

[transcription]
backend = "whisperlocal"
model = "base.en"
model_path = ""
whisper_server_path = ""
language = "en"
prompt = ""
api_url = ""
api_key = ""

[transform]
enabled = false
backend = "passthrough"
model = ""
system_prompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."
api_url = ""
api_key = ""

[injection]
prefer_osc52 = true
bracketed_paste = true
electron_strategy = "clipboard"
default_strategy = ""
app_overrides = []

[tray]
enabled = false
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
