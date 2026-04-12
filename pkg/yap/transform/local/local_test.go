package local_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/local"
)

// inputChunks feeds fixed chunks into a closed channel so Transform's
// drain loop terminates.
func inputChunks(chunks ...transcribe.TranscriptChunk) <-chan transcribe.TranscriptChunk {
	ch := make(chan transcribe.TranscriptChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// drain consumes every chunk from out and returns them as a slice.
func drain(out <-chan transcribe.TranscriptChunk) []transcribe.TranscriptChunk {
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	return got
}

func TestNew_DefaultURL(t *testing.T) {
	b, err := local.New(transform.Config{Model: "llama3"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b == nil {
		t.Fatal("backend is nil")
	}
}

func TestNew_EmptyModel_Rejected(t *testing.T) {
	_, err := local.New(transform.Config{APIURL: "http://example.invalid"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Model") {
		t.Errorf("error = %v, want to mention Model", err)
	}
}

func TestTransform_StreamsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hello"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":", world"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"!"},"done":true}`)
	}))
	defer srv.Close()

	b, err := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hello world", Language: "en"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 3 {
		t.Fatalf("chunks = %d, want 3: %+v", len(got), got)
	}
	if got[0].Text != "Hello" || got[1].Text != ", world" || got[2].Text != "!" {
		t.Errorf("wrong text: %+v", got)
	}
	if !got[2].IsFinal {
		t.Errorf("last chunk not final: %+v", got[2])
	}
	if got[2].Language != "en" {
		t.Errorf("language = %q, want en", got[2].Language)
	}
}

func TestTransform_EmptyInput_EmitsFinalOnly(t *testing.T) {
	// Server should never be called.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server unexpectedly called with empty input")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "   ", Language: "en"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || !got[0].IsFinal || got[0].Text != "" {
		t.Errorf("got = %+v, want single empty IsFinal chunk", got)
	}
}

func TestTransform_UpstreamError_Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called on upstream error")
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	upstreamErr := errors.New("transcribe failed")
	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Err: upstreamErr, IsFinal: true},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || got[0].Err == nil || !errors.Is(got[0].Err, upstreamErr) {
		t.Errorf("got = %+v, want single chunk with upstream error", got)
	}
}

func TestTransform_ServerError_SurfacesAsChunkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || got[0].Err == nil || !got[0].IsFinal {
		t.Errorf("got = %+v, want single IsFinal chunk with Err", got)
	}
	if !strings.Contains(got[0].Err.Error(), "bad model") {
		t.Errorf("err = %v, want to contain %q", got[0].Err, "bad model")
	}
}

func TestTransform_MalformedJSON_SurfacesAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "this-is-not-json\n")
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || got[0].Err == nil {
		t.Fatalf("got = %+v, want one error chunk", got)
	}
	if !strings.Contains(got[0].Err.Error(), "decode") {
		t.Errorf("err = %v, want to contain %q", got[0].Err, "decode")
	}
}

func TestTransform_ServerReportedError_SurfacesAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"error":"model not loaded"}`)
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	got := drain(out)
	if len(got) != 1 || got[0].Err == nil {
		t.Fatalf("got = %+v, want error chunk", got)
	}
	if !strings.Contains(got[0].Err.Error(), "model not loaded") {
		t.Errorf("err = %v, want to contain %q", got[0].Err, "model not loaded")
	}
}

func TestTransform_CtxCancelMidStream_ClosesCleanly(t *testing.T) {
	// Server writes one chunk then blocks. Cancelling ctx must close
	// the output channel without sending more chunks.
	blocker := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"Hello"},"done":false}`)
		if flusher != nil {
			flusher.Flush()
		}
		<-blocker
	}))
	defer srv.Close()
	defer close(blocker)

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})

	ctx, cancel := context.WithCancel(context.Background())
	out, err := b.Transform(ctx, inputChunks(transcribe.TranscriptChunk{Text: "hi"}), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	first := <-out
	if first.Text != "Hello" {
		t.Errorf("first chunk = %q, want Hello", first.Text)
	}
	cancel()

	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output channel not closed after ctx cancel")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("health path = %q, want /", r.URL.Path)
		}
		_, _ = io.WriteString(w, "Ollama is running")
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	if err := b.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b, _ := local.New(transform.Config{APIURL: srv.URL, Model: "llama3"})
	err := b.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want to mention 500", err)
	}
}

func TestNewFactory_RegistersRoundTrip(t *testing.T) {
	tr, err := local.NewFactory(transform.Config{Model: "x"})
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	if tr == nil {
		t.Fatal("nil transformer")
	}
}
