package hint

import (
	"os"
	"strconv"
	"strings"
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
// For terminals, walks /proc from the terminal emulator PID down to
// the deepest descendant shell and reads its cwd — the shell's cwd IS
// the project directory. The terminal emulator's own cwd is typically
// $HOME (where it was launched), which is useless. For non-terminals,
// falls back to os.Getwd().
func ResolveTargetCwd(target inject.Target) string {
	if target.AppType == inject.AppTerminal && target.WindowID != "" {
		pid, err := strconv.Atoi(target.WindowID)
		if err == nil {
			if cwd := resolveShellCwd(pid); cwd != "" {
				return cwd
			}
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// resolveShellCwd walks from rootPID down to the deepest descendant
// process and returns its cwd. The deepest child of a terminal
// emulator is typically the active shell (or a program the shell
// launched). Its cwd is where the user is working.
func resolveShellCwd(rootPID int) string {
	// BFS to find the deepest descendant.
	var deepest int
	queue := []int{rootPID}
	visited := map[int]bool{rootPID: true}

	for len(queue) > 0 {
		if len(visited) > 200 {
			break
		}
		pid := queue[0]
		queue = queue[1:]
		deepest = pid

		children := procChildren(pid)
		for _, child := range children {
			if visited[child] {
				continue
			}
			visited[child] = true
			queue = append(queue, child)
		}
	}

	if deepest == rootPID {
		// No descendants — try the root itself.
		deepest = rootPID
	}

	link, err := os.Readlink("/proc/" + strconv.Itoa(deepest) + "/cwd")
	if err != nil {
		return ""
	}
	return link
}

// procChildren returns the PIDs of direct children of pid by reading
// /proc/<pid>/task/*/children. Returns nil on any error.
func procChildren(pid int) []int {
	taskDir := "/proc/" + strconv.Itoa(pid) + "/task"
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(taskDir + "/" + e.Name() + "/children")
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			n, err := strconv.Atoi(field)
			if err != nil || n <= 0 {
				continue
			}
			out = append(out, n)
		}
	}
	return out
}
