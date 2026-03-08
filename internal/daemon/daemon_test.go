package daemon

import (
	"testing"
	"time"

	"github.com/hybridz/yap/internal/config"
)

// TestDaemonRunBlocks confirms that Run blocks until SIGTERM.
func TestDaemonRunBlocksStub(t *testing.T) {
	t.Skip("Wave 0 stub — requires injected context for testable signal handling")
}

// TestPIDFileWrittenBeforeAudioInit verifies ordering (DAEMON-01).
// Wave 0 stub — implement in Plan 03-01 iteration 2 if needed.
func TestPIDFileWrittenBeforeAudioInit(t *testing.T) {
	t.Skip("Wave 0 stub — implement with dependency injection of context")
}

// TestDaemonCleanupOnExit verifies defer cleanup (AUDIO-07, DAEMON-05).
// Wave 0 stub — requires test double for PortAudio.
func TestDaemonCleanupOnExit(t *testing.T) {
	t.Skip("Wave 0 stub — requires mock Recorder for deterministic cleanup testing")
}

// TestDaemonAlreadyRunning verifies DAEMON-05 (duplicate detection).
// This is tested via pidfile_test.go (IsLive check).
