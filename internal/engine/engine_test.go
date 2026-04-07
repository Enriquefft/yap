package engine_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/hybridz/yap/internal/engine"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/mock"
	"github.com/hybridz/yap/pkg/yap/transform/passthrough"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock platform implementations ---

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
	playCount int
}

func (m *mockChime) Play(r io.Reader) {
	m.playCount++
}

type mockPaster struct {
	pasted []string
	err    error
}

func (m *mockPaster) Paste(text string) error {
	if m.err != nil {
		return m.err
	}
	m.pasted = append(m.pasted, text)
	return nil
}

type mockNotifier struct {
	notifications []string
}

func (m *mockNotifier) Notify(title, message string) {
	m.notifications = append(m.notifications, title+": "+message)
}

// errorTranscriber implements transcribe.Transcriber by emitting one
// chunk with Err set. Used to exercise the engine's error-handling
// path without making real API calls.
type errorTranscriber struct{ err error }

func (e errorTranscriber) Transcribe(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	_, _ = io.Copy(io.Discard, audio)
	ch := make(chan transcribe.TranscriptChunk, 1)
	ch <- transcribe.TranscriptChunk{IsFinal: true, Err: e.err}
	close(ch)
	return ch, nil
}

// --- Helpers ---

func silentChime() engine.ChimeSource {
	return func() (io.Reader, error) { return bytes.NewReader(nil), nil }
}

func makeEngine(rec *mockRecorder, chime *mockChime, paster *mockPaster, notifier *mockNotifier, transcriber transcribe.Transcriber) *engine.Engine {
	return engine.New(rec, chime, paster, notifier, transcriber, passthrough.New())
}

func runWithAutoCancel(e *engine.Engine, delay time.Duration) {
	recCtx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(delay); cancel() }()
	e.RecordAndPaste(context.Background(), recCtx, 60, silentChime(), silentChime(), silentChime())
}

// --- Tests ---

func TestRecordAndPaste_HappyPath(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(transcribe.TranscriptChunk{Text: "hello world", IsFinal: true})

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 10*time.Millisecond)

	require.True(t, rec.started)
	require.Equal(t, []string{"hello world"}, paster.pasted)
	assert.Empty(t, notifier.notifications)
}

func TestRecordAndPaste_HappyPathMultipleChunks(t *testing.T) {
	// Multi-chunk transcribers concatenate into a single paste.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(
		transcribe.TranscriptChunk{Text: "hello "},
		transcribe.TranscriptChunk{Text: "world", IsFinal: true},
	)

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 10*time.Millisecond)

	require.Equal(t, []string{"hello world"}, paster.pasted)
	assert.Empty(t, notifier.notifications)
}

func TestRecordAndPaste_AudioDeviceError(t *testing.T) {
	rec := &mockRecorder{startErr: errors.New("device unavailable")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New()

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 5*time.Millisecond)

	require.Len(t, notifier.notifications, 1)
	assert.Contains(t, notifier.notifications[0], "audio device error")
	assert.Empty(t, paster.pasted)
}

func TestRecordAndPaste_EncodeError(t *testing.T) {
	rec := &mockRecorder{encodeErr: errors.New("encode failed")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New()

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 5*time.Millisecond)

	require.Len(t, notifier.notifications, 1)
	assert.Contains(t, notifier.notifications[0], "audio encode error")
	assert.Empty(t, paster.pasted)
}

func TestRecordAndPaste_TranscriptionError(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := errorTranscriber{err: errors.New("api error")}

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 5*time.Millisecond)

	require.Len(t, notifier.notifications, 1)
	assert.Contains(t, notifier.notifications[0], "transcription failed")
	assert.Empty(t, paster.pasted)
}

func TestRecordAndPaste_ContextCancelledIsNotError(t *testing.T) {
	// context.Canceled from recorder.Start() should not trigger a
	// device error notification. This is the normal case: user
	// releases the hotkey, recCtx is cancelled.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(transcribe.TranscriptChunk{Text: "hello", IsFinal: true})

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 5*time.Millisecond)

	for _, n := range notifier.notifications {
		assert.NotContains(t, n, "audio device error", "context.Canceled should not be reported as device error")
	}
}

func TestRecordAndPaste_NilChimeSources(t *testing.T) {
	// Engine should not panic when chime sources are nil.
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(transcribe.TranscriptChunk{Text: "ok", IsFinal: true})

	e := makeEngine(rec, chime, paster, notifier, transcriber)

	recCtx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	e.RecordAndPaste(context.Background(), recCtx, 60, nil, nil, nil)

	assert.Empty(t, notifier.notifications)
}

func TestRecordAndPaste_ChimesArePlayed(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(transcribe.TranscriptChunk{Text: "ok", IsFinal: true})

	e := makeEngine(rec, chime, paster, notifier, transcriber)
	runWithAutoCancel(e, 5*time.Millisecond)

	// start + stop chimes = 2 plays (warning won't fire in 5ms with
	// 60s timeout)
	assert.Equal(t, 2, chime.playCount)
}

func TestEngine_NilTransformerDefaultsToPassthrough(t *testing.T) {
	rec := &mockRecorder{wavData: []byte("fake-wav")}
	chime := &mockChime{}
	paster := &mockPaster{}
	notifier := &mockNotifier{}
	transcriber := mock.New(transcribe.TranscriptChunk{Text: "defaulted", IsFinal: true})

	e := engine.New(rec, chime, paster, notifier, transcriber, nil)
	recCtx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	e.RecordAndPaste(context.Background(), recCtx, 60, silentChime(), silentChime(), silentChime())

	require.Equal(t, []string{"defaulted"}, paster.pasted)
}
