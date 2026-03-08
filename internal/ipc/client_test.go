package ipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSendStatusCommand sends status and receives response.
func TestSendStatusCommand(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Start server.
	srv, err := NewServer(sockPath)
	require.NoError(t, err)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond) // Let server start.

	// Send status command.
	resp, err := Send(sockPath, CmdStatus, 1*time.Second)
	require.NoError(t, err)
	require.True(t, resp.Ok)
	require.Equal(t, "idle", resp.State)

	cancel()
}

// TestSendToNonExistentSocket returns error gracefully.
func TestSendToNonExistentSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	resp, err := Send(sockPath, CmdStatus, 1*time.Second)
	require.Error(t, err)
	// resp should be zero-valued on error.
	require.False(t, resp.Ok)
}

// TestSendTimeout triggers if server is slow.
func TestSendTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create a "slow" server that accepts but doesn't respond.
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer ln.Close()
	os.Chmod(sockPath, 0600)

	// Accept but never send response (will timeout).
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			time.Sleep(2 * time.Second) // Longer than timeout.
			conn.Close()
		}
	}()

	resp, err := Send(sockPath, CmdStatus, 100*time.Millisecond)
	require.Error(t, err)
	require.False(t, resp.Ok)
}

// TestSendStopCommand sends stop request.
func TestSendStopCommand(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	srv, err := NewServer(sockPath)
	require.NoError(t, err)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	resp, err := Send(sockPath, CmdStop, 1*time.Second)
	require.NoError(t, err)
	require.True(t, resp.Ok)

	cancel()
}
