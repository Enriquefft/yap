package yap

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/passthrough"
)

// Client is the top-level wrapper over a Transcriber and a
// Transformer. Construct one with New and a set of Option values.
// The zero value is not usable — use New.
type Client struct {
	transcriber transcribe.Transcriber
	transformer transform.Transformer
}

// Option configures a Client at construction. Options are applied in
// order; later options override earlier ones.
type Option func(*Client)

// WithTranscriber sets the Client's Transcriber. Required: New
// returns an error if no Transcriber is provided.
func WithTranscriber(t transcribe.Transcriber) Option {
	return func(c *Client) { c.transcriber = t }
}

// WithTransformer sets the Client's Transformer. When unset, the
// Client uses the passthrough transformer, which forwards chunks
// unchanged.
func WithTransformer(t transform.Transformer) Option {
	return func(c *Client) { c.transformer = t }
}

// New constructs a Client from the supplied options. At least
// WithTranscriber is required; the transformer defaults to
// passthrough. Returns an error if the configuration is incomplete.
func New(opts ...Option) (*Client, error) {
	c := &Client{transformer: passthrough.New()}
	for _, opt := range opts {
		opt(c)
	}
	if c.transcriber == nil {
		return nil, errors.New("yap: Transcriber is required; use WithTranscriber")
	}
	if c.transformer == nil {
		// An option explicitly passed a nil transformer; fall
		// back to passthrough rather than NPEing at runtime.
		c.transformer = passthrough.New()
	}
	return c, nil
}

// Transcribe runs the full pipeline (Transcriber → Transformer) and
// returns the accumulated text. The audio reader is read to
// completion by the Transcriber. On success the returned string is
// the concatenation of every chunk's Text field, in order. On the
// first chunk with a non-nil Err, Transcribe returns that error.
// If ctx is cancelled before the pipeline finishes, Transcribe
// returns ctx.Err() so the caller observes cancellation even when
// backends drop their in-flight chunks on ctx.Done().
func (c *Client) Transcribe(ctx context.Context, audio io.Reader) (string, error) {
	out, err := c.TranscribeStream(ctx, audio)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for chunk := range out {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		sb.WriteString(chunk.Text)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// TranscribeStream wires the Transcriber and Transformer together and
// returns the chunk channel from the transform stage. Consumers that
// want streaming semantics (e.g. partial injection into a text field)
// use this entry point directly.
func (c *Client) TranscribeStream(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	chunks, err := c.transcriber.Transcribe(ctx, audio)
	if err != nil {
		return nil, err
	}
	transformed, err := c.transformer.Transform(ctx, chunks)
	if err != nil {
		return nil, err
	}
	return transformed, nil
}
