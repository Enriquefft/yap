package cli

import (
	"fmt"
	"io"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

// candidateRowWidth is the left-column width of the candidate table.
// The label or path is left-padded to this width so the status
// suffix aligns across rows. Longer paths overflow gracefully — the
// suffix simply shifts right; the output stays readable on a
// terminal with word wrapping disabled.
const candidateRowWidth = 44

func newConfigPathCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "print the config file path",
		Long: `path prints the absolute path to the yap configuration file
that the current invocation would load, along with every candidate
path yap considered and its status. The single line labelled
"resolved:" is the file Load() will actually read; the "candidates:"
block lists every path in precedence order so operators can see at
a glance which files exist and which one is shadowing the others.

The resolution order is:

  1. $YAP_CONFIG (explicit env override)
  2. $XDG_CONFIG_HOME/yap/config.toml (per-user)
  3. /etc/yap/config.toml (system, written by the NixOS module)

The first existing file wins; if none exist, the per-user XDG path
is marked ACTIVE as the first-run Save target.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigPath(cmd.OutOrStdout())
		},
	}
}

// runConfigPath writes the resolved path and candidate table to w.
// It is separated from the cobra closure so tests can exercise the
// rendering without spinning up a *cobra.Command.
func runConfigPath(w io.Writer) error {
	candidates, err := config.CandidatePaths()
	if err != nil {
		return fmt.Errorf("config: path: %w", err)
	}

	var active config.Candidate
	for _, c := range candidates {
		if c.Active {
			active = c
			break
		}
	}
	if active.Path == "" {
		// CandidatePaths guarantees exactly one Active candidate.
		// A zero value here means the invariant is broken.
		return fmt.Errorf("config: path: no active candidate")
	}

	fmt.Fprintf(w, "resolved: %s\n", active.Path)
	fmt.Fprintln(w, "candidates:")

	// The env row is always printed first so users see it as an
	// available override even when unset. CandidatePaths only
	// includes the env candidate in the slice when $YAP_CONFIG is
	// set, so we synthesise an "unset" row when it is absent.
	var envCandidate *config.Candidate
	for i := range candidates {
		if candidates[i].Name == config.CandidateEnv {
			envCandidate = &candidates[i]
			break
		}
	}
	if envCandidate != nil {
		// When set, show "$YAP_CONFIG=<path>" in one column so the
		// user sees both the override name and the target.
		writeCandidateRow(w, fmt.Sprintf("%s=%s", config.CandidateEnv, envCandidate.Path), envStatus(*envCandidate))
	} else {
		writeCandidateRow(w, string(config.CandidateEnv), "(unset)")
	}

	for _, c := range candidates {
		if c.Name == config.CandidateEnv {
			continue
		}
		writeCandidateRow(w, c.Path, diskStatus(c))
	}

	return nil
}

// writeCandidateRow prints one row of the candidate table. The left
// column (label or path) is padded to candidateRowWidth so the
// status suffix aligns across rows. Two spaces of indent match the
// "candidates:" header above.
func writeCandidateRow(w io.Writer, left, status string) {
	fmt.Fprintf(w, "  %-*s  %s\n", candidateRowWidth, left, status)
}

// envStatus formats the status suffix for the $YAP_CONFIG row when
// the env var is set. CandidatePaths always marks the env row Active
// when present, so the suffix reports whether the pointed-at file
// exists on disk (a missing file is not a CandidatePaths error — it
// is a configuration error Load will surface as a read failure).
func envStatus(c config.Candidate) string {
	if c.Exists {
		return "(exists, ACTIVE)"
	}
	return "(missing, ACTIVE — Load will error)"
}

// diskStatus formats the status suffix for a disk candidate (user
// XDG or system /etc). The label distinguishes the active winner,
// an existing-but-shadowed file (the bug-8 case), and the first-run
// Save target.
func diskStatus(c config.Candidate) string {
	switch {
	case c.Active && c.Exists:
		return "(exists, ACTIVE)"
	case c.Active && !c.Exists:
		return "(missing, ACTIVE — first-run Save target)"
	case c.Exists:
		return "(exists, shadowed)"
	default:
		return "(missing)"
	}
}
