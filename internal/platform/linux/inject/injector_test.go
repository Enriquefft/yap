package inject

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// captureHandler buffers slog records as JSON for assertion. We use a
// JSONHandler against a *bytes.Buffer rather than a custom record
// shape so the test asserts the actual on-the-wire output users see.
func newCaptureHandler() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

// recordedLogLine is the deserialised structure of one captured slog
// JSON line. Test assertions read these fields directly.
type recordedLogLine struct {
	Time              time.Time `json:"time"`
	Level             string    `json:"level"`
	Msg               string    `json:"msg"`
	TargetDisplay     string    `json:"target.display_server"`
	TargetAppClass    string    `json:"target.app_class"`
	TargetAppType     string    `json:"target.app_type"`
	TargetTmux        bool      `json:"target.tmux"`
	TargetSSHRemote   bool      `json:"target.ssh_remote"`
	Strategy          string    `json:"strategy"`
	Outcome           string    `json:"outcome"`
	Bytes             int       `json:"bytes"`
	DurationMS        int64     `json:"duration_ms"`
	Attempts          int       `json:"attempts"`
	Error             string    `json:"error"`
	Reason            string    `json:"reason"`
}

func parseLogLines(t *testing.T, buf *bytes.Buffer) []recordedLogLine {
	t.Helper()
	out := []recordedLogLine{}
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var rec recordedLogLine
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("parse log line: %v: %s", err, string(line))
		}
		out = append(out, rec)
	}
	return out
}

// fixedClock returns a deterministic Deps.Now function that increments
// 1ms on every call so duration_ms is non-zero in tests.
func fixedClock() func() time.Time {
	t := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(1 * time.Millisecond)
		return t
	}
}

// TestInjectEmitsStructuredAuditLog is the canonical audit-trail
// guard. It captures the slog output of a successful Inject and
// verifies the structured fields the rest of the codebase relies on.
func TestInjectEmitsStructuredAuditLog(t *testing.T) {
	logger, buf := newCaptureHandler()
	deliveredAt := []string{}
	osc := &recordingStrategy{name: "osc52", supportsFn: supportsType(yinject.AppTerminal), deliveredAt: &deliveredAt}
	wayland := &recordingStrategy{name: "wayland", supportsFn: func(t yinject.Target) bool {
		return t.DisplayServer == "wayland"
	}, deliveredAt: &deliveredAt}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
		}),
		Sleep: func(time.Duration) {},
		Now:   fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{PreferOSC52: true}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Pre-load a deterministic Target by overriding the strategies list
	// with the recording stubs and forcing a wayland-generic detect.
	inj.strategies = []Strategy{osc, wayland}

	if err := inj.Inject(context.Background(), "hello"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	lines := parseLogLines(t, buf)
	if len(lines) == 0 {
		t.Fatal("expected at least one log line")
	}
	final := lines[len(lines)-1]
	if final.Msg != "inject" {
		t.Errorf("final msg = %q, want inject", final.Msg)
	}
	if final.Outcome != "success" {
		t.Errorf("outcome = %q, want success", final.Outcome)
	}
	if final.Strategy != "wayland" {
		// On a generic Wayland target with no SWAYSOCK / Hyprland env,
		// detection produces AppGeneric so OSC52 is not in the natural
		// order — wayland wins.
		t.Errorf("strategy = %q, want wayland", final.Strategy)
	}
	if final.TargetDisplay != "wayland" {
		t.Errorf("display = %q, want wayland", final.TargetDisplay)
	}
	if final.TargetAppType != "generic" {
		t.Errorf("app_type = %q, want generic", final.TargetAppType)
	}
	if final.Bytes != len("hello") {
		t.Errorf("bytes = %d, want %d", final.Bytes, len("hello"))
	}
	if final.DurationMS <= 0 {
		t.Errorf("duration_ms = %d, want > 0", final.DurationMS)
	}
}

func TestInjectFailsAggregateAfterAllStrategiesFail(t *testing.T) {
	logger, buf := newCaptureHandler()
	failing := &recordingStrategy{
		name:       "wayland",
		supportsFn: func(yinject.Target) bool { return true },
		deliverErr: errors.New("nope"),
	}
	deps := Deps{
		EnvGet: envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inj.strategies = []Strategy{failing}
	err = inj.Inject(context.Background(), "x")
	if err == nil {
		t.Fatal("expected aggregate failure")
	}
	lines := parseLogLines(t, buf)
	// Expect one warn (attempt failed) + one error (final).
	sawAttemptFail := false
	sawFinalError := false
	for _, l := range lines {
		if l.Msg == "inject attempt failed" && l.Strategy == "wayland" {
			sawAttemptFail = true
		}
		if l.Msg == "inject" && l.Outcome == "failed" {
			sawFinalError = true
		}
	}
	if !sawAttemptFail {
		t.Error("missing attempt-failed log line")
	}
	if !sawFinalError {
		t.Error("missing final failure log line")
	}
}

func TestInjectFallsThroughOnUnsupportedSentinel(t *testing.T) {
	logger, _ := newCaptureHandler()
	deliveredAt := []string{}
	first := &recordingStrategy{
		name:       "first",
		supportsFn: func(yinject.Target) bool { return true },
		deliverErr: yinject.ErrStrategyUnsupported,
		deliveredAt: &deliveredAt,
	}
	second := &recordingStrategy{
		name:       "second",
		supportsFn: func(yinject.Target) bool { return true },
		deliveredAt: &deliveredAt,
	}
	deps := Deps{
		EnvGet: envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inj.strategies = []Strategy{first, second}
	if err := inj.Inject(context.Background(), "x"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if len(deliveredAt) != 2 || deliveredAt[0] != "first" || deliveredAt[1] != "second" {
		t.Errorf("delivery order = %v, want [first second]", deliveredAt)
	}
}

func TestInjectStreamBuffersUntilClose(t *testing.T) {
	logger, _ := newCaptureHandler()
	delivered := ""
	delivering := &recordingStrategy{
		name:       "wayland",
		supportsFn: func(yinject.Target) bool { return true },
		deliverErr: nil,
	}
	delivering.supportsFn = func(yinject.Target) bool { return true }
	deps := Deps{
		EnvGet: envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Replace strategies with a stub that captures the text it sees.
	captureStrat := &captureStrategy{}
	inj.strategies = []Strategy{captureStrat}

	in := make(chan transcribe.TranscriptChunk, 3)
	in <- transcribe.TranscriptChunk{Text: "hel"}
	in <- transcribe.TranscriptChunk{Text: "lo "}
	in <- transcribe.TranscriptChunk{Text: "world", IsFinal: true}
	close(in)

	if err := inj.InjectStream(context.Background(), in); err != nil {
		t.Fatalf("InjectStream: %v", err)
	}
	if captureStrat.last != "hello world" {
		t.Errorf("delivered = %q, want %q", captureStrat.last, "hello world")
	}
	_ = delivered
}

func TestInjectStreamPropagatesChunkError(t *testing.T) {
	logger, _ := newCaptureHandler()
	deps := Deps{
		EnvGet: func(string) string { return "" },
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	in := make(chan transcribe.TranscriptChunk, 1)
	want := errors.New("transcribe broke")
	in <- transcribe.TranscriptChunk{Err: want}
	close(in)
	if err := inj.InjectStream(context.Background(), in); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestInjectNoApplicableStrategiesLogsFailure(t *testing.T) {
	logger, buf := newCaptureHandler()
	declining := &recordingStrategy{
		name:       "decline",
		supportsFn: func(yinject.Target) bool { return false },
	}
	deps := Deps{
		EnvGet: envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inj.strategies = []Strategy{declining}
	if err := inj.Inject(context.Background(), "x"); err == nil {
		t.Fatal("expected error when no strategy supports the target")
	}
	lines := parseLogLines(t, buf)
	hasFailure := false
	for _, l := range lines {
		if l.Outcome == "failed" && strings.Contains(l.Reason, "no applicable") {
			hasFailure = true
		}
	}
	if !hasFailure {
		t.Errorf("expected failure log with reason, got %+v", lines)
	}
}

func TestInjectWithNilLoggerDoesNotPanic(t *testing.T) {
	deps := Deps{
		EnvGet: envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
		Sleep:  func(time.Duration) {},
		Now:    fixedClock(),
	}
	inj, err := New(platform.InjectionOptions{}, deps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inj.strategies = []Strategy{&recordingStrategy{name: "x", supportsFn: func(yinject.Target) bool { return true }}}
	if err := inj.Inject(context.Background(), "x"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
}

// captureStrategy records the last text it received via Deliver. It
// is used by InjectStream tests to assert the buffered payload.
type captureStrategy struct {
	last string
}

func (c *captureStrategy) Name() string                     { return "capture" }
func (c *captureStrategy) Supports(yinject.Target) bool     { return true }
func (c *captureStrategy) Deliver(_ context.Context, _ yinject.Target, text string) error {
	c.last = text
	return nil
}

// Compile-time guards on test types.
var _ Strategy = (*captureStrategy)(nil)
var _ Strategy = (*recordingStrategy)(nil)
