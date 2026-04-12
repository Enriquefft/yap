package hint

import (
	"os"
	"unicode/utf8"

	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// HeadBytes returns the first n bytes of s, clipping on a UTF-8 rune
// boundary. Used for vocabulary (project name/description is at the
// start of the document).
func HeadBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// TailBytes returns the last n bytes of s, clipping on a UTF-8 rune
// boundary. Used for conversation (recent messages are at the end).
func TailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// ResolveTargetCwd resolves the working directory of the focused app.
// For terminals, reads /proc/<pid>/cwd — the terminal's cwd IS the
// project directory. For non-terminals (browsers, Electron apps), the
// process cwd is meaningless, so falls back to os.Getwd(). Also falls
// back when no PID is available or /proc is unreadable.
func ResolveTargetCwd(target inject.Target) string {
	if target.AppType == inject.AppTerminal && target.WindowID != "" {
		link, err := os.Readlink("/proc/" + target.WindowID + "/cwd")
		if err == nil {
			return link
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}
