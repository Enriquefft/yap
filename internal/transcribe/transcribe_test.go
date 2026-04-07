package transcribe_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/internal/transcribe"
)

// newTestOptions returns Options pointing at srv with safe defaults.
// Tests that need a different Model/Timeout mutate the returned value.
func newTestOptions(srv *httptest.Server) transcribe.Options {
	return transcribe.Options{
		APIURL:  srv.URL,
		Model:   "whisper-large-v3-turbo",
		Timeout: 30 * time.Second,
		Client:  srv.Client(),
	}
}

func TestModelParam(t *testing.T) {
	receivedModel := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		receivedModel = r.FormValue("model")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test transcription"}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("fake wav data"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if receivedModel != "whisper-large-v3-turbo" {
		t.Errorf("got model %q, want whisper-large-v3-turbo", receivedModel)
	}
}

func TestModelParam_CustomModel(t *testing.T) {
	var receivedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatal(err)
		}
		receivedModel = r.FormValue("model")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "ok"}`)
	}))
	defer srv.Close()

	opts := newTestOptions(srv)
	opts.Model = "custom-model-name"
	_, err := transcribe.Transcribe(context.Background(), opts, "test-key", []byte("wav"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if receivedModel != "custom-model-name" {
		t.Errorf("got model %q, want custom-model-name", receivedModel)
	}
}

func TestAPIURLOverride_PostedHere(t *testing.T) {
	// Proves that the package uses Options.APIURL, not a package-
	// level default. If the package were still reading a global,
	// httptest.NewServer's URL would be ignored.
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "hit"}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "key", []byte("wav"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !hit {
		t.Error("request did not reach httptest server — Options.APIURL not honored")
	}
}

func TestMultipartForm(t *testing.T) {
	var gotFilename string
	var gotLanguage string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			switch part.FormName() {
			case "file":
				gotFilename = part.FileName()
			case "language":
				lang, _ := io.ReadAll(part)
				gotLanguage = string(lang)
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("fake wav data"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotFilename != "audio.wav" {
		t.Errorf("filename: got %q, want audio.wav", gotFilename)
	}
	if gotLanguage != "en" {
		t.Errorf("language: got %q, want en", gotLanguage)
	}
}

func TestHTTPTimeout(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	// Supply our own client with a short timeout so we exercise the
	// timeout path without waiting 30 seconds.
	opts := transcribe.Options{
		APIURL: srv.URL,
		Model:  "whisper-large-v3-turbo",
		Client: &http.Client{Timeout: 50 * time.Millisecond},
	}

	_, err := transcribe.Transcribe(context.Background(), opts, "test-key", []byte("wav"), "en")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if requestCount < 1 {
		t.Errorf("expected at least 1 HTTP request, got %d", requestCount)
	}
}

func TestRetryClassification_4xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "invalid API key", "type": "authentication_error"}}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if callCount != 1 {
		t.Errorf("got %d calls, want 1 (no retry on 4xx)", callCount)
	}
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

func TestRetryClassification_5xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected 503 error, got nil")
	}
	if callCount != 4 {
		t.Errorf("got %d calls, want 4 (initial + 3 retries)", callCount)
	}
}

func TestRetryClassification_Timeout(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	opts := transcribe.Options{
		APIURL: srv.URL,
		Model:  "whisper-large-v3-turbo",
		Client: &http.Client{Timeout: 50 * time.Millisecond},
	}

	_, err := transcribe.Transcribe(context.Background(), opts, "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if callCount < 2 {
		t.Errorf("got %d calls, want >= 2 (timeout retried)", callCount)
	}
}

func TestAPIKey(t *testing.T) {
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	testKey := "gsk_test_api_key_12345"
	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), testKey, []byte("wav"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := "Bearer " + testKey
	if gotAuthHeader != want {
		t.Errorf("auth header: got %q, want %q", gotAuthHeader, want)
	}
}

func TestSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "hello world"}`)
	}))
	defer srv.Close()

	result, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "invalid file format", "type": "invalid_request_error"}}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(err.Error(), "invalid file format") {
		t.Errorf("error should contain API message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "transcription API error") {
		t.Errorf("error should be APIError shape, got: %v", err)
	}
}

func TestRetryBackoff(t *testing.T) {
	var timestamps []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected 503 error")
	}
	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}
	expected := []time.Duration{500 * time.Millisecond, 1000 * time.Millisecond, 2000 * time.Millisecond}
	for i, e := range expected {
		actual := timestamps[i+1].Sub(timestamps[i])
		lo := time.Duration(float64(e) * 0.9)
		hi := time.Duration(float64(e) * 1.1)
		if actual < lo || actual > hi {
			t.Errorf("retry %d: got %v, want %v ±10%%", i, actual, e)
		}
	}
}

func TestTranscribeEmptyWave(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit when wav is empty")
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "test-key", nil, "en")
	if err == nil {
		t.Fatal("expected error for empty WAV data")
	}
}

func TestTranscribeNoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit when API key is empty")
	}))
	defer srv.Close()

	_, err := transcribe.Transcribe(context.Background(), newTestOptions(srv), "", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestTranscribe_RequiresAPIURL(t *testing.T) {
	opts := transcribe.Options{Model: "whisper-large-v3-turbo", Timeout: time.Second}
	_, err := transcribe.Transcribe(context.Background(), opts, "key", []byte("wav"), "en")
	if err == nil || !strings.Contains(err.Error(), "APIURL is required") {
		t.Errorf("expected APIURL-required error, got %v", err)
	}
}

func TestTranscribe_RequiresModel(t *testing.T) {
	opts := transcribe.Options{APIURL: "http://example.test", Timeout: time.Second}
	_, err := transcribe.Transcribe(context.Background(), opts, "key", []byte("wav"), "en")
	if err == nil || !strings.Contains(err.Error(), "Model is required") {
		t.Errorf("expected Model-required error, got %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := transcribe.Transcribe(ctx, newTestOptions(srv), "test-key", []byte("wav"), "en")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
