package cli

import (
	"errors"
	"strings"
	"testing"
)

// errReader is an io.Reader that always returns the configured error.
// Used to verify readTextInput surfaces stdin read failures cleanly.
type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

// TestReadTextInput_Arg returns the positional arg verbatim and never
// touches the supplied stdin reader.
func TestReadTextInput_Arg(t *testing.T) {
	in := errReader{err: errors.New("reader must not be touched")}
	got, err := readTextInput([]string{"hello world"}, false, in, func() bool { return true })
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

// TestReadTextInput_ForceStdin honors --stdin even when an arg is
// supplied. The arg is ignored and the reader is consumed.
func TestReadTextInput_ForceStdin(t *testing.T) {
	got, err := readTextInput(
		[]string{"ignored arg"},
		true,
		strings.NewReader("piped payload"),
		func() bool { return true },
	)
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != "piped payload" {
		t.Errorf("got %q, want %q", got, "piped payload")
	}
}

// TestReadTextInput_PipedStdin reads from a non-terminal stdin when
// no positional arg is supplied. This is the canonical "shell pipe
// without --stdin" path.
func TestReadTextInput_PipedStdin(t *testing.T) {
	got, err := readTextInput(
		nil,
		false,
		strings.NewReader("piped via shell"),
		func() bool { return false },
	)
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != "piped via shell" {
		t.Errorf("got %q, want %q", got, "piped via shell")
	}
}

// TestReadTextInput_InteractiveStdinRefused refuses to block on a
// terminal when no arg and no --stdin were supplied — the function
// must return an error explaining how to supply input instead of
// hanging forever.
func TestReadTextInput_InteractiveStdinRefused(t *testing.T) {
	in := errReader{err: errors.New("must not read from terminal")}
	_, err := readTextInput(nil, false, in, func() bool { return true })
	if err == nil {
		t.Fatal("expected interactive-terminal guard to error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no text provided") {
		t.Errorf("error did not name the no-input case: %v", err)
	}
	if !strings.Contains(msg, "--stdin") {
		t.Errorf("error did not mention --stdin: %v", err)
	}
}

// TestReadTextInput_StdinReadError surfaces underlying stdin read
// errors with the read-stdin prefix so callers can chain them.
func TestReadTextInput_StdinReadError(t *testing.T) {
	in := errReader{err: errors.New("disk on fire")}
	_, err := readTextInput(nil, true, in, func() bool { return false })
	if err == nil {
		t.Fatal("expected reader error to surface")
	}
	if !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("error did not surface inner failure: %v", err)
	}
	if !strings.Contains(err.Error(), "read stdin") {
		t.Errorf("error did not include the read-stdin prefix: %v", err)
	}
}

// TestReadTextInput_NilTerminalProbe treats a nil isTerminal as
// "not a terminal" and falls through to reading the supplied io.Reader.
// Production wires stdinIsTerminal explicitly; this guard means a
// caller mistake (forgetting to wire the probe) does not crash with
// a nil panic.
func TestReadTextInput_NilTerminalProbe(t *testing.T) {
	got, err := readTextInput(nil, false, strings.NewReader("ok"), nil)
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

