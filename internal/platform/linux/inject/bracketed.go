package inject

// Bracketed paste escape sequences. The terminal interprets text
// arriving between these markers as a literal paste rather than
// individual keystrokes, which prevents shells from auto-indenting,
// auto-completing, or executing each line as it arrives.
const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// wrapBracketed wraps text in the bracketed-paste begin/end markers.
// The function is intentionally trivial — its purpose is to keep the
// magic byte sequences in one place so the OSC52 and tmux strategies
// share a single source of truth.
func wrapBracketed(text string) string {
	return bracketedPasteStart + text + bracketedPasteEnd
}
