package cli

import (
	"io"

	"github.com/hybridz/yap/internal/config"
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
//
// Phase 7: daemon spawning is handled by the YAP_DAEMON=1 env
// sentinel in cmd/yap/main.go, which routes detached child processes
// directly into daemon.Run before cobra ever sees os.Args. The
// previous hidden spawn flag has been removed entirely.
func newRootCmd(p platform.Platform) *cobra.Command {
	var rootCfg config.Config

	root := &cobra.Command{
		Use:   "yap",
		Short: "Hold-to-talk voice dictation daemon",
		Long:  "yap — record speech, transcribe locally or via a remote backend, inject text at the cursor.",
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	// Pipeline / lifecycle commands.
	root.AddCommand(newListenCmd(&rootCfg, p))
	root.AddCommand(newRecordCmd(&rootCfg, p))
	root.AddCommand(newTranscribeCmd(&rootCfg))
	root.AddCommand(newTransformCmd(&rootCfg))
	root.AddCommand(newPasteCmd(&rootCfg, p))
	root.AddCommand(newStopCmd(&rootCfg))
	root.AddCommand(newStatusCmd(&rootCfg))
	root.AddCommand(newToggleCmd(&rootCfg))
	root.AddCommand(newDevicesCmd(p))

	// Configuration / models.
	root.AddCommand(newConfigCmd(&rootCfg, p))
	root.AddCommand(newModelsCmd())

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
	return ExecuteForTestWithPlatform(linux.NewPlatform(), argv, stdout, stderr)
}

// ExecuteForTestWithPlatform is the lower-level test entry point that
// lets a test inject a custom Platform — including fake injectors,
// fake recorders, and fake device listers — without touching the
// production linux factory. The cobra command tree is constructed
// fresh on every call, exactly like ExecuteForTest, so test cases
// remain isolated.
func ExecuteForTestWithPlatform(p platform.Platform, argv []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	root := newRootCmd(p)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(argv)
	return root.Execute()
}
