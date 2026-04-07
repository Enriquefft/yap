package cli

import (
	"io"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/internal/platform"
	linux "github.com/hybridz/yap/internal/platform/linux"
	"github.com/spf13/cobra"
)

// newRootCmd constructs a fresh root command tree bound to a new
// config struct. Every mutable state lives inside the closure so
// tests can invoke newRootCmd repeatedly without leaking state
// between runs.
//
// newRootCmd is the single place where subcommand factories are
// wired up. Callers never share cobra.Command state across calls.
func newRootCmd(p platform.Platform) *cobra.Command {
	var rootCfg config.Config
	var daemonRun bool

	root := &cobra.Command{
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemonRun {
				return daemon.Run(&rootCfg, daemon.DefaultDeps(p))
			}
			return cmd.Help()
		},
		// Silence usage on --daemon-run errors (daemon crashes shouldn't print help).
		SilenceUsage: true,
	}

	root.AddCommand(newStartCmd(&rootCfg, p))
	root.AddCommand(newStopCmd(&rootCfg))
	root.AddCommand(newStatusCmd(&rootCfg))
	root.AddCommand(newToggleCmd(&rootCfg))
	root.AddCommand(newConfigCmd(&rootCfg, p))
	root.AddCommand(newModelsCmd())

	// Hidden flag for internal daemon spawning.
	root.PersistentFlags().BoolVar(&daemonRun, "daemon-run", false, "")
	_ = root.PersistentFlags().MarkHidden("daemon-run")

	return root
}

// Execute runs the root command against os.Args. Called from main().
func Execute() error {
	return newRootCmd(linux.NewPlatform()).Execute()
}

// ExecuteForTest builds a fresh command tree, runs it with argv, and
// redirects stdout/stderr to the supplied writers. Tests use this to
// exercise the full command dispatch without any package-level state.
func ExecuteForTest(argv []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	root := newRootCmd(linux.NewPlatform())
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(argv)
	return root.Execute()
}

