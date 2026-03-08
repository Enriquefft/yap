package cmd

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

// rootCfg is populated by PersistentPreRunE before any subcommand RunE fires.
// It is passed by pointer into each newXxxCmd() factory so subcommands close
// over it — this is the injection point. There is no exported global; callers
// outside this package cannot mutate it.
var rootCfg config.Config

var rootCmd = &cobra.Command{
	Use:   "yap",
	Short: "Hold-to-talk voice dictation daemon",
	Long:  "yap — record speech, transcribe via Groq Whisper, paste at cursor.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		rootCfg = cfg
		return nil
	},
}

// Execute runs the root command. Called from main().
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newStartCmd(&rootCfg))
	rootCmd.AddCommand(newStopCmd(&rootCfg))
	rootCmd.AddCommand(newStatusCmd(&rootCfg))
	rootCmd.AddCommand(newToggleCmd(&rootCfg))
	rootCmd.AddCommand(newConfigCmd(&rootCfg))
}
