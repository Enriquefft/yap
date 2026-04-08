package inject

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// detect_wlroots.go implements generic wlroots active-window detection
// over the wlr-foreign-toplevel-management-unstable-v1 protocol. The
// detector talks to the compositor directly over the wayland UNIX
// socket using a hand-rolled wire-format implementation; this avoids
// pulling a full wayland client library into the dependency tree for
// what is, in practice, a single-shot roundtrip that consumes a
// handful of small messages.
//
// Single source of truth: every protocol constant (object id layout,
// message opcodes, the wlr "activated" state value) is declared inside
// the function or method that uses it. There is no package-level state
// associated with the wayland wire format — the noglobals guard
// (noglobals_test.go) verifies this property at build time.
//
// Latency budget: a hard 500 ms ceiling is applied to the entire
// roundtrip. The budget is enforced by deriving a context with the
// caller's existing deadline (if tighter) and translating ctx.Done()
// into WaylandConn.SetDeadline. There are no background goroutines
// and no asynchronous timers — every blocking I/O is bounded by the
// connection deadline alone.

// errWlrootsProtocolUnsupported signals that the compositor does not
// expose zwlr_foreign_toplevel_manager_v1. The Detect dispatcher
// converts this into a fall-through to the generic-GUI Target.
var errWlrootsProtocolUnsupported = errors.New("inject: compositor does not expose zwlr_foreign_toplevel_manager_v1")

// errWlrootsNoFocusedWindow signals that the protocol is supported but
// no toplevel currently has the activated state. This typically means
// the user is on the desktop with no window in focus.
var errWlrootsNoFocusedWindow = errors.New("inject: no focused wlroots toplevel")

// detectWlrootsLatencyBudget is the hard ceiling on a single detection
// roundtrip. It is intentionally fixed (not a Deps-tunable knob)
// because the value is dictated by Pillar 2's perceived-latency
// requirement: text injection must feel synchronous, and a half-second
// floor is the upper limit users tolerate before the daemon's
// "transcribe → inject" loop feels broken.
const detectWlrootsLatencyBudget = 500 * time.Millisecond

// detectWlroots opens a wayland connection, binds the wlr foreign
// toplevel manager, and returns the focused toplevel's classified
// Target. The detector returns errWlrootsProtocolUnsupported when the
// compositor does not advertise the manager (caller falls through to
// the generic-GUI strategy), errWlrootsNoFocusedWindow when no
// toplevel is activated, and any other error for socket-level
// failures.
func detectWlroots(ctx context.Context, deps Deps) (yinject.Target, error) {
	if deps.WaylandDial == nil {
		return yinject.Target{}, errors.New("inject: WaylandDial dep is nil")
	}
	socketPath, err := resolveWaylandSocketPath(deps)
	if err != nil {
		return yinject.Target{}, err
	}

	// Derive a per-call deadline. The 500 ms ceiling is the hard
	// upper bound; if the caller already supplied a tighter deadline
	// we honour theirs.
	now := time.Now
	if deps.Now != nil {
		now = deps.Now
	}
	budget := now().Add(detectWlrootsLatencyBudget)
	if d, ok := ctx.Deadline(); ok && d.Before(budget) {
		budget = d
	}

	conn, err := deps.WaylandDial(socketPath)
	if err != nil {
		return yinject.Target{}, fmt.Errorf("wayland dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(budget); err != nil {
		return yinject.Target{}, fmt.Errorf("wayland set deadline: %w", err)
	}

	det := wlrDetector{
		conn:     conn,
		ctx:      ctx,
		toplevel: map[uint32]*wlrToplevelState{},
	}
	return det.run()
}

// resolveWaylandSocketPath turns the (XDG_RUNTIME_DIR, WAYLAND_DISPLAY)
// pair into an absolute UNIX socket path. WAYLAND_DISPLAY may itself
// already be absolute (uncommon but spec-permitted), in which case
// XDG_RUNTIME_DIR is ignored. Either var being empty is a hard error.
func resolveWaylandSocketPath(deps Deps) (string, error) {
	display := deps.EnvGet("WAYLAND_DISPLAY")
	if display == "" {
		return "", errors.New("inject: WAYLAND_DISPLAY unset")
	}
	if filepath.IsAbs(display) {
		return display, nil
	}
	runtimeDir := deps.EnvGet("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", errors.New("inject: XDG_RUNTIME_DIR unset")
	}
	return filepath.Join(runtimeDir, display), nil
}

// wlrToplevelState collects the events received for a single
// zwlr_foreign_toplevel_handle_v1 between its toplevel-created event
// and the matching done event. The detector picks the first toplevel
// whose state array contains the wlr "activated" value.
type wlrToplevelState struct {
	appID     string
	activated bool
}

// wlrDetector encapsulates the per-call wayland exchange. The struct
// is intentionally short-lived: one instance per detectWlroots call,
// allocated on the stack of run(). It owns the connection-local
// object-id allocator and the in-flight toplevel map.
//
// All wire-format constants live as method-local consts so the
// noglobals guard stays satisfied without sacrificing readability.
type wlrDetector struct {
	conn       WaylandConn
	ctx        context.Context
	rxBuf      []byte
	nextID     uint32
	managerID  uint32
	managerVer uint32
	toplevel   map[uint32]*wlrToplevelState
}

// run performs the full exchange:
//
//  1. Send wl_display.get_registry → object 2.
//  2. Send wl_display.sync(callback=3) so we know when the registry
//     finishes enumerating globals.
//  3. Read messages until callback 3 fires; remember whether
//     zwlr_foreign_toplevel_manager_v1 was advertised.
//  4. If absent, return errWlrootsProtocolUnsupported.
//  5. Otherwise wl_registry.bind it to object 4 and send another
//     wl_display.sync(callback=5).
//  6. Read messages until callback 5 fires, accumulating per-handle
//     app_id and state events.
//  7. Send manager.stop and rely on the deferred conn.Close() to
//     finish tearing down server-side state.
//  8. Return the activated toplevel as a classified Target, or
//     errWlrootsNoFocusedWindow if none was activated.
func (d *wlrDetector) run() (yinject.Target, error) {
	// Object id 1 is wl_display by spec; we start allocating at 2.
	d.nextID = 2

	registryID := d.allocID()
	if err := d.sendGetRegistry(registryID); err != nil {
		return yinject.Target{}, err
	}
	syncCallbackID := d.allocID()
	if err := d.sendDisplaySync(syncCallbackID); err != nil {
		return yinject.Target{}, err
	}

	if err := d.readUntilCallback(syncCallbackID, d.handleRegistryEvent(registryID)); err != nil {
		return yinject.Target{}, err
	}

	if d.managerID == 0 {
		return yinject.Target{}, errWlrootsProtocolUnsupported
	}

	managerObjectID := d.allocID()
	if err := d.sendRegistryBind(registryID, d.managerID, "zwlr_foreign_toplevel_manager_v1", d.managerVer, managerObjectID); err != nil {
		return yinject.Target{}, err
	}
	roundtrip2 := d.allocID()
	if err := d.sendDisplaySync(roundtrip2); err != nil {
		return yinject.Target{}, err
	}

	if err := d.readUntilCallback(roundtrip2, d.handleManagerEvent(managerObjectID)); err != nil {
		return yinject.Target{}, err
	}

	// Best-effort clean shutdown. We send manager.stop so the
	// compositor stops dispatching toplevel events on this object;
	// the manager has no destroy request in v1-v3 (the server emits
	// the finished event and tears the object down on its side),
	// so closing the connection — which the deferred Close() does —
	// is the only way to fully release the handle. Errors here are
	// non-fatal: the conn is about to be torn down anyway.
	_ = d.sendManagerStop(managerObjectID)

	for _, t := range d.toplevel {
		if t.activated && t.appID != "" {
			return classifyAndBuildTarget("wayland", t.appID, ""), nil
		}
	}
	return yinject.Target{}, errWlrootsNoFocusedWindow
}

func (d *wlrDetector) allocID() uint32 {
	id := d.nextID
	d.nextID++
	return id
}

// sendGetRegistry sends wl_display.get_registry(new_id registry).
// wl_display has object id 1 by spec; opcode 1 is get_registry.
func (d *wlrDetector) sendGetRegistry(registryID uint32) error {
	const (
		displayObjectID          uint32 = 1
		displayGetRegistryOpcode uint32 = 1
	)
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], displayObjectID)
	binary.LittleEndian.PutUint32(buf[4:8], (uint32(12)<<16)|displayGetRegistryOpcode)
	binary.LittleEndian.PutUint32(buf[8:12], registryID)
	return d.write(buf)
}

// sendDisplaySync sends wl_display.sync(new_id callback). Opcode 0.
// We use the returned wl_callback's done event as a barrier so the
// detector knows the compositor has finished sending all globals or
// all initial toplevel events.
func (d *wlrDetector) sendDisplaySync(callbackID uint32) error {
	const (
		displayObjectID   uint32 = 1
		displaySyncOpcode uint32 = 0
	)
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], displayObjectID)
	binary.LittleEndian.PutUint32(buf[4:8], (uint32(12)<<16)|displaySyncOpcode)
	binary.LittleEndian.PutUint32(buf[8:12], callbackID)
	return d.write(buf)
}

// sendRegistryBind sends wl_registry.bind(name, interface, version,
// new_id). The bind request takes a "new_id" with the interface name
// and version inlined as additional arguments — this is the only
// place in the wayland protocol where the typed-new-id encoding
// differs from the standard new_id arg. The wire format is:
//
//	uint32 sender_id  | uint32 size<<16 | opcode
//	uint32 name
//	uint32 iface_strlen (incl. NUL) | iface_bytes (NUL-padded to 4)
//	uint32 version
//	uint32 new_id
//
// Opcode 0 is the only request on wl_registry.
func (d *wlrDetector) sendRegistryBind(registryID, name uint32, iface string, version, newID uint32) error {
	const registryBindOpcode uint32 = 0
	ifaceBytes := append([]byte(iface), 0)
	ifacePadded := paddedLen(len(ifaceBytes))
	totalLen := 8 + 4 + 4 + ifacePadded + 4 + 4
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(buf[0:4], registryID)
	binary.LittleEndian.PutUint32(buf[4:8], (uint32(totalLen)<<16)|registryBindOpcode)
	binary.LittleEndian.PutUint32(buf[8:12], name)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(len(ifaceBytes)))
	copy(buf[16:16+len(ifaceBytes)], ifaceBytes)
	off := 16 + ifacePadded
	binary.LittleEndian.PutUint32(buf[off:off+4], version)
	binary.LittleEndian.PutUint32(buf[off+4:off+8], newID)
	return d.write(buf)
}

// sendManagerStop sends zwlr_foreign_toplevel_manager_v1.stop. Opcode
// 0 is the only request on the manager interface.
func (d *wlrDetector) sendManagerStop(managerID uint32) error {
	const managerStopOpcode uint32 = 0
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], managerID)
	binary.LittleEndian.PutUint32(buf[4:8], (uint32(8)<<16)|managerStopOpcode)
	return d.write(buf)
}

// write pushes a complete wayland message to the socket. The deadline
// is already armed by detectWlroots; if it expires, the underlying
// Write returns net.ErrClosed-equivalent and we propagate it.
func (d *wlrDetector) write(buf []byte) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	off := 0
	for off < len(buf) {
		n, err := d.conn.Write(buf[off:])
		if err != nil {
			return fmt.Errorf("wayland write: %w", err)
		}
		off += n
	}
	return nil
}

// readUntilCallback reads messages from the socket and dispatches
// them through handleEvent until a wl_callback.done event arrives for
// the supplied callback id. handleEvent receives the (sender, opcode,
// payload) tuple and returns an error to abort the loop early.
func (d *wlrDetector) readUntilCallback(callbackID uint32, handleEvent func(sender, opcode uint32, payload []byte) error) error {
	const (
		callbackDoneOpcode uint32 = 0
	)
	for {
		if err := d.ctx.Err(); err != nil {
			return err
		}
		sender, opcode, payload, err := d.readMessage()
		if err != nil {
			return err
		}
		if sender == callbackID && opcode == callbackDoneOpcode {
			return nil
		}
		if err := d.handleDisplayEvent(sender, opcode, payload); err != nil {
			return err
		}
		if err := handleEvent(sender, opcode, payload); err != nil {
			return err
		}
	}
}

// handleDisplayEvent intercepts wl_display.error events (object id 1,
// opcode 0) so a protocol violation surfaces as a real Go error
// instead of an indefinite hang waiting for a callback that will
// never arrive. wl_display.delete_id (opcode 1) is acknowledged but
// otherwise ignored — we never reuse object ids inside a single
// detection call.
func (d *wlrDetector) handleDisplayEvent(sender, opcode uint32, payload []byte) error {
	const (
		displayObjectID    uint32 = 1
		displayErrorOpcode uint32 = 0
	)
	if sender != displayObjectID {
		return nil
	}
	if opcode == displayErrorOpcode {
		// payload: object_id (u32), code (u32), message (string).
		if len(payload) < 12 {
			return errors.New("wayland display.error: short payload")
		}
		objectID := binary.LittleEndian.Uint32(payload[0:4])
		code := binary.LittleEndian.Uint32(payload[4:8])
		msg, _, err := readString(payload[8:])
		if err != nil {
			return fmt.Errorf("wayland display.error: %w", err)
		}
		return fmt.Errorf("wayland display.error: object=%d code=%d msg=%q", objectID, code, msg)
	}
	return nil
}

// handleRegistryEvent returns the event handler used while listening
// for wl_registry events. wl_registry has two events:
//
//	opcode 0 — global(name uint32, interface string, version uint32)
//	opcode 1 — global_remove(name uint32)
//
// We only care about opcode 0 with interface ==
// "zwlr_foreign_toplevel_manager_v1". The version is clamped at 3
// (the highest supported by the protocol XML at the time of writing);
// clamping is necessary because compositors may advertise newer
// versions than we know how to parse, and binding to an unsupported
// version would deserialize state events with extra fields we don't
// understand.
func (d *wlrDetector) handleRegistryEvent(registryID uint32) func(sender, opcode uint32, payload []byte) error {
	const (
		registryGlobalOpcode uint32 = 0
		managerInterfaceName        = "zwlr_foreign_toplevel_manager_v1"
		managerMaxVersion    uint32 = 3
	)
	return func(sender, opcode uint32, payload []byte) error {
		if sender != registryID || opcode != registryGlobalOpcode {
			return nil
		}
		if len(payload) < 4 {
			return errors.New("wayland registry.global: short payload")
		}
		name := binary.LittleEndian.Uint32(payload[0:4])
		iface, rest, err := readString(payload[4:])
		if err != nil {
			return fmt.Errorf("wayland registry.global: %w", err)
		}
		if iface != managerInterfaceName {
			return nil
		}
		if len(rest) < 4 {
			return errors.New("wayland registry.global: missing version")
		}
		version := binary.LittleEndian.Uint32(rest[0:4])
		if version > managerMaxVersion {
			version = managerMaxVersion
		}
		d.managerID = name
		d.managerVer = version
		return nil
	}
}

// handleManagerEvent returns the event handler used while listening
// for toplevel + per-handle events. We close over the manager's
// object id so we know which sender announces new toplevels vs.
// which senders are individual handles.
//
// The wlr-foreign-toplevel-management v3 event opcodes are:
//
//	zwlr_foreign_toplevel_manager_v1
//	  0 — toplevel(new_id handle)
//	  1 — finished              [unused: we close the connection]
//	zwlr_foreign_toplevel_handle_v1
//	  0 — title(string)         [unused: we key on app_id]
//	  1 — app_id(string)
//	  2 — output_enter(object)  [unused]
//	  3 — output_leave(object)  [unused]
//	  4 — state(array<uint32>)
//	  5 — done                  [unused: the second sync barrier
//	                             signals "all initial state delivered"
//	                             across the whole manager]
//	  6 — closed                [unused: per-call lifetime]
//	  7 — parent(object)        [v3, unused]
//
// We declare only the opcodes we actually consume; the rest live in
// the comment above so a reader can cross-reference the protocol
// without grep-ing the XML.
func (d *wlrDetector) handleManagerEvent(managerID uint32) func(sender, opcode uint32, payload []byte) error {
	const (
		managerToplevelOpcode uint32 = 0
		handleAppIDOpcode     uint32 = 1
		handleStateOpcode     uint32 = 4
		// wlr state enum, "activated" entry.
		stateActivated uint32 = 2
	)

	return func(sender, opcode uint32, payload []byte) error {
		if sender == managerID {
			if opcode == managerToplevelOpcode {
				if len(payload) < 4 {
					return errors.New("wayland manager.toplevel: short payload")
				}
				toplevelID := binary.LittleEndian.Uint32(payload[0:4])
				d.toplevel[toplevelID] = &wlrToplevelState{}
			}
			return nil
		}
		state, known := d.toplevel[sender]
		if !known {
			return nil
		}
		switch opcode {
		case handleAppIDOpcode:
			appID, _, err := readString(payload)
			if err != nil {
				return fmt.Errorf("wayland handle.app_id: %w", err)
			}
			state.appID = appID
		case handleStateOpcode:
			values, err := readArrayUint32(payload)
			if err != nil {
				return fmt.Errorf("wayland handle.state: %w", err)
			}
			for _, v := range values {
				if v == stateActivated {
					state.activated = true
					break
				}
			}
		}
		return nil
	}
}

// readMessage parses one full wayland message from the socket. The
// internal buffer is reused across calls so a single Read() that
// returned multiple messages does not require multiple syscalls to
// drain.
func (d *wlrDetector) readMessage() (sender, opcode uint32, payload []byte, err error) {
	for {
		if len(d.rxBuf) >= 8 {
			size := binary.LittleEndian.Uint32(d.rxBuf[4:8]) >> 16
			if size < 8 {
				return 0, 0, nil, fmt.Errorf("wayland message size %d below header", size)
			}
			if uint32(len(d.rxBuf)) >= size {
				sender = binary.LittleEndian.Uint32(d.rxBuf[0:4])
				opcode = binary.LittleEndian.Uint32(d.rxBuf[4:8]) & 0xffff
				payload = make([]byte, int(size)-8)
				copy(payload, d.rxBuf[8:size])
				d.rxBuf = d.rxBuf[size:]
				return sender, opcode, payload, nil
			}
		}
		if err := d.ctx.Err(); err != nil {
			return 0, 0, nil, err
		}
		chunk := make([]byte, 4096)
		n, rerr := d.conn.Read(chunk)
		if n > 0 {
			d.rxBuf = append(d.rxBuf, chunk[:n]...)
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) && len(d.rxBuf) >= 8 {
				continue
			}
			return 0, 0, nil, fmt.Errorf("wayland read: %w", rerr)
		}
	}
}

// paddedLen rounds n up to the nearest multiple of 4. Wayland strings
// and arrays are 32-bit aligned on the wire.
func paddedLen(n int) int {
	if n&3 == 0 {
		return n
	}
	return n + (4 - n&3)
}

// readString parses a wayland string out of the front of payload and
// returns it together with the remaining bytes. The wire format is
// (uint32 length, length bytes (NUL-terminated), padding to 4 bytes).
// length includes the NUL.
func readString(payload []byte) (string, []byte, error) {
	if len(payload) < 4 {
		return "", nil, errors.New("string: short header")
	}
	length := binary.LittleEndian.Uint32(payload[0:4])
	padded := paddedLen(int(length))
	if uint32(len(payload)-4) < uint32(padded) {
		return "", nil, errors.New("string: short body")
	}
	if length == 0 {
		return "", payload[4+padded:], nil
	}
	body := payload[4 : 4+length-1] // strip trailing NUL
	return string(body), payload[4+padded:], nil
}

// readArrayUint32 parses a wayland array of uint32 out of payload.
// State arrays in wlr-foreign-toplevel-management are encoded this
// way. The wire format is identical to a string except the body is
// raw bytes instead of NUL-terminated.
func readArrayUint32(payload []byte) ([]uint32, error) {
	if len(payload) < 4 {
		return nil, errors.New("array: short header")
	}
	length := binary.LittleEndian.Uint32(payload[0:4])
	padded := paddedLen(int(length))
	if uint32(len(payload)-4) < uint32(padded) {
		return nil, errors.New("array: short body")
	}
	if length%4 != 0 {
		return nil, fmt.Errorf("array: length %d not a multiple of 4", length)
	}
	out := make([]uint32, length/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(payload[4+i*4 : 8+i*4])
	}
	return out, nil
}
