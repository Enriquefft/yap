package engine_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Enriquefft/yap/internal/engine"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transcribe/mock"
	"github.com/Enriquefft/yap/pkg/yap/transform/passthrough"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock platform implementations ---

// mockRecorder satisfies platform.Recorder. Start blocks on the
// recording context until it is cancelled (mirroring the real
// recorder's hotkey-driven lifecycle), then returns ctx.Err(). Encode
// emits the pre-staged WAV blob.
type mockRecorder struct {
	startErr  error
	wavData   []byte
	encodeErr error
	started   bool
}

func (m *mockRecorder) Start(ctx context.Context) error {
	m.started = true
	if m.startErr != nil {
		return m.startErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockRecorder) Encode() ([]byte, error) {
	return m.wavData, m.encodeErr
}

func (m *mockRecorder) Close() {}

type mockChime struct {
	mu        sync.Mutex
	playCount int
}

func (m *mockChime) Play(io.Reader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playCount++
}

func (m *mockChime) Plays() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playCount
}

// recordingInjector is the engine-test fake for pkg/yap/inject.Injector.
// It records every chunk it sees on InjectStream so tests can assert
// the streaming pipeline actually piped chunks through (not collected
// at the boundary). The injectErr field forces a delivery failure
// after the first chunk arrives so the engine's error-propagation
// path is exercised.
type recordingInjector struct {
	mu         sync.Mutex
	wg         sync.WaitGroup
	received   []string
	chunks     int
	lastFinal  bool
	injectErr  error
	streamErr  error
	streamDone bool
}

// Inject is required by the inject.Injector interface but is not the
// path the engine takes — Run drives InjectStream exclusively. We
// keep a working implementation so an accidental call still records
// something testable.
func (r *recordingInjector) Inject(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.injectErr != nil {
		return r.injectErr
	}
	r.received = append(r.received, text)
	r.chunks = 1
	r.lastFinal = true
	return nil
}

// InjectStream drains in fully (or until ctx cancels), recording every
// chunk it sees. The wg counter increments before the consumer loop
// starts and decrements when it finishes — Run is supposed to block
// until this returns, so wg.Wait() in a test is a goroutine-leak
// guard for the inject stage.
func (r *recordingInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	r.wg.Add(1)
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.streamDone = true
			err := r.streamErr
			if err == nil {
				err = ctx.Err()
			}
			r.mu.Unlock()
			return err
		case chunk, ok := <-in:
			if !ok {
				r.mu.Lock()
				r.streamDone = true
				err := r.streamErr
				r.mu.Unlock()
				return err
			}
			if chunk.Err != nil {
				r.mu.Lock()
				r.streamDone = true
				r.mu.Unlock()
				return chunk.Err
			}
			r.mu.Lock()
			r.received = append(r.received, chunk.Text)
			r.chunks++
			if chunk.IsFinal {
				r.lastFinal = true
			}
			r.mu.Unlock()
		}
	}
}

func (r *recordingInjector) snapshot() (received []string, chunks int, lastFinal bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.received))
	copy(out, r.received)
	return out, r.chunks, r.lastFinal
}

// Compile-time guard that the test fake satisfies the public
// interface — guards against future signature drift.
var _ yinject.Injector = (*recordingInjector)(nil)

// errorTranscriber emits one chunk with Err set to exercise the
// engine's transcription-error propagation path without making real
// API calls.
type errorTranscriber struct{ err error }

func (e errorTranscriber) Transcribe(_ context.Context, audio io.Reader, _ transcribe.Options) (<-chan transcribe.TranscriptChunk, error) {
	if audio != nil {
		_, _ = io.Copy(io.Discard, audio)
	}
	ch := make(chan transcribe.TranscriptChunk, 1)
	ch <- transcribe.TranscriptChunk{IsFinal: true, Err: e.err}
	close(ch)
	return ch, nil
}

// blockingInjector blocks on InjectStream until ctx is cancelled. It
// is used by the cancellation test to prove that an outer ctx cancel
// tears the pipeline down without leaking goroutines.
type blockingInjector struct {
	wg sync.WaitGroup
}

func (b *blockingInjector) Inject(_ context.Context, _ string) error { return nil }

func (b *blockingInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	b.wg.Add(1)
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-in:
			if !ok {
				return nil
			}
		}
	}
}

// stuckTranscriber keeps its output channel open until ctx is
// cancelled, never emitting a chunk. It is the canonical test fixture
// for "pipeline is alive but nothing has arrived" cancellation
// scenarios — without it a mock transcriber would close its channel
// almost immediately and the cancellation path would never get a
// chance to fire.
type stuckTranscriber struct{}

func (stuckTranscriber) Transcribe(ctx context.Context, audio io.Reader, _ transcribe.Options) (<-chan transcribe.TranscriptChunk, error) {
	if audio != nil {
		_, _ = io.Copy(io.Discard, audio)
	}
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}

// --- Helpers ---

// silentChime returns a chime source that yields an empty reader.
func silentChime() engine.ChimeSource {
	return func() (io.Reader, error) { return strings.NewReader(""), nil }
}

// newEngine builds a default engine for tests, returning the
// constructed pointer (failing the test on construction error).
// Notifier is intentionally absent — the engine no longer routes
// failures through a notifier; the daemon does that at its own
// layer based on the wrapped error returned from Engine.Run.
func newEngine(t *testing.T, rec *mockRecorder, chime *mockChime, transcriber transcribe.Transcriber, injector yinject.Injector) *engine.Engine {
	t.Helper()
	eng, err := engine.New(rec, chime, transcriber, passthrough.New(), injector, nil)
	require.NoError(t, err)
	return eng
}

// preCancelledRecCtx returns a recording context that is already
// cancelled, so the recorder.Start loop returns immediately and the
// pipeline proceeds straight to transcription.
func preCancelledRecCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// goroutineLeakGuard snapshots the live goroutine count and returns a
// callback that asserts it has returned to the original value (modulo
// a small slack to absorb stdlib housekeeping). It is the engine's
// canonical "no goroutine leaks" assertion: every goroutine the engine
// spawned must have returned by the time Run returns.
func goroutineLeakGuard(t *testing.T) func() {
	t.Helper()
	runtime.GC()
	before := runtime.NumGoroutine()
	return func() {
		t.Helper()
		// Give scheduled goroutines a chance to wind down.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			runtime.Gosched()
			if runtime.NumGoroutine() <= before+1 {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		after := runtime.NumGoroutine()
		if after > before+1 {
			t.Fatalf("goroutine leak: before=%d after=%d", before, after)
		}
	}
}

// --- Tests ---

func TestEngineRun_StreamingMultiChunk(t *testing.T) {
	// 3 chunks in, 3 chunks delivered to the injector — proves the
	// engine pipes the channel through end to end with
	// stream_partials=true.
	backend := mock.New(
		transcribe.TranscriptChunk{Text: "hello ", Offset: 100 * time.Millisecond},
		transcribe.TranscriptChunk{Text: "world", Offset: 300 * time.Millisecond},
		transcribe.TranscriptChunk{Text: "!", IsFinal: true, Offset: 500 * time.Millisecond},
	)
	rec := &mockRecorder{wavData: []byte("fake-wav-data")}
	chime := &mockChime{}
	injector := &recordingInjector{}

	eng := newEngine(t, rec, chime, backend, injector)

	guard := goroutineLeakGuard(t)
	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
		StartChime:     silentChime(),
		StopChime:      silentChime(),
	})
	require.NoError(t, err)
	guard()

	received, chunks, lastFinal := injector.snapshot()
	require.Equal(t, []string{"hello ", "world", "!"}, received)
	require.Equal(t, 3, chunks)
	require.True(t, lastFinal)
}

func TestEngineRun_StreamPartialsFalse_BatchesToSingleChunk(t *testing.T) {
	// 2 chunks in, 1 IsFinal chunk out — proves the batchChunks
	// helper still routes through the channel-based pipeline (not a
	// short-circuit) and the injector sees exactly one delivery.
	backend := mock.New(
		transcribe.TranscriptChunk{Text: "hello "},
		transcribe.TranscriptChunk{Text: "world", IsFinal: true, Language: "en"},
	)
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}

	eng := newEngine(t, rec, chime, backend, injector)

	guard := goroutineLeakGuard(t)
	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: false,
	})
	require.NoError(t, err)
	guard()

	received, chunks, lastFinal := injector.snapshot()
	require.Equal(t, []string{"hello world"}, received)
	require.Equal(t, 1, chunks)
	require.True(t, lastFinal)
}

func TestEngineRun_TranscribeError_PropagatesCleanly(t *testing.T) {
	// A transcribe error chunk surfaces as a Run error. The injector
	// receives the error chunk via InjectStream and returns it; the
	// engine wraps it as inject:... — the test checks that the
	// underlying error is preserved via errors.Is.
	apiErr := errors.New("api boom")
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}

	eng := newEngine(t, rec, chime, errorTranscriber{err: apiErr}, injector)

	guard := goroutineLeakGuard(t)
	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, apiErr)
	guard()

	received, _, _ := injector.snapshot()
	assert.Empty(t, received)
}

func TestEngineRun_CancelDrainsCleanly(t *testing.T) {
	// Cancelling the outer ctx must tear the pipeline down without
	// leaking goroutines. The transcriber holds its output channel
	// open until ctx cancels so the injector is genuinely blocked
	// waiting for chunks when we pull the plug.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &blockingInjector{}

	eng, err := engine.New(rec, chime, stuckTranscriber{}, passthrough.New(), injector, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	guard := goroutineLeakGuard(t)
	err = eng.Run(ctx, engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)

	// Wait for the blocking injector goroutine itself to wind down.
	injector.wg.Wait()
	guard()
}

func TestEngineRun_CancelDrainsCleanly_BatchMode(t *testing.T) {
	// The stream_partials=false path also has to survive cancellation
	// — batchChunks is a goroutine the engine owns and the test
	// makes sure it winds down via the shared pipeCtx.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &blockingInjector{}

	eng, err := engine.New(rec, chime, stuckTranscriber{}, passthrough.New(), injector, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	guard := goroutineLeakGuard(t)
	err = eng.Run(ctx, engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: false,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)
	injector.wg.Wait()
	guard()
}

func TestEngineRun_InjectorError_Propagates(t *testing.T) {
	// When the injector's InjectStream returns a non-cancel error,
	// Run must surface it wrapped with the inject: prefix and the
	// underlying error must be retrievable via errors.Is.
	backend := mock.New(transcribe.TranscriptChunk{Text: "hi", IsFinal: true})
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injectErr := errors.New("no strategy applicable")
	injector := &recordingInjector{streamErr: injectErr}

	eng := newEngine(t, rec, chime, backend, injector)

	guard := goroutineLeakGuard(t)
	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, injectErr)
	require.Contains(t, err.Error(), "inject:")
	guard()
}

func TestEngineRun_RequiresRecordCtx(t *testing.T) {
	// nil RecordCtx is a programmer error — Run must fail loudly
	// before touching the recorder.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New()

	eng := newEngine(t, rec, chime, backend, injector)

	err := eng.Run(context.Background(), engine.RunOptions{StreamPartials: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "RecordCtx")
	require.False(t, rec.started, "recorder must not start when RecordCtx is nil")
}

func TestEngineRun_NilTransformer_Rejected(t *testing.T) {
	// engine.New must reject every required-nil dependency. The
	// caller is responsible for explicitly supplying passthrough when
	// the user disables the transform stage.
	rec := &mockRecorder{}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New()

	_, err := engine.New(rec, chime, backend, nil, injector, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Transformer")
}

func TestEngineRun_NilRecorder_Rejected(t *testing.T) {
	chime := &mockChime{}
	_, err := engine.New(nil, chime, mock.New(), passthrough.New(), &recordingInjector{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Recorder")
}

func TestEngineRun_NilTranscriber_Rejected(t *testing.T) {
	rec := &mockRecorder{}
	_, err := engine.New(rec, &mockChime{}, nil, passthrough.New(), &recordingInjector{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Transcriber")
}

func TestEngineRun_NilInjector_Rejected(t *testing.T) {
	rec := &mockRecorder{}
	_, err := engine.New(rec, &mockChime{}, mock.New(), passthrough.New(), nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Injector")
}

func TestEngineRun_AudioDeviceError_IsWrapped(t *testing.T) {
	// A real audio-device error from Recorder.Start surfaces as a
	// wrapped record: error so the daemon's notifier path can
	// distinguish it from cancellation.
	deviceErr := errors.New("device unavailable")
	rec := &mockRecorder{startErr: deviceErr}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New()

	eng := newEngine(t, rec, chime, backend, injector)

	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, deviceErr)
	require.Contains(t, err.Error(), "record:")
}

func TestEngineRun_EncodeError_IsWrapped(t *testing.T) {
	encodeErr := errors.New("encode failed")
	rec := &mockRecorder{encodeErr: encodeErr}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New()

	eng := newEngine(t, rec, chime, backend, injector)

	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, encodeErr)
	require.Contains(t, err.Error(), "encode:")
}

func TestEngineRun_NilChimeSourcesAreSafe(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New(transcribe.TranscriptChunk{Text: "ok", IsFinal: true})

	eng := newEngine(t, rec, chime, backend, injector)

	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StreamPartials: false,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, chime.Plays(), "no chimes should fire when sources are nil")
}

func TestEngineRun_StartAndStopChimesPlay(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New(transcribe.TranscriptChunk{Text: "ok", IsFinal: true})

	eng := newEngine(t, rec, chime, backend, injector)

	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:      preCancelledRecCtx(),
		StartChime:     silentChime(),
		StopChime:      silentChime(),
		WarningChime:   silentChime(),
		TimeoutSec:     60,
		StreamPartials: false,
	})
	require.NoError(t, err)
	// Start + stop fire synchronously; warning is scheduled 50s out
	// (TimeoutSec - 10) and the test returns long before then.
	assert.Equal(t, 2, chime.Plays())
}

func TestEngineRun_OnRecordingStopCallback(t *testing.T) {
	// OnRecordingStop fires exactly once, after recording ends and
	// before the stop chime plays.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	injector := &recordingInjector{}
	backend := mock.New(transcribe.TranscriptChunk{Text: "ok", IsFinal: true})

	eng := newEngine(t, rec, chime, backend, injector)

	var callbackState string
	var callbackChimeCount int

	err := eng.Run(context.Background(), engine.RunOptions{
		RecordCtx:  preCancelledRecCtx(),
		StartChime: silentChime(),
		StopChime: func() (io.Reader, error) {
			callbackChimeCount = chime.Plays()
			return strings.NewReader(""), nil
		},
		StreamPartials: false,
		OnRecordingStop: func() {
			callbackState = "called"
			// The callback fires AFTER recording stops but BEFORE the
			// stop chime — so at this point only the start chime has
			// played.
			callbackChimeCount = chime.Plays()
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "called", callbackState, "OnRecordingStop should be called")
	assert.Equal(t, 1, callbackChimeCount, "callback should fire after start chime but before stop chime")
}
