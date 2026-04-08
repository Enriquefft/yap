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
	_ "github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/fallback"
	// Register every transform backend. Phase 3 only shipped
	// passthrough; Phase 8 adds local (Ollama native) and openai
	// (any OpenAI-compatible SSE endpoint).
	_ "github.com/hybridz/yap/pkg/yap/transform/local"
	_ "github.com/hybridz/yap/pkg/yap/transform/openai"
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
		Prompt:            tc.Prompt,
		ModelPath:         tc.ModelPath,
		WhisperServerPath: tc.WhisperServerPath,
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
		state := "idle"
		if d.state.isActive() {
			state = "recording"
		}
		return ipc.Response{
			Ok:         true,
			State:      state,
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
