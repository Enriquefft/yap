package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/audio"
	"github.com/hybridz/yap/internal/assets"
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/hotkey"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/notify"
	"github.com/hybridz/yap/internal/paste"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/hybridz/yap/internal/transcribe"
	"github.com/adrg/xdg"
	"github.com/holoplot/go-evdev"
)

// Package-level variables for testability
var (
	audioPlayChime     = audio.PlayChime
	audioNewRecorder   = audio.NewRecorder
	hotkeyFindKeyboards = hotkey.FindKeyboards
	hotkeyHotkeyCode    = hotkey.HotkeyCode
	transcribeTranscribe = transcribe.Transcribe
	pastePaste          = paste.Paste
	notifyOnTranscriptionError = notify.OnTranscriptionError
	notifyOnPermissionError    = notify.OnPermissionError
	notifyOnDeviceError        = notify.OnDeviceError
	xdgDataFile              = xdg.DataFile
	pidfileWrite             = pidfile.Write
	pidfileRemove            = pidfile.Remove
	ipcNewServer            = ipc.NewServer
)

// recordState holds the recording state machine.
type recordState struct {
	mu     sync.Mutex
	active bool
	cancel context.CancelFunc
}

// isActive returns true if recording is active.
func (rs *recordState) isActive() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.active
}

// setIsActive sets the recording active state.
func (rs *recordState) setIsActive(active bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.active = active
}

// setCancel sets the cancel function for the current recording.
func (rs *recordState) setCancel(cancel context.CancelFunc) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.cancel = cancel
}

// cancelRecording cancels the current recording if active.
func (rs *recordState) cancelRecording() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.cancel != nil {
		rs.cancel()
		rs.cancel = nil
	}
	rs.active = false
}

// Daemon represents the background process.
type Daemon struct {
	cfg        *config.Config
	state      recordState
	recorder   audio.AudioRecorder
	toggleFn   func() string
}

// New creates a new Daemon.
func New(cfg *config.Config) *Daemon {
	return &Daemon{cfg: cfg}
}

// Run starts the daemon event loop and blocks until SIGTERM.
// All cleanup (PortAudio, PID file removal) is deferred and guaranteed to execute.
//
// Sequence:
// 1. Resolve PID path via xdg.DataFile ("yap/yap.pid")
// 2. Write PID using O_EXCL atomic create (DAEMON-01, DAEMON-05)
// 3. Init PortAudio and Recorder (audio.NewRecorder)
// 4. Defer cleanup: Recorder.Close() runs before return (AUDIO-07)
// 5. Defer cleanup: pidfile.Remove() runs before return
// 6. Setup signal.NotifyContext for SIGTERM/SIGINT (DAEMON-04)
// 7. Start IPC server in goroutine
// 8. Start hotkey listener in goroutine
// 9. Block on <-ctx.Done()
// 10. Return with all defers executing
//
// Reference: github.com/adrg/xdg creates $XDG_DATA_HOME/yap automatically.
func Run(cfg *config.Config) error {
	pidPath, err := xdgDataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	// Write PID using O_EXCL for atomic creation (prevents DAEMON-05 race).
	if err := pidfileWrite(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer pidfileRemove(pidPath)

	// Init PortAudio and create Recorder (AUDIO-07: deferred cleanup).
	rec, err := audioNewRecorder(cfg.MicDevice)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	defer rec.Close()

	// Signal-driven shutdown: SIGTERM or SIGINT cancels context.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Initialize hotkey listener
	listener, err := hotkeyFindKeyboards()
	if err != nil {
		if os.IsPermission(err) {
			notifyOnPermissionError()
		}
		return fmt.Errorf("hotkey setup: %w", err)
	}
	defer listener.Close()

	// Parse hotkey code from config
	hotkeyCode, err := hotkeyHotkeyCode(cfg.Hotkey)
	if err != nil {
		return fmt.Errorf("invalid hotkey %q: %w", cfg.Hotkey, err)
	}

	// Start IPC server
	sockPath, err := xdgDataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	srv, err := ipcNewServer(sockPath)
	if err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer srv.Close()

	// Create daemon instance with state
	d := &Daemon{
		cfg:      cfg,
		recorder: rec,
	}

	// Set toggle function for IPC
	srv.SetToggleFn(d.toggleRecording)
	srv.SetStatusFn(func() string {
		if d.state.isActive() {
			return "recording"
		}
		return "idle"
	})

	// Start IPC server in goroutine
	go srv.Serve(ctx)

	// Define press and release callbacks for hotkey
	onPress := func() {
		// Start recording if not already recording
		if d.state.isActive() {
			return
		}

		// Play start chime
		chime, err := assets.StartChime()
		if err == nil {
			audioPlayChime(chime)
		}

		// Start recording in goroutine
		recCtx, recCancel := context.WithTimeout(ctx, 60*time.Second)
		d.state.setCancel(recCancel)
		d.state.setIsActive(true)

		go d.recordAndTranscribe(recCtx, recCancel)
	}

	onRelease := func() {
		// Stop recording if active
		if !d.state.isActive() {
			return
		}

		// Cancel recording context
		d.state.cancelRecording()

		// Play stop chime
		chime, err := assets.StopChime()
		if err == nil {
			audioPlayChime(chime)
		}
	}

	// Start hotkey listener in goroutine
	go listener.Run(ctx, hotkeyCode, onPress, onRelease)

	// Block until signal received.
	<-ctx.Done()

	// All defers execute as we return: rec.Close(), pidfile.Remove()
	return nil
}

// recordAndTranscribe runs the recording and transcription pipeline.
func (d *Daemon) recordAndTranscribe(ctx context.Context, cancel context.CancelFunc) {
	defer func() {
		d.state.setIsActive(false)
	}()

	// Start 50s warning timer
	warningTimer := time.AfterFunc(50*time.Second, func() {
		chime, err := assets.WarningChime()
		if err == nil {
			audioPlayChime(chime)
		}
	})
	defer warningTimer.Stop()

	// Record audio
	if err := d.recorder.Start(ctx); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		notifyOnDeviceError(err)
		return
	}

	// Transcribe audio
	wavData, err := d.recorder.Encode()
	if err != nil {
		notifyOnDeviceError(err)
		return
	}

	text, err := transcribeTranscribe(ctx, d.cfg.APIKey, wavData, d.cfg.Language)
	if err != nil {
		notifyOnTranscriptionError(err)
		return
	}

	// Paste text at cursor
	if err := pastePaste(text); err != nil {
		// Paste errors are logged by the paste package
		// Text remains in clipboard for manual paste
		return
	}
}

// toggleRecording toggles recording state for IPC toggle command.
// Returns new state: "recording" or "idle".
func (d *Daemon) toggleRecording() string {
	if d.state.isActive() {
		// Cancel current recording
		d.state.cancelRecording()

		// Play stop chime
		chime, err := assets.StopChime()
		if err == nil {
			audioPlayChime(chime)
		}

		return "idle"
	}

	// Start recording
	recCtx, recCancel := context.WithTimeout(context.Background(), 60*time.Second)
	d.state.setCancel(recCancel)
	d.state.setIsActive(true)

	// Play start chime
	chime, err := assets.StartChime()
	if err == nil {
		audioPlayChime(chime)
	}

	// Start recording in goroutine
	go d.recordAndTranscribe(recCtx, recCancel)

	return "recording"
}
