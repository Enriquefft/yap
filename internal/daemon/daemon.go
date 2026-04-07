package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	// Register every transcribe backend the daemon can select at
	// runtime. Side-effect imports are the Phase 3 contract.
	_ "github.com/hybridz/yap/pkg/yap/transcribe/groq"
	_ "github.com/hybridz/yap/pkg/yap/transcribe/mock"
	_ "github.com/hybridz/yap/pkg/yap/transcribe/openai"
	"github.com/hybridz/yap/pkg/yap/transform"
	// Register every transform backend. Phase 3 only ships
	// passthrough; Phase 8 adds local + openai.
	_ "github.com/hybridz/yap/pkg/yap/transform/passthrough"
)

// Deps holds all injectable dependencies for the daemon.
// Use DefaultDeps for production; substitute fields in tests.
type Deps struct {
	Platform     platform.Platform
	XDGDataFile  func(string) (string, error)
	PIDWrite     func(string) error
	PIDRemove    func(string)
	NewIPCServer func(string) (*ipc.Server, error)
}

// DefaultDeps returns production dependencies for the given platform.
func DefaultDeps(p platform.Platform) Deps {
	return Deps{
		Platform:     p,
		XDGDataFile:  xdg.DataFile,
		PIDWrite:     pidfile.Write,
		PIDRemove:    pidfile.Remove,
		NewIPCServer: ipc.NewServer,
	}
}

// newTranscriber bridges the on-disk transcription config into the
// runtime transcribe.Config and looks up the factory via the registry.
// pcfg.TranscriptionConfig is the on-disk schema; transcribe.Config
// is the runtime library contract. Keeping them separate means the
// public pkg/yap/transcribe surface does not depend on the TOML
// schema package.
func newTranscriber(tc pcfg.TranscriptionConfig) (transcribe.Transcriber, error) {
	factory, err := transcribe.Get(tc.Backend)
	if err != nil {
		return nil, fmt.Errorf("transcription backend %q: %w", tc.Backend, err)
	}
	return factory(transcribe.Config{
		APIURL:    tc.ResolvedAPIURL(),
		APIKey:    tc.APIKey,
		Model:     tc.Model,
		Language:  tc.Language,
		Prompt:    tc.Prompt,
		ModelPath: tc.ModelPath,
		Timeout:   pcfg.DefaultTimeout,
	})
}

// newTransformer bridges pcfg.TransformConfig into transform.Config
// and looks up the factory via the registry. When the transform stage
// is disabled in config, the factory is forced to "passthrough" so
// the engine pipeline always has a non-nil transformer.
func newTransformer(tc pcfg.TransformConfig) (transform.Transformer, error) {
	name := tc.Backend
	if !tc.Enabled || name == "" {
		name = "passthrough"
	}
	factory, err := transform.Get(name)
	if err != nil {
		return nil, fmt.Errorf("transform backend %q: %w", name, err)
	}
	return factory(transform.Config{
		APIURL:       tc.APIURL,
		APIKey:       tc.APIKey,
		Model:        tc.Model,
		SystemPrompt: tc.SystemPrompt,
	})
}

// injectionOptionsFromConfig bridges pcfg.InjectionConfig into the
// runtime platform.InjectionOptions struct. The two types stay
// separate so the public TOML schema and the internal platform
// adapter layer can evolve independently — same pattern as the
// transcribe / transform bridges above.
func injectionOptionsFromConfig(ic pcfg.InjectionConfig) platform.InjectionOptions {
	out := platform.InjectionOptions{
		PreferOSC52:      ic.PreferOSC52,
		BracketedPaste:   ic.BracketedPaste,
		ElectronStrategy: ic.ElectronStrategy,
	}
	if len(ic.AppOverrides) > 0 {
		out.AppOverrides = make([]platform.AppOverride, 0, len(ic.AppOverrides))
		for _, ov := range ic.AppOverrides {
			out.AppOverrides = append(out.AppOverrides, platform.AppOverride{
				Match:    ov.Match,
				Strategy: ov.Strategy,
			})
		}
	}
	return out
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
	cfg      *config.Config
	ctx      context.Context
	state    recordState
	eng      *engine.Engine
	notifier platform.Notifier
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
	rec, err := deps.Platform.NewRecorder(cfg.General.AudioDevice)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	defer rec.Close()

	// Signal-driven shutdown: SIGTERM or SIGINT cancels context.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Parse hotkey code from config.
	hotkeyCode, err := deps.Platform.HotkeyCfg.ParseKey(cfg.General.Hotkey)
	if err != nil {
		return fmt.Errorf("invalid hotkey %q: %w", cfg.General.Hotkey, err)
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

	// Build transcribe + transform backends from the registry.
	transcriber, err := newTranscriber(cfg.Transcription)
	if err != nil {
		return fmt.Errorf("build transcriber: %w", err)
	}
	transformer, err := newTransformer(cfg.Transform)
	if err != nil {
		return fmt.Errorf("build transformer: %w", err)
	}

	// Build the per-session injector from the bridged Phase 4
	// InjectionOptions. The platform factory owns its strategy list
	// and audit logger.
	injector, err := deps.Platform.NewInjector(injectionOptionsFromConfig(cfg.Injection))
	if err != nil {
		return fmt.Errorf("build injector: %w", err)
	}

	// Build engine. The constructor rejects nil dependencies — every
	// stage is required and the daemon supplies a real one. The
	// passthrough fallback for transformer is owned by newTransformer
	// (above), not the engine, keeping the engine free of backend
	// imports. Notifications are owned by the daemon, not the
	// engine: startRecording inspects Run's wrapped error and routes
	// non-cancellation failures into the platform Notifier directly.
	eng, err := engine.New(
		rec,
		deps.Platform.Chime,
		transcriber,
		transformer,
		injector,
		slog.Default(),
	)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}

	d := &Daemon{
		cfg:      cfg,
		ctx:      ctx,
		eng:      eng,
		notifier: deps.Platform.Notifier,
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

	timeoutSec := cfg.General.MaxDuration
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	onPress := func() {
		d.startRecording(timeoutSec)
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

// startRecording is the shared press/toggle entry point. It is
// idempotent on the active state — both onPress (hotkey) and
// toggleRecording (IPC) call it; the deduplication keeps the goroutine
// shell single-sourced and immune to drift between the two paths.
//
// timeoutSec is the recording timeout in seconds; the engine schedules
// the warning chime against it and the recording context inherits a
// time.Duration*Second deadline so the recorder can never run forever.
//
// startRecording returns true if a new recording was started, false if
// one was already in flight. Pipeline errors are routed to the
// notifier so the user gets an OS toast on real failures; cancellation
// (the normal stop path) is silently ignored.
func (d *Daemon) startRecording(timeoutSec int) bool {
	if d.state.isActive() {
		return false
	}

	recCtx, recCancel := context.WithTimeout(d.ctx, time.Duration(timeoutSec)*time.Second)
	d.state.setCancel(recCancel)
	d.state.setIsActive(true)

	go func() {
		defer d.state.setIsActive(false)
		err := d.eng.Run(d.ctx, engine.RunOptions{
			RecordCtx:      recCtx,
			StartChime:     assets.StartChime,
			StopChime:      assets.StopChime,
			WarningChime:   assets.WarningChime,
			TimeoutSec:     timeoutSec,
			StreamPartials: d.cfg.General.StreamPartials,
		})
		if err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) &&
			d.notifier != nil {
			d.notifier.Notify("yap pipeline error", err.Error())
		}
	}()
	return true
}

// toggleRecording toggles recording state for the IPC toggle command.
// Returns the new state: "recording" or "idle".
func (d *Daemon) toggleRecording() string {
	if d.state.isActive() {
		d.state.cancelRecording()
		return "idle"
	}
	timeoutSec := d.cfg.General.MaxDuration
	if timeoutSec == 0 {
		timeoutSec = 60
	}
	d.startRecording(timeoutSec)
	return "recording"
}
