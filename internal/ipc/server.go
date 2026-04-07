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
	ln         net.Listener
	sockPath   string
	toggleFn   func() string
	statusFn   func() Response
	shutdownFn func()
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

// SetToggleFn sets the toggle function for recording state.
// Called by daemon to provide callback for toggle command.
func (s *Server) SetToggleFn(fn func() string) {
	s.toggleFn = fn
}

// SetStatusFn sets the status function for querying daemon state.
// The callback returns a fully-populated Response (Ok, State, Mode,
// ConfigPath, Version, PID, Backend, Model) so the server can write
// it as-is. The daemon owns building this struct because it knows
// every field; the server stays a transport.
func (s *Server) SetStatusFn(fn func() Response) {
	s.statusFn = fn
}

// SetShutdownFn sets the function called when CmdStop is received.
// The daemon passes its context cancel function here.
func (s *Server) SetShutdownFn(fn func()) {
	s.shutdownFn = fn
}

// Close shuts down listener and removes of socket file.
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
func (s *Server) dispatch(ctx context.Context, req Request) Response {
	switch req.Cmd {
	case CmdStop:
		if s.shutdownFn != nil {
			s.shutdownFn()
		}
		return Response{Ok: true, State: "stopped"}

	case CmdStatus:
		// Report daemon status. The daemon-supplied callback returns
		// a fully populated Response; the server forwards it verbatim
		// so optional fields (Mode, ConfigPath, Version, PID, Backend,
		// Model) round-trip without the transport reshaping them.
		if s.statusFn != nil {
			resp := s.statusFn()
			if !resp.Ok && resp.Error == "" {
				// Defensive: a status callback that forgets to set
				// Ok still produces a valid wire shape.
				resp.Ok = true
			}
			return resp
		}
		return Response{Ok: true, State: "idle"}

	case CmdToggle:
		// Toggle recording state.
		if s.toggleFn != nil {
			return Response{Ok: true, State: s.toggleFn()}
		}
		return Response{Ok: false, Error: "toggle function not set"}

	default:
		return Response{Ok: false, Error: "unknown command: " + req.Cmd}
	}
}
