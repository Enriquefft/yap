package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"golang.org/x/term"
)

// closeIfCloser releases resources held by an opaque value if it
// implements io.Closer. Used by the one-shot CLI commands when they
// have no compile-time guarantee that a particular Transcriber or
// Transformer owns OS-level resources (whisperlocal does, mock does
// not). Errors from Close are logged at warn level so they show up in
// CI without breaking the user-visible exit code on the happy path.
//
// The name parameter identifies the resource in the warning log so
// operators can tell whether the failing close was the transcriber,
// the transformer, or some other component.
func closeIfCloser(v any, name string) {
	c, ok := v.(io.Closer)
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		slog.Default().Warn("close error",
			"component", name,
			"type", fmt.Sprintf("%T", v),
			"err", err)
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
// When no arg, no --stdin, and stdin is an interactive terminal, the
// function refuses to block on user input and returns a clear error
// telling the operator how to supply text. This guard is what makes
// `yap transform` safe to invoke from a TTY without arguments — it
// fails fast instead of leaving the terminal hanging on a never-EOF
// read.
//
// in is the io.Reader stdin source — os.Stdin in production,
// substitutable in tests. isTerminal reports whether that source is
// an interactive terminal; tests inject a stub that returns false to
// exercise the piped path.
func readTextInput(args []string, forceStdin bool, in io.Reader, isTerminal func() bool) (string, error) {
	if len(args) > 0 && !forceStdin {
		return args[0], nil
	}
	if !forceStdin && len(args) == 0 && isTerminal != nil && isTerminal() {
		return "", fmt.Errorf(
			"no text provided; pass a positional arg, use --stdin, or pipe via stdin")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(data), nil
}

// stdinIsTerminal is the production isTerminal probe for readTextInput.
// Returns true when os.Stdin is connected to an interactive TTY,
// false when it is a pipe, redirect, or otherwise non-terminal.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
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
