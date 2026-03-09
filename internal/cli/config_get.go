package cli

import (
	"fmt"
	"os"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newConfigGetCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a configuration value. Available keys:
  api_key         - Groq API key for transcription
  hotkey          - Keyboard key for hold-to-talk
  language        - Language code for transcription
  mic_device      - Specific microphone device
  timeout_seconds - Recording timeout in seconds`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

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

			// Load config
			loadedCfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Print value based on key
			switch key {
			case "api_key":
				fmt.Fprintln(os.Stdout, loadedCfg.APIKey)
			case "hotkey":
				fmt.Fprintln(os.Stdout, loadedCfg.Hotkey)
			case "language":
				fmt.Fprintln(os.Stdout, loadedCfg.Language)
			case "mic_device":
				fmt.Fprintln(os.Stdout, loadedCfg.MicDevice)
			case "timeout_seconds":
				fmt.Fprintln(os.Stdout, loadedCfg.TimeoutSeconds)
			}

			return nil
		},
	}
}
