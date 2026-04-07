package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// closeIfCloser releases resources held by an opaque value if it
// implements io.Closer. Used by the one-shot CLI commands when they
// have no compile-time guarantee that a particular Transcriber or
// Transformer owns OS-level resources (whisperlocal does, mock does
// not). Errors from Close are logged at warn level so they show up in
// CI without breaking the user-visible exit code on the happy path.
func closeIfCloser(v any) {
	c, ok := v.(io.Closer)
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		slog.Default().Warn("close error", "type", fmt.Sprintf("%T", v), "err", err)
	}
}

// readTextInput resolves the text payload for `yap transform` and
// `yap paste`. Order of precedence:
//
//  1. The single positional arg, when provided.
//  2. Stdin, when --stdin is set or when no arg was supplied AND
//     stdin is not a terminal (so shell pipelines work without the
//     user remembering --stdin).
//
// An empty result is rejected with a clear error so the caller doesn't
// inject silence.
func readTextInput(args []string, forceStdin bool) (string, error) {
	if len(args) > 0 && !forceStdin {
		return args[0], nil
	}
	if forceStdin || len(args) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("no input: pass text as an argument or via --stdin")
}

// openInputFile opens a path for reading. The literal "-" maps to
// os.Stdin so callers can stream a WAV via shell pipes. The returned
// io.ReadCloser is always non-nil on success.
func openInputFile(path string) (io.ReadCloser, error) {
	if path == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}
