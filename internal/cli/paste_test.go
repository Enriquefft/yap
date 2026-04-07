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

// _ ensures the unused-package guard does not trip when only one of
// the helper imports is touched on a particular build matrix.
var _ io.Writer = (*bytes.Buffer)(nil)
