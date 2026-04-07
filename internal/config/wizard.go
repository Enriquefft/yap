package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hybridz/yap/internal/platform"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// apiKeyPattern validates a Groq API key format: gsk_ followed by 52
// alphanumeric characters. The wizard uses this to give immediate
// feedback; the validator in pkg/yap/config does not enforce provider
// formats because users of OpenAI-compatible endpoints may paste any
// opaque token.
var apiKeyPattern = regexp.MustCompile(`^gsk_[a-zA-Z0-9]{52}$`)

// wizardOfferedBackends is the list of transcription backends the
// interactive wizard offers. Phase 6 adds "whisperlocal" here. The
// validator still accepts every backend defined in
// pkg/yap/config.ValidBackends(); this list is a UX knob.
var wizardOfferedBackends = []string{"groq"}

// RunWizard runs an interactive first-run setup wizard. If YAP_API_KEY
// or GROQ_API_KEY is set in the environment, the wizard skips prompts
// and writes the default config with the env key applied.
//
// hotkeyCfg is used for key detection and validation during hotkey
// selection. input and output are the reader/writer the wizard reads
// prompts from and writes messages to.
//
// The wizard writes a nested TOML config. Tests override ConfigPath
// to inject a scratch file.
func RunWizard(input io.Reader, output io.Writer, hotkeyCfg platform.HotkeyConfig) (Config, error) {
	// Env short-circuit: if the user already has an API key in the
	// environment, we do not prompt. We still build a nested Config
	// with the env key applied.
	if apiKey := envAPIKey(); apiKey != "" {
		cfg := pcfg.DefaultConfig()
		cfg.Transcription.APIKey = apiKey
		fmt.Fprintln(output, "Using API key from environment (YAP_API_KEY/GROQ_API_KEY)")
		configPath, err := ConfigPath()
		if err != nil {
			return cfg, fmt.Errorf("resolve config path: %w", err)
		}
		if err := writeConfigAtomic(cfg, configPath); err != nil {
			return cfg, fmt.Errorf("write config: %w", err)
		}
		return cfg, nil
	}

	fmt.Fprintln(output, "Welcome to yap! Let's set up your configuration...")
	fmt.Fprintln(output)

	scanner := bufio.NewScanner(input)
	cfg := pcfg.DefaultConfig()

	// Transcription section
	fmt.Fprintln(output, "[transcription]")
	backend, err := promptBackend(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get transcription backend: %w", err)
	}
	cfg.Transcription.Backend = backend

	apiKey, err := promptAPIKey(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get API key: %w", err)
	}
	cfg.Transcription.APIKey = apiKey

	// General section
	fmt.Fprintln(output)
	fmt.Fprintln(output, "[general]")
	hotkey, err := promptHotkey(scanner, output, hotkeyCfg)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get hotkey: %w", err)
	}
	cfg.General.Hotkey = hotkey

	language, err := promptLanguage(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get language: %w", err)
	}
	cfg.Transcription.Language = language

	// Write config file
	configPath, err := ConfigPath()
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve config path: %w", err)
	}

	if err := writeConfigAtomic(cfg, configPath); err != nil {
		return Config{}, fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Fprintf(output, "Config saved to %s\n", configPath)
	fmt.Fprintln(output)

	return cfg, nil
}

// envAPIKey returns the first populated transcription API key env var.
// Precedence matches pkg/yap/config.ApplyEnvOverrides.
func envAPIKey() string {
	if v := os.Getenv(pcfg.EnvAPIKey); v != "" {
		return v
	}
	return os.Getenv(pcfg.EnvGroqAPIKey)
}

// promptBackend prompts the user to choose a transcription backend.
// The offered list is wizardOfferedBackends; in Phase 2 that's only
// "groq", and the wizard short-circuits to it. Phase 6 widens the
// list and this function will loop.
func promptBackend(scanner *bufio.Scanner, output io.Writer) (string, error) {
	if len(wizardOfferedBackends) == 1 {
		backend := wizardOfferedBackends[0]
		fmt.Fprintf(output, "Transcription backend: %s\n", backend)
		return backend, nil
	}
	for {
		fmt.Fprintf(output, "Choose transcription backend %v: ", wizardOfferedBackends)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return wizardOfferedBackends[0], nil
		}
		choice := strings.TrimSpace(scanner.Text())
		if choice == "" {
			return wizardOfferedBackends[0], nil
		}
		for _, b := range wizardOfferedBackends {
			if choice == b {
				return choice, nil
			}
		}
		fmt.Fprintf(output, "Invalid backend %q. Choose one of %v\n", choice, wizardOfferedBackends)
	}
}

// promptAPIKey prompts for and validates the Groq API key.
func promptAPIKey(scanner *bufio.Scanner, output io.Writer) (string, error) {
	for {
		fmt.Fprintf(output, "Enter your Groq API key (gsk_xxxx...): ")

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("no input provided")
		}

		apiKey := strings.TrimSpace(scanner.Text())

		if !apiKeyPattern.MatchString(apiKey) {
			fmt.Fprintf(output, "Invalid API key format. Expected format: gsk_ followed by 52 alphanumeric characters\n")
			continue
		}

		return apiKey, nil
	}
}

// promptHotkey detects a physical key press or falls back to manual
// entry. On Linux, uses evdev for perfect detection (including
// modifiers like Right Ctrl).
func promptHotkey(scanner *bufio.Scanner, output io.Writer, hotkeyCfg platform.HotkeyConfig) (string, error) {
	defaultHotkey := pcfg.DefaultConfig().General.Hotkey

	fmt.Fprintf(output, "Press the key you want to use as hotkey [default: %s]\n", defaultHotkey)
	fmt.Fprintf(output, "  (or type 'm' then Enter to manually enter a key name)\n")
	fmt.Fprintf(output, "  Waiting for key press... ")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	detected, err := hotkeyCfg.DetectKey(ctx)
	if err == nil {
		fmt.Fprintf(output, "\n  Detected: %s\n", detected)
		fmt.Fprintf(output, "  Use this key? [Y/n]: ")

		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer == "" || answer == "y" || answer == "yes" {
				return detected, nil
			}
		}
		// User declined — fall through to manual entry
	} else {
		fmt.Fprintf(output, "\n  Could not detect key: %v\n", err)
	}

	// Manual entry fallback
	return promptHotkeyManual(scanner, output, hotkeyCfg, defaultHotkey)
}

// promptHotkeyManual prompts the user to type an evdev key name with
// validation.
func promptHotkeyManual(scanner *bufio.Scanner, output io.Writer, hotkeyCfg platform.HotkeyConfig, defaultHotkey string) (string, error) {
	for {
		fmt.Fprintf(output, "Enter hotkey name [default: %s]: ", defaultHotkey)

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return defaultHotkey, nil
		}

		hk := strings.TrimSpace(scanner.Text())
		if hk == "" {
			return defaultHotkey, nil
		}

		// Combo validation: every segment must be a valid key.
		valid := true
		for _, seg := range strings.Split(hk, "+") {
			seg = strings.TrimSpace(seg)
			if !hotkeyCfg.ValidKey(seg) {
				fmt.Fprintf(output, "Invalid hotkey segment %q. Use evdev names like KEY_RIGHTCTRL, KEY_SPACE, KEY_K\n", seg)
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		return hk, nil
	}
}

// promptLanguage prompts for the language with a default value.
func promptLanguage(scanner *bufio.Scanner, output io.Writer) (string, error) {
	defaultLanguage := pcfg.DefaultConfig().Transcription.Language
	fmt.Fprintf(output, "Choose language [default: %s]: ", defaultLanguage)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return defaultLanguage, nil
	}

	language := strings.TrimSpace(scanner.Text())
	if language == "" {
		return defaultLanguage, nil
	}

	return language, nil
}

// writeConfigAtomic writes cfg to path via a temp file + atomic
// rename. Encoding uses BurntSushi/toml to emit the nested sections
// exactly as the decoder expects.
func writeConfigAtomic(cfg Config, path string) error {
	tempPath := path + ".tmp"

	configDir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("encode config: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename config file: %w", err)
	}

	return nil
}
