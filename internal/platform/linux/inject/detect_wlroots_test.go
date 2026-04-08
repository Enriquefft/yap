package inject

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// scriptedWaylandConn is a deterministic in-memory WaylandConn used by
// the wlroots detector tests. The conn delivers reads from a
// pre-recorded list of "batches": each Read pops one batch off the
// front of the list and returns it. Writes are accumulated for
// later assertion. SetDeadline records the latest deadline so tests
// can verify the detector arms it.
//
// The conn is intentionally synchronous and never blocks: the
// detector either drains all batches and returns a result, or it
// hits an empty queue and the test fails fast on the resulting EOF.
// This keeps the wlroots tests free of timing flake.
type scriptedWaylandConn struct {
	mu         sync.Mutex
	readQueue  [][]byte
	writeLog   []byte
	deadline   time.Time
	closed     bool
	readCalls  int
	writeCalls int
	// onWrite, when non-nil, is invoked synchronously after each
	// successful Write. It receives the cumulative writeCalls
	// counter so tests can stage replenishment of the read queue
	// in response to specific requests.
	onWrite func(writeCallNumber int)
}

func (s *scriptedWaylandConn) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readCalls++
	if s.closed {
		return 0, net.ErrClosed
	}
	if len(s.readQueue) == 0 {
		return 0, io.EOF
	}
	chunk := s.readQueue[0]
	n := copy(p, chunk)
	if n < len(chunk) {
		s.readQueue[0] = chunk[n:]
	} else {
		s.readQueue = s.readQueue[1:]
	}
	return n, nil
}

func (s *scriptedWaylandConn) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, net.ErrClosed
	}
	s.writeLog = append(s.writeLog, p...)
	s.writeCalls++
	cb := s.onWrite
	count := s.writeCalls
	s.mu.Unlock()
	if cb != nil {
		cb(count)
	}
	return len(p), nil
}

func (s *scriptedWaylandConn) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *scriptedWaylandConn) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadline = t
	return nil
}

// Wire-format builders ----------------------------------------------
//
// These helpers assemble the byte sequences that a real compositor
// would send. Keeping them inside the test file (rather than
// reaching into detect_wlroots.go) means the test exercises the
// detector against the published wire format, not against any
// internal helper that could change in lockstep with the parser.

func encodeMessage(sender, opcode uint32, payload []byte) []byte {
	size := uint32(8 + len(payload))
	out := make([]byte, size)
	binary.LittleEndian.PutUint32(out[0:4], sender)
	binary.LittleEndian.PutUint32(out[4:8], (size<<16)|opcode)
	copy(out[8:], payload)
	return out
}

func encodeString(s string) []byte {
	body := append([]byte(s), 0)
	length := uint32(len(body))
	padded := length
	if pad := length & 3; pad != 0 {
		padded += 4 - pad
	}
	out := make([]byte, 4+padded)
	binary.LittleEndian.PutUint32(out[0:4], length)
	copy(out[4:], body)
	return out
}

func encodeArrayUint32(values []uint32) []byte {
	bodyLen := uint32(len(values) * 4)
	padded := bodyLen
	if pad := bodyLen & 3; pad != 0 {
		padded += 4 - pad
	}
	out := make([]byte, 4+padded)
	binary.LittleEndian.PutUint32(out[0:4], bodyLen)
	for i, v := range values {
		binary.LittleEndian.PutUint32(out[4+i*4:8+i*4], v)
	}
	return out
}

// registryGlobalEvent builds a wl_registry.global event (object id 2,
// opcode 0): name(uint32), interface(string), version(uint32).
func registryGlobalEvent(registryID, name uint32, iface string, version uint32) []byte {
	payload := make([]byte, 0, 32)
	payload = binary.LittleEndian.AppendUint32(payload, name)
	payload = append(payload, encodeString(iface)...)
	payload = binary.LittleEndian.AppendUint32(payload, version)
	return encodeMessage(registryID, 0, payload)
}

// callbackDoneEvent builds a wl_callback.done event (opcode 0,
// payload: serial uint32). The callback id is whichever id we
// allocated when issuing the wl_display.sync request.
func callbackDoneEvent(callbackID uint32) []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 0)
	return encodeMessage(callbackID, 0, payload)
}

// managerToplevelEvent builds zwlr_foreign_toplevel_manager_v1.toplevel
// (opcode 0, payload: new_id toplevel handle).
func managerToplevelEvent(managerID, handleID uint32) []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, handleID)
	return encodeMessage(managerID, 0, payload)
}

// handleAppIDEvent builds zwlr_foreign_toplevel_handle_v1.app_id
// (opcode 1, payload: string app_id).
func handleAppIDEvent(handleID uint32, appID string) []byte {
	return encodeMessage(handleID, 1, encodeString(appID))
}

// handleStateEvent builds zwlr_foreign_toplevel_handle_v1.state
// (opcode 4, payload: array<uint32>). Pass stateActivated (=2) to
// mark the toplevel as focused.
func handleStateEvent(handleID uint32, states ...uint32) []byte {
	return encodeMessage(handleID, 4, encodeArrayUint32(states))
}

// handleDoneEvent builds zwlr_foreign_toplevel_handle_v1.done
// (opcode 5, no payload). The detector does not key on done events
// directly — the wl_callback.done from the second sync barrier is
// what unblocks the read loop — but emitting it keeps the test
// stream faithful to a real compositor's output.
func handleDoneEvent(handleID uint32) []byte {
	return encodeMessage(handleID, 5, nil)
}

// Tests --------------------------------------------------------------

const wlrManagerInterface = "zwlr_foreign_toplevel_manager_v1"

func TestDetectWlrootsHappyPath(t *testing.T) {
	const (
		registryID     uint32 = 2
		callback1ID    uint32 = 3
		managerID      uint32 = 4
		callback2ID    uint32 = 5
		toplevelHandle uint32 = 6
		// wlr state enum: activated == 2.
		stateActivated uint32 = 2
		// wlr global "name" the registry advertises.
		managerGlobalName uint32 = 17
	)

	// Round 1: registry global advertising the manager + callback1 done.
	round1 := append([]byte(nil), registryGlobalEvent(registryID, managerGlobalName, wlrManagerInterface, 3)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)

	// Round 2: one toplevel with app_id=ghostty and the activated state.
	round2 := append([]byte(nil), managerToplevelEvent(managerID, toplevelHandle)...)
	round2 = append(round2, handleAppIDEvent(toplevelHandle, "com.mitchellh.ghostty")...)
	round2 = append(round2, handleStateEvent(toplevelHandle, stateActivated)...)
	round2 = append(round2, handleDoneEvent(toplevelHandle)...)
	round2 = append(round2, callbackDoneEvent(callback2ID)...)

	conn := &scriptedWaylandConn{}
	// Stage round1 immediately and round2 after the second client
	// request (the second sync), mirroring real compositor pacing.
	conn.readQueue = append(conn.readQueue, round1)
	conn.onWrite = func(n int) {
		if n == 4 { // get_registry, sync, bind, sync
			conn.mu.Lock()
			conn.readQueue = append(conn.readQueue, round2)
			conn.mu.Unlock()
		}
	}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(path string) (WaylandConn, error) {
			if path != "/run/user/1000/wayland-1" {
				t.Fatalf("dial path = %q, want /run/user/1000/wayland-1", path)
			}
			return conn, nil
		},
	}

	tgt, err := detectWlroots(context.Background(), deps)
	if err != nil {
		t.Fatalf("detectWlroots: %v", err)
	}
	if tgt.DisplayServer != "wayland" {
		t.Errorf("display = %q, want wayland", tgt.DisplayServer)
	}
	if tgt.AppClass != "com.mitchellh.ghostty" {
		t.Errorf("class = %q, want com.mitchellh.ghostty", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppTerminal {
		t.Errorf("type = %v, want AppTerminal", tgt.AppType)
	}
	if conn.deadline.IsZero() {
		t.Error("expected SetDeadline to be called")
	}
}

func TestDetectWlrootsNoFocusedWindow(t *testing.T) {
	const (
		registryID        uint32 = 2
		callback1ID       uint32 = 3
		managerID         uint32 = 4
		callback2ID       uint32 = 5
		toplevelHandle    uint32 = 6
		managerGlobalName uint32 = 17
	)

	round1 := append([]byte(nil), registryGlobalEvent(registryID, managerGlobalName, wlrManagerInterface, 3)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)

	// Round 2: one toplevel exists but its state is empty (not
	// activated). The detector should return errWlrootsNoFocusedWindow.
	round2 := append([]byte(nil), managerToplevelEvent(managerID, toplevelHandle)...)
	round2 = append(round2, handleAppIDEvent(toplevelHandle, "firefox")...)
	round2 = append(round2, handleStateEvent(toplevelHandle)...)
	round2 = append(round2, handleDoneEvent(toplevelHandle)...)
	round2 = append(round2, callbackDoneEvent(callback2ID)...)

	conn := &scriptedWaylandConn{}
	conn.readQueue = append(conn.readQueue, round1)
	conn.onWrite = func(n int) {
		if n == 4 {
			conn.mu.Lock()
			conn.readQueue = append(conn.readQueue, round2)
			conn.mu.Unlock()
		}
	}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) { return conn, nil },
	}

	_, err := detectWlroots(context.Background(), deps)
	if !errors.Is(err, errWlrootsNoFocusedWindow) {
		t.Fatalf("err = %v, want errWlrootsNoFocusedWindow", err)
	}
}

func TestDetectWlrootsProtocolUnsupported(t *testing.T) {
	const (
		registryID  uint32 = 2
		callback1ID uint32 = 3
	)

	// Round 1: registry advertises only wl_compositor — no wlr
	// foreign-toplevel manager. The detector should return
	// errWlrootsProtocolUnsupported and never proceed to bind.
	round1 := append([]byte(nil), registryGlobalEvent(registryID, 1, "wl_compositor", 5)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)

	conn := &scriptedWaylandConn{readQueue: [][]byte{round1}}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) { return conn, nil },
	}

	_, err := detectWlroots(context.Background(), deps)
	if !errors.Is(err, errWlrootsProtocolUnsupported) {
		t.Fatalf("err = %v, want errWlrootsProtocolUnsupported", err)
	}
	// Verify we did not bind anything: only the get_registry +
	// sync requests should appear in the write log (24 bytes total
	// at 12 bytes per request).
	if len(conn.writeLog) != 24 {
		t.Errorf("write log = %d bytes, want 24 (get_registry + sync only)", len(conn.writeLog))
	}
}

func TestDetectWlrootsContextTimeout(t *testing.T) {
	// The conn never returns any data; with a context that is
	// already cancelled, the detector must surface ctx.Err()
	// instead of hanging on Read.
	conn := &scriptedWaylandConn{}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) { return conn, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := detectWlroots(ctx, deps)
	if err == nil {
		t.Fatal("detectWlroots: expected error, got nil")
	}
	// Either the explicit ctx.Err() check or the post-EOF read
	// path is acceptable as the trigger; both indicate the
	// detector honoured cancellation rather than hanging.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		t.Errorf("err = %v, want context.Canceled / io.EOF / net.ErrClosed family", err)
	}
}

func TestDetectWlrootsMissingWaylandDisplay(t *testing.T) {
	deps := Deps{
		EnvGet:      envFunc(map[string]string{}),
		WaylandDial: func(string) (WaylandConn, error) { t.Fatal("dial should not be called"); return nil, nil },
	}
	_, err := detectWlroots(context.Background(), deps)
	if err == nil {
		t.Fatal("expected error for missing WAYLAND_DISPLAY")
	}
}

func TestDetectWlrootsAbsoluteWaylandDisplay(t *testing.T) {
	const (
		registryID        uint32 = 2
		callback1ID       uint32 = 3
		managerGlobalName uint32 = 17
	)
	// Absolute WAYLAND_DISPLAY bypasses XDG_RUNTIME_DIR. The dial
	// hook captures the path so we can assert the resolution rule.
	var dialedPath string
	round1 := append([]byte(nil), registryGlobalEvent(registryID, managerGlobalName, "wl_compositor", 5)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)
	conn := &scriptedWaylandConn{readQueue: [][]byte{round1}}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "/tmp/wayland-direct.sock",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(path string) (WaylandConn, error) {
			dialedPath = path
			return conn, nil
		},
	}

	_, err := detectWlroots(context.Background(), deps)
	if !errors.Is(err, errWlrootsProtocolUnsupported) {
		t.Fatalf("err = %v, want errWlrootsProtocolUnsupported", err)
	}
	if dialedPath != "/tmp/wayland-direct.sock" {
		t.Errorf("dialed = %q, want /tmp/wayland-direct.sock", dialedPath)
	}
}

func TestDetectWlrootsClampVersion(t *testing.T) {
	const (
		registryID        uint32 = 2
		callback1ID       uint32 = 3
		managerID         uint32 = 4
		callback2ID       uint32 = 5
		toplevelHandle    uint32 = 6
		managerGlobalName uint32 = 17
		stateActivated    uint32 = 2
	)
	// Compositor advertises version 99 (newer than we support).
	// The detector must clamp at 3 in the bind request to avoid
	// binding to an interface vocabulary it cannot decode.
	round1 := append([]byte(nil), registryGlobalEvent(registryID, managerGlobalName, wlrManagerInterface, 99)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)

	round2 := append([]byte(nil), managerToplevelEvent(managerID, toplevelHandle)...)
	round2 = append(round2, handleAppIDEvent(toplevelHandle, "code")...)
	round2 = append(round2, handleStateEvent(toplevelHandle, stateActivated)...)
	round2 = append(round2, callbackDoneEvent(callback2ID)...)

	conn := &scriptedWaylandConn{}
	conn.readQueue = append(conn.readQueue, round1)
	conn.onWrite = func(n int) {
		if n == 4 {
			conn.mu.Lock()
			conn.readQueue = append(conn.readQueue, round2)
			conn.mu.Unlock()
		}
	}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) { return conn, nil },
	}

	tgt, err := detectWlroots(context.Background(), deps)
	if err != nil {
		t.Fatalf("detectWlroots: %v", err)
	}
	if tgt.AppType != yinject.AppElectron {
		t.Errorf("type = %v, want AppElectron", tgt.AppType)
	}
	// Inspect the bind request to confirm the version field was
	// clamped to 3. The bind request appears as the third write
	// in the log; its layout is:
	//   [0..4]   sender (registry id = 2)
	//   [4..8]   size<<16 | opcode 0
	//   [8..12]  global name
	//   [12..16] interface string length
	//   [16..16+padded] interface bytes (NUL-padded to 4)
	//   [...]    version uint32
	//   [...]    new_id uint32
	bindStart := 12 + 12 // skip get_registry (12) + sync (12)
	if len(conn.writeLog) < bindStart+8 {
		t.Fatalf("write log too short: %d bytes", len(conn.writeLog))
	}
	bind := conn.writeLog[bindStart:]
	ifaceLen := int(binary.LittleEndian.Uint32(bind[12:16]))
	padded := ifaceLen
	if pad := ifaceLen & 3; pad != 0 {
		padded += 4 - pad
	}
	versionOff := 16 + padded
	version := binary.LittleEndian.Uint32(bind[versionOff : versionOff+4])
	if version != 3 {
		t.Errorf("bind version = %d, want 3 (clamped)", version)
	}
}

func TestDetectWlrootsDialFailure(t *testing.T) {
	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) {
			return nil, errors.New("connect: connection refused")
		},
	}
	_, err := detectWlroots(context.Background(), deps)
	if err == nil {
		t.Fatal("expected dial error to surface")
	}
}

// TestDetectWlrootsWiredIntoFallthrough verifies the Detect()
// dispatcher exercises the wlroots branch when the env points at a
// generic Wayland compositor (no SWAYSOCK, no
// HYPRLAND_INSTANCE_SIGNATURE) and the wayland exchange succeeds.
func TestDetectWlrootsWiredIntoFallthrough(t *testing.T) {
	const (
		registryID        uint32 = 2
		callback1ID       uint32 = 3
		managerID         uint32 = 4
		callback2ID       uint32 = 5
		toplevelHandle    uint32 = 6
		managerGlobalName uint32 = 17
		stateActivated    uint32 = 2
	)
	round1 := append([]byte(nil), registryGlobalEvent(registryID, managerGlobalName, wlrManagerInterface, 3)...)
	round1 = append(round1, callbackDoneEvent(callback1ID)...)
	round2 := append([]byte(nil), managerToplevelEvent(managerID, toplevelHandle)...)
	round2 = append(round2, handleAppIDEvent(toplevelHandle, "firefox")...)
	round2 = append(round2, handleStateEvent(toplevelHandle, stateActivated)...)
	round2 = append(round2, callbackDoneEvent(callback2ID)...)

	conn := &scriptedWaylandConn{}
	conn.readQueue = append(conn.readQueue, round1)
	conn.onWrite = func(n int) {
		if n == 4 {
			conn.mu.Lock()
			conn.readQueue = append(conn.readQueue, round2)
			conn.mu.Unlock()
		}
	}

	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"XDG_RUNTIME_DIR": "/run/user/1000",
		}),
		WaylandDial: func(string) (WaylandConn, error) { return conn, nil },
	}

	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.AppClass != "firefox" {
		t.Errorf("class = %q, want firefox", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppBrowser {
		t.Errorf("type = %v, want AppBrowser", tgt.AppType)
	}
}
