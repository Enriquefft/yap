package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "yap",
	Short: "Hold-to-talk voice dictation daemon",
	Long:  "yap — record speech, transcribe via Groq Whisper, paste at cursor.",
}

// Execute runs the root command. Called from main().
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newStartCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newToggleCmd())
	rootCmd.AddCommand(newConfigCmd())
}

// isHelpCmd is a helper for PersistentPreRunE — skip config load for help.
func isHelpCmd(cmd *cobra.Command) bool {
	return cmd.Name() == "help" || cmd.Name() == "completion"
}

// placeholder avoids "declared and not used" until config is wired in Plan 02
var _ = fmt.Sprintf
