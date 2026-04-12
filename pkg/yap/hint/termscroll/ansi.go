package termscroll

import "regexp"

// ansiStripper holds compiled regex patterns for ANSI escape sequence
// removal. Zero globals: the regexes are compiled at construction time
// and stored on the struct.
type ansiStripper struct {
	csi   *regexp.Regexp
	osc   *regexp.Regexp
	other *regexp.Regexp
}

// newANSIStripper compiles the ANSI stripping regexes once.
func newANSIStripper() *ansiStripper {
	return &ansiStripper{
		// CSI: ESC [ <params> <intermediates> <final>
		csi: regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]`),
		// OSC: ESC ] ... (ST | BEL)
		osc: regexp.MustCompile(`\x1b\].*?(\x1b\\|\x07)`),
		// Other single-character escapes (not [ or ]).
		other: regexp.MustCompile(`\x1b[^\[\]]`),
	}
}

// Strip removes all ANSI escape sequences from s.
func (a *ansiStripper) Strip(s string) string {
	s = a.osc.ReplaceAllString(s, "")
	s = a.csi.ReplaceAllString(s, "")
	s = a.other.ReplaceAllString(s, "")
	return s
}
