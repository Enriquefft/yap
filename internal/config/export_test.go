package config

import "sync"

// ResetMigrationNoticeForTest restores the one-shot migration notice
// guard so a single test binary can exercise the notice path more
// than once. Production code never calls this.
func ResetMigrationNoticeForTest() {
	migrationNoticeOnce = sync.Once{}
}
