package main

import (
	"fmt"
	"os"

	"github.com/Enriquefft/yap/internal/cli"
	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/daemon"
	linux "github.com/Enriquefft/yap/internal/platform/linux"
)

// main routes execution between two modes:
//
//  1. Daemon child mode — when YAP_DAEMON=1 in the environment we know
//     the parent (`yap listen`) spawned us as the detached daemon
//     process. We bypass cobra entirely and call daemon.Run directly
//     so the user-visible CLI stays free of any hidden bootstrap
//     flag. The env var is internal: documented as "not for users"
//     in ARCHITECTURE.md.
//
//  2. Foreground CLI mode — every other invocation goes through cobra
//     via cli.Execute(). Subcommands like `yap listen --foreground`
//     route into daemon.Run themselves; the env-sentinel branch is
//     specific to the spawned-child path.
func main() {
	if os.Getenv("YAP_DAEMON") == "1" {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "yap: load config: %v\n", err)
			os.Exit(1)
		}
		if err := daemon.Run(&cfg, daemon.DefaultDeps(linux.NewPlatform())); err != nil {
			fmt.Fprintf(os.Stderr, "yap: daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
