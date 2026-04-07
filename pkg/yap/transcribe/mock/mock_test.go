package mock_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/mock"
)

func TestNewDefaultChunks(t *testing.T) {
	b := mock.New()
	if len(b.Chunks) != 1 {
		t.Fatalf("default chunks: got %d, want 1", len(b.Chunks))
	}
	if !b.Chunks[0].IsFinal {
		t.Errorf("default chunk IsFinal=false, want true")
	}
	if b.Chunks[0].Text == "" {
		t.Error("default chunk text is empty")
	}
}

func TestTranscribeEmitsChunksInOrder(t *testing.T) {
	chunks := []transcribe.TranscriptChunk{
		{Text: "hello "},
		{Text: "world", IsFinal: true},
	}
	b := mock.New(chunks...)

	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("any audio")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var got []transcribe.TranscriptChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != len(chunks) {
		t.Fatalf("got %d chunks, want %d", len(got), len(chunks))
	}
	if got[0].Text != "hello " || got[1].Text != "world" {
		t.Errorf("chunk text order wrong: %+v", got)
	}
	if !got[1].IsFinal {
		t.Errorf("final chunk IsFinal=false")
	}
}

func TestTranscribeClosesChannel(t *testing.T) {
	b := mock.New()
	ch, err := b.Transcribe(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	for range ch {
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after drain")
		}
	default:
		t.Error("channel receive should not block after drain")
	}
}

func TestTranscribeRespectsCancel(t *testing.T) {
	chunks := make([]transcribe.TranscriptChunk, 0, 8)
	for i := 0; i < 8; i++ {
		chunks = append(chunks, transcribe.TranscriptChunk{Text: "x"})
	}
	chunks[len(chunks)-1].IsFinal = true
	b := mock.New(chunks...)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := b.Transcribe(ctx, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// The goroutine should return on ctx.Done() and close the
	// channel. We may see a subset of chunks but the range must
	// terminate.
	for range ch {
	}
}

func TestTranscribeNilAudio(t *testing.T) {
	b := mock.New()
	ch, err := b.Transcribe(context.Background(), nil)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	for range ch {
	}
}

func TestRegistryHasMock(t *testing.T) {
	factory, err := transcribe.Get("mock")
	if err != nil {
		t.Fatalf("transcribe.Get(mock): %v", err)
	}
	tr, err := factory(transcribe.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if tr == nil {
		t.Fatal("factory returned nil")
	}
}
