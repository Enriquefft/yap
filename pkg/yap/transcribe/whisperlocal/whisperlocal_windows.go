//go:build windows

package whisperlocal

import (
	"errors"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// windowsNotSupportedMsg is the sticky error message returned by
// every whisperlocal entry point on Windows. It is a const so the
// whisperlocal package contains zero package-level vars (matching
// the noglobals AST guard enforced on the Linux build and extended
// to the Windows stub for uniformity).
//
// The package stays importable (so side-effect imports in the
// daemon do not break the Windows build) but construction always
// fails with this diagnostic pointing users at the cross-platform
// groq / openai backends instead.
const windowsNotSupportedMsg = "whisperlocal: Windows support is not yet implemented; use groq or openai backend"

// newPlatformBackend is the Windows stub of the Backend constructor.
// It returns a fresh "not supported" error without touching the
// filesystem so discovery errors never mask the fundamental "not
// supported" message.
//
// The function intentionally ignores cfg: there is nothing to
// validate because the backend cannot run regardless of config.
func newPlatformBackend(_ transcribe.Config) (*Backend, error) {
	return nil, errors.New(windowsNotSupportedMsg)
}

// closeError is the Windows stub for the unix wait-status handler.
// It is never called in practice because New always fails before a
// subprocess is spawned; the function exists only so the shared
// whisperlocal.go references resolve on Windows builds.
func closeError(proc *serverProc) error {
	if proc == nil {
		return nil
	}
	return proc.waitErr
}
