package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/httpstream"
)

// DefaultAPIURL is the Ollama default endpoint. New substitutes this
// when cfg.APIURL is empty. It is a compile-time constant to keep the
// package globals-free.
const DefaultAPIURL = "http://localhost:11434"

// DefaultSystemPrompt is the fallback system prompt used when
// cfg.SystemPrompt is empty. Mirrors the on-disk default owned by
// pkg/yap/config; the string lives in both places intentionally so
// library consumers that skip the config package still get a sensible
// default.
const DefaultSystemPrompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."

// httpTimeout is the per-request HTTP timeout. It has to cover the
// full streamed response, so it is deliberately generous.
const httpTimeout = 120 * time.Second

// healthCheckTimeout is the upper bound for the startup health
// probe. It is much tighter than httpTimeout because the health
// check is a single synchronous GET with no streamed body.
const healthCheckTimeout = 3 * time.Second

// Backend is the Ollama-native transform implementation.
type Backend struct {
	cfg    transform.Config
	client *httpstream.Client
}

// New constructs a Backend. An empty APIURL is substituted with
// DefaultAPIURL; Model is required and rejected when empty.
func New(cfg transform.Config) (*Backend, error) {
	if cfg.Model == "" {
		return nil, errors.New("local: Config.Model is required")
	}
	if cfg.APIURL == "" {
		cfg.APIURL = DefaultAPIURL
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	return &Backend{
		cfg:    cfg,
		client: httpstream.NewClient(httpTimeout, "yap-local/"+config.Version),
	}, nil
}

// NewFactory adapts New into the transform.Factory signature so it
// can be registered in the backend registry.
func NewFactory(cfg transform.Config) (transform.Transformer, error) {
	return New(cfg)
}

// chatRequest is the /api/chat request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatMessage is a single message in an Ollama chat turn.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatStreamChunk is one NDJSON frame of the Ollama streaming
// response.
type chatStreamChunk struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	// Error carries a server-reported error message when non-empty.
	// Ollama emits this in the streaming response for model-load
	// failures and similar conditions.
	Error string `json:"error,omitempty"`
}

// Transform drains the input channel into a single prompt,
// concatenates the Text fields, POSTs the result to
// {api_url}/api/chat with stream=true, and emits one
// transcribe.TranscriptChunk per streamed delta. The final chunk has
// IsFinal=true. See the package doc for the wire format.
//
// The backend intentionally buffers the full input before issuing
// the request: streaming-in-streaming is not worth the complexity for
// the dictation use case, where transcripts are short and LLM prompts
// are one-shot. See plan §1.9.
func (b *Backend) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	var sb strings.Builder
	var lang string
	for chunk := range in {
		if chunk.Err != nil {
			// Upstream error: propagate without contacting the
			// backend. This lets the caller distinguish transcription
			// failures from transform failures.
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

	body, err := b.client.PostJSON(ctx, b.cfg.APIURL+"/api/chat", b.cfg.APIKey, req)
	if err != nil {
		out := make(chan transcribe.TranscriptChunk, 1)
		out <- transcribe.TranscriptChunk{
			IsFinal:  true,
			Language: lang,
			Err:      fmt.Errorf("local: %w", err),
		}
		close(out)
		return out, nil
	}

	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		// NDJSON lines can be large when the model replies in one
		// chunk; bump the buffer to 1 MiB.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var frame chatStreamChunk
			if err := json.Unmarshal(line, &frame); err != nil {
				sendFinal(ctx, out, transcribe.TranscriptChunk{
					IsFinal:  true,
					Language: lang,
					Err:      fmt.Errorf("local: decode: %w", err),
				})
				return
			}
			if frame.Error != "" {
				sendFinal(ctx, out, transcribe.TranscriptChunk{
					IsFinal:  true,
					Language: lang,
					Err:      fmt.Errorf("local: server: %s", frame.Error),
				})
				return
			}
			chunk := transcribe.TranscriptChunk{
				Text:     frame.Message.Content,
				IsFinal:  frame.Done,
				Language: lang,
			}
			select {
			case <-ctx.Done():
				return
			case out <- chunk:
			}
			if frame.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			sendFinal(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: lang,
				Err:      fmt.Errorf("local: stream: %w", err),
			})
			return
		}
		// Stream ended without a done=true frame. Emit a synthetic
		// final marker so the consumer sees a complete stream.
		sendFinal(ctx, out, transcribe.TranscriptChunk{IsFinal: true, Language: lang})
	}()
	return out, nil
}

// HealthCheck probes GET {api_url}/ and returns nil on any 2xx
// response. Ollama returns 200 with a plain-text banner; the plain
// response is intentionally not parsed — a 2xx is the signal.
func (b *Backend) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.cfg.APIURL+"/", nil)
	if err != nil {
		return fmt.Errorf("local: health: %w", err)
	}
	resp, err := b.client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("local: health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("local: health: http %d", resp.StatusCode)
	}
	return nil
}

// sendFinal delivers chunk to out unless ctx is cancelled first.
// Factored out to keep the streaming loop readable.
func sendFinal(ctx context.Context, out chan<- transcribe.TranscriptChunk, chunk transcribe.TranscriptChunk) {
	select {
	case <-ctx.Done():
	case out <- chunk:
	}
}
