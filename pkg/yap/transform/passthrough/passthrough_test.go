package passthrough_test

import (
	"context"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/passthrough"
)

func TestForwardsChunksUnchanged(t *testing.T) {
	in := make(chan transcribe.TranscriptChunk, 3)
	in <- transcribe.TranscriptChunk{Text: "a"}
	in <- transcribe.TranscriptChunk{Text: "b"}
	in <- transcribe.TranscriptChunk{Text: "c", IsFinal: true}
	close(in)

	out, err := passthrough.New().Transform(context.Background(), in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chunks, want 3", len(got))
	}
	want := []string{"a", "b", "c"}
	for i, c := range got {
		if c.Text != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, c.Text, want[i])
		}
	}
	if !got[2].IsFinal {
		t.Errorf("final chunk IsFinal=false")
	}
}

func TestClosesOnInputClose(t *testing.T) {
	in := make(chan transcribe.TranscriptChunk)
	close(in)

	out, err := passthrough.New().Transform(context.Background(), in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	for range out {
		t.Error("unexpected chunk on closed input")
	}
}

func TestRespectsContextCancel(t *testing.T) {
	in := make(chan transcribe.TranscriptChunk)
	ctx, cancel := context.WithCancel(context.Background())

	out, err := passthrough.New().Transform(ctx, in)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	cancel()

	select {
	case _, ok := <-out:
		if ok {
			t.Error("out channel should be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("out channel did not close within 1s of cancel")
	}
}

func TestRegistryHasPassthrough(t *testing.T) {
	factory, err := transform.Get("passthrough")
	if err != nil {
		t.Fatalf("transform.Get(passthrough): %v", err)
	}
	tr, err := factory(transform.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if tr == nil {
		t.Fatal("factory returned nil")
	}
}
