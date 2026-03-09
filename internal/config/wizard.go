package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/hybridz/yap/internal/hotkey"
)

// apiKeyPattern validates Groq API key format: sk- followed by 48 alphanumeric characters
var apiKeyPattern = regexp.MustCompile(`^gsk_[a-zA-Z0-9]{52}$`)

// RunWizard runs an interactive first-run setup wizard.
// If GROQ_API_KEY environment variable is set, the wizard is skipped and
// a config with that key is returned immediately.
// Otherwise, it prompts for API key, hotkey, and language, then writes the config.
func RunWizard(input io.Reader, output io.Writer) (Config, error) {
	// Check if GROQ_API_KEY env var is set — skip wizard if true
	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		cfg := Config{
			APIKey:   apiKey,
			Hotkey:   defaults().Hotkey,
			Language: defaults().Language,
		}
		fmt.Fprintln(output, "Using GROQ_API_KEY from environment")
		return cfg, nil
	}

	// Print welcome message
	fmt.Fprintln(output, "Welcome to yap! Let's set up your configuration...")
	fmt.Fprintln(output)

	// Create scanner for input
	scanner := bufio.NewScanner(input)

	// Prompt for API key
	apiKey, err := promptAPIKey(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get API key: %w", err)
	}

	// Prompt for hotkey
	hotkey, err := promptHotkey(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get hotkey: %w", err)
	}

	// Prompt for language
	language, err := promptLanguage(scanner, output)
	if err != nil {
		return Config{}, fmt.Errorf("failed to get language: %w", err)
	}

	// Build config
	cfg := Config{
		APIKey:   apiKey,
		Hotkey:   hotkey,
		Language: language,
		TimeoutSeconds: defaults().TimeoutSeconds,
	}

	// Write config file
	configPath, err := ConfigPath()
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Write config atomically (write to temp file, then rename)
	if err := writeConfigAtomic(cfg, configPath); err != nil {
		return Config{}, fmt.Errorf("failed to write config: %w", err)
	}

	// Confirm config path to user
	fmt.Fprintf(output, "Config saved to %s\n", configPath)
	fmt.Fprintln(output)

	return cfg, nil
}

// promptAPIKey prompts for and validates the Groq API key
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

		// Validate format
		if !apiKeyPattern.MatchString(apiKey) {
			fmt.Fprintf(output, "Invalid API key format. Expected format: gsk_ followed by 52 alphanumeric characters\n")
			continue
		}

		return apiKey, nil
	}
}

// promptHotkey prompts for the hotkey with a default value and validates against evdev key names
func promptHotkey(scanner *bufio.Scanner, output io.Writer) (string, error) {
	defaultHotkey := defaults().Hotkey
	for {
		fmt.Fprintf(output, "Choose hotkey [default: %s]: ", defaultHotkey)

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

		if !hotkey.ValidHotkey(hk) {
			fmt.Fprintf(output, "Invalid hotkey name %q. Use evdev names like KEY_RIGHTCTRL, KEY_SPACE, KEY_K\n", hk)
			continue
		}

		return hk, nil
	}
}

// promptLanguage prompts for the language with a default value
func promptLanguage(scanner *bufio.Scanner, output io.Writer) (string, error) {
	defaultLanguage := defaults().Language
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

// writeConfigAtomic writes the config file atomically by writing to a temp file then renaming
func writeConfigAtomic(cfg Config, configPath string) error {
	// Create temp file in same directory to ensure atomic rename
	tempPath := configPath + ".tmp"

	// Encode config to TOML
	// We can't use toml.EncodeFile directly because we need to ensure atomic write
	// Instead, we'll use the existing Load/Save pattern from config.go
	// But since we need to create a new file, let's write directly

	// Create directory if it doesn't exist
	configDir := configPath[:strings.LastIndex(configPath, "/")]
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Write temp file
	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	// Write TOML manually (simple format for our config)
	if _, err := fmt.Fprintf(f, "api_key = %q\n", cfg.APIKey); err != nil {
		f.Close()
		return fmt.Errorf("write api_key: %w", err)
	}
	if _, err := fmt.Fprintf(f, "hotkey = %q\n", cfg.Hotkey); err != nil {
		f.Close()
		return fmt.Errorf("write hotkey: %w", err)
	}
	if _, err := fmt.Fprintf(f, "language = %q\n", cfg.Language); err != nil {
		f.Close()
		return fmt.Errorf("write language: %w", err)
	}
	if _, err := fmt.Fprintf(f, "timeout_seconds = %d\n", cfg.TimeoutSeconds); err != nil {
		f.Close()
		return fmt.Errorf("write timeout_seconds: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, configPath); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}

	return nil
}
