package cli

import (
	"fmt"
	"os/signal"
	"syscall"

	"github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal/models"
	"github.com/spf13/cobra"
)

// newModelsCmd builds the `yap models` subcommand tree.
//
//	yap models list                    list known models with install state and size
//	yap models download <name>         download a model into the cache, verifying SHA256
//	yap models path [name]             print the cache directory, or the path to a specific model
//
// Every command is a thin wrapper over pkg/yap/transcribe/whisperlocal/models.
// No transcription pipeline logic lives here.
func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage local whisper.cpp models",
		Long: `Manage the model cache used by the local whisper.cpp transcription backend.

Models are stored under $XDG_CACHE_HOME/yap/models/ on Linux,
~/Library/Caches/yap/models/ on macOS, and %LOCALAPPDATA%/yap/Cache/models/
on Windows. Phase 6 ships exactly one pinned model (base.en); the cache
directory may also contain hand-downloaded files referenced via
transcription.model_path.`,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newModelsListCmd())
	cmd.AddCommand(newModelsDownloadCmd())
	cmd.AddCommand(newModelsPathCmd())
	return cmd
}

func newModelsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List known models and their install state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := models.List()
			if err != nil {
				return fmt.Errorf("list models: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-12s %-12s %-8s %s\n", "NAME", "INSTALLED", "SIZE", "PATH")
			for _, m := range entries {
				installed := "no"
				if m.Installed {
					installed = "yes"
				}
				fmt.Fprintf(out, "%-12s %-12s %-8s %s\n",
					m.Name, installed, fmt.Sprintf("%dMB", m.SizeMB), m.Path)
			}
			return nil
		},
	}
}

func newModelsDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download <name>",
		Short: "Download a model into the cache, verifying SHA256",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Honor SIGINT/SIGTERM during the download so the user
			// can ctrl-c a slow connection without leaking a temp
			// file. Download cleans up its temp file on every error
			// path including ctx cancellation.
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err := models.Download(ctx, name, cmd.OutOrStdout()); err != nil {
				return fmt.Errorf("download %s: %w", name, err)
			}
			path, err := models.Path(name)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s -> %s\n", name, path)
			return nil
		},
	}
}

func newModelsPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path [name]",
		Short: "Print the cache directory, or the path to a specific model",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				dir, err := models.CacheDir()
				if err != nil {
					return fmt.Errorf("cache dir: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), dir)
				return nil
			}
			path, err := models.Path(args[0])
			if err != nil {
				return fmt.Errorf("path %s: %w", args[0], err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

