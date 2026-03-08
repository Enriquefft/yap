package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newConfigSetCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value. Available keys:
  api_key         - Groq API key for transcription
  hotkey          - Keyboard key for hold-to-talk (e.g., KEY_RIGHTCTRL)
  language        - Language code for transcription (default: en)
  mic_device      - Specific microphone device (optional)
  timeout_seconds - Recording timeout in seconds (1-300, default: 60)`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			// Validate key
			validKeys := map[string]bool{
				"api_key":         true,
				"hotkey":          true,
				"language":        true,
				"mic_device":      true,
				"timeout_seconds": true,
			}

			if !validKeys[key] {
				return fmt.Errorf("invalid key %q. Valid keys: api_key, hotkey, language, mic_device, timeout_seconds", key)
			}

			// Load existing config
			loadedCfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Update config based on key type
			switch key {
			case "api_key":
				loadedCfg.APIKey = value
			case "hotkey":
				loadedCfg.Hotkey = value
			case "language":
				loadedCfg.Language = value
			case "mic_device":
				loadedCfg.MicDevice = value
			case "timeout_seconds":
				timeout, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("timeout_seconds must be a number: %w", err)
				}
				if timeout < 1 {
					return fmt.Errorf("timeout_seconds must be at least 1 second")
				}
				if timeout > 300 {
					return fmt.Errorf("timeout_seconds cannot exceed 300 seconds (5 minutes)")
				}
				loadedCfg.TimeoutSeconds = timeout
			}

			// Save updated config
			if err := config.Save(loadedCfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			// Print confirmation
			fmt.Fprintf(os.Stdout, "Set %s to %s\n", key, value)
			return nil
		},
	}
}
