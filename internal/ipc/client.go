package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Send sends a single IPC command to the daemon and returns the response.
// If the socket does not exist or the daemon is not running, an error is returned.
// timeout controls both dial and read deadlines.
//
// Callers (CLI commands) are responsible for retry logic (e.g., yap stop with 3 retries).
// IPC-03: CLI exits with status 0 on success, 1 on error.
func Send(sockPath string, cmd string, timeout time.Duration) (Response, error) {
	// Dial with timeout (includes both TCP dial and Unix socket connect).
	conn, err := net.DialTimeout("unix", sockPath, timeout)
	if err != nil {
		return Response{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	// Set read/write deadline for entire interaction.
	conn.SetDeadline(time.Now().Add(timeout))

	// Send request.
	enc := json.NewEncoder(conn)
	if err := enc.Encode(Request{Cmd: cmd}); err != nil {
		return Response{}, fmt.Errorf("send command: %w", err)
	}

	// Receive response.
	dec := json.NewDecoder(conn)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	return resp, nil
}
