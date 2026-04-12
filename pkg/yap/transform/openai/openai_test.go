package openai_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/openai"
)

// inputChunks feeds fixed chunks into a closed channel.
func inputChunks(chunks ...transcribe.TranscriptChunk) <-chan transcribe.TranscriptChunk {
	ch := make(chan transcribe.TranscriptChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func drain(out <-chan transcribe.TranscriptChunk) []transcribe.TranscriptChunk {
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	return got
}

// writeSSE writes a single "data: <json>\n\n" frame to w and flushes
// if the writer supports it.
func writeSSE(w http.ResponseWriter, payload string) {
	fmt.Fprintf(w, "data: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestNew_EmptyAPIURL_Rejected(t *testing.T) {
	_, err := openai.New(transform.Config{Model: "gpt-4o-mini"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "APIURL") {
		t.Errorf("err = %v, want to mention APIURL", err)
	}
}

func TestNew_EmptyModel_Rejected(t *testing.T) {
	_, err := openai.New(transform.Config{APIURL: "http://example.invalid/v1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Model") {
		t.Errorf("err = %v, want to mention Model", err)
	}
}

func TestTransform_StreamsSSEChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(w, `{"choices":[{"delta":{"content":"Hel"}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"lo"}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}`)
		writeSSE(w, `[DONE]`)
	}))
	defer srv.Close()

	b, err := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "helo", Language: "en"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 4 {
		t.Fatalf("chunks = %d, want 4: %+v", len(got), got)
	}
	if got[0].Text != "Hel" || got[1].Text != "lo" || got[2].Text != "!" {
		t.Errorf("wrong text: %+v", got)
	}
	if !got[3].IsFinal || got[3].Text != "" {
		t.Errorf("final = %+v, want empty IsFinal", got[3])
	}
}

func TestTransform_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		writeSSE(w, `[DONE]`)
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m", APIKey: "sk-test"})
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	drain(out)
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
}

func TestTransform_SSEMalformed_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeSSE(w, "this-is-not-json")
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	got := drain(out)
	if len(got) != 1 || got[0].Err == nil {
		t.Fatalf("got = %+v, want one error chunk", got)
	}
	if !strings.Contains(got[0].Err.Error(), "decode") {
		t.Errorf("err = %v, want to mention decode", got[0].Err)
	}
}

func TestTransform_EmptyChoices_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeSSE(w, `{"choices":[]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"Hi"}}]}`)
		writeSSE(w, `[DONE]`)
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	got := drain(out)
	if len(got) != 2 {
		t.Fatalf("chunks = %d, want 2 (empty choices skipped): %+v", len(got), got)
	}
	if got[0].Text != "Hi" {
		t.Errorf("got[0].Text = %q, want Hi", got[0].Text)
	}
	if !got[1].IsFinal {
		t.Errorf("got[1] = %+v, want IsFinal", got[1])
	}
}

func TestTransform_EmptyContentDelta_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeSSE(w, `{"choices":[{"delta":{}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"Ok"}}]}`)
		writeSSE(w, `[DONE]`)
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "hi"},
	), transform.Options{})
	got := drain(out)
	if len(got) != 2 || got[0].Text != "Ok" {
		t.Errorf("got = %+v, want [Ok, final]", got)
	}
}

func TestTransform_EmptyInput_NoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called on empty input")
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	out, _ := b.Transform(context.Background(), inputChunks(), transform.Options{})
	got := drain(out)
	if len(got) != 1 || !got[0].IsFinal {
		t.Errorf("got = %+v, want single IsFinal chunk", got)
	}
}

func TestTransform_UpstreamError_Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called on upstream error")
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	sentinel := errors.New("transcribe boom")
	out, _ := b.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Err: sentinel, IsFinal: true},
	), transform.Options{})
	got := drain(out)
	if len(got) != 1 || !errors.Is(got[0].Err, sentinel) {
		t.Errorf("got = %+v, want propagated upstream error", got)
	}
}

func TestTransform_CtxCancelMidStream_ClosesCleanly(t *testing.T) {
	blocker := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeSSE(w, `{"choices":[{"delta":{"content":"Hel"}}]}`)
		<-blocker
	}))
	defer srv.Close()
	defer close(blocker)

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	ctx, cancel := context.WithCancel(context.Background())
	out, err := b.Transform(ctx, inputChunks(transcribe.TranscriptChunk{Text: "hi"}), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	first := <-out
	if first.Text != "Hel" {
		t.Errorf("first = %q, want Hel", first.Text)
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
		t.Fatal("output not closed after cancel")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m", APIKey: "sk-test"})
	if err := b.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	b, _ := openai.New(transform.Config{APIURL: srv.URL + "/v1", Model: "m"})
	err := b.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want to mention 401", err)
	}
}

func TestNewFactory_RegistersRoundTrip(t *testing.T) {
	tr, err := openai.NewFactory(transform.Config{APIURL: "http://x/v1", Model: "m"})
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	if tr == nil {
		t.Fatal("nil transformer")
	}
}
