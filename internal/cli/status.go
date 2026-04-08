package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "report yap daemon state and configuration as JSON",
		Long: `status queries the running yap daemon over IPC and prints its
state, mode, config path, version, PID, transcription backend, and
model as a single JSON object.

When no daemon is running, status still prints a JSON object with
ok=false, the local config_path, and the local version so operators
can identify which build is installed.

Exit code: 0 when the daemon is running, 1 when it is not.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, cfg)
		},
	}
}

// runStatus queries the daemon over IPC. When the daemon is reachable
// the response is printed verbatim (it already carries the extended
// fields). When unreachable, status falls back to a JSON object with
// the locally-known config_path and version so operators can still
// identify the installation; the function still exits with status 1
// in that case so scripts treat it as "not running".
func runStatus(cmd *cobra.Command, cfg *config.Config) error {
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	out := cmd.OutOrStdout()

	resp, err := ipc.Send(sockPath, ipc.CmdStatus, 1*time.Second)
	if err != nil {
		// Daemon not reachable — return the local fallback shape.
		// Pull Mode/Backend/Model from the on-disk config so the
		// operator still sees the configuration that *would* be
		// active if the daemon were running. PID stays empty: there
		// is no live process to report.
		fallback := ipc.Response{
			Ok:      false,
			Error:   "not running",
			Version: config.Version,
			Mode:    cfg.General.Mode,
			Backend: cfg.Transcription.Backend,
			Model:   cfg.Transcription.Model,
		}
		if path, perr := config.ConfigPath(); perr == nil {
			fallback.ConfigPath = path
		}
		data, _ := json.Marshal(fallback)
		fmt.Fprintf(out, "%s\n", string(data))
		return fmt.Errorf("status: daemon not running")
	}

	data, mErr := json.Marshal(resp)
	if mErr != nil {
		return fmt.Errorf("status: marshal response: %w", mErr)
	}
	fmt.Fprintf(out, "%s\n", string(data))
	return nil
}
