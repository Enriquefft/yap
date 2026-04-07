package config

import "sync"

// ResetMigrationNoticeForTest restores the one-shot migration notice
// guard so a single test binary can exercise the notice path more
// than once. Production code never calls this.
func ResetMigrationNoticeForTest() {
	migrationNoticeOnce = sync.Once{}
}

// ResetGroqDeprecationNoticeForTest restores the one-shot Phase 6
// deprecation notice guard. Production code never calls this.
func ResetGroqDeprecationNoticeForTest() {
	groqDeprecationNoticeOnce = sync.Once{}
}
