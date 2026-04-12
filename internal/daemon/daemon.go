package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Enriquefft/yap/internal/assets"
	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/engine"
	"github.com/Enriquefft/yap/internal/ipc"
	"github.com/Enriquefft/yap/internal/pidfile"
	"github.com/Enriquefft/yap/internal/platform"
	pcfg "github.com/Enriquefft/yap/pkg/yap/config"
	"github.com/Enriquefft/yap/pkg/yap/silence"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	// Register every transcribe backend the daemon can select at
	// runtime. Side-effect imports are the Phase 3 contract.
	_ "github.com/Enriquefft/yap/pkg/yap/transcribe/groq"
	_ "github.com/Enriquefft/yap/pkg/yap/transcribe/mock"
	_ "github.com/Enriquefft/yap/pkg/yap/transcribe/openai"
	_ "github.com/Enriquefft/yap/pkg/yap/transcribe/whisperlocal"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/fallback"
	// Register every transform backend. Phase 3 only shipped
	// passthrough; Phase 8 adds local (Ollama native) and openai
	// (any OpenAI-compatible SSE endpoint).
	_ "github.com/Enriquefft/yap/pkg/yap/transform/local"
	_ "github.com/Enriquefft/yap/pkg/yap/transform/openai"
	_ "github.com/Enriquefft/yap/pkg/yap/transform/passthrough"
)

// Deps holds all injectable dependencies for the daemon.
// Use DefaultDeps for production; substitute fields in tests.
//
// Path resolution is intentionally not pluggable: the daemon, the
// CLI, and tests all go through internal/pidfile's path helpers,
// which honor XDG environment variables (XDG_RUNTIME_DIR) tests
// already set via t.Setenv + xdg.Reload. Routing every path through
// one helper package is the project's single-source-of-truth rule
// for runtime file layout.
type Deps struct {
	Platform platform.Platform
	// PIDLock acquires the daemon's exclusive pidfile lock. On
	// success the returned Handle's Close method releases the flock
	// and removes the file — callers defer it for the lifetime of
	// the daemon process. Tests substitute this field with a stub
	// that returns an in-memory Handle instead of touching disk.
	PIDLock      func(string) (*pidfile.Handle, error)
	NewIPCServer func(string) (*ipc.Server, error)
}

// DefaultDeps returns production dependencies for the given platform.
func DefaultDeps(p platform.Platform) Deps {
	return Deps{
		Platform:     p,
		PIDLock:      pidfile.Acquire,
		NewIPCServer: ipc.NewServer,
	}
}

// NewTranscriber bridges the on-disk transcription config into the
// runtime transcribe.Config and looks up the factory via the registry.
// pcfg.TranscriptionConfig is the on-disk schema; transcribe.Config
// is the runtime library contract. Keeping them separate means the
// public pkg/yap/transcribe surface does not depend on the TOML
// schema package.
//
// NewTranscriber is the single source of truth for "how the daemon
// turns the on-disk transcription block into a Transcriber". Phase 7
// exposed it publicly so the CLI's one-shot commands (`yap record`,
// `yap transcribe`) reuse the same bridge instead of duplicating it.
func NewTranscriber(tc pcfg.TranscriptionConfig) (transcribe.Transcriber, error) {
	factory, err := transcribe.Get(tc.Backend)
	if err != nil {
		return nil, fmt.Errorf("transcription backend %q: %w", tc.Backend, err)
	}
	return factory(transcribe.Config{
		APIURL:            tc.ResolvedAPIURL(),
		APIKey:            tc.APIKey,
		Model:             tc.Model,
		Language:          tc.Language,
		ModelPath:         tc.ModelPath,
		WhisperServerPath: tc.WhisperServerPath,
		WhisperThreads:    tc.WhisperThreads,
		WhisperUseGPU:     tc.WhisperUseGPU,
		Timeout:           pcfg.DefaultTimeout,
	})
}

// NewTransformer bridges pcfg.TransformConfig into transform.Config
// and looks up the factory via the registry. When the transform stage
// is disabled in config, the factory is forced to "passthrough" so
// the engine pipeline always has a non-nil transformer.
//
// NewTransformer is the single source of truth for "how the daemon
// turns the on-disk transform block into a Transformer" without
// graceful-degradation wrapping. The CLI's debug-oriented
// `yap transform` command uses this so backend failures surface
// loudly instead of silently falling back.
//
// Callers that want graceful degradation (the daemon startup path,
// `yap record`) should use NewTransformerWithFallback and supply a
// live Notifier plus the user's stream_partials preference.
func NewTransformer(tc pcfg.TransformConfig) (transform.Transformer, error) {
	// streamPartials is irrelevant when notifier is nil — wrapping is
	// skipped either way — so we pass false here to keep the API
	// minimal.
	return NewTransformerWithFallback(tc, nil, false)
}

// NewTransformerWithFallback builds a Transformer per the on-disk
// transform config and optionally wraps it in a fallback decorator
// that degrades to passthrough on backend failure.
//
// The wrapping only happens when all of the following are true:
//
//   - transform.enabled is true
//   - the configured backend is non-trivial (not "passthrough")
//   - a non-nil notifier was supplied
//   - streamPartials is false
//
// When any of those conditions is false the primary transformer is
// returned directly. The streamPartials check is the streaming
// escape hatch: the fallback decorator buffers the primary's output
// and delivers it atomically (see pkg/yap/transform/fallback/doc.go),
// which would defeat the partial-injection promise the user opted
// into via general.stream_partials. Callers that want both
// streaming partials AND graceful fallback have to pick one — and
// the user picked streaming.
//
// The nil-notifier branch preserves Phase 7's public API for the
// CLI's debug `yap transform` command, which intentionally passes a
// nil notifier to see real errors.
//
// When wrapping is active, a startup health check runs synchronously
// via the backend's optional transform.Checker interface. On health
// failure the notifier receives one user-visible message and the
// returned transformer is the passthrough — no network round-trip
// per recording for the duration of this session.
//
// On mid-recording transform failures (the primary emits an error
// chunk), the fallback decorator replays the buffered input through
// passthrough and calls the notifier once with the primary's error.
func NewTransformerWithFallback(
	tc pcfg.TransformConfig,
	notifier platform.Notifier,
	streamPartials bool,
) (transform.Transformer, error) {
	name := tc.Backend
	if !tc.Enabled || name == "" {
		name = "passthrough"
	}
	factory, err := transform.Get(name)
	if err != nil {
		return nil, fmt.Errorf("transform backend %q: %w", name, err)
	}
	primary, err := factory(transform.Config{
		APIURL:       tc.APIURL,
		APIKey:       tc.APIKey,
		Model:        tc.Model,
		SystemPrompt: tc.SystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("transform backend %q: %w", name, err)
	}

	if name == "passthrough" || notifier == nil {
		return primary, nil
	}

	// streamPartials skips fallback wrapping — see the function
	// comment for the rationale. The user opted into partial
	// injection and the buffered fallback decorator would defeat
	// that promise. We still run the health probe so a misconfigured
	// backend surfaces a notification and swaps to passthrough at
	// startup time.
	if streamPartials {
		if checker, ok := primary.(transform.Checker); ok {
			checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := checker.HealthCheck(checkCtx)
			cancel()
			if err != nil {
				notifier.Notify(
					"yap: transform backend unreachable",
					fmt.Sprintf("%s: %v — falling back to passthrough", name, err),
				)
				return passthroughTransformer()
			}
		}
		return primary, nil
	}

	// Run the startup health probe if the backend supports it. A
	// failure does not refuse daemon startup — it raises a
	// notification and swaps the primary out for passthrough for the
	// rest of the session. This matches the graceful-degradation
	// ethos documented on ROADMAP Phase 8.
	if checker, ok := primary.(transform.Checker); ok {
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := checker.HealthCheck(checkCtx)
		cancel()
		if err != nil {
			notifier.Notify(
				"yap: transform backend unreachable",
				fmt.Sprintf("%s: %v — falling back to passthrough", name, err),
			)
			return passthroughTransformer()
		}
	}

	fb, err := passthroughTransformer()
	if err != nil {
		return nil, err
	}
	wrapped, err := fallback.New(primary, fb, func(err error) {
		notifier.Notify(
			"yap: transform failed",
			fmt.Sprintf("%s: %v — injected raw transcription", name, err),
		)
	})
	if err != nil {
		return nil, err
	}
	return wrapped, nil
}

// passthroughTransformer constructs the default identity transformer.
// Used by NewTransformerWithFallback when the primary backend fails
// its health probe (swap in passthrough for this session) and when
// wrapping the primary in a fallback decorator (passthrough is the
// identity tail of that decorator).
func passthroughTransformer() (transform.Transformer, error) {
	factory, err := transform.Get("passthrough")
	if err != nil {
		return nil, fmt.Errorf("transform passthrough: %w", err)
	}
	return factory(transform.Config{})
}

// InjectionOptionsFromConfig bridges pcfg.InjectionConfig into the
// runtime platform.InjectionOptions struct. The two types stay
// separate so the public TOML schema and the internal platform
// adapter layer can evolve independently — same pattern as the
// transcribe / transform bridges above.
//
// InjectionOptionsFromConfig is the single source of truth for "how
// the daemon turns the on-disk injection block into the platform
// runtime options". The CLI's `yap paste` command reuses it.
func InjectionOptionsFromConfig(ic pcfg.InjectionConfig) platform.InjectionOptions {
	out := platform.InjectionOptions{
		PreferOSC52:      ic.PreferOSC52,
		BracketedPaste:   ic.BracketedPaste,
		ElectronStrategy: ic.ElectronStrategy,
		DefaultStrategy:  ic.DefaultStrategy,
	}
	if len(ic.AppOverrides) > 0 {
		out.AppOverrides = make([]platform.AppOverride, 0, len(ic.AppOverrides))
		for _, ov := range ic.AppOverrides {
			out.AppOverrides = append(out.AppOverrides, platform.AppOverride{
				Match:       ov.Match,
				Strategy:    ov.Strategy,
				AppendEnter: ov.AppendEnter,
			})
		}
	}
	return out
}

// State machine constants for the recording lifecycle.
const (
	stateIdle       = "idle"
	stateRecording  = "recording"
	stateProcessing = "processing"
)

// recordState holds the recording state machine.
type recordState struct {
	mu     sync.Mutex
	st     string // "idle", "recording", "processing"
	cancel context.CancelFunc
}

// state returns the current state string. The zero value ("") is
// treated as idle so the recordState zero-value is usable without
// explicit initialization.
func (rs *recordState) state() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.st == "" {
		return stateIdle
	}
	return rs.st
}

// setState transitions to the given state.
func (rs *recordState) setState(s string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.st = s
}

// isActive returns true when in recording or processing state.
// The zero value ("") is treated as idle.
func (rs *recordState) isActive() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.st != "" && rs.st != stateIdle
}

// isRecording returns true only when in the recording state.
func (rs *recordState) isRecording() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.st == stateRecording
}

// setCancel sets the cancel function for the current recording.
func (rs *recordState) setCancel(cancel context.CancelFunc) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.cancel = cancel
}

// cancelRecording cancels the current recording context. It does NOT
// change the state — state transitions are handled by OnRecordingStop
// (recording → processing) and the deferred cleanup in startRecording
// (processing → idle).
func (rs *recordState) cancelRecording() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.cancel != nil {
		rs.cancel()
		rs.cancel = nil
	}
}

// Daemon represents the background process.
type Daemon struct {
	cfg      *config.Config
	ctx      context.Context
	state    recordState
	eng      *engine.Engine
	recorder platform.Recorder
	chime    platform.ChimePlayer
	notifier platform.Notifier
}

// New creates a new Daemon. Kept for test compatibility.
func New(cfg *config.Config) *Daemon {
	return &Daemon{cfg: cfg}
}

// Run starts the daemon event loop and blocks until SIGTERM/SIGINT.
// All cleanup (audio, PID file removal) is deferred and guaranteed to execute.
func Run(cfg *config.Config, deps Deps) error {
	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	// Take the exclusive flock for the daemon pidfile. The kernel
	// releases the lock automatically if this process dies — stale
	// locks are impossible, which means systemd Restart=on-failure
	// can never loop on a "pid file already exists" error.
	pidHandle, err := deps.PIDLock(pidPath)
	if err != nil {
		return fmt.Errorf("lock pid file: %w", err)
	}
	defer func() {
		if cerr := pidHandle.Close(); cerr != nil {
			slog.Default().Warn("pid file close error", "err", cerr)
		}
	}()

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
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	srv, err := deps.NewIPCServer(sockPath)
	if err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer srv.Close()

	// Build transcribe + transform backends from the registry.
	transcriber, err := NewTranscriber(cfg.Transcription)
	if err != nil {
		return fmt.Errorf("build transcriber: %w", err)
	}
	// Backends that own per-process resources (e.g. whisperlocal's
	// long-lived whisper-server subprocess) implement io.Closer. The
	// daemon is the canonical place to call Close because it owns
	// the lifetime of the registered transcriber. The Phase 3
	// Transcriber interface intentionally does not require Close —
	// the type assertion stays opt-in so library backends without
	// resources to release pay nothing.
	if closer, ok := transcriber.(io.Closer); ok {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				slog.Default().Warn("transcriber close error", "err", cerr)
			}
		}()
	}
	// Phase 8: wrap the configured transform backend in a fallback
	// decorator that falls back to passthrough on primary failure
	// and raises a user-visible notification. A startup health probe
	// is issued synchronously inside NewTransformerWithFallback when
	// the backend supports transform.Checker. When the user opted
	// into stream_partials the wrapping is skipped — the buffered
	// fallback decorator would defeat the partial-injection promise.
	transformer, err := NewTransformerWithFallback(
		cfg.Transform,
		deps.Platform.Notifier,
		cfg.General.StreamPartials,
	)
	if err != nil {
		return fmt.Errorf("build transformer: %w", err)
	}

	// Build the per-session injector from the bridged Phase 4
	// InjectionOptions. The platform factory owns its strategy list
	// and audit logger.
	injector, err := deps.Platform.NewInjector(InjectionOptionsFromConfig(cfg.Injection))
	if err != nil {
		return fmt.Errorf("build injector: %w", err)
	}

	// Build engine. The constructor rejects nil dependencies — every
	// stage is required and the daemon supplies a real one. The
	// passthrough fallback for transformer is owned by NewTransformer
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
		recorder: rec,
		chime:    deps.Platform.Chime,
		notifier: deps.Platform.Notifier,
	}

	// Resolve the on-disk config path so the status response can
	// surface it to operators. ConfigPath honors $YAP_CONFIG and the
	// /etc/yap fallback, matching what `yap config path` returns.
	configPath, err := config.ConfigPath()
	if err != nil {
		// Path resolution failures are non-fatal — the daemon can
		// still serve everything else, the status response just
		// omits the field.
		configPath = ""
	}

	// Wire IPC handlers.
	srv.SetShutdownFn(stop)
	srv.SetToggleFn(d.toggleRecording)
	srv.SetStatusFn(func() ipc.Response {
		return ipc.Response{
			Ok:         true,
			State:      d.state.state(),
			Mode:       cfg.General.Mode,
			ConfigPath: configPath,
			Version:    config.Version,
			PID:        os.Getpid(),
			Backend:    cfg.Transcription.Backend,
			Model:      cfg.Transcription.Model,
		}
	})

	go srv.Serve(ctx)

	timeoutSec := cfg.General.MaxDuration
	if timeoutSec == 0 {
		timeoutSec = 60
	}

	onPress := func() {
		if cfg.General.Mode == "toggle" {
			d.toggleRecording()
		} else {
			d.startRecording(timeoutSec)
		}
	}

	onRelease := func() {
		if cfg.General.Mode == "hold" {
			if !d.state.isRecording() {
				return
			}
			d.state.cancelRecording()
		}
		// toggle mode: onRelease is a no-op
	}

	go listener.Listen(ctx, hotkeyCode, onPress, onRelease)

	// Emit a single startup line so operators running under systemd
	// (or `yap listen --foreground`) see the daemon is alive without
	// having to trigger a recording first. Every field here is what
	// an operator needs to diagnose a "daemon appears dead" report
	// without shelling into the box.
	slog.Default().Info("yap daemon started",
		"socket", sockPath,
		"pid", os.Getpid(),
		"config", configPath,
		"backend", cfg.Transcription.Backend,
		"model", cfg.Transcription.Model,
		"hotkey", cfg.General.Hotkey,
		"mode", cfg.General.Mode,
	)

	<-ctx.Done()
	slog.Default().Info("yap daemon stopped")
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
	slog.Default().Info("state", "from", stateIdle, "to", stateRecording)
	d.state.setState(stateRecording)

	// Wire silence detection into the recorder's frame callback when
	// enabled. The detector fires onWarning ~1s before silence auto-stop
	// and onSilence to cancel the recording context.
	if d.cfg.General.SilenceDetection {
		if fn, ok := d.recorder.(platform.FrameNotifier); ok {
			silenceDur := d.cfg.General.SilenceDuration
			warningBefore := 1.0 // seconds before auto-stop
			if silenceDur < warningBefore+0.1 {
				warningBefore = silenceDur * 0.5
			}
			detector := silence.New(
				d.cfg.General.SilenceThreshold,
				silenceDur,
				warningBefore,
				func() { // onWarning
					if d.chime != nil {
						if r, err := assets.WarningChime(); err == nil {
							d.chime.Play(r)
						}
					}
				},
				func() { // onSilence
					d.state.cancelRecording()
				},
			)
			fn.SetOnFrame(detector.Process)
		}
	}

	go func() {
		defer func() {
			// Clear the frame notifier callback so the detector is inert.
			if fn, ok := d.recorder.(platform.FrameNotifier); ok {
				fn.SetOnFrame(nil)
			}
			slog.Default().Info("state", "from", stateProcessing, "to", stateIdle)
			d.state.setState(stateIdle)
		}()

		err := d.eng.Run(d.ctx, engine.RunOptions{
			RecordCtx:      recCtx,
			StartChime:     assets.StartChime,
			StopChime:      assets.StopChime,
			WarningChime:   assets.WarningChime,
			TimeoutSec:     timeoutSec,
			StreamPartials: d.cfg.General.StreamPartials,
			OnRecordingStop: func() {
				slog.Default().Info("state", "from", stateRecording, "to", stateProcessing)
				d.state.setState(stateProcessing)
			},
			TranscribeOpts: transcribe.Options{},
			TransformOpts:  transform.Options{},
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

// toggleRecording toggles recording state for the IPC toggle command
// and the toggle hotkey mode. Returns the intended new state:
// "recording" if starting, "idle" if stopping.
func (d *Daemon) toggleRecording() string {
	if d.state.isActive() {
		d.state.cancelRecording()
		return stateIdle
	}
	timeoutSec := d.cfg.General.MaxDuration
	if timeoutSec == 0 {
		timeoutSec = 60
	}
	d.startRecording(timeoutSec)
	return stateRecording
}
