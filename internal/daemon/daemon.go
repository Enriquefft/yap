package daemon

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/hybridz/yap/internal/audio"
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/adrg/xdg"
)

// Daemon represents the background process.
type Daemon struct {
	cfg *config.Config
}

// New creates a new Daemon.
func New(cfg *config.Config) *Daemon {
	return &Daemon{cfg: cfg}
}

// Run starts the daemon event loop and blocks until SIGTERM.
// All cleanup (PortAudio, PID file removal) is deferred and guaranteed to execute.
//
// Sequence:
// 1. Resolve PID path via xdg.DataFile ("yap/yap.pid")
// 2. Write PID using O_EXCL atomic create (DAEMON-01, DAEMON-05)
// 3. Init PortAudio and Recorder (audio.NewRecorder)
// 4. Defer cleanup: Recorder.Close() runs before return (AUDIO-07)
// 5. Defer cleanup: pidfile.Remove() runs before return
// 6. Setup signal.NotifyContext for SIGTERM/SIGINT (DAEMON-04)
// 7. Block on <-ctx.Done()
// 8. Return with all defers executing
//
// Reference: github.com/adrg/xdg creates $XDG_DATA_HOME/yap automatically.
func Run(cfg *config.Config) error {
	pidPath, err := xdg.DataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	// Write PID using O_EXCL for atomic creation (prevents DAEMON-05 race).
	if err := pidfile.Write(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer pidfile.Remove(pidPath)

	// Init PortAudio and create Recorder (AUDIO-07: deferred cleanup).
	rec, err := audio.NewRecorder(cfg.MicDevice)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	defer rec.Close()

	// Signal-driven shutdown: SIGTERM or SIGINT cancels context.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// TODO(Phase 3-02): Start IPC server in goroutine here.
	// For Phase 3-01, just block on signal.

	// Block until signal received.
	<-ctx.Done()

	// All defers execute as we return: rec.Close(), pidfile.Remove()
	return nil
}
