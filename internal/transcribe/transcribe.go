// Package transcribe provides the current (Phase 2) Groq transcription
// client with a constructor-injection API. Phase 3 moves this into
// pkg/yap/transcribe/groq/ with an interface-driven surface; the
// package remains internal until then.
//
// Phase 2 removed every package-level mutable variable from this
// package: no global apiURL, no global clientTimeout, no global
// notifyFn. Every knob is an explicit Options field injected by the
// caller.
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Options configures a single Transcribe call. Zero values are never
// safe — callers must populate APIURL and Model explicitly. Client
// may be nil, in which case a fresh *http.Client with Timeout is
// constructed per call.
type Options struct {
	// APIURL is the full endpoint to POST the multipart form to.
	APIURL string
	// Model is the transcription model name forwarded in the
	// "model" form field (e.g. "whisper-large-v3-turbo").
	Model string
	// Timeout is the per-request HTTP timeout. Ignored if Client is
	// non-nil (the caller's client owns its own timeouts).
	Timeout time.Duration
	// Client is an optional HTTP client. Pass httptest.NewServer's
	// client in tests; leave nil in production.
	Client *http.Client
}

// APIError represents a remote transcription API error response.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("transcription API error %d: %s", e.StatusCode, e.Message)
}

// Transcribe sends wavData to a Whisper-compatible endpoint and
// returns the transcribed text. Retries on 5xx and HTTP-client
// timeout (up to 3 attempts, backoff 500ms / 1s / 2s). 4xx responses
// fail fast.
//
// opts carries the endpoint URL, model name, and optional HTTP
// client. apiKey is the Authorization Bearer value.
func Transcribe(ctx context.Context, opts Options, apiKey string, wavData []byte, language string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("API key cannot be empty")
	}
	if len(wavData) == 0 {
		return "", fmt.Errorf("WAV data cannot be empty")
	}
	if opts.APIURL == "" {
		return "", fmt.Errorf("transcribe: Options.APIURL is required")
	}
	if opts.Model == "" {
		return "", fmt.Errorf("transcribe: Options.Model is required")
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}

	const maxRetries = 3
	backoffDelays := [maxRetries]time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
	}

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", "audio.wav")
		if err != nil {
			return "", fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err := part.Write(wavData); err != nil {
			return "", fmt.Errorf("failed to write wav data: %w", err)
		}

		if err := writer.WriteField("model", opts.Model); err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}
		if err := writer.WriteField("language", language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
		if err := writer.WriteField("response_format", "json"); err != nil {
			return "", fmt.Errorf("failed to write response_format field: %w", err)
		}

		if err := writer.Close(); err != nil {
			return "", fmt.Errorf("failed to close multipart writer: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.APIURL, body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() == context.Canceled {
				return "", fmt.Errorf("request cancelled: %w", ctx.Err())
			}
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(backoffDelays[attempt])
				continue
			}
			return "", fmt.Errorf("request failed after %d retries: %w", attempt, lastErr)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			var apiErrResp struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(respBody, &apiErrResp); err != nil {
				return "", &APIError{
					StatusCode: resp.StatusCode,
					Message:    string(respBody),
				}
			}
			apiErr := &APIError{
				StatusCode: resp.StatusCode,
				Message:    apiErrResp.Error.Message,
				Type:       apiErrResp.Error.Type,
			}
			if resp.StatusCode/100 == 4 {
				return "", apiErr
			}
			lastErr = apiErr
			if attempt < maxRetries {
				time.Sleep(backoffDelays[attempt])
				continue
			}
			return "", apiErr
		}

		var successResp struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(respBody, &successResp); err != nil {
			return "", fmt.Errorf("failed to parse success response: %w", err)
		}
		return successResp.Text, nil
	}

	return "", fmt.Errorf("unexpected: no successful transcription after %d attempts", maxRetries+1)
}
