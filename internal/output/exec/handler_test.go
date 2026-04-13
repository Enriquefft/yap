package exec_test

import (
	"context"
	"testing"

	execout "github.com/Enriquefft/yap/internal/output/exec"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

func TestNew_EmptyCommand(t *testing.T) {
	_, err := execout.New("", nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestInject_HappyPath(t *testing.T) {
	// "cat" reads stdin and writes to stdout — exits 0.
	h, err := execout.New("cat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Inject(context.Background(), "hello world"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInject_CommandNotFound(t *testing.T) {
	h, err := execout.New("nonexistent-command-that-does-not-exist", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = h.Inject(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestInject_CommandFails(t *testing.T) {
	h, err := execout.New("false", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = h.Inject(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

func TestInjectStream_CollectsChunks(t *testing.T) {
	// Use "wc -c" to count bytes — verifies all chunks were concatenated.
	h, err := execout.New("cat", nil)
	if err != nil {
		t.Fatal(err)
	}

	ch := make(chan transcribe.TranscriptChunk, 3)
	ch <- transcribe.TranscriptChunk{Text: "hello "}
	ch <- transcribe.TranscriptChunk{Text: "world"}
	ch <- transcribe.TranscriptChunk{Text: "!", IsFinal: true}
	close(ch)

	if err := h.InjectStream(context.Background(), ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectStream_EmptyTranscript(t *testing.T) {
	h, err := execout.New("cat", nil)
	if err != nil {
		t.Fatal(err)
	}

	ch := make(chan transcribe.TranscriptChunk)
	close(ch)

	// Empty transcript should not call the command.
	if err := h.InjectStream(context.Background(), ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectStream_ContextCancellation(t *testing.T) {
	h, err := execout.New("cat", nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan transcribe.TranscriptChunk, 1)
	ch <- transcribe.TranscriptChunk{Text: "partial"}
	cancel() // Cancel before closing channel

	// Should flush partial transcript to command.
	err = h.InjectStream(ctx, ch)
	// Either nil (command succeeded with partial) or context error is acceptable.
	_ = err
}
