package config

import "sync"

// This file exposes test hooks that must be callable from OUTSIDE
// the config package (e.g. internal/cli tests that exercise
// `yap config path` end-to-end). Helpers only needed within the
// config package's own tests belong in export_test.go instead;
// helpers that must be cross-package live here.
//
// Nothing in this file is called from production code paths. The
// names end in "ForTest" and every function has a doc comment
// stating so. Keeping them in a regular (non-`_test.go`) file is
// unavoidable because Go's _test.go visibility rules only export
// symbols within the same package's test binary.

// ResetShadowWarningForTest restores the one-shot shadow warning
// guard so a single test binary can exercise the warning path more
// than once. Production code never calls this.
func ResetShadowWarningForTest() {
	shadowWarningOnce = sync.Once{}
}

// SetSystemConfigPathForTest points the package-level systemConfigPath
// at path and returns a cleanup closure that restores the previous
// value. Tests use this to simulate the NixOS-managed file from a
// TempDir without needing root.
//
// Production code never calls this — the production default is the
// fixed FHS path "/etc/yap/config.toml" set at package init.
func SetSystemConfigPathForTest(path string) func() {
	orig := systemConfigPath
	systemConfigPath = path
	return func() { systemConfigPath = orig }
}
