package yap

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/passthrough"
)

// Client is the top-level wrapper over a Transcriber and a
// Transformer. Construct one with New and a set of Option values.
// The zero value is not usable — use New.
type Client struct {
	transcriber         transcribe.Transcriber
	transformer         transform.Transformer
	transformerExplicit bool // true when WithTransformer was called
}

// Option configures a Client at construction. Options are applied in
// order; later options override earlier ones.
type Option func(*Client)

// WithTranscriber sets the Client's Transcriber. Required: New
// returns an error if no Transcriber is provided.
func WithTranscriber(t transcribe.Transcriber) Option {
	return func(c *Client) { c.transcriber = t }
}

// WithTransformer sets the Client's Transformer. The supplied
// Transformer must be non-nil — passing nil is a programming error
// and New returns an error rather than silently substituting
// passthrough. Mirrors the WithTranscriber contract: explicit
// dependencies are validated, defensive substitution is reserved
// for the omission path (i.e. WithTransformer never being called at
// all).
//
// To use the default identity transformer, simply omit
// WithTransformer from the options.
func WithTransformer(t transform.Transformer) Option {
	return func(c *Client) {
		c.transformer = t
		c.transformerExplicit = true
	}
}

// New constructs a Client from the supplied options. At least
// WithTranscriber is required; when WithTransformer is omitted the
// Client uses the passthrough transformer. Returns an error if the
// configuration is incomplete or if WithTransformer was called with
// a nil Transformer.
func New(opts ...Option) (*Client, error) {
	c := &Client{transformer: passthrough.New()}
	for _, opt := range opts {
		opt(c)
	}
	if c.transcriber == nil {
		return nil, errors.New("yap: Transcriber is required; use WithTranscriber")
	}
	if c.transformerExplicit && c.transformer == nil {
		return nil, errors.New("yap: WithTransformer was called with a nil Transformer; pass a non-nil value or omit WithTransformer to use the passthrough default")
	}
	if c.transformer == nil {
		// Defensive paranoia: should never happen because the
		// constructor seeds passthrough.New() and the explicit-nil
		// case is rejected above. Keep the guard so a future change
		// to the seeding logic does not introduce a runtime NPE.
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
//
// Transcribe discards every per-chunk metadata field except Text
// (Language, Offset, IsFinal, ...). Callers that need that
// information should use TranscribeAll for a slice of every chunk
// or TranscribeStream for a live channel.
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

// TranscribeAll runs the full pipeline (Transcriber → Transformer)
// and returns every chunk emitted, in order. Unlike Transcribe, it
// preserves per-chunk metadata (Language, Offset, IsFinal) so
// callers that want detected language, partial timing, or the final
// marker do not have to fall back to TranscribeStream.
//
// On the first chunk with a non-nil Err, TranscribeAll returns the
// chunks accumulated so far plus that error. On ctx cancellation it
// returns whatever has accumulated plus ctx.Err.
func (c *Client) TranscribeAll(ctx context.Context, audio io.Reader) ([]transcribe.TranscriptChunk, error) {
	out, err := c.TranscribeStream(ctx, audio)
	if err != nil {
		return nil, err
	}
	var got []transcribe.TranscriptChunk
	for chunk := range out {
		if chunk.Err != nil {
			return got, chunk.Err
		}
		got = append(got, chunk)
	}
	if err := ctx.Err(); err != nil {
		return got, err
	}
	return got, nil
}

// TranscribeStream wires the Transcriber and Transformer together and
// returns the chunk channel from the transform stage. Consumers that
// want streaming semantics (e.g. partial injection into a text field)
// use this entry point directly.
func (c *Client) TranscribeStream(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	chunks, err := c.transcriber.Transcribe(ctx, audio, transcribe.Options{})
	if err != nil {
		return nil, err
	}
	transformed, err := c.transformer.Transform(ctx, chunks, transform.Options{})
	if err != nil {
		return nil, err
	}
	return transformed, nil
}
