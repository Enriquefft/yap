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

// apiURL is the Groq API endpoint for audio transcription.
// Made package-level variable for testability.
var apiURL = "https://api.groq.com/openai/v1/audio/transcriptions"

// clientTimeout is the HTTP client timeout.
// Made package-level variable for testability.
var clientTimeout = 30 * time.Second

// APIError represents a Groq API error response.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("groq API error %d: %s", e.StatusCode, e.Message)
}

// Transcribe sends wavData to Groq Whisper API and returns the transcribed text.
//
// Retries on 5xx and timeout (up to 3 times, backoff: 500ms/1s/2s).
// Fails immediately on 4xx (TRANS-04).
// API key from apiKey param (caller reads from Config.APIKey which already applies GROQ_API_KEY override).
func Transcribe(ctx context.Context, apiKey string, wavData []byte, language string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("API key cannot be empty")
	}

	if len(wavData) == 0 {
		return "", fmt.Errorf("WAV data cannot be empty")
	}

	// HTTP client with timeout per TRANS-03
	client := &http.Client{
		Timeout: clientTimeout,
	}

	// Retry configuration
	maxRetries := 3
	backoffDelays := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Build multipart request body
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add file field (TRANS-02)
		part, err := writer.CreateFormFile("file", "audio.wav")
		if err != nil {
			return "", fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err := part.Write(wavData); err != nil {
			return "", fmt.Errorf("failed to write wav data: %w", err)
		}

		// Add model field (TRANS-01)
		if err := writer.WriteField("model", "whisper-large-v3-turbo"); err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}

		// Add language field (TRANS-02)
		if err := writer.WriteField("language", language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}

		// Add response_format
		if err := writer.WriteField("response_format", "json"); err != nil {
			return "", fmt.Errorf("failed to write response_format field: %w", err)
		}

		if err := writer.Close(); err != nil {
			return "", fmt.Errorf("failed to close multipart writer: %w", err)
		}

		// Create request with context (allows cancellation)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+apiKey) // TRANS-05

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			// Check if it's an explicit context cancellation (not timeout)
			if ctx.Err() == context.Canceled {
				// Explicitly cancelled by caller - not retryable
				return "", fmt.Errorf("request cancelled: %w", ctx.Err())
			}

			// Timeout or other errors are retryable (TRANS-04)
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(backoffDelays[attempt])
				continue
			}
			return "", fmt.Errorf("request failed after %d retries: %w", attempt, lastErr)
		}
		defer resp.Body.Close()

		// Read response body
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		// Check HTTP status code
		if resp.StatusCode != http.StatusOK {
			// Parse error response
			var apiErrResp struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(respBody, &apiErrResp); err != nil {
				return "", &APIError{
					StatusCode: resp.StatusCode,
					Message:     string(respBody),
				}
			}

			apiErr := &APIError{
				StatusCode: resp.StatusCode,
				Message:    apiErrResp.Error.Message,
				Type:       apiErrResp.Error.Type,
			}

			// TRANS-04: 4xx errors are not retryable
			if resp.StatusCode/100 == 4 {
				return "", apiErr
			}

			// 5xx errors are retryable
			lastErr = apiErr
			if attempt < maxRetries {
				time.Sleep(backoffDelays[attempt])
				continue
			}
			return "", apiErr
		}

		// Parse success response
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
