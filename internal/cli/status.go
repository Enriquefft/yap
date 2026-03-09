package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the yap daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cfg)
		},
	}
}

// runStatus queries daemon state via IPC.
// DAEMON-03: Reports JSON state.
// IPC-02: Response is newline-delimited JSON.
// IPC-03: Exit 0 on daemon running, 1 if not running.
func runStatus(cfg *config.Config) error {
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	// Send IPC status command (1s timeout per CONTEXT.md — fast check).
	resp, err := ipc.Send(sockPath, ipc.CmdStatus, 1*time.Second)
	if err != nil {
		// Daemon not running.
		output := ipc.Response{Ok: false, Error: "not running"}
		data, _ := json.Marshal(output)
		fmt.Printf("%s\n", string(data))
		return fmt.Errorf("daemon not running") // Exit 1 for script compatibility.
	}

	// Print raw JSON response.
	data, _ := json.Marshal(resp)
	fmt.Printf("%s\n", string(data))
	return nil // Exit 0 on success.
}
