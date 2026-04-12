package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/platform"
	pcfg "github.com/Enriquefft/yap/pkg/yap/config"
	"github.com/spf13/cobra"
)

// newConfigSetCmd constructs the `yap config set <key> <value>`
// subcommand.
//
// The command uses a custom scalar-value line editor in pkg/yap/config
// so comments, custom indentation, and key ordering in a hand-edited
// config file are preserved across edits. The editor refuses array,
// inline-table, and multi-line-string values; struct-level mutations
// like appending to injection.app_overrides go through
// `yap config overrides` instead.
//
// Flow:
//
//  1. Resolve the config path via config.ConfigPath.
//  2. Refuse read-only /nix/store paths with a clear message.
//  3. On first run (file does not exist) fall back to a Save-based
//     path — there are no comments to preserve.
//  4. Serialize the raw argv value to a TOML literal using the
//     schema-aware reflection walker in pkg/yap/config.
//  5. Run the editor (pkg/yap/config.SetKey) over the file bytes.
//  6. Post-write validation: LoadBytes + Validate against the
//     edited bytes; refuse the write on failure so the on-disk
//     file is untouched.
//  7. Atomic write via create-temp + rename.
func newConfigSetCmd(_ *config.Config, p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "set a configuration value",
		Long: `Set a configuration value by dot-notation path.

Examples:
  yap config set general.hotkey KEY_SPACE
  yap config set general.max_duration 120
  yap config set transcription.backend groq
  yap config set transform.enabled true
  yap config set injection.electron_strategy keystroke

The editor preserves comments, custom indentation, and key ordering
in your config file. Array, inline-table, and multi-line-string
values are out of scope — use the dedicated subcommands for those.

Mutations to injection.app_overrides are handled by
  yap config overrides add|remove|clear`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd.OutOrStdout(), p, args[0], args[1])
		},
	}
}

// runConfigSet is the command closure factored out so tests can
// exercise it directly without a cobra.Command. It owns the entire
// set-then-validate-then-write pipeline.
func runConfigSet(out io.Writer, p platform.Platform, dotPath, rawValue string) error {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("config: set: resolve path: %w", err)
	}

	// Refuse read-only Nix store paths early. The NixOS module
	// writes the file in the store, and any mutation there is
	// meaningless — the rebuild would overwrite it.
	if strings.HasPrefix(cfgPath, "/nix/store/") {
		return fmt.Errorf("config: set: config file is managed by the NixOS module at %s (read-only); edit services.yap.settings in your NixOS configuration instead", cfgPath)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// First-run path: no existing file to preserve
			// comments from. Delegate to a schema-aware Save
			// via the original reflection walker so the new
			// file is well-formed.
			return saveFirstRunConfig(out, p, dotPath, rawValue)
		}
		return fmt.Errorf("config: set: read: %w", err)
	}

	// Schema-driven type check + serialization. Runs BEFORE any
	// file edit so a bad value fails fast and leaves the file
	// untouched.
	literal, err := pcfg.TOMLLiteralFor(dotPath, rawValue)
	if err != nil {
		return fmt.Errorf("config: set: %w", err)
	}

	newData, err := pcfg.SetKey(data, dotPath, literal)
	if err != nil {
		return fmt.Errorf("config: set: %w", err)
	}

	// Post-write validation: parse the edited bytes via the TOML
	// library and run Validate. If either step fails, refuse the
	// write so the user's on-disk file is untouched.
	parsed, err := config.LoadBytes(io.Discard, newData)
	if err != nil {
		return fmt.Errorf("config: set: edited file failed to parse: %w", err)
	}
	if err := parsed.Validate(p.HotkeyCfg); err != nil {
		return fmt.Errorf("config: set: validation: %w", err)
	}

	if err := writeFileAtomic(cfgPath, newData); err != nil {
		return fmt.Errorf("config: set: write: %w", err)
	}

	fmt.Fprintf(out, "Set %s to %s\n", dotPath, rawValue)
	return nil
}

// saveFirstRunConfig handles the first-run case where no config
// file exists yet. It builds a DefaultConfig, applies the user's
// Set via the reflection walker, validates, and writes via the
// BurntSushi encoder. No comments or custom layout exist to
// preserve on a fresh install.
func saveFirstRunConfig(out io.Writer, p platform.Platform, dotPath, rawValue string) error {
	cfg := pcfg.DefaultConfig()
	if err := pcfg.Set(&cfg, dotPath, rawValue); err != nil {
		return fmt.Errorf("config: set: %w", err)
	}
	if err := cfg.Validate(p.HotkeyCfg); err != nil {
		return fmt.Errorf("config: set: validation: %w", err)
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("config: set: save: %w", err)
	}
	fmt.Fprintf(out, "Set %s to %s\n", dotPath, rawValue)
	return nil
}

// writeFileAtomic writes data to path via a create-temp + rename
// pattern so a crash mid-write leaves the original file intact.
// The temp file is created in the same directory so the rename is
// a filesystem-local operation.
//
// This helper intentionally does not use any BurntSushi encoder —
// data is already the canonical bytes produced by the line editor,
// and re-encoding would destroy the comment-preserving property.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.CreateTemp(dir, "yap-config-*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

