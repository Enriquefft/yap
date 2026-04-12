package config

import (
	"time"
)

// Config is the root configuration document. TOML tags define the
// on-disk format; yap tags carry metadata consumed by the NixOS
// generator (enum values, numeric ranges, documentation strings).
type Config struct {
	General       GeneralConfig       `toml:"general"`
	Transcription TranscriptionConfig `toml:"transcription"`
	Transform     TransformConfig     `toml:"transform"`
	Injection     InjectionConfig     `toml:"injection"`
	Hint          HintConfig          `toml:"hint"`
	Tray          TrayConfig          `toml:"tray"`
}

// GeneralConfig covers hotkey binding, recording behavior, and the
// history flag. These are user-visible knobs that affect every
// recording session.
type GeneralConfig struct {
	Hotkey           string  `toml:"hotkey"            yap:"doc=Evdev key name or plus-delimited combo, e.g. KEY_RIGHTCTRL or KEY_LEFTSHIFT+KEY_SPACE"`
	Mode             string  `toml:"mode"              yap:"enum=hold,toggle;doc=Hold-to-talk or press-to-toggle"`
	MaxDuration      int     `toml:"max_duration"      yap:"min=1;max=300;doc=Maximum recording length in seconds"`
	AudioFeedback    bool    `toml:"audio_feedback"    yap:"doc=Play start/stop chimes"`
	AudioDevice      string  `toml:"audio_device"      yap:"doc=Capture device name; empty selects the system default"`
	SilenceDetection bool    `toml:"silence_detection" yap:"doc=Auto-stop on sustained silence"`
	SilenceThreshold float64 `toml:"silence_threshold" yap:"min=0.0;max=1.0;doc=Amplitude threshold (0..1)"`
	SilenceDuration  float64 `toml:"silence_duration"  yap:"gt=0;doc=Seconds of silence before auto-stop"`
	History          bool    `toml:"history"           yap:"doc=Append every transcription to history.jsonl"`
	StreamPartials   bool    `toml:"stream_partials"   yap:"doc=Inject partials into partial-safe targets while speaking"`
}

// TranscriptionConfig configures the transcription backend. Only one
// backend is active at a time; fields are a superset of every backend's
// needs so the schema does not fragment.
type TranscriptionConfig struct {
	Backend           string `toml:"backend"             yap:"enum=whisperlocal,groq,openai,custom;doc=Transcription backend"`
	Model             string `toml:"model"               yap:"doc=Model name (whisperlocal: base.en; remote: backend-specific)"`
	ModelPath         string `toml:"model_path"          yap:"doc=Explicit local model path (whisperlocal only); empty auto-downloads"`
	WhisperServerPath string `toml:"whisper_server_path" yap:"doc=Path to the whisper-server binary (whisperlocal only); empty resolves via $YAP_WHISPER_SERVER, $PATH, then a Nix profile fallback"`
	WhisperThreads    int    `toml:"whisper_threads"     yap:"min=0;max=64;doc=whisper.cpp thread count (whisperlocal only); 0 picks runtime.NumCPU()/2 rounded up to at least 1"`
	WhisperUseGPU     bool   `toml:"whisper_use_gpu"     yap:"doc=use GPU backend for whisper.cpp when available (whisperlocal only)"`
	Language          string `toml:"language"            yap:"doc=ISO language code; empty auto-detects"`
	APIURL            string `toml:"api_url"             yap:"doc=Remote endpoint URL; required when backend is remote"`
	APIKey            string `toml:"api_key"             yap:"secret;doc=API key; env: YAP_API_KEY or GROQ_API_KEY"`
}

// ResolvedAPIURL returns the URL the transcriber should POST to. For
// remote backends with an empty APIURL, it returns the well-known
// default for that backend. Phase 3 will move this into the backend
// constructors.
func (t TranscriptionConfig) ResolvedAPIURL() string {
	if t.APIURL != "" {
		return t.APIURL
	}
	switch t.Backend {
	case "groq":
		return "https://api.groq.com/openai/v1/audio/transcriptions"
	}
	return ""
}

// TransformConfig configures the optional LLM transform stage that
// sits between transcription and injection. Phase 8 adds the concrete
// backends; Phase 2 only owns the schema.
type TransformConfig struct {
	Enabled      bool   `toml:"enabled"       yap:"doc=Route transcription through a transform backend before injection"`
	Backend      string `toml:"backend"       yap:"enum=passthrough,local,openai;doc=Transform backend"`
	Model        string `toml:"model"         yap:"doc=Model name; required when enabled and backend is not passthrough"`
	SystemPrompt string `toml:"system_prompt" yap:"doc=System prompt for the transform backend"`
	APIURL       string `toml:"api_url"       yap:"doc=Transform endpoint; local backends default to http://localhost:11434/v1"`
	APIKey       string `toml:"api_key"       yap:"secret;doc=API key; env: YAP_TRANSFORM_API_KEY"`
}

// InjectionConfig configures how transcribed text is delivered to the
// focused application. App-level overrides are evaluated in order with
// first-match wins.
type InjectionConfig struct {
	PreferOSC52      bool          `toml:"prefer_osc52"      yap:"doc=Use OSC52 for terminals when supported"`
	BracketedPaste   bool          `toml:"bracketed_paste"   yap:"doc=Retained for schema compatibility; the injector no longer wraps payloads, tmux paste-buffer -p and the terminal handle framing"`
	ElectronStrategy string        `toml:"electron_strategy" yap:"enum=clipboard,keystroke;doc=How to deliver to Electron apps"`
	DefaultStrategy  string        `toml:"default_strategy"  yap:"doc=Fallback strategy name (tmux|osc52|electron|wayland|x11) forced when no app_overrides entry matches; empty disables"`
	AppOverrides     []AppOverride `toml:"app_overrides"     yap:"doc=Per-app strategy overrides, first match wins"`
}

// AppOverride maps a window class / process-name substring to an
// explicit injection strategy.
type AppOverride struct {
	Match       string `toml:"match"        yap:"doc=WM_CLASS or process name substring"`
	Strategy    string `toml:"strategy"     yap:"doc=Strategy name (tmux, osc52, electron, wayland, x11)"`
	AppendEnter bool   `toml:"append_enter" yap:"doc=Append a trailing newline after injection so keystroke strategies submit/execute the dictation; default false because whisper's trailing newline artifact is always stripped and auto-Enter must be an explicit per-app opt-in"`
}

// HintConfig configures the Phase 12 context-aware pipeline. When
// enabled, the daemon reads project-level vocabulary files and queries
// hint providers for conversation context on every recording. The
// vocabulary biases Whisper's token probabilities (fixing domain-term
// misrecognition at the source); the conversation context grounds the
// LLM transform stage.
type HintConfig struct {
	Enabled              bool     `toml:"enabled"                yap:"doc=Enable context-aware hint pipeline"`
	VocabularyFiles      []string `toml:"vocabulary_files"       yap:"doc=Project doc filenames to read for base vocabulary (walks cwd to git root)"`
	Providers            []string `toml:"providers"              yap:"doc=Ordered hint provider list for conversation context; first match wins"`
	VocabularyMaxChars   int      `toml:"vocabulary_max_chars"   yap:"min=0;max=8000;doc=Max bytes of vocabulary passed to Whisper prompt"`
	ConversationMaxChars int      `toml:"conversation_max_chars" yap:"min=0;max=32000;doc=Max bytes of conversation context passed to transform"`
	TimeoutMS            int      `toml:"timeout_ms"             yap:"min=0;max=5000;doc=Max wall time in ms for hint provider fetch"`
}

// TrayConfig controls the optional system tray icon (Phase 15).
type TrayConfig struct {
	Enabled bool `toml:"enabled" yap:"doc=Show system tray icon (Phase 15)"`
}

// DefaultConfig returns a Config populated with the defaults documented
// in ARCHITECTURE.md. Every field has an explicit value; zero-values
// are intentional only where documented.
//
// Phase 6 flipped Transcription.Backend from "groq" to "whisperlocal"
// and Transcription.Model from "whisper-large-v3-turbo" to "base.en"
// so the default install is local-first. Users who want the cloud
// backend run `yap config set transcription.backend groq` and supply
// an API key.
func DefaultConfig() Config {
	return Config{
		General: GeneralConfig{
			Hotkey:           "KEY_RIGHTCTRL",
			Mode:             "hold",
			MaxDuration:      60,
			AudioFeedback:    true,
			AudioDevice:      "",
			SilenceDetection: false,
			SilenceThreshold: 0.02,
			SilenceDuration:  2.0,
			History:          false,
			StreamPartials:   true,
		},
		Transcription: TranscriptionConfig{
			Backend:           "whisperlocal",
			Model:             "base.en",
			ModelPath:         "",
			WhisperServerPath: "",
			WhisperThreads:    0,
			WhisperUseGPU:     true,
			Language:          "en",
			APIURL:            "",
			APIKey:            "",
		},
		Transform: TransformConfig{
			Enabled:      false,
			Backend:      "passthrough",
			Model:        "",
			SystemPrompt: "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text.",
			APIURL:       "",
			APIKey:       "",
		},
		Injection: InjectionConfig{
			PreferOSC52:      true,
			BracketedPaste:   true,
			ElectronStrategy: "clipboard",
			DefaultStrategy:  "",
			AppOverrides:     nil,
		},
		Hint: HintConfig{
			Enabled:              true,
			VocabularyFiles:      []string{"CLAUDE.md", "AGENTS.md", "README.md"},
			Providers:            []string{"claudecode", "tmuxpane"},
			VocabularyMaxChars:   1000,
			ConversationMaxChars: 8000,
			TimeoutMS:            300,
		},
		Tray: TrayConfig{Enabled: false},
	}
}

// DefaultTimeout is the per-request transcription HTTP timeout. It is
// intentionally not a user-visible config field in Phase 2; the value
// is passed via transcribe.Options so tests can override it without
// exposing a knob nobody should need.
const DefaultTimeout = 30 * time.Second
