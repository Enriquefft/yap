package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/assets"
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/engine"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/internal/transcribe"
)

// transcribeAdapter wraps transcribe.Transcribe to implement engine.Transcriber.
type transcribeAdapter struct{}

func (transcribeAdapter) Transcribe(ctx context.Context, apiKey string, wavData []byte, language string) (string, error) {
	return transcribe.Transcribe(ctx, apiKey, wavData, language)
}

// Deps holds all injectable dependencies for the daemon.
// Use DefaultDeps for production; substitute fields in tests.
type Deps struct {
	Platform     platform.Platform
	Transcriber  engine.Transcriber
	XDGDataFile  func(string) (string, error)
	PIDWrite     func(string) error
	PIDRemove    func(string)
	NewIPCServer func(string) (*ipc.Server, error)
}

// DefaultDeps returns production dependencies for the given platform.
func DefaultDeps(p platform.Platform) Deps {
	return Deps{
		Platform:     p,
		Transcriber:  transcribeAdapter{},
		XDGDataFile:  xdg.DataFile,
		PIDWrite:     pidfile.Write,
		PIDRemove:    pidfile.Remove,
		NewIPCServer: ipc.NewServer,
	}
}

// recordState holds the recording state machine.
type recordState struct {
	mu     sync.Mutex
	active bool
	cancel context.CancelFunc
}

// isActive returns true if recording is active.
func (rs *recordState) isActive() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.active
}

// setIsActive sets the recording active state.
func (rs *recordState) setIsActive(active bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.active = active
}

// setCancel sets the cancel function for the current recording.
func (rs *recordState) setCancel(cancel context.CancelFunc) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.cancel = cancel
}

// cancelRecording cancels the current recording if active.
func (rs *recordState) cancelRecording() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.cancel != nil {
		rs.cancel()
		rs.cancel = nil
	}
	rs.active = false
}

// Daemon represents the background process.
type Daemon struct {
	cfg   *config.Config
	ctx   context.Context
	state recordState
	eng   *engine.Engine
}

// New creates a new Daemon. Kept for test compatibility.
func New(cfg *config.Config) *Daemon {
	return &Daemon{cfg: cfg}
}

// Run starts the daemon event loop and blocks until SIGTERM/SIGINT.
// All cleanup (audio, PID file removal) is deferred and guaranteed to execute.
func Run(cfg *config.Config, deps Deps) error {
	pidPath, err := deps.XDGDataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	if err := deps.PIDWrite(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer deps.PIDRemove(pidPath)

	// Create recorder for this session.
	rec, err := deps.Platform.NewRecorder(cfg.MicDevice)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	defer rec.Close()

	// Signal-driven shutdown: SIGTERM or SIGINT cancels context.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Parse hotkey code from config.
	hotkeyCode, err := deps.Platform.HotkeyCfg.ParseKey(cfg.Hotkey)
	if err != nil {
		return fmt.Errorf("invalid hotkey %q: %w", cfg.Hotkey, err)
	}

	// Initialize hotkey listener.
	listener, err := deps.Platform.NewHotkey()
	if err != nil {
		if os.IsPermission(err) {
			deps.Platform.Notifier.Notify("yap: permission error", err.Error())
		}
		return fmt.Errorf("hotkey setup: %w", err)
	}
	defer listener.Close()

	// Start IPC server.
	sockPath, err := deps.XDGDataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	srv, err := deps.NewIPCServer(sockPath)
	if err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer srv.Close()

	// Build engine.
	eng := engine.New(
		rec,
		deps.Platform.Chime,
		deps.Platform.Paster,
		deps.Platform.Notifier,
		deps.Transcriber,
		cfg.APIKey,
		cfg.Language,
	)

	d := &Daemon{
		cfg: cfg,
		ctx: ctx,
		eng: eng,
	}

	// Wire IPC handlers.
	srv.SetShutdownFn(stop)
	srv.SetToggleFn(d.toggleRecording)
	srv.SetStatusFn(func() string {
		if d.state.isActive() {
			return "recording"
		}
		return "idle"
	})

	go srv.Serve(ctx)

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	onPress := func() {
		if d.state.isActive() {
			return
		}

		recCtx, recCancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		d.state.setCancel(recCancel)
		d.state.setIsActive(true)

		go func() {
			defer d.state.setIsActive(false)
			d.eng.RecordAndPaste(ctx, recCtx, timeoutSec,
				assets.StartChime, assets.StopChime, assets.WarningChime)
		}()
	}

	onRelease := func() {
		if !d.state.isActive() {
			return
		}
		d.state.cancelRecording()
	}

	go listener.Listen(ctx, hotkeyCode, onPress, onRelease)

	<-ctx.Done()
	return nil
}

// toggleRecording toggles recording state for IPC toggle command.
// Returns new state: "recording" or "idle".
func (d *Daemon) toggleRecording() string {
	if d.state.isActive() {
		d.state.cancelRecording()
		return "idle"
	}

	timeoutSec := d.cfg.TimeoutSeconds
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	recCtx, recCancel := context.WithTimeout(d.ctx, time.Duration(timeoutSec)*time.Second)
	d.state.setCancel(recCancel)
	d.state.setIsActive(true)

	go func() {
		defer d.state.setIsActive(false)
		d.eng.RecordAndPaste(d.ctx, recCtx, timeoutSec,
			assets.StartChime, assets.StopChime, assets.WarningChime)
	}()

	return "recording"
}
