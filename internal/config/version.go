// Package config holds the version string for the yap binary.
//
// Version is a package-level variable (not a const) so release builds
// can override it via ldflags:
//
//	go build -ldflags "-X github.com/hybridz/yap/internal/config.Version=v0.2.0" ./cmd/yap
//
// The Makefile passes the value from `git describe --tags --always --dirty`,
// and the Nix flake threads its declared package version through the same
// linker flag. A raw `go build` with no ldflags falls back to the "dev"
// sentinel below so unreleased builds are unambiguous in `yap status`
// output.
package config

// Version is the running yap binary version. It is reported by
// `yap status` (via the daemon's IPC status response) and the
// no-daemon fallback path so users can identify which build they have
// installed. Release tooling overrides this via -ldflags; raw
// `go build` invocations leave it at "dev".
var Version = "dev"
