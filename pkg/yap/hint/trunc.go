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

// ResolveTargetCwd resolves the focused window's working directory
// via /proc/<pid>/cwd. Falls back to os.Getwd() when the target has
// no PID or /proc is unreadable (e.g. wlroots compositors that don't
// expose PID in the toplevel protocol).
func ResolveTargetCwd(target inject.Target) string {
	if target.WindowID != "" {
		link, err := os.Readlink("/proc/" + target.WindowID + "/cwd")
		if err == nil {
			return link
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}
