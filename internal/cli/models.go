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
//
// mgr is the model Manager the subcommands operate against. The
// production wiring in newRootCmd passes models.Default(); tests
// inject a fixture Manager wired to an httptest server so they can
// exercise the download path without going to Hugging Face.
func newModelsCmd(mgr *models.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "manage local whisper.cpp models",
		Long: `manage the model cache used by the local whisper.cpp transcription backend.

Models are stored under $XDG_CACHE_HOME/yap/models/ on Linux,
~/Library/Caches/yap/models/ on macOS, and %LOCALAPPDATA%/yap/Cache/models/
on Windows. The pinned manifest covers the four English-only whisper.cpp
models (tiny.en, base.en, small.en, medium.en); each is downloaded from
Hugging Face and verified against a compile-time SHA256. The cache
directory may also contain hand-downloaded files referenced via
transcription.model_path.`,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newModelsListCmd(mgr))
	cmd.AddCommand(newModelsDownloadCmd(mgr))
	cmd.AddCommand(newModelsPathCmd(mgr))
	return cmd
}

func newModelsListCmd(mgr *models.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list known models and their install state",
		Long: `list prints every model in the pinned manifest along with
whether it is currently in the cache, its on-disk size in MB, and
the absolute path it would resolve to.

Use this to check before running 'models download' or to confirm a
hand-downloaded file landed in the right place.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := mgr.List()
			if err != nil {
				return fmt.Errorf("models: list: %w", err)
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

func newModelsDownloadCmd(mgr *models.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "download <name>",
		Short: "download a model into the cache, verifying SHA256",
		Long: `download fetches a model from the pinned manifest into the
local cache and verifies its SHA256 against the compile-time hash
before swapping the temp file into place. SHA mismatch aborts the
install and leaves the cache untouched.

Ctrl-C is honored during the download — the temp file is cleaned
up on every error path including context cancellation.

Example:

  yap models download base.en`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Honor SIGINT/SIGTERM during the download so the user
			// can ctrl-c a slow connection without leaking a temp
			// file. Download cleans up its temp file on every error
			// path including ctx cancellation.
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err := mgr.Download(ctx, name, cmd.OutOrStdout()); err != nil {
				return fmt.Errorf("models: download %s: %w", name, err)
			}
			path, err := mgr.Path(name)
			if err != nil {
				return fmt.Errorf("models: download %s: resolve: %w", name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s -> %s\n", name, path)
			return nil
		},
	}
}

func newModelsPathCmd(mgr *models.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "path [name]",
		Short: "print the cache directory, or the path to a specific model",
		Long: `path with no argument prints the cache directory yap reads
its models from. With a model name argument it prints the absolute
path that 'models download <name>' would write to (without
checking whether the file actually exists yet).

Example:

  yap models path
  yap models path base.en`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				dir, err := models.CacheDir()
				if err != nil {
					return fmt.Errorf("models: path: cache dir: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), dir)
				return nil
			}
			path, err := mgr.Path(args[0])
			if err != nil {
				return fmt.Errorf("models: path %s: %w", args[0], err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

