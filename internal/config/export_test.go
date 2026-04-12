package config

import "sync"

// ResetMigrationNoticeForTest restores the one-shot migration notice
// guard so a single test binary can exercise the notice path more
// than once. Production code never calls this.
func ResetMigrationNoticeForTest() {
	migrationNoticeOnce = sync.Once{}
}

// ResetShadowWarningForTest and SetSystemConfigPathForTest live in
// testhooks.go (a non-_test.go file) because they must be callable
// from cross-package tests such as internal/cli. Go's _test.go
// visibility rules only export symbols to the same package's test
// binary, which would prevent the cli tests from accessing them.
