package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// Server listens on a Unix domain socket for IPC commands.
type Server struct {
	ln       net.Listener
	sockPath string
}

// NewServer creates a listener at sockPath with IPC-01 security (mode 0600).
// IPC-04: Removes stale socket file before listening.
func NewServer(sockPath string) (*Server, error) {
	// IPC-04: Clean up stale socket from crashed daemon.
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", sockPath, err)
	}

	// IPC-01: Restrict socket to owner only.
	// net.Listen creates with default umask-derived perms; must explicit chmod.
	if err := os.Chmod(sockPath, 0600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return &Server{ln: ln, sockPath: sockPath}, nil
}

// Close shuts down the listener and removes the socket file.
func (s *Server) Close() error {
	err := s.ln.Close()
	os.Remove(s.sockPath)
	return err
}

// Serve accepts connections and handles each in a goroutine.
// Blocks until ctx is cancelled (SIGTERM from daemon.Run).
func (s *Server) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := s.ln.Accept()
		if err != nil {
			// Listener closed — normal shutdown path (Close called).
			return nil
		}
		go s.handleConn(ctx, conn)
	}
}

// handleConn reads one request, dispatches to handler, sends response.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req Request
	if err := dec.Decode(&req); err != nil {
		// Malformed request — send error and disconnect.
		enc.Encode(Response{
			Ok:    false,
			Error: fmt.Sprintf("malformed request: %v", err),
		})
		return
	}

	resp := s.dispatch(ctx, req)
	enc.Encode(resp) // IPC-02: json.Encoder.Encode appends \n automatically (NDJSON)
}

// dispatch routes command to handler and returns response.
// TODO(Phase 4): Implement actual recording state machine.
// For Phase 3-02, status always returns "idle".
func (s *Server) dispatch(ctx context.Context, req Request) Response {
	switch req.Cmd {
	case CmdStop:
		// Stop daemon (trigger ctx cancellation from daemon.Run).
		// For Phase 3-02, just return success; actual shutdown is handled by daemon.Run context.
		return Response{Ok: true, State: "stopped"}

	case CmdStatus:
		// Report daemon status.
		// TODO(Phase 4): Query actual recording state.
		return Response{Ok: true, State: "idle"}

	case CmdToggle:
		// Toggle recording state.
		// TODO(Phase 4): Implement state toggle logic.
		return Response{Ok: true, State: "idle"}

	default:
		return Response{Ok: false, Error: "unknown command: " + req.Cmd}
	}
}
