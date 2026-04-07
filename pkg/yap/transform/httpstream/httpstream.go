package httpstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hybridz/yap/internal/config"
)

// DefaultMaxRetries is the number of retry attempts beyond the initial
// request for a transient failure. Matches the Phase 3 groq backend.
const DefaultMaxRetries = 3

// DefaultTimeout is the per-attempt HTTP timeout applied when callers
// do not supply their own *http.Client. It is intentionally generous
// because transform backends stream responses and the timeout has to
// cover the full streamed body, not just the headers.
const DefaultTimeout = 60 * time.Second

// Client is an HTTP helper that posts JSON payloads and returns the
// streamed response body for the caller to parse. A zero-value Client
// is not usable — always construct via NewClient or populate the
// required fields explicitly.
type Client struct {
	// HTTP is the underlying client. NewClient populates this with a
	// sensible default. Callers may override it (e.g. tests using
	// httptest.Server) by assigning a new value before use.
	HTTP *http.Client
	// UserAgent is sent as the User-Agent header on every request.
	UserAgent string
	// MaxRetries is the number of retry attempts beyond the initial
	// request. Zero disables retries entirely.
	MaxRetries int
	// Backoff is the per-attempt sleep duration. Backoff[i] is slept
	// after attempt i fails with a retryable error. If the slice is
	// shorter than MaxRetries the last value is reused.
	Backoff []time.Duration
}

// NewClient constructs a Client with the default timeout, the yap
// user-agent, and the default retry policy. Callers that need a
// custom *http.Client (for tests, mTLS, etc.) can overwrite the HTTP
// field afterwards.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		HTTP:       &http.Client{Timeout: timeout},
		UserAgent:  "yap/" + config.Version,
		MaxRetries: DefaultMaxRetries,
		Backoff: []time.Duration{
			500 * time.Millisecond,
			1 * time.Second,
			2 * time.Second,
		},
	}
}

// NonRetryableError is returned by PostJSON when the server responded
// with a 4xx status code. Callers that want to surface the status and
// body to users or logs can type-assert for this error.
type NonRetryableError struct {
	// StatusCode is the HTTP status code from the response.
	StatusCode int
	// Body is the response body, read into memory. It is included in
	// the error string so logs show the server's error message.
	Body string
}

// Error implements the error interface.
func (e *NonRetryableError) Error() string {
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body)
}

// PostJSON posts body (marshalled as JSON) to url with the supplied
// bearer token (optional — empty string omits the Authorization
// header). On a 2xx response it returns the response body reader and
// nil; the caller is responsible for closing the reader. On a 4xx
// response it drains the body and returns a NonRetryableError. On a
// 5xx response or a transport error it retries according to the
// client's policy. When every attempt fails, the last error observed
// is returned.
//
// Retries honour context cancellation: a cancelled ctx short-circuits
// the backoff sleep and returns ctx.Err.
func (c *Client) PostJSON(ctx context.Context, url, apiKey string, body any) (io.ReadCloser, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("httpstream: marshal: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("httpstream: new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = fmt.Errorf("httpstream: transport: %w", err)
			if attempt < c.MaxRetries {
				if sleepErr := sleepCtx(ctx, c.backoffFor(attempt)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode/100 == 2 {
			return resp.Body, nil
		}

		// Non-2xx: consume the body so we can reuse the connection
		// (or at least format a useful error) and decide retry vs
		// fail-fast.
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("httpstream: read body: %w", readErr)
			if attempt < c.MaxRetries {
				if sleepErr := sleepCtx(ctx, c.backoffFor(attempt)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode/100 == 4 {
			return nil, &NonRetryableError{
				StatusCode: resp.StatusCode,
				Body:       string(respBody),
			}
		}

		// 5xx and anything else — retry.
		lastErr = fmt.Errorf("httpstream: http %d: %s", resp.StatusCode, string(respBody))
		if attempt < c.MaxRetries {
			if sleepErr := sleepCtx(ctx, c.backoffFor(attempt)); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		return nil, lastErr
	}

	if lastErr == nil {
		lastErr = errors.New("httpstream: exhausted retries with no error recorded")
	}
	return nil, lastErr
}

// backoffFor returns the sleep duration to use after the attempt-th
// failed attempt. It returns the last element when attempt is beyond
// the end of the Backoff slice, so callers can supply a short slice
// and still benefit from a stable upper-bound sleep.
func (c *Client) backoffFor(attempt int) time.Duration {
	if len(c.Backoff) == 0 {
		return 0
	}
	if attempt >= len(c.Backoff) {
		return c.Backoff[len(c.Backoff)-1]
	}
	return c.Backoff[attempt]
}

// sleepCtx sleeps for d or returns ctx.Err when the context is
// cancelled. Zero d returns immediately.
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
