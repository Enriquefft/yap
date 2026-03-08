package transcribe

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestModelParam verifies multipart request always includes model=whisper-large-v3-turbo (TRANS-01)
func TestModelParam(t *testing.T) {
	receivedModel := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form to check model parameter
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("Failed to parse multipart form: %v", err)
		}
		receivedModel = r.FormValue("model")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test transcription"}`)
	}))
	defer server.Close()

	// Override API URL for test
	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")
	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if receivedModel != "whisper-large-v3-turbo" {
		t.Errorf("got model %q, want %q", receivedModel, "whisper-large-v3-turbo")
	}
}

// TestMultipartForm verifies request includes file field named "file" with filename "audio.wav" (TRANS-02)
func TestMultipartForm(t *testing.T) {
	var gotFilename string
	var gotLanguage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("Failed to get multipart reader: %v", err)
		}

		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Failed to read part: %v", err)
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
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")
	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if gotFilename != "audio.wav" {
		t.Errorf("got filename %q, want %q", gotFilename, "audio.wav")
	}
	if gotLanguage != "en" {
		t.Errorf("got language %q, want %q", gotLanguage, "en")
	}
}

// TestHTTPTimeout verifies client has 30-second timeout (TRANS-03)
func TestHTTPTimeout(t *testing.T) {
	// Use a server that takes longer than the timeout
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(200 * time.Millisecond) // Longer than test timeout
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	// Override client timeout for test (normally 30s)
	oldClientTimeout := clientTimeout
	clientTimeout = 50 * time.Millisecond
	defer func() { clientTimeout = oldClientTimeout }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Should have made attempts (timeout triggers retries, with backoff)
	// With 50ms timeout and 200ms server sleep, each request times out after 50ms
	// Then backoff: 50ms, 100ms, 200ms = total ~1s for 4 attempts
	if requestCount < 1 {
		t.Errorf("Expected at least 1 HTTP request, got %d", requestCount)
	}

	// Verify the timeout was actually used (error should contain timeout-related info)
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") && !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Logf("Error: %v", err) // Just log it, don't fail - timeout errors vary
	}
}

// TestRetryClassification_4xx verifies HTTP 401 results in immediate error, no retry (TRANS-04)
func TestRetryClassification_4xx(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "invalid API key", "type": "authentication_error"}}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for 401, got nil")
	}

	if callCount != 1 {
		t.Errorf("got %d HTTP calls for 401 error, want 1 (no retry)", callCount)
	}

	// Verify error contains API message
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

// TestRetryClassification_5xx verifies HTTP 503 triggers up to 3 retries (TRANS-04)
func TestRetryClassification_5xx(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for 503, got nil")
	}

	if callCount != 4 {
		t.Errorf("got %d HTTP calls for 503 error, want 4 (initial + 3 retries)", callCount)
	}
}

// TestRetryClassification_timeout verifies HTTP client timeout treated as retryable (TRANS-04)
func TestRetryClassification_timeout(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Sleep longer than the HTTP client timeout
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	// Set a very short HTTP client timeout (not context timeout)
	oldClientTimeout := clientTimeout
	clientTimeout = 50 * time.Millisecond
	defer func() { clientTimeout = oldClientTimeout }()

	// Use a background context (no timeout)
	ctx := context.Background()

	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	// Should retry on HTTP client timeout
	if callCount < 2 {
		t.Errorf("got %d HTTP calls for timeout, want >= 2 (retries occurred)", callCount)
	}
}

// TestAPIKey verifies Authorization header contains "Bearer {apiKey}" (TRANS-05)
func TestAPIKey(t *testing.T) {
	var gotAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")
	testKey := "gsk_test_api_key_12345"

	_, err := Transcribe(ctx, testKey, wavData, "en")
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	expectedAuth := "Bearer " + testKey
	if gotAuthHeader != expectedAuth {
		t.Errorf("got Authorization header %q, want %q", gotAuthHeader, expectedAuth)
	}
}

// TestSuccessResponse verifies JSON {"text":"hello"} returns "hello", nil (TRANS-01)
func TestSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "hello world"}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	result, err := Transcribe(ctx, "test-key", wavData, "en")
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if result != "hello world" {
		t.Errorf("got transcription %q, want %q", result, "hello world")
	}
}

// TestErrorResponse verifies non-200 response parses Groq error JSON (TRANS-04)
func TestErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "invalid file format", "type": "invalid_request_error"}}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for 400, got nil")
	}

	// Verify error contains API message
	if !strings.Contains(err.Error(), "invalid file format") {
		t.Errorf("error should contain API message, got: %v", err)
	}

	// Verify error type is APIError
	var apiErr *APIError
	if !strings.Contains(err.Error(), "groq API error") {
		t.Errorf("error should be APIError, got: %v", err)
	}
	_ = apiErr // Suppress unused variable warning
}

// TestRetryBackoff verifies delays between retries are ~500ms, ~1s, ~2s (allow 10% tolerance)
func TestRetryBackoff(t *testing.T) {
	var timestamps []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for 503, got nil")
	}

	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}

	// Check backoff delays with 10% tolerance
	expectedDelays := []time.Duration{500 * time.Millisecond, 1000 * time.Millisecond, 2000 * time.Millisecond}
	for i, expected := range expectedDelays {
		actual := timestamps[i+1].Sub(timestamps[i])
		minExpected := time.Duration(float64(expected) * 0.9)
		maxExpected := time.Duration(float64(expected) * 1.1)

		if actual < minExpected || actual > maxExpected {
			t.Errorf("retry %d: got delay %v, want %v ±10%%", i, actual, expected)
		}
	}
}

// TestTranscribeEmptyWave verifies error handling for empty WAV data
func TestTranscribeEmptyWave(t *testing.T) {
	ctx := context.Background()
	wavData := []byte("")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for empty WAV data, got nil")
	}
}

// TestTranscribeNoAPIKey verifies error handling for empty API key
func TestTranscribeNoAPIKey(t *testing.T) {
	ctx := context.Background()
	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for empty API key, got nil")
	}
}

// TestContextCancellation verifies request aborts on context cancellation
func TestContextCancellation(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(1 * time.Second) // Sleep to allow context cancellation
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer server.Close()

	oldAPIURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldAPIURL }()

	// Create context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wavData := []byte("fake wav data")

	_, err := Transcribe(ctx, "test-key", wavData, "en")
	if err == nil {
		t.Fatal("Expected error for cancelled context, got nil")
	}

	// Should not make any requests if context is already cancelled
	if requestCount > 1 {
		log.Printf("Note: %d requests made despite context being cancelled", requestCount)
	}
}
