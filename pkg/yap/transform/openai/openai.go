package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/httpstream"
)

// DefaultSystemPrompt mirrors the on-disk transform default so
// library consumers that construct a Backend directly get the same
// behaviour as daemon-driven callers.
const DefaultSystemPrompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."

// httpTimeout is the per-request HTTP timeout. It must cover the
// full streamed response.
const httpTimeout = 120 * time.Second

// healthCheckTimeout caps the startup /models probe.
const healthCheckTimeout = 3 * time.Second

// Backend is the OpenAI-compatible SSE transform implementation.
type Backend struct {
	cfg    transform.Config
	client *httpstream.Client
}

// New constructs a Backend. APIURL and Model are both required —
// unlike the local backend, there is no sensible default URL here
// because the correct value depends on which upstream server the
// user is targeting.
func New(cfg transform.Config) (*Backend, error) {
	if cfg.APIURL == "" {
		return nil, errors.New("openai: Config.APIURL is required (point at the /v1 collection root)")
	}
	if cfg.Model == "" {
		return nil, errors.New("openai: Config.Model is required")
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	return &Backend{
		cfg:    cfg,
		client: httpstream.NewClient(httpTimeout),
	}, nil
}

// NewFactory adapts New into the transform.Factory signature so it
// can be registered in the backend registry.
func NewFactory(cfg transform.Config) (transform.Transformer, error) {
	return New(cfg)
}

// chatRequest is the OpenAI /chat/completions request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatMessage is a single message in a chat turn.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// sseFrame is the on-wire JSON shape of a single SSE "data:" frame.
// Only the fields the backend needs are unmarshalled; unknown fields
// are ignored per the openapi spec.
type sseFrame struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

// Transform drains the input channel, concatenates the chunk text
// into a single user prompt, POSTs the request, parses the SSE
// stream, and emits one transcribe.TranscriptChunk per delta. The
// final chunk has IsFinal=true. See the package doc for the wire
// format.
func (b *Backend) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	var sb strings.Builder
	var lang string
	for chunk := range in {
		if chunk.Err != nil {
			out := make(chan transcribe.TranscriptChunk, 1)
			out <- chunk
			close(out)
			return out, nil
		}
		sb.WriteString(chunk.Text)
		if chunk.Language != "" {
			lang = chunk.Language
		}
	}
	if err := ctx.Err(); err != nil {
		out := make(chan transcribe.TranscriptChunk)
		close(out)
		return out, err
	}

	prompt := sb.String()
	if strings.TrimSpace(prompt) == "" {
		out := make(chan transcribe.TranscriptChunk, 1)
		out <- transcribe.TranscriptChunk{IsFinal: true, Language: lang}
		close(out)
		return out, nil
	}

	system := b.cfg.SystemPrompt
	if system == "" {
		system = DefaultSystemPrompt
	}

	req := chatRequest{
		Model: b.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}

	body, err := b.client.PostJSON(ctx, b.cfg.APIURL+"/chat/completions", b.cfg.APIKey, req)
	if err != nil {
		out := make(chan transcribe.TranscriptChunk, 1)
		out <- transcribe.TranscriptChunk{
			IsFinal:  true,
			Language: lang,
			Err:      fmt.Errorf("openai: %w", err),
		}
		close(out)
		return out, nil
	}

	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		done := false
		for scanner.Scan() {
			line := scanner.Text()
			// SSE frames are terminated by a blank line; everything
			// other than "data: ..." is metadata the backend
			// ignores (event:, id:, retry:, comments).
			if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimPrefix(data, "data:")
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				sendFinal(ctx, out, transcribe.TranscriptChunk{IsFinal: true, Language: lang})
				done = true
				break
			}
			var frame sseFrame
			if err := json.Unmarshal([]byte(data), &frame); err != nil {
				sendFinal(ctx, out, transcribe.TranscriptChunk{
					IsFinal:  true,
					Language: lang,
					Err:      fmt.Errorf("openai: decode: %w", err),
				})
				return
			}
			if len(frame.Choices) == 0 {
				continue
			}
			content := frame.Choices[0].Delta.Content
			if content == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- transcribe.TranscriptChunk{Text: content, Language: lang}:
			}
		}
		if err := scanner.Err(); err != nil {
			sendFinal(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: lang,
				Err:      fmt.Errorf("openai: stream: %w", err),
			})
			return
		}
		if !done {
			// Stream ended without [DONE]; emit a synthetic final
			// marker so the consumer still sees a complete stream.
			sendFinal(ctx, out, transcribe.TranscriptChunk{IsFinal: true, Language: lang})
		}
	}()
	return out, nil
}

// HealthCheck probes GET {api_url}/models and returns nil on any 2xx
// response. Most OpenAI-compatible servers implement this endpoint
// (real OpenAI, llama.cpp-server, vLLM, Ollama /v1, Together.ai…).
func (b *Backend) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.cfg.APIURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("openai: health: %w", err)
	}
	if b.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)
	}
	resp, err := b.client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("openai: health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("openai: health: http %d", resp.StatusCode)
	}
	return nil
}

// sendFinal delivers chunk to out unless ctx is cancelled first.
func sendFinal(ctx context.Context, out chan<- transcribe.TranscriptChunk, chunk transcribe.TranscriptChunk) {
	select {
	case <-ctx.Done():
	case out <- chunk:
	}
}
