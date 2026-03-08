package ipc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNewServerCreatesSocket creates a socket and verifies permissions.
func TestNewServerCreatesSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	srv, err := NewServer(sockPath)
	require.NoError(t, err)
	defer srv.Close()

	// Verify socket exists.
	stat, err := os.Stat(sockPath)
	require.NoError(t, err)

	// Verify mode is 0600 (IPC-01).
	mode := stat.Mode().Perm()
	require.Equal(t, os.FileMode(0600), mode)
}

// TestNewServerRemovesStaleSocket cleans up old socket (IPC-04).
func TestNewServerRemovesStaleSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create a stale socket file.
	err := os.WriteFile(sockPath, []byte("stale"), 0600)
	require.NoError(t, err)

	// NewServer should remove it.
	srv, err := NewServer(sockPath)
	require.NoError(t, err)
	defer srv.Close()

	// Verify new socket is a socket (not regular file).
	stat, err := os.Stat(sockPath)
	require.NoError(t, err)
	require.True(t, stat.Mode()&os.ModeSocket != 0, "socket should be a Unix socket, not regular file")
}

// TestHandleConnNDJSON verifies IPC-02 (newline-delimited JSON).
func TestHandleConnNDJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	srv, err := NewServer(sockPath)
	require.NoError(t, err)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in goroutine.
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx)
	}()

	// Give server time to start listening.
	time.Sleep(50 * time.Millisecond)

	// Connect and send request.
	conn, err := net.DialTimeout("unix", sockPath, 1*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Send request.
	enc := json.NewEncoder(conn)
	err = enc.Encode(Request{Cmd: CmdStatus})
	require.NoError(t, err)

	// Receive response.
	dec := json.NewDecoder(conn)
	var resp Response
	err = dec.Decode(&resp)
	require.NoError(t, err)

	require.True(t, resp.Ok)
	require.Equal(t, "idle", resp.State)

	cancel()
	<-done
}

// TestDispatchUnknownCommand returns error for unknown cmd.
func TestDispatchUnknownCommand(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")
	srv := &Server{sockPath: sockPath}

	resp := srv.dispatch(context.Background(), Request{Cmd: "invalid"})
	require.False(t, resp.Ok)
	require.NotEmpty(t, resp.Error)
}
