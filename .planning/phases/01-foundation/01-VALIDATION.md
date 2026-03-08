---
phase: 1
slug: foundation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-07
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — Wave 0 creates `internal/config/config_test.go` |
| **Quick run command** | `go test ./...` |
| **Full suite command** | `go test -v ./... && ldd ./yap` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./...`
- **After every plan wave:** Run `go test -v ./... && ldd ./yap`
- **Before `/gsd:verify-work`:** Full suite must be green + `ldd ./yap` outputs `not a dynamic executable`
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 1-01-01 | 01 | 1 | NFR-01 | build | `go build ./cmd/yap && ldd ./yap \| grep -c 'not a dynamic' \|\| true` | ❌ W0 | ⬜ pending |
| 1-01-02 | 01 | 1 | DIST-02 | build | `nix build && ls result/bin/yap` | ❌ W0 | ⬜ pending |
| 1-01-03 | 01 | 1 | NFR-02 | unit | `go test ./cmd/...` | ❌ W0 | ⬜ pending |
| 1-02-01 | 02 | 1 | CONFIG-01 | unit | `go test ./internal/config/...` | ❌ W0 | ⬜ pending |
| 1-02-02 | 02 | 1 | CONFIG-02 | unit | `go test ./internal/config/... -run TestXDGPaths` | ❌ W0 | ⬜ pending |
| 1-02-03 | 02 | 1 | CONFIG-03 | unit | `go test ./internal/config/... -run TestMissingConfig` | ❌ W0 | ⬜ pending |
| 1-02-04 | 02 | 1 | CONFIG-04 | unit | `go test ./internal/config/... -run TestEnvOverride` | ❌ W0 | ⬜ pending |
| 1-02-05 | 02 | 1 | CONFIG-05 | unit | `go test ./internal/config/... -run TestTOMLParse` | ❌ W0 | ⬜ pending |
| 1-03-01 | 03 | 2 | ASSETS-01 | unit | `go test ./internal/assets/... -run TestEmbedded` | ❌ W0 | ⬜ pending |
| 1-03-02 | 03 | 2 | ASSETS-02 | unit | `go test ./internal/assets/... -run TestChimeSize` | ❌ W0 | ⬜ pending |
| 1-03-03 | 03 | 2 | NFR-05 | manual | `ls -la assets/*.wav` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/config/config_test.go` — stubs for CONFIG-01 through CONFIG-05
- [ ] `internal/assets/assets_test.go` — stubs for ASSETS-01, ASSETS-02
- [ ] `cmd/yap/main_test.go` — stub for cobra subcommand tree test
- [ ] `go.mod` initialized with module name `github.com/yourusername/yap`

*Framework `go test` is stdlib — no install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `nix build` produces runnable binary | DIST-02 | Requires Nix installed in environment | Run `nix build && ./result/bin/yap --help` |
| `ldd ./yap` shows static binary | NFR-01 | OS-level binary inspection | Run `ldd ./yap` and verify output is `not a dynamic executable` |
| `yap --help` shows all subcommands | NFR-02 | CLI integration check | Run `./yap --help` and verify `start`, `stop`, `status`, `toggle`, `config` are listed |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
