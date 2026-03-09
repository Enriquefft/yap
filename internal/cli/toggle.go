package cli

import (
	"fmt"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

func newToggleCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "toggle",
		Short: "Toggle recording (start if idle, stop if recording)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToggle(cfg)
		},
	}
}

// runToggle sends IPC toggle command.
// DAEMON-06: Toggle sends command to daemon.
// IPC-02: Request/response are NDJSON.
// IPC-03: Exit 0 on success, 1 on error.
func runToggle(cfg *config.Config) error {
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	// Send IPC toggle command (5s timeout per CONTEXT.md).
	resp, err := ipc.Send(sockPath, ipc.CmdToggle, 5*time.Second)
	if err != nil {
		return fmt.Errorf("toggle failed: %w", err)
	}

	if !resp.Ok {
		return fmt.Errorf("toggle error: %s", resp.Error)
	}

	fmt.Printf("Toggle successful (state: %s)\n", resp.State)
	return nil
}
