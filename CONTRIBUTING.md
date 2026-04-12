# Contributing to yap

Issues and pull requests are welcome. This document covers everything you need to get a working development environment, understand the codebase, and submit changes that pass CI.

## Prerequisites

- **Go 1.25+** -- required for building from source.
- **Linux** -- required for full testing. The daemon, hotkey listener, audio capture, and injection layer are Linux-only today. macOS and Windows support are on the roadmap (Phases 15-16).
- **Nix (recommended)** -- provides a reproducible devShell with Go, ffmpeg, whisper-cpp, alsa-utils, and every C library needed for the build. Install with flakes enabled:

```bash
curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh
```

If you use Nix, that is the only global dependency. Without Nix, you need Go 1.25+, a C compiler (for CGo/malgo), and optionally `musl-gcc` for static builds.

## Development Setup

```bash
git clone https://github.com/Enriquefft/yap.git
cd yap
nix develop          # drops you into a shell with Go, tooling, and all native deps
```

If you use [direnv](https://direnv.net/), the included `.envrc` activates the devShell automatically.

## Building

```bash
# Dynamic build (development)
nix develop --command go build ./cmd/yap
# or
make build

# Static build (musl, production binary)
nix develop .#static --command make build-static
# or equivalently:
nix build .#static
```

The static binary must remain under 20 MB. CI enforces this via `make build-check` (static build + `ldd` verification + size check). If your change increases binary size significantly, investigate before submitting.

## Testing

```bash
nix develop --command go test ./...
# or
make test
```

Every package has tests. Many packages include `noglobals_test.go` AST guards that walk production `.go` files and fail the build if package-level mutable state is introduced. Do not remove or weaken these guards.

## CI

GitHub Actions runs on every push to `main` and every pull request. The pipeline:

1. `go test ./...` -- full test suite
2. `make build-static` -- musl static binary
3. `make verify-static size-check` -- confirms static linking and the <20 MB size constraint

Your PR must pass all three before review.

## Code Style and Conventions

**Go conventions apply.** `gofmt` is the formatter. No linter config file exists; the codebase relies on the compiler, `go vet`, and the AST-based no-globals guards for correctness.

**No package-level mutable state.** Every dependency is injected via constructors. The only exception is the three backend registries (`transcribe`, `transform`, `hint`), each of which has an explicitly whitelisted `registryMu`/`registry` pair. AST guards enforce this on every build. If you add a `var` at package scope, the build will fail.

**Constructor injection, not monkey-patching.** Tests construct fresh instances with real or mock backends. There are no `SetXForTest` functions.

**Error handling:** wrap errors with context (`fmt.Errorf("encode: %w", err)`). Do not swallow errors silently.

**Logging:** use `log/slog` with structured fields. The injector takes a `*slog.Logger` at construction; follow the same pattern.

## Commit Conventions

yap uses [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short description

Optional body explaining why, not what.
```

Common types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `perf`, `revert`.

Scope is typically a package name or phase name (e.g., `hint`, `phase12`, `nix`, `readme`). Look at `git log --oneline` for the established style.

Keep the subject line under 72 characters. Use imperative mood ("add", not "added").

## Pull Request Process

1. Branch from `main`. Name your branch descriptively (e.g., `feat/history-backend`, `fix/osc52-tmux-wrap`).
2. Make your changes. Ensure `go test ./...` and `make build-static` pass locally before pushing.
3. Open a PR against `main`. Describe what changed and why. Reference any relevant issue or roadmap phase.
4. CI must pass. Address review feedback with additional commits (do not force-push during review).
5. Squash-merge is the default merge strategy.

## Architecture Overview

Read these before diving into code:

- [`ARCHITECTURE.md`](ARCHITECTURE.md) -- the system design, module layout, interface contracts, data flow diagrams, and key technical decisions. This is the single source of truth for what yap is.
- [`ROADMAP.md`](ROADMAP.md) -- the phased plan with 18 phases (0-17). Check which phases are done and which are pending before starting work on a new area.

### Where to find what

| Area | Location |
|---|---|
| Binary entry point | `cmd/yap/main.go` |
| CLI commands | `internal/cli/` (thin Cobra wrappers, no pipeline logic) |
| Pipeline orchestrator | `internal/engine/engine.go` (channels, zero backend imports) |
| Daemon lifecycle | `internal/daemon/daemon.go` |
| Public library | `pkg/yap/` (transcribe, transform, inject, hint, silence, audioprep, config) |
| Transcription backends | `pkg/yap/transcribe/{whisperlocal,groq,openai,mock}/` |
| Transform backends | `pkg/yap/transform/{passthrough,local,openai,fallback}/` |
| Hint providers | `pkg/yap/hint/{claudecode,termscroll}/` |
| Audio preprocessing | `pkg/yap/audioprep/` |
| Platform abstraction | `internal/platform/platform.go` (interfaces) |
| Linux platform impl | `internal/platform/linux/` (audio, hotkey, inject, chime, devices) |
| Linux injection strategies | `internal/platform/linux/inject/` |
| Config types + schema | `pkg/yap/config/` (TOML, NixOS module, validation -- all generated from these types) |
| NixOS/HM module generator | `internal/cmd/gen-nixos/` |
| IPC (daemon <-> CLI) | `internal/ipc/` |

## Adding a New Backend

The main extension points are the three registries: **transcribe**, **transform**, and **hint**. All three follow the same pattern.

### Transcription backend

1. Create `pkg/yap/transcribe/yourbackend/`.
2. Implement the `transcribe.Transcriber` interface:
   ```go
   type Transcriber interface {
       Transcribe(ctx context.Context, audio io.Reader, opts Options) (<-chan TranscriptChunk, error)
   }
   ```
3. Register in an `init.go` file:
   ```go
   func init() {
       transcribe.Register("yourbackend", func(cfg transcribe.Config) (transcribe.Transcriber, error) {
           return New(cfg)
       })
   }
   ```
4. Side-effect import in `internal/daemon/daemon.go`:
   ```go
   _ "github.com/Enriquefft/yap/pkg/yap/transcribe/yourbackend"
   ```
5. Add `"yourbackend"` to `config.ValidBackends()` in `pkg/yap/config/`.
6. Add a `noglobals_test.go` AST guard to your package (copy from any existing backend).
7. Write tests. The `mock` backend is a good reference for the expected chunk semantics.

### Transform backend

Same pattern, different interface:

```go
type Transformer interface {
    Transform(ctx context.Context, in <-chan TranscriptChunk, opts Options) (<-chan TranscriptChunk, error)
}
```

Register via `transform.Register("yourbackend", factory)`. Optionally implement `transform.Checker` for a startup health probe.

### Hint provider

```go
type Provider interface {
    Name() string
    Supports(target inject.Target) bool
    Fetch(ctx context.Context, target inject.Target) (Bundle, error)
}
```

Register via `hint.Register("yourprovider", factory)`. The provider returns a `Bundle` with `Vocabulary` (feeds Whisper prompt) and/or `Conversation` (feeds transform context). See `claudecode` and `termscroll` for real examples.

### Key rules for all backends

- Zero package-level mutable state (except the registry entry itself).
- Constructor injection for all dependencies (HTTP clients, loggers, config).
- The engine has zero backend imports. Only the daemon side-effect-imports backends.
- Thread `Options` through faithfully. The `Prompt`/`Context` fields vary per recording.
- Handle `ctx` cancellation. Drain channels on cancel. Never leak goroutines.

## Config Changes

Config types live in `pkg/yap/config/config.go`. When you add or modify a config field:

1. Update the struct and its `yap:"..."` tags.
2. Run `go generate ./pkg/yap/config/...` to regenerate the NixOS/Home Manager module.
3. A golden-file test will fail the build if the committed `nixosModules.nix` drifts from the generator output.
4. Update validation in `pkg/yap/config/validate.go` if the field has constraints.
5. Update `DefaultConfig()` with the appropriate default.

## Platform Work

The platform layer is abstracted behind interfaces in `internal/platform/platform.go`. Linux lives in `internal/platform/linux/`. macOS (Phase 15) and Windows (Phase 16) will add `darwin/` and `windows/` directories with the same interface implementations.

If your change touches platform-specific code, keep it behind the existing build tags and interface boundaries.

## What Not to Do

- Do not add package-level `var` declarations in production code. The AST guards will reject it.
- Do not add `time.Sleep` in production code. Use context-aware sleeps or injected sleep functions.
- Do not put pipeline logic in `internal/cli/`. CLI commands are thin wrappers over `pkg/yap/`.
- Do not import backend packages from `internal/engine/`. The engine knows only interfaces.
- Do not skip the `noglobals_test.go` guard when adding a new package. Copy one from an adjacent package and adapt the whitelist.

## Reporting Issues

Open an issue at [github.com/Enriquefft/yap/issues](https://github.com/Enriquefft/yap/issues). Include:

- **What you did** -- the command you ran or the action you took.
- **What you expected** -- the behavior you wanted.
- **What happened** -- the actual behavior, including any error output.
- **Environment** -- OS, desktop environment / compositor (Sway, Hyprland, GNOME, KDE, X11), terminal emulator, Go version, yap version (`yap status`).
- **Config** (if relevant) -- the output of `yap config get <relevant-section>`. Redact API keys.

For transcription quality issues, note which backend you are using (`whisperlocal`, `groq`, `openai`) and whether the hint system is enabled (`yap hint` output is helpful).

For injection issues, include the output of `yap resolve` (shows the detected target and chosen strategy).

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
