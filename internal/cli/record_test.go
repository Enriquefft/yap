package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/Enriquefft/yap/internal/platform"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeRecorder is a test platform.Recorder that returns immediately
// on Start (or after a configurable delay) with a fixed WAV-shaped
// payload from Encode. The mock transcribe backend (registered via
// the daemon import) drains the bytes and emits its canned chunks,
// so the record pipeline runs end-to-end without touching audio
// hardware.
type fakeRecorder struct {
	mu       sync.Mutex
	started  bool
	wait     time.Duration
	payload  []byte
	startErr error
}

func (f *fakeRecorder) Start(ctx context.Context) error {
	f.mu.Lock()
	f.started = true
	wait := f.wait
	startErr := f.startErr
	f.mu.Unlock()
	if startErr != nil {
		return startErr
	}
	if wait > 0 {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
			return nil
		}
	}
	// Block until ctx is cancelled — same contract as the real
	// platform recorder.
	<-ctx.Done()
	return nil
}

func (f *fakeRecorder) Encode() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.payload) == 0 {
		return []byte("RIFF-fake-wav-payload"), nil
	}
	return f.payload, nil
}

func (f *fakeRecorder) Close() {}

// makeRecordPlatform builds a platform.Platform suitable for record
// tests: a fake recorder, a recording injector, and otherwise nil
// fields the record command does not exercise.
func makeRecordPlatform(rec *fakeRecorder, inj inject.Injector) platform.Platform {
	return platform.Platform{
		NewRecorder: func(deviceName string) (platform.Recorder, error) {
			return rec, nil
		},
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return inj, nil
		},
	}
}

// withRecordConfig writes a config that selects the mock transcribe
// backend and disables transform / sets injection so the record
// pipeline can run without touching real backends.
func withRecordConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	runtimeDir := filepath.Join(tmp, "run")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	xdg.Reload()
	writeConfigFile(t, cfgFile, `
[general]
  max_duration = 1
  audio_feedback = false
  stream_partials = false

[transcription]
  backend = "mock"
  model = "mock"

[transform]
  enabled = false
  backend = "passthrough"

[injection]
  prefer_osc52 = true
`)
}

func TestRecord_HappyPath_TextOut(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{wait: 10 * time.Millisecond}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	// --max-duration 1 plus the fake recorder waiting 10ms means
	// the recorder returns long before the timeout. --out=text routes
	// the transcription to stdout instead of the injector.
	stdout, _, err := runCLIWithPlatform(t, p, "record", "--out=text", "--max-duration", "1")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !strings.Contains(stdout, "mock transcription") {
		t.Errorf("stdout did not contain transcribed text:\n%s", stdout)
	}
	// The text-out path bypasses the injector.
	if got := inj.lastText(); got != "" {
		t.Errorf("injector should not have been called, got %q", got)
	}
}

func TestRecord_HappyPath_Inject(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{wait: 10 * time.Millisecond}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	_, _, err := runCLIWithPlatform(t, p, "record", "--max-duration", "1")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(inj.streamed) == 0 {
		t.Fatal("injector InjectStream was not called")
	}
	var sb strings.Builder
	for _, c := range inj.streamed {
		sb.WriteString(c.Text)
	}
	if !strings.Contains(sb.String(), "mock transcription") {
		t.Errorf("injected stream missing mock transcription, got %q", sb.String())
	}
}

func TestRecord_RecorderError(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{startErr: errors.New("audio device melted")}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	_, _, err := runCLIWithPlatform(t, p, "record", "--out=text", "--max-duration", "1")
	if err == nil {
		t.Fatal("expected recorder failure to surface")
	}
	if !strings.Contains(err.Error(), "audio device melted") {
		t.Errorf("error did not surface inner failure: %v", err)
	}
}

func TestRecord_InvalidOut(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	_, _, err := runCLIWithPlatform(t, p, "record", "--out=garbage")
	if err == nil {
		t.Fatal("expected --out validation error")
	}
	if !strings.Contains(err.Error(), "invalid --out") {
		t.Errorf("error did not name --out: %v", err)
	}
}

// TestRecord_Resolve_PrintsDecision guards that --resolve runs the
// full record+transcribe pipeline and then writes the Resolve
// decision INSTEAD of injecting the text. The recording injector
// must NOT see any text: --resolve replaces delivery with a render.
func TestRecord_Resolve_PrintsDecision(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{wait: 10 * time.Millisecond}
	inj := &resolvingInjector{
		decision: inject.StrategyDecision{
			Target: inject.Target{
				DisplayServer: "wayland",
				WindowID:      "0xabcd",
				AppClass:      "foot",
				AppType:       inject.AppTerminal,
			},
			Strategy:  "osc52",
			Tool:      "osc52",
			Fallbacks: []string{"osc52", "wayland"},
			Reason:    "natural order",
		},
	}
	p := makeRecordPlatform(rec, inj)

	stdout, _, err := runCLIWithPlatform(t, p, "record", "--resolve", "--max-duration", "1")
	if err != nil {
		t.Fatalf("record --resolve: %v", err)
	}
	// The wrapper drains the stream and renders the decision. The
	// underlying Inject path (InjectStream on the recordingInjector)
	// must NOT have been invoked on the inj embedded fake — the
	// resolveInjector wrapper intercepts before delegation.
	if len(inj.streamed) != 0 {
		t.Errorf("underlying injector saw streamed chunks = %d, want 0 (resolve must intercept)", len(inj.streamed))
	}
	if got := inj.lastText(); got != "" {
		t.Errorf("underlying injector saw Inject(%q), want empty (resolve must not deliver)", got)
	}
	if inj.resolveCalls != 1 {
		t.Errorf("resolveCalls = %d, want 1", inj.resolveCalls)
	}
	// The decision must render to stdout with every field the user
	// asked for. Mirrors the paste --dry-run contract.
	for _, want := range []string{
		"target:",
		"display_server: wayland",
		"window_id:      0xabcd",
		"app_class:      foot",
		"app_type:       terminal",
		"strategy:  osc52",
		"tool:      osc52",
		"fallbacks: osc52, wayland",
		"reason:    natural order",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}
}

// TestRecord_Resolve_UnsupportedInjector guards that --resolve on a
// platform whose injector does not implement StrategyResolver
// returns a clean error BEFORE any audio is captured. Failing fast
// avoids wasting the user's microphone time on a broken debug path.
func TestRecord_Resolve_UnsupportedInjector(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{wait: 10 * time.Millisecond}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	_, _, err := runCLIWithPlatform(t, p, "record", "--resolve", "--max-duration", "1")
	if err == nil {
		t.Fatal("expected error when injector does not implement StrategyResolver")
	}
	if !strings.Contains(err.Error(), "--resolve not supported") {
		t.Errorf("error message = %q, want '--resolve not supported' substring", err.Error())
	}
	if !strings.Contains(err.Error(), "StrategyResolver") {
		t.Errorf("error message = %q, want 'StrategyResolver' substring", err.Error())
	}
	// The recorder must not have been started — we bailed out before
	// the pipeline launched.
	rec.mu.Lock()
	started := rec.started
	rec.mu.Unlock()
	if started {
		t.Error("recorder should not have started on the unsupported-injector error path")
	}
}

// TestRecord_Resolve_WinsOverOutText guards the precedence rule:
// when both --resolve and --out=text are set, --resolve wins. The
// decision is written to stdout instead of the transcription.
func TestRecord_Resolve_WinsOverOutText(t *testing.T) {
	withRecordConfig(t)
	rec := &fakeRecorder{wait: 10 * time.Millisecond}
	inj := &resolvingInjector{
		decision: inject.StrategyDecision{
			Target: inject.Target{
				DisplayServer: "wayland",
				AppClass:      "kitty",
				AppType:       inject.AppTerminal,
			},
			Strategy:  "osc52",
			Tool:      "osc52",
			Fallbacks: []string{"osc52", "wayland"},
			Reason:    "natural order",
		},
	}
	p := makeRecordPlatform(rec, inj)

	stdout, _, err := runCLIWithPlatform(t, p, "record", "--resolve", "--out=text", "--max-duration", "1")
	if err != nil {
		t.Fatalf("record --resolve --out=text: %v", err)
	}
	// --resolve wins: the decision is printed, NOT the transcribed text.
	if !strings.Contains(stdout, "strategy:  osc52") {
		t.Errorf("stdout missing decision render; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "mock transcription") {
		t.Errorf("stdout contains transcribed text even with --resolve set; got:\n%s", stdout)
	}
}

// TestRecord_SIGUSR1_CancelsRecCtx asserts that sending SIGUSR1 to
// the running record command cancels the recording context, which
// causes the fake recorder to return early. The pipeline still
// completes — the injector is called with the canned mock chunks —
// proving SIGUSR1 only cancels recCtx, not the outer ctx.
func TestRecord_SIGUSR1_CancelsRecCtx(t *testing.T) {
	withRecordConfig(t)
	// Wait long enough that the recorder would not return on its
	// own; SIGUSR1 must be the thing that causes recCtx cancellation.
	rec := &fakeRecorder{wait: 5 * time.Second}
	inj := &recordingInjector{}
	p := makeRecordPlatform(rec, inj)

	// Send SIGUSR1 to ourselves shortly after the command starts.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	}()

	start := time.Now()
	_, _, err := runCLIWithPlatform(t, p, "record", "--max-duration", "10")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("record took too long (%v) — SIGUSR1 may not have cancelled recCtx", elapsed)
	}
	if len(inj.streamed) == 0 {
		t.Fatal("injector should still have received the captured audio after SIGUSR1")
	}
}
