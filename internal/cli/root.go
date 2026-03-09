package cli

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	linux "github.com/hybridz/yap/internal/platform/linux"
	"github.com/spf13/cobra"
)

// rootCfg is populated by PersistentPreRunE before any subcommand RunE fires.
// It is passed by pointer into each newXxxCmd() factory so subcommands close
// over it — this is the injection point. There is no exported global; callers
// outside this package cannot mutate it.
var rootCfg config.Config
var daemonRun bool

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
	// RunE handles --daemon-run: the detached child spawned by "yap start".
	// When --daemon-run is set, this blocks running the daemon event loop.
	// Without the flag, cobra prints help (default behavior).
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonRun {
			p := linux.NewPlatform()
			return daemon.Run(&rootCfg, daemon.DefaultDeps(p))
		}
		return cmd.Help()
	},
	// Silence usage on --daemon-run errors (daemon crashes shouldn't print help).
	SilenceUsage: true,
}

// Execute runs the root command. Called from main().
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	p := linux.NewPlatform()
	rootCmd.AddCommand(newStartCmd(&rootCfg, p))
	rootCmd.AddCommand(newStopCmd(&rootCfg))
	rootCmd.AddCommand(newStatusCmd(&rootCfg))
	rootCmd.AddCommand(newToggleCmd(&rootCfg))
	rootCmd.AddCommand(newConfigCmd(&rootCfg))

	// Hidden flag for internal daemon spawning.
	// Used by "yap start" to spawn a detached child that runs "yap --daemon-run".
	rootCmd.PersistentFlags().BoolVar(&daemonRun, "daemon-run", false, "")
	rootCmd.PersistentFlags().MarkHidden("daemon-run")
}
