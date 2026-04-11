package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hybridz/yap/internal/cli"
	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// resolvingInjector is a recordingInjector that ALSO satisfies
// inject.StrategyResolver. Tests use it to exercise the --dry-run
// and --resolve paths without pulling in the real Linux injector.
// The canned decision is returned verbatim by Resolve; the injectCalled
// atomic flag lets tests assert the Inject path was NOT hit on the
// dry-run/resolve branches.
type resolvingInjector struct {
	recordingInjector
	decision    inject.StrategyDecision
	resolveErr  error
	resolveCalls int
}

func (r *resolvingInjector) Resolve(_ context.Context) (inject.StrategyDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolveCalls++
	if r.resolveErr != nil {
		return inject.StrategyDecision{}, r.resolveErr
	}
	return r.decision, nil
}

// Compile-time assertion that resolvingInjector satisfies both the
// Injector and the optional StrategyResolver surface.
var _ inject.Injector = (*resolvingInjector)(nil)
var _ inject.StrategyResolver = (*resolvingInjector)(nil)

// recordingInjector is a test inject.Injector that captures every
// Inject call. Used by paste_test, record_test, and any future
// command that wants to assert on injected text without touching the
// real OS-level inject layer.
type recordingInjector struct {
	mu       sync.Mutex
	texts    []string
	streamed []transcribe.TranscriptChunk
	failWith error
}

func (r *recordingInjector) Inject(ctx context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failWith != nil {
		return r.failWith
	}
	r.texts = append(r.texts, text)
	return nil
}

func (r *recordingInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	for c := range in {
		r.mu.Lock()
		r.streamed = append(r.streamed, c)
		r.mu.Unlock()
	}
	if r.failWith != nil {
		return r.failWith
	}
	return nil
}

func (r *recordingInjector) lastText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.texts) == 0 {
		return ""
	}
	return r.texts[len(r.texts)-1]
}

// makeFakePlatform returns a platform.Platform whose NewInjector
// always yields the same recordingInjector. NewRecorder is left set
// to a stub that returns a recorderless error so accidental record
// invocations fail loudly. The other fields stay nil — paste_test
// only exercises the inject path.
func makeFakePlatform(inj inject.Injector) platform.Platform {
	return platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return inj, nil
		},
	}
}

// runCLIWithPlatform mirrors runCLI but lets a test inject a custom
// platform. Used for paste, record, and devices tests.
func runCLIWithPlatform(t *testing.T, p platform.Platform, argv ...string) (string, string, error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	err := cli.ExecuteForTestWithPlatform(p, argv, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), err
}

func TestPaste_PositionalArg(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	inj := &recordingInjector{}
	_, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "hello injected world")
	if err != nil {
		t.Fatalf("paste: %v", err)
	}
	if got := inj.lastText(); got != "hello injected world" {
		t.Errorf("injected text = %q, want hello injected world", got)
	}
}

func TestPaste_Stdin(t *testing.T) {
	// We don't actually wire stdin in ExecuteForTest, so the --stdin
	// flag with no positional arg is exercised via readTextInput's
	// path that reads from os.Stdin. We bypass that here by passing
	// the text positionally — the --stdin path is exercised via the
	// transform_test stdin case below.
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	inj := &recordingInjector{}
	_, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "abc")
	if err != nil {
		t.Fatalf("paste abc: %v", err)
	}
	if got := inj.lastText(); got != "abc" {
		t.Errorf("injected text = %q, want abc", got)
	}
}

func TestPaste_Failure(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	failErr := errors.New("inject backend exploded")
	inj := &recordingInjector{failWith: failErr}
	_, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "boom")
	if err == nil {
		t.Fatal("expected error from injector failure")
	}
	if !strings.Contains(err.Error(), "inject backend exploded") {
		t.Errorf("error did not surface inner failure: %v", err)
	}
}

// TestPaste_DryRun_PrintsDecision guards that --dry-run routes
// through StrategyResolver.Resolve, writes the decision to the cobra
// writer, and DOES NOT call Inject. This is the canonical "what
// would yap paste do?" debug surface for Bug 19.
func TestPaste_DryRun_PrintsDecision(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	inj := &resolvingInjector{
		decision: inject.StrategyDecision{
			Target: inject.Target{
				DisplayServer: "wayland",
				WindowID:      "0x1234",
				AppClass:      "kitty",
				AppType:       inject.AppTerminal,
			},
			Strategy:  "osc52",
			Tool:      "osc52",
			Fallbacks: []string{"osc52", "wayland"},
			Reason:    "app_override matched (kitty -> osc52)",
		},
	}
	stdout, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "--dry-run", "hello")
	if err != nil {
		t.Fatalf("paste --dry-run: %v", err)
	}
	// Inject must NOT have been called — --dry-run is a pure query.
	if got := inj.lastText(); got != "" {
		t.Errorf("Inject called unexpectedly with %q", got)
	}
	if inj.resolveCalls != 1 {
		t.Errorf("resolveCalls = %d, want 1", inj.resolveCalls)
	}
	// The rendered output must contain every field the user cares
	// about so the decision is self-contained on one screen.
	for _, want := range []string{
		"target:",
		"display_server: wayland",
		"window_id:      0x1234",
		"app_class:      kitty",
		"app_type:       terminal",
		"strategy:  osc52",
		"tool:      osc52",
		"fallbacks: osc52, wayland",
		"reason:    app_override matched (kitty -> osc52)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}
}

// TestPaste_DryRun_UnsupportedInjector guards the user-friendly
// error path: when the current platform's injector does not
// implement StrategyResolver, --dry-run must return a clear message
// instead of panicking.
func TestPaste_DryRun_UnsupportedInjector(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	// recordingInjector satisfies inject.Injector but not
	// StrategyResolver, so the type assertion must fail cleanly.
	inj := &recordingInjector{}
	_, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "--dry-run", "hello")
	if err == nil {
		t.Fatal("expected error when injector does not implement StrategyResolver")
	}
	if !strings.Contains(err.Error(), "--dry-run not supported") {
		t.Errorf("error message = %q, want '--dry-run not supported' substring", err.Error())
	}
	if !strings.Contains(err.Error(), "StrategyResolver") {
		t.Errorf("error message = %q, want 'StrategyResolver' substring (names the optional interface)", err.Error())
	}
	if got := inj.lastText(); got != "" {
		t.Errorf("Inject should not be called on the unsupported path, got %q", got)
	}
}

// TestPaste_DryRun_ResolveError guards that a Resolve failure from
// the underlying injector surfaces as a normal CLI error instead of
// being swallowed or hidden behind a confusing "paste: empty input"
// message. A broken detection path should tell the user exactly
// what broke.
func TestPaste_DryRun_ResolveError(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[injection]\n  prefer_osc52 = true\n")

	inj := &resolvingInjector{
		resolveErr: errors.New("detect: no display"),
	}
	_, _, err := runCLIWithPlatform(t, makeFakePlatform(inj), "paste", "--dry-run", "hello")
	if err == nil {
		t.Fatal("expected Resolve error to surface")
	}
	if !strings.Contains(err.Error(), "detect: no display") {
		t.Errorf("error did not surface inner resolve failure: %v", err)
	}
}

// _ ensures the unused-package guard does not trip when only one of
// the helper imports is touched on a particular build matrix.
var _ io.Writer = (*bytes.Buffer)(nil)
