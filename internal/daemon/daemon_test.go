package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/hybridz/yap/internal/platform"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/fallback"
)

// TestRecordState verifies recording state machine operations.
func TestRecordState(t *testing.T) {
	var rs recordState

	// Initial state is idle.
	if rs.state() != stateIdle {
		t.Errorf("initial state = %q, want %q", rs.state(), stateIdle)
	}
	if rs.isActive() {
		t.Error("initially should not be active")
	}
	if rs.isRecording() {
		t.Error("initially should not be recording")
	}

	// Transition to recording.
	rs.setState(stateRecording)
	if rs.state() != stateRecording {
		t.Errorf("state = %q, want %q", rs.state(), stateRecording)
	}
	if !rs.isActive() {
		t.Error("recording state should be active")
	}
	if !rs.isRecording() {
		t.Error("should report isRecording")
	}

	// Transition to processing.
	rs.setState(stateProcessing)
	if rs.state() != stateProcessing {
		t.Errorf("state = %q, want %q", rs.state(), stateProcessing)
	}
	if !rs.isActive() {
		t.Error("processing state should be active")
	}
	if rs.isRecording() {
		t.Error("processing should not report isRecording")
	}

	// cancelRecording cancels the context but does not change state.
	cancelCalled := false
	rs.setCancel(func() {
		cancelCalled = true
	})
	rs.cancelRecording()
	if !cancelCalled {
		t.Error("cancel function should be called")
	}
	if rs.state() != stateProcessing {
		t.Errorf("cancelRecording should not change state, got %q", rs.state())
	}

	// Back to idle.
	rs.setState(stateIdle)
	if rs.isActive() {
		t.Error("idle should not be active")
	}

	// Calling cancelRecording again should be safe (nil cancel).
	rs.cancelRecording()
}

// TestNew creates a Daemon instance with a nested config.
func TestNew(t *testing.T) {
	cfg := pcfg.DefaultConfig()
	cfg.General.Hotkey = "KEY_RIGHTCTRL"
	cfg.Transcription.Language = "en"
	cfg.Transcription.APIKey = "test-key"

	c := config.Config(cfg)
	d := New(&c)
	if d == nil {
		t.Error("New() returned nil")
	}
	if d.cfg != &c {
		t.Error("Daemon config not set correctly")
	}
}

// TestInjectionOptionsFromConfigBridge guards the structural mapping
// the daemon performs between the on-disk pcfg.InjectionConfig and
// the runtime platform.InjectionOptions. The fields are intentionally
// 1:1; the test fails the build if a future schema change forgets to
// extend either side.
func TestInjectionOptionsFromConfigBridge(t *testing.T) {
	in := pcfg.InjectionConfig{
		PreferOSC52:      true,
		BracketedPaste:   false,
		ElectronStrategy: "clipboard",
		AppOverrides: []pcfg.AppOverride{
			{Match: "kitty", Strategy: "osc52"},
			{Match: "code", Strategy: "clipboard"},
		},
	}
	got := InjectionOptionsFromConfig(in)
	want := platform.InjectionOptions{
		PreferOSC52:      true,
		BracketedPaste:   false,
		ElectronStrategy: "clipboard",
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "osc52"},
			{Match: "code", Strategy: "clipboard"},
		},
	}
	if got.PreferOSC52 != want.PreferOSC52 {
		t.Errorf("PreferOSC52 = %v, want %v", got.PreferOSC52, want.PreferOSC52)
	}
	if got.BracketedPaste != want.BracketedPaste {
		t.Errorf("BracketedPaste = %v, want %v", got.BracketedPaste, want.BracketedPaste)
	}
	if got.ElectronStrategy != want.ElectronStrategy {
		t.Errorf("ElectronStrategy = %q, want %q", got.ElectronStrategy, want.ElectronStrategy)
	}
	if len(got.AppOverrides) != len(want.AppOverrides) {
		t.Fatalf("AppOverrides len = %d, want %d", len(got.AppOverrides), len(want.AppOverrides))
	}
	for i := range got.AppOverrides {
		if got.AppOverrides[i] != want.AppOverrides[i] {
			t.Errorf("AppOverrides[%d] = %+v, want %+v", i, got.AppOverrides[i], want.AppOverrides[i])
		}
	}
}

// TestInjectionOptionsFromConfigEmptyOverrides guards the nil-vs-empty
// behavior: an empty config slice produces a nil platform slice (no
// allocation), preserving the zero-cost path for the common case where
// the user has no app overrides configured.
func TestInjectionOptionsFromConfigEmptyOverrides(t *testing.T) {
	in := pcfg.InjectionConfig{
		PreferOSC52:      true,
		BracketedPaste:   true,
		ElectronStrategy: "clipboard",
	}
	got := InjectionOptionsFromConfig(in)
	if got.AppOverrides != nil {
		t.Errorf("AppOverrides = %v, want nil", got.AppOverrides)
	}
}

// countingNotifier records how many times Notify was called and
// captures the last payload for assertions.
type countingNotifier struct {
	calls   int32
	title   string
	message string
}

func (c *countingNotifier) Notify(title, message string) {
	atomic.AddInt32(&c.calls, 1)
	c.title = title
	c.message = message
}

var _ platform.Notifier = (*countingNotifier)(nil)

// TestNewTransformer_PassthroughWhenDisabled asserts that a config
// with transform.enabled = false yields the passthrough backend
// regardless of the configured Backend field, and no wrapping takes
// place.
func TestNewTransformer_PassthroughWhenDisabled(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: false,
		Backend: "openai",
		APIURL:  "http://example.invalid/v1",
		Model:   "gpt-4o-mini",
	}
	tr, err := NewTransformer(tc)
	if err != nil {
		t.Fatalf("NewTransformer: %v", err)
	}
	if tr == nil {
		t.Fatal("transformer is nil")
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unexpected fallback wrapping for disabled transform")
	}
}

// TestNewTransformerWithFallback_NoNotifier_NoWrapping asserts that
// a nil notifier returns the primary backend directly, preserving
// the Phase 7 debug-surface semantics of `yap transform`.
func TestNewTransformerWithFallback_NoNotifier_NoWrapping(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "local",
		Model:   "llama3",
	}
	tr, err := NewTransformerWithFallback(tc, nil, false)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unexpected fallback wrapping when notifier is nil")
	}
}

// TestNewTransformerWithFallback_HealthCheckSuccess_Wraps asserts
// that a healthy backend and live notifier yields a fallback-wrapped
// transformer.
func TestNewTransformerWithFallback_HealthCheckSuccess_Wraps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both /v1/models (openai) and / (local) respond 200.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "openai",
		APIURL:  srv.URL + "/v1",
		Model:   "gpt-4o-mini",
		APIKey:  "sk-test",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier, false)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if _, ok := tr.(*fallback.Transformer); !ok {
		t.Errorf("transformer type = %T, want *fallback.Transformer", tr)
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 0 {
		t.Errorf("notifier calls = %d, want 0 on healthy check", got)
	}
}

// TestNewTransformerWithFallback_HealthCheckFailure_Notifies asserts
// that a failing health probe fires the notifier exactly once and
// the returned transformer is the passthrough (not wrapped).
func TestNewTransformerWithFallback_HealthCheckFailure_Notifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "local",
		APIURL:  srv.URL,
		Model:   "llama3",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier, false)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 1 {
		t.Errorf("notifier calls = %d, want 1", got)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unhealthy backend should yield passthrough, not fallback wrapper")
	}

	// Verify the returned transformer is usable end-to-end: it
	// should forward input verbatim (passthrough semantics).
	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: "hi", IsFinal: true}
	close(in)
	out, err := tr.Transform(context.Background(), in)
	if err != nil {
		t.Fatalf("fallback Transform: %v", err)
	}
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("got = %+v, want passthrough echo", got)
	}
}

// TestNewTransformerWithFallback_UnknownBackend_Errors asserts that a
// bogus backend name is a hard error, not a silent fallback. Users
// who misconfigure the backend name want to know immediately.
func TestNewTransformerWithFallback_UnknownBackend_Errors(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "this-backend-does-not-exist",
		Model:   "x",
	}
	_, err := NewTransformerWithFallback(tc, &countingNotifier{}, false)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestNewTransformerWithFallback_StreamPartials_NoFallbackWrapping
// asserts the F1-alternative fix: when the user opted into
// stream_partials the daemon must skip wrapping the primary in the
// buffered fallback decorator, even when a notifier is supplied. The
// buffered decorator would defeat the partial-injection promise.
func TestNewTransformerWithFallback_StreamPartials_NoFallbackWrapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "openai",
		APIURL:  srv.URL + "/v1",
		Model:   "gpt-4o-mini",
		APIKey:  "sk-test",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier, true /* streamPartials */)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("stream_partials must skip fallback wrapping; got *fallback.Transformer")
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 0 {
		t.Errorf("notifier calls = %d, want 0 on healthy check + stream_partials", got)
	}
}

// TestNewTransformerWithFallback_StreamPartials_HealthCheckFailureSwapsToPassthrough
// asserts the streaming-mode health-probe path: even with
// stream_partials = true, an unhealthy backend at startup time still
// triggers the notifier and swaps to passthrough for the session.
func TestNewTransformerWithFallback_StreamPartials_HealthCheckFailureSwapsToPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "local",
		APIURL:  srv.URL,
		Model:   "llama3",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier, true /* streamPartials */)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 1 {
		t.Errorf("notifier calls = %d, want 1", got)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unhealthy backend with stream_partials should yield passthrough, not fallback wrapper")
	}

	// The returned transformer must still be usable.
	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: "hi", IsFinal: true}
	close(in)
	out, err := tr.Transform(context.Background(), in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("got = %+v, want passthrough echo", got)
	}
}

// Ensure the fallback wrapper still satisfies the transform.Transformer
// interface contract the engine consumes.
func TestFallbackInterfaceSatisfied(t *testing.T) {
	var _ transform.Transformer = (*fallback.Transformer)(nil)
}

// -- daemon.Run startup log test fixture --------------------------------
//
// Bug 13 fix: daemon.Run must emit a single structured INFO line the
// moment wiring is complete so operators running under systemd (or
// `yap listen --foreground`) see a "daemon is alive" confirmation
// without having to trigger a recording first. The test below drives
// a full daemon.Run with stub platform dependencies, watches for the
// startup log line, then shuts the daemon down via the IPC Stop
// command. The assertion is structural: the captured slog record
// must be msg="yap daemon started" with every mandated attribute
// present (socket, pid, config, backend, model, hotkey, mode).

// fakeDaemonRecorder satisfies platform.Recorder. Start blocks until
// ctx cancellation so the daemon's engine never kicks off a real
// transcription during this test — we just want the wiring complete.
type fakeDaemonRecorder struct{}

func (f *fakeDaemonRecorder) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
func (f *fakeDaemonRecorder) Encode() ([]byte, error) { return []byte("RIFF-test"), nil }
func (f *fakeDaemonRecorder) Close()                  {}

// fakeDaemonHotkey satisfies platform.Hotkey. Listen blocks on ctx —
// the hotkey is never fired during the startup-log test.
type fakeDaemonHotkey struct{}

func (f *fakeDaemonHotkey) Listen(ctx context.Context, key platform.KeyCode, onPress, onRelease func()) {
	<-ctx.Done()
}
func (f *fakeDaemonHotkey) Close() {}

// fakeDaemonHotkeyCfg satisfies platform.HotkeyConfig. Only ParseKey
// is exercised by daemon.Run — it maps the config string to a code
// the fake listener will never see.
type fakeDaemonHotkeyCfg struct{}

func (f *fakeDaemonHotkeyCfg) ValidKey(name string) bool                   { return true }
func (f *fakeDaemonHotkeyCfg) ParseKey(name string) (platform.KeyCode, error) {
	return platform.KeyCode(1), nil
}
func (f *fakeDaemonHotkeyCfg) DetectKey(ctx context.Context) (string, error) {
	return "KEY_RIGHTCTRL", nil
}

// fakeDaemonChime / fakeDaemonNotifier are no-op stubs.
type fakeDaemonChime struct{}

func (f *fakeDaemonChime) Play(r io.Reader) {}

type fakeDaemonNotifier struct{}

func (f *fakeDaemonNotifier) Notify(title, message string) {}

// fakeDaemonInjector satisfies inject.Injector. Never called during
// the startup test because the fake recorder never emits audio.
type fakeDaemonInjector struct{}

func (f *fakeDaemonInjector) Inject(ctx context.Context, text string) error { return nil }
func (f *fakeDaemonInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	for range in {
	}
	return nil
}

// fakeDaemonPlatform builds a platform.Platform whose every method
// points at a fake that does nothing the daemon actually blocks on.
func fakeDaemonPlatform() platform.Platform {
	return platform.Platform{
		NewRecorder: func(deviceName string) (platform.Recorder, error) {
			return &fakeDaemonRecorder{}, nil
		},
		Chime:    &fakeDaemonChime{},
		NewHotkey: func() (platform.Hotkey, error) {
			return &fakeDaemonHotkey{}, nil
		},
		HotkeyCfg: &fakeDaemonHotkeyCfg{},
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &fakeDaemonInjector{}, nil
		},
		Notifier: &fakeDaemonNotifier{},
	}
}

// captureState is the shared mutable state of a captureHandler tree.
// A single *captureState is created by newCaptureHandler and threaded
// through every derived handler (WithAttrs, WithGroup) so the whole
// tree writes under one mutex into one buffer. Without this, a naive
// "copy the handler struct" derivation would leave each derived
// handler with its own zero-value mutex but aliased buffer storage —
// a latent data race waiting for the first slog.With(...) caller.
type captureState struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// captureHandler is a thread-safe slog.Handler that records every
// INFO-and-above record as JSON lines into an in-memory buffer. The
// startup-log test asserts on these lines directly instead of shelling
// out to the filesystem. All handlers in a WithAttrs/WithGroup tree
// share the same *captureState, so concurrent writes across parent
// and derived handlers are serialized on one mutex and collect into
// one canonical buffer.
type captureHandler struct {
	state *captureState
	h     slog.Handler
}

func newCaptureHandler() *captureHandler {
	st := &captureState{}
	return &captureHandler{
		state: st,
		h:     slog.NewJSONHandler(&st.buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
	}
}

func (c *captureHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return c.h.Enabled(ctx, lvl)
}
func (c *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.h.Handle(ctx, r)
}
func (c *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{state: c.state, h: c.h.WithAttrs(attrs)}
}
func (c *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{state: c.state, h: c.h.WithGroup(name)}
}

// bufString returns the current contents of the shared buffer under
// the shared mutex. Callers that want to log the captured bytes on
// assertion failure must go through this helper instead of touching
// state.buf directly, so the race detector stays green even if the
// daemon goroutine is still emitting records.
func (c *captureHandler) bufString() string {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.buf.String()
}

// records splits the captured buffer into one decoded map per line
// so individual slog records can be inspected structurally.
func (c *captureHandler) records() []map[string]any {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(c.state.buf.String(), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// TestRun_EmitsStartupLog is the Bug 13 regression. It drives a full
// daemon.Run with fake platform deps, waits for the "yap daemon
// started" log line to appear in the captured slog buffer, then shuts
// the daemon down via the IPC CmdStop path so the goroutine exits
// cleanly. We also assert the matching "yap daemon stopped" line is
// captured on shutdown.
func TestRun_EmitsStartupLog(t *testing.T) {
	// Scratch XDG layout — every runtime path the daemon touches
	// (pidfile, IPC socket) resolves under $XDG_RUNTIME_DIR.
	tmp := t.TempDir()
	runtimeDir := filepath.Join(tmp, "run")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	cfgFile := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgFile, []byte("# intentionally empty\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	xdg.Reload()

	// Swap in a capturing slog handler for the duration of the test.
	// Restoring the previous default via t.Cleanup keeps subsequent
	// tests on the original handler.
	prev := slog.Default()
	ch := newCaptureHandler()
	slog.SetDefault(slog.New(ch))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := pcfg.DefaultConfig()
	cfg.General.Hotkey = "KEY_RIGHTCTRL"
	cfg.General.Mode = "hold"
	cfg.Transcription.Backend = "mock"
	cfg.Transcription.Model = "mock"
	cfg.Transform.Enabled = false
	cfg.Transform.Backend = "passthrough"
	c := config.Config(cfg)

	deps := Deps{
		Platform:     fakeDaemonPlatform(),
		PIDLock:      pidfile.Acquire,
		NewIPCServer: ipc.NewServer,
	}

	runDone := make(chan error, 1)
	go func() { runDone <- Run(&c, deps) }()

	// Poll the capturing handler for the startup line. The daemon
	// emits it synchronously inside Run right before blocking on
	// <-ctx.Done(), so a live daemon must surface it within a few
	// hundred milliseconds. A 3s budget gives slow CI VMs headroom
	// without hanging a broken regression forever.
	deadline := time.Now().Add(3 * time.Second)
	var started map[string]any
	for time.Now().Before(deadline) && started == nil {
		for _, rec := range ch.records() {
			if rec["msg"] == "yap daemon started" {
				started = rec
				break
			}
		}
		if started == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if started == nil {
		t.Fatalf("startup log line never appeared within 3s; captured:\n%s", ch.bufString())
	}

	// Assert every mandated structured attribute is present and
	// non-empty where applicable. String concatenation is disallowed
	// by the bug fix contract — the assertions below catch any
	// regression that flattens fields into the msg.
	for _, field := range []string{"socket", "pid", "config", "backend", "model", "hotkey", "mode"} {
		if _, ok := started[field]; !ok {
			t.Errorf("startup log missing field %q: %+v", field, started)
		}
	}
	if got, want := started["backend"], "mock"; got != want {
		t.Errorf("backend = %v, want %v", got, want)
	}
	if got, want := started["model"], "mock"; got != want {
		t.Errorf("model = %v, want %v", got, want)
	}
	if got, want := started["hotkey"], "KEY_RIGHTCTRL"; got != want {
		t.Errorf("hotkey = %v, want %v", got, want)
	}
	if got, want := started["mode"], "hold"; got != want {
		t.Errorf("mode = %v, want %v", got, want)
	}
	if got := started["config"]; got != cfgFile {
		t.Errorf("config = %v, want %v", got, cfgFile)
	}

	// Shut the daemon down via the IPC stop command. signal-based
	// shutdown would race with the test process's own signal
	// handlers; IPC is the single-source-of-truth shutdown channel
	// for operators and for this test alike.
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		t.Fatalf("resolve sock path: %v", err)
	}
	resp, err := ipc.Send(sockPath, ipc.CmdStop, 2*time.Second)
	if err != nil {
		t.Fatalf("ipc stop: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("ipc stop not ok: %+v", resp)
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("daemon Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon Run did not exit within 3s of IPC stop")
	}

	// The matching shutdown line must be present too.
	var stopped bool
	for _, rec := range ch.records() {
		if rec["msg"] == "yap daemon stopped" {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Errorf("shutdown log line never appeared; captured:\n%s", ch.bufString())
	}
}

// TestCaptureHandler_WithAttrsGroup_RaceFree is the regression guard
// for review finding M4. Before the fix, captureHandler.WithAttrs and
// WithGroup produced derived handlers that copied the parent's
// bytes.Buffer by value (aliasing the backing slice) and initialized a
// fresh zero-value mutex. Concurrent writes from the parent and the
// child would then race on the shared slice storage under two
// different locks. Running this test under `go test -race` exercises
// both derivation paths and fires bursts of concurrent Info calls; if
// the shared-state pattern ever regresses, the race detector will
// flag it immediately.
func TestCaptureHandler_WithAttrsGroup_RaceFree(t *testing.T) {
	ch := newCaptureHandler()
	root := slog.New(ch)

	// Derive via WithAttrs (slog.With funnels through Handler.WithAttrs).
	childAttrs := root.With("child", "attrs")
	// Derive via WithGroup so the group-derivation path is exercised too.
	childGroup := slog.New(ch.WithGroup("grp")).With("child", "group")

	const writersPerBranch = 8
	const writesPerWriter = 32

	var wg sync.WaitGroup
	wg.Add(3 * writersPerBranch)
	for i := 0; i < writersPerBranch; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				root.Info("root-log", "writer", id, "seq", j)
			}
		}(i)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				childAttrs.Info("attrs-log", "writer", id, "seq", j)
			}
		}(i)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				childGroup.Info("group-log", "writer", id, "seq", j)
			}
		}(i)
	}
	wg.Wait()

	// Every write must be present in the one canonical buffer: the
	// shared-state pattern guarantees the derived handlers funnel
	// their records into the root's buffer, not their own copies.
	var rootCount, attrsCount, groupCount int
	var sawChildAttrs, sawChildGroup bool
	for _, rec := range ch.records() {
		switch rec["msg"] {
		case "root-log":
			rootCount++
		case "attrs-log":
			attrsCount++
			if rec["child"] == "attrs" {
				sawChildAttrs = true
			}
		case "group-log":
			groupCount++
			// WithGroup nests subsequent attrs under "grp", so the
			// decoded record has grp: {child: "group", ...}.
			if grp, ok := rec["grp"].(map[string]any); ok && grp["child"] == "group" {
				sawChildGroup = true
			}
		}
	}
	want := writersPerBranch * writesPerWriter
	if rootCount != want {
		t.Errorf("root-log count = %d, want %d", rootCount, want)
	}
	if attrsCount != want {
		t.Errorf("attrs-log count = %d, want %d", attrsCount, want)
	}
	if groupCount != want {
		t.Errorf("group-log count = %d, want %d", groupCount, want)
	}
	if !sawChildAttrs {
		t.Error("derived WithAttrs handler did not emit its bound attribute into the shared buffer")
	}
	if !sawChildGroup {
		t.Error("derived WithGroup handler did not emit its bound attribute into the shared buffer")
	}
}
