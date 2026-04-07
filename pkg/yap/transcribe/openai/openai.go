// Package openai implements a generic OpenAI-compatible speech-to-text
// backend. Any server that speaks the /v1/audio/transcriptions protocol
// (OpenAI itself, Groq, vLLM, llama.cpp server, litellm, Fireworks,
// ...) is a valid target. Callers must provide Config.APIURL — this
// backend does not substitute a default endpoint because there is no
// universal default for "OpenAI-compatible."
//
// Wire behavior, retry semantics, and request shape are identical to
// the Groq backend. The packages are kept separate so each can pick
// its own defaults and so future divergence (e.g. Groq-specific
// response fields) does not cross-contaminate.
//
// Importing this package for side effects registers the backend under
// the name "openai" in the transcribe registry:
//
//	import _ "github.com/hybridz/yap/pkg/yap/transcribe/openai"
package openai

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

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// APIError represents an error response from an OpenAI-compatible
// transcription endpoint.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("transcription API error %d: %s", e.StatusCode, e.Message)
}

// Backend is the OpenAI-compatible implementation of
// transcribe.Transcriber.
type Backend struct {
	cfg    transcribe.Config
	client *http.Client
}

// New builds an OpenAI-compatible backend from cfg. APIURL, APIKey,
// and Model are all required — this backend has no sensible default
// endpoint.
func New(cfg transcribe.Config) (*Backend, error) {
	if cfg.APIURL == "" {
		return nil, errors.New("openai: Config.APIURL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("openai: Config.APIKey is required")
	}
	if cfg.Model == "" {
		return nil, errors.New("openai: Config.Model is required")
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
// configured endpoint, and emits the response as a single IsFinal
// chunk on the returned channel.
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	if audio == nil {
		return nil, errors.New("openai: audio reader is nil")
	}
	out := make(chan transcribe.TranscriptChunk, 1)
	go func() {
		defer close(out)

		wavData, err := io.ReadAll(audio)
		if err != nil {
			send(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.cfg.Language,
				Err:      fmt.Errorf("openai: read audio: %w", err),
			})
			return
		}
		if len(wavData) == 0 {
			send(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.cfg.Language,
				Err:      errors.New("openai: audio data is empty"),
			})
			return
		}

		text, err := b.post(ctx, wavData)
		send(ctx, out, transcribe.TranscriptChunk{
			Text:     text,
			IsFinal:  true,
			Language: b.cfg.Language,
			Err:      err,
		})
	}()
	return out, nil
}

// send delivers a chunk to out unless ctx is cancelled first.
func send(ctx context.Context, out chan<- transcribe.TranscriptChunk, chunk transcribe.TranscriptChunk) {
	select {
	case <-ctx.Done():
	case out <- chunk:
	}
}

// post POSTs a multipart form to the configured endpoint and returns
// the decoded transcription text. Retries on 5xx and client-timeout
// errors with the same backoff as the Groq backend.
func (b *Backend) post(ctx context.Context, wavData []byte) (string, error) {
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
		if b.cfg.Prompt != "" {
			if err := writer.WriteField("prompt", b.cfg.Prompt); err != nil {
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
