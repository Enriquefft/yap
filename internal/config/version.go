package config

// Version is the running yap binary version. It is reported by
// `yap status` (via the daemon's IPC status response) and by the
// hidden `yap status` no-daemon fallback path so users can identify
// which build they have installed.
//
// Phase 7 ships a hardcoded constant: distribution CI is responsible
// for overriding it via `-ldflags '-X github.com/hybridz/yap/internal/config.Version=...'`
// once Phase 12 wires release tooling. Until then a constant value is
// the single source of truth — there is exactly one place to bump on
// every release.
var Version = "0.1.0-dev"
