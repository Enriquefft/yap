// Package groq implements the Groq speech-to-text backend. It posts
// audio to Groq's OpenAI-compatible /audio/transcriptions endpoint and
// wraps the single JSON response as a one-chunk TranscriptChunk stream
// so it satisfies the streaming transcribe.Transcriber interface.
//
// The backend retries on HTTP 5xx responses and HTTP-client timeouts
// (up to 3 attempts, with 500ms / 1s / 2s backoff). 4xx responses
// fail fast because they indicate a client bug (bad key, bad model,
// invalid file) that will not recover on retry.
//
// Importing this package for side effects registers the backend under
// the name "groq" in the transcribe registry:
//
//	import _ "github.com/Enriquefft/yap/pkg/yap/transcribe/groq"
//
// Direct construction is also supported via New for library callers
// who do not want to pay for the registry indirection.
package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// DefaultAPIURL is the Groq transcription endpoint. New substitutes
// this when cfg.APIURL is empty. It is a compile-time constant, not a
// package-level var, to keep this package globals-free.
const DefaultAPIURL = "https://api.groq.com/openai/v1/audio/transcriptions"

// APIError represents an error response from the Groq transcription
// API. It is the same shape the previous internal/transcribe package
// exposed; callers that already type-assert against it continue to
// work.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("transcription API error %d: %s", e.StatusCode, e.Message)
}

// Backend is the Groq implementation of transcribe.Transcriber.
type Backend struct {
	cfg    transcribe.Config
	client *http.Client
}

// DefaultTimeout is the per-request HTTP timeout substituted when
// cfg.Timeout is zero or negative. Without it a stalled response
// would hang the caller forever — &http.Client{Timeout: 0} disables
// the timeout entirely. 30 s is the same value the rest of yap uses
// for transcription HTTP defaults.
const DefaultTimeout = 30 * time.Second

// New builds a Groq backend from cfg. It validates required fields
// (APIKey, Model) and substitutes DefaultAPIURL when cfg.APIURL is
// empty. When cfg.HTTPClient is nil, a fresh *http.Client is built
// using cfg.Timeout (or DefaultTimeout when cfg.Timeout <= 0).
func New(cfg transcribe.Config) (*Backend, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("groq: Config.APIKey is required")
	}
	if cfg.Model == "" {
		return nil, errors.New("groq: Config.Model is required")
	}
	if cfg.APIURL == "" {
		cfg.APIURL = DefaultAPIURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Backend{cfg: cfg, client: client}, nil
}

// NewFactory adapts New into the transcribe.Factory signature for the
// registry.
func NewFactory(cfg transcribe.Config) (transcribe.Transcriber, error) {
	return New(cfg)
}

// Transcribe reads the full audio stream into memory, POSTs it to the
// Groq endpoint, and emits the response as a single IsFinal chunk on
// the returned channel. The channel is closed exactly once when the
// work is done or the context is cancelled.
//
// opts.Prompt, when non-empty, is forwarded as the `prompt` multipart
// field on the outgoing request so Whisper biases its token
// probabilities toward the supplied vocabulary.
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader, opts transcribe.Options) (<-chan transcribe.TranscriptChunk, error) {
	if audio == nil {
		return nil, errors.New("groq: audio reader is nil")
	}
	out := make(chan transcribe.TranscriptChunk, 1)
	go func() {
		defer close(out)

		wavData, err := io.ReadAll(audio)
		if err != nil {
			send(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.cfg.Language,
				Err:      fmt.Errorf("groq: read audio: %w", err),
			})
			return
		}
		if len(wavData) == 0 {
			send(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.cfg.Language,
				Err:      errors.New("groq: audio data is empty"),
			})
			return
		}

		text, err := b.post(ctx, wavData, opts.Prompt)
		send(ctx, out, transcribe.TranscriptChunk{
			Text:     text,
			IsFinal:  true,
			Language: b.cfg.Language,
			Err:      err,
		})
	}()
	return out, nil
}

// send delivers a chunk to out unless ctx is cancelled first. It is a
// small helper to keep Transcribe readable.
func send(ctx context.Context, out chan<- transcribe.TranscriptChunk, chunk transcribe.TranscriptChunk) {
	select {
	case <-ctx.Done():
	case out <- chunk:
	}
}

// sleepCtx sleeps for d or returns ctx.Err when the context is
// cancelled. Zero d returns immediately. This is the cancellation-
// aware analog of time.Sleep used in the retry backoff loop so a
// caller-cancelled ctx mid-backoff returns within microseconds rather
// than waiting for the full delay.
//
// The transform-side pkg/yap/transform/httpstream package has the
// same helper. Lifting this into a shared internal package would
// require a new Go module path; the duplication is two ten-line
// functions and is intentionally accepted.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// post is the former internal/transcribe.Transcribe body, now a method
// on Backend. Behavior is preserved exactly: multipart upload with
// model/language/response_format fields, Bearer auth, 4xx fail-fast,
// 5xx and client-timeout retry up to 3 attempts with 500ms/1s/2s
// backoff. The prompt parameter carries the per-call Whisper prompt
// (from transcribe.Options.Prompt) and is forwarded on the retry path
// so every attempt uses the same vocabulary bias.
func (b *Backend) post(ctx context.Context, wavData []byte, prompt string) (string, error) {
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

		if err := writer.WriteField("model", b.cfg.Model); err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}
		if err := writer.WriteField("language", b.cfg.Language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
		if prompt != "" {
			if err := writer.WriteField("prompt", prompt); err != nil {
				return "", fmt.Errorf("failed to write prompt field: %w", err)
			}
		}
		if err := writer.WriteField("response_format", "json"); err != nil {
			return "", fmt.Errorf("failed to write response_format field: %w", err)
		}

		if err := writer.Close(); err != nil {
			return "", fmt.Errorf("failed to close multipart writer: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.APIURL, body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)

		resp, err := b.client.Do(req)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return "", fmt.Errorf("request cancelled: %w", ctx.Err())
			}
			lastErr = err
			if attempt < maxRetries {
				if sleepErr := sleepCtx(ctx, backoffDelays[attempt]); sleepErr != nil {
					return "", sleepErr
				}
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
			if attempt < maxRetries {
				if sleepErr := sleepCtx(ctx, backoffDelays[attempt]); sleepErr != nil {
					return "", sleepErr
				}
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
