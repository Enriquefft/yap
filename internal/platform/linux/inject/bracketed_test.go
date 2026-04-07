package inject

import (
	"strings"
	"testing"
)

func TestWrapBracketed(t *testing.T) {
	out := wrapBracketed("ls -la\necho hi")
	if !strings.HasPrefix(out, "\x1b[200~") {
		t.Errorf("missing start marker: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b[201~") {
		t.Errorf("missing end marker: %q", out)
	}
	body := strings.TrimPrefix(strings.TrimSuffix(out, "\x1b[201~"), "\x1b[200~")
	if body != "ls -la\necho hi" {
		t.Errorf("body = %q, want preserved", body)
	}
}

func TestWrapBracketedEmpty(t *testing.T) {
	if got := wrapBracketed(""); got != "\x1b[200~\x1b[201~" {
		t.Errorf("got %q, want bare markers", got)
	}
}

func TestBracketedConstantsMatchSpec(t *testing.T) {
	if bracketedPasteStart != "\x1b[200~" {
		t.Errorf("start marker drifted: %q", bracketedPasteStart)
	}
	if bracketedPasteEnd != "\x1b[201~" {
		t.Errorf("end marker drifted: %q", bracketedPasteEnd)
	}
}
