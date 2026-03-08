# Phase 1: Foundation — Research

**Researched:** 2026-03-07
**Domain:** Go project scaffold, CGo static linking, Nix flake packaging, TOML config, XDG paths, asset embedding
**Confidence:** HIGH

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| CONFIG-01 | Config file at `$XDG_CONFIG_HOME/yap/config.toml`; XDG paths via `github.com/adrg/xdg` | `xdg.ConfigFile("yap/config.toml")` returns compliant path; creates parent dirs automatically |
| CONFIG-02 | Config parsed via `github.com/BurntSushi/toml` | `toml.DecodeFile(path, &cfg)` pattern; v1.6.0 verified |
| CONFIG-03 | Config keys: `api_key`, `hotkey`, `language`, `mic_device`, `timeout_seconds` | Direct struct field mapping via `toml:"key_name"` tags |
| CONFIG-04 | Env var overrides: `GROQ_API_KEY` overrides `api_key`; `YAP_HOTKEY` overrides `hotkey` | Post-decode `os.Getenv` override pattern; no extra library needed |
| CONFIG-05 | Config struct passed via dependency injection; no global mutable config | Constructor injection pattern; `Config` passed to each cobra command via closure |
| ASSETS-01 | Start/stop chime WAV files embedded via `//go:embed` | `//go:embed assets/*.wav` into `embed.FS`; stdlib, zero dependencies |
| ASSETS-02 | Chime files at 16kHz mono PCM; each under 100KB | ffmpeg encoding target; verified size budget (< 3 seconds at 16kHz mono ≈ 96KB) |
| DIST-01 | Nix flake with `packages.default` producing the static binary | `buildGoModule` with `pkgsStatic` or musl linker flags; portaudio in `buildInputs` |
| DIST-02 | Nix build sets: `buildInputs = [pkgs.portaudio]`, `nativeBuildInputs = [pkgs.pkg-config]`, `CGO_ENABLED = "1"` | Confirmed required pattern for CGo + portaudio on NixOS |
| NFR-01 | Binary fully statically linked; `ldd ./yap` outputs `not a dynamic executable` | musl-gcc + `-linkmode external -extldflags '-static'` strategy confirmed |
| NFR-02 | Build command: `CGO_ENABLED=1 CC=musl-gcc go build -tags netgo,osusergo -ldflags="-linkmode external -extldflags '-static'" ./cmd/yap` | Exact flags documented and verified against multiple sources |
| NFR-05 | Binary size under 20MB | Pure Go stack + musl static; stripped binary with `-s -w` flags targets < 10MB without audio pipeline |
| NFR-07 | No telemetry, no network calls except Groq API | No analytics libraries; network calls scoped to transcription package (Phase 4) |
</phase_requirements>

---

## Summary

Phase 1 establishes the load-bearing scaffold that every subsequent phase builds upon. The core deliverable is a statically linked binary from day one — if this is wrong, the entire curl-installable distribution story fails. The three mutually reinforcing concerns are: (1) CGo static linking via musl-gcc, which must be correct before any library that uses CGo can be added; (2) the Nix flake, which requires the same portaudio/pkg-config/CGO_ENABLED wiring and must be validated end-to-end; and (3) the config/XDG subsystem, which every other component depends on for file paths.

The recommended approach for static linking is `CGO_ENABLED=1 CC=musl-gcc go build -tags netgo,osusergo -ldflags="-linkmode external -extldflags '-static'" ./cmd/yap`. The Nix flake should use `pkgsStatic.callPackage` to automatically compile all C dependencies (including portaudio) against musl, avoiding manual linker flag juggling. The config package uses `adrg/xdg` v0.5.3 (not stdlib `os.UserConfigDir()` which has a confirmed XDG_CONFIG_HOME bug) and `BurntSushi/toml` v1.6.0 for TOML parsing.

Asset embedding uses stdlib `//go:embed` with `embed.FS` to hold start/stop chime WAV files. Chimes must be pre-encoded to 16kHz mono PCM and kept under 100KB each to avoid bloating the binary. Phase 1 does not wire up chime playback (that is Phase 2) — it only embeds the assets and verifies they are accessible.

**Primary recommendation:** Build in this order: go.mod + module structure → Cobra root + subcommand stubs → config package → `//go:embed` assets package → musl static build verification → Nix flake. Verify `ldd ./yap` and `nix build` before moving to Phase 2.

---

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | v1.10.2 | CLI subcommand framework | Industry standard for Go CLIs; auto-generated help; used by kubectl, docker, hugo |
| `github.com/adrg/xdg` | v0.5.3 | XDG Base Directory paths | Required: stdlib `os.UserConfigDir()` has confirmed bug (Go issue #76320) for `XDG_CONFIG_HOME` |
| `github.com/BurntSushi/toml` | v1.6.0 | TOML config parsing | Simpler API than go-toml/v2 for yap's flat config surface; battle-tested |
| stdlib `embed` | Go 1.21+ | WAV asset embedding | Zero dependencies; compile-time embedding; goroutine-safe read-only FS |

### Supporting (Phase 1 only — no CGo yet)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| stdlib `os` | — | Env var overrides, file existence check | `os.Getenv("GROQ_API_KEY")` for config overrides |
| stdlib `log/slog` | Go 1.21+ | Structured logging | Debug output; `-v` flag; no external logger needed at this scale |
| `musl-gcc` / `zig cc` | system / Zig 0.12+ | Static C linker | Required for CGo static binary; musl for local, zig for CI cross-compile |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `adrg/xdg` | `os.UserConfigDir()` | stdlib has bug #76320 — wrong value when `XDG_CONFIG_HOME` is set |
| `BurntSushi/toml` | `go-toml/v2` | go-toml/v2 has richer API but more complex for a 5-key flat config |
| `pkgsStatic` in Nix | manual musl flags | `pkgsStatic` is cleaner and handles all transitive C deps automatically |
| `embed.FS` | bundling assets separately | FS keeps single-binary promise; no runtime file path needed |

**Installation:**
```bash
go get github.com/spf13/cobra@latest
go get github.com/adrg/xdg@latest
go get github.com/BurntSushi/toml@latest
```

---

## Architecture Patterns

### Recommended Project Structure
```
cmd/
└── yap/
    └── main.go          # entry point; calls Execute()
internal/
├── cmd/
│   ├── root.go          # rootCmd, persistent flags, config loading
│   ├── start.go         # yap start subcommand stub
│   ├── stop.go          # yap stop subcommand stub
│   ├── status.go        # yap status subcommand stub
│   ├── toggle.go        # yap toggle subcommand stub
│   └── config.go        # yap config subcommand stub
├── config/
│   ├── config.go        # Config struct, Load(), env var overrides
│   └── config_test.go   # unit tests for loading, defaults, overrides
└── assets/
    ├── assets.go         # embed.FS declaration + accessor functions
    ├── start.wav         # 16kHz mono PCM, < 100KB
    └── stop.wav          # 16kHz mono PCM, < 100KB
go.mod
go.sum
flake.nix
flake.lock
Makefile
```

### Pattern 1: Cobra Root with Config Injection
**What:** Root command loads config in `PersistentPreRunE`, passes it to subcommands via closure-captured pointer.
**When to use:** All CLI tools where config must be available to all subcommands.

```go
// Source: cobra v1.10.2 docs + project architecture decision
// internal/cmd/root.go

var rootCmd = &cobra.Command{
    Use:   "yap",
    Short: "Hold-to-talk voice dictation daemon",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Skip config load for help commands
        if cmd.Name() == "help" {
            return nil
        }
        cfg, err := config.Load()
        if err != nil {
            return fmt.Errorf("config: %w", err)
        }
        // Store on context or package-level var for subcommand access
        // Use pointer to allow subcommands to receive same instance
        currentConfig = cfg
        return nil
    },
}

func Execute() error {
    return rootCmd.Execute()
}

func init() {
    rootCmd.AddCommand(newStartCmd())
    rootCmd.AddCommand(newStopCmd())
    rootCmd.AddCommand(newStatusCmd())
    rootCmd.AddCommand(newToggleCmd())
    rootCmd.AddCommand(newConfigCmd())
}
```

### Pattern 2: Config Loading with XDG + TOML + Env Overrides
**What:** Load config from XDG path, apply defaults for missing file, apply env overrides post-decode.
**When to use:** Any Go tool following XDG spec with env var escape hatches.

```go
// Source: adrg/xdg v0.5.3 docs + BurntSushi/toml v1.6.0 docs
// internal/config/config.go

type Config struct {
    APIKey         string `toml:"api_key"`
    Hotkey         string `toml:"hotkey"`
    Language       string `toml:"language"`
    MicDevice      string `toml:"mic_device"`
    TimeoutSeconds int    `toml:"timeout_seconds"`
}

func defaults() Config {
    return Config{
        Hotkey:         "KEY_RIGHTCTRL",
        Language:       "en",
        TimeoutSeconds: 60,
    }
}

func Load() (Config, error) {
    cfg := defaults()

    // Use adrg/xdg — NOT os.UserConfigDir() which has XDG_CONFIG_HOME bug
    configPath, err := xdg.ConfigFile("yap/config.toml")
    if err != nil {
        return cfg, fmt.Errorf("xdg config path: %w", err)
    }

    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        // Missing config is not an error — use defaults
        applyEnvOverrides(&cfg)
        return cfg, nil
    }

    if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
        return cfg, fmt.Errorf("parse config: %w", err)
    }

    applyEnvOverrides(&cfg)
    return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
    if v := os.Getenv("GROQ_API_KEY"); v != "" {
        cfg.APIKey = v
    }
    if v := os.Getenv("YAP_HOTKEY"); v != "" {
        cfg.Hotkey = v
    }
}

func ConfigPath() (string, error) {
    return xdg.ConfigFile("yap/config.toml")
}
```

### Pattern 3: Embedded Assets with embed.FS
**What:** Declare embed.FS at package level; provide typed accessor functions for consumers.
**When to use:** Any Go binary requiring embedded static files.

```go
// Source: pkg.go.dev/embed (stdlib)
// internal/assets/assets.go

package assets

import (
    "embed"
    "io"
    "bytes"
)

//go:embed start.wav stop.wav
var fs embed.FS

// StartChime returns an io.Reader for the start chime WAV bytes.
// Caller is responsible for closing if the underlying type requires it.
func StartChime() (io.Reader, error) {
    data, err := fs.ReadFile("start.wav")
    if err != nil {
        return nil, err
    }
    return bytes.NewReader(data), nil
}

// StopChime returns an io.Reader for the stop chime WAV bytes.
func StopChime() (io.Reader, error) {
    data, err := fs.ReadFile("stop.wav")
    if err != nil {
        return nil, err
    }
    return bytes.NewReader(data), nil
}

// ListAssets returns the names of all embedded asset files.
// Used by --list-assets debug flag.
func ListAssets() ([]string, error) {
    entries, err := fs.ReadDir(".")
    if err != nil {
        return nil, err
    }
    names := make([]string, 0, len(entries))
    for _, e := range entries {
        names = append(names, e.Name())
    }
    return names, nil
}
```

### Pattern 4: Nix Flake with Static CGo Build
**What:** Use `pkgsStatic.callPackage` to automatically compile portaudio and all C deps against musl; export both dynamic (dev) and static (release) packages.
**When to use:** Any Go program with CGo dependencies targeting curl-installable static binary.

```nix
# flake.nix
{
  description = "yap — hold-to-talk voice dictation daemon";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        # pkgsStatic compiles all C deps (including portaudio) against musl
        pkgsS = pkgs.pkgsStatic;

        yapPkg = { buildGoModule, pkg-config, portaudio, lib, withStatic ? false }:
          buildGoModule {
            pname = "yap";
            version = "0.1.0";
            src = ./.;
            vendorHash = null; # set to sha256 hash after first build

            CGO_ENABLED = "1";

            nativeBuildInputs = [ pkg-config ];
            buildInputs = [ portaudio ];

            ldflags = [ "-s" "-w" ]
              ++ lib.optionals withStatic [
                "-linkmode external"
                "-extldflags \"-static\""
              ];

            tags = lib.optionals withStatic [ "netgo" "osusergo" ];
          };
      in {
        packages = {
          # Dynamic build (for development/NixOS users)
          default = pkgs.callPackage yapPkg {};
          # Fully static binary (for curl install)
          static = pkgsS.callPackage yapPkg { withStatic = true; };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            pkg-config
            portaudio
            musl
          ];
        };
      });
}
```

### Pattern 5: Static Build Makefile Target
**What:** Local development static build using musl-gcc.
**When to use:** Developing on non-NixOS Linux where `nix build` is not the default workflow.

```makefile
# Makefile
BINARY := yap
CMD    := ./cmd/yap
LDFLAGS := -linkmode external -extldflags '-static'
TAGS    := netgo,osusergo

.PHONY: build build-static verify-static

build:
	go build -o $(BINARY) $(CMD)

build-static:
	CGO_ENABLED=1 CC=musl-gcc \
	go build \
	  -tags $(TAGS) \
	  -ldflags="$(LDFLAGS)" \
	  -o $(BINARY) $(CMD)

verify-static:
	@ldd ./$(BINARY) 2>&1 | grep -q "not a dynamic executable" && \
	  echo "OK: binary is static" || \
	  (echo "FAIL: binary has dynamic deps" && ldd ./$(BINARY) && exit 1)

build-check: build-static verify-static
```

### Anti-Patterns to Avoid

- **Using `os.UserConfigDir()` for XDG paths:** Has a confirmed bug (Go issue #76320) where it does not respect `XDG_CONFIG_HOME` correctly. Always use `adrg/xdg`.
- **Global mutable config:** Config struct must be passed via dependency injection (constructor or closure). No package-level `var Config` that subcommands mutate.
- **Calling `os.Exit()` directly anywhere:** Prevents deferred cleanup from running. In Phase 1 this is less critical but establishes the pattern for Phase 3 daemon.
- **Missing `-tags netgo,osusergo`:** These tags force pure-Go DNS resolver and OS user lookup, avoiding glibc dynamic linking through those stdlib packages. Without them, `ldd` may still show dynamic deps even with musl.
- **CGO_ENABLED=0 for static build:** Wrong approach when portaudio is in the binary. Must use `CGO_ENABLED=1` with musl linker, not disable CGo entirely.
- **Hardcoding `~/.config/yap/config.toml`:** Breaks NixOS users and anyone with custom XDG dirs. Always resolve via `xdg.ConfigFile`.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| XDG path resolution | Custom `$HOME/.config` path logic | `adrg/xdg v0.5.3` | XDG spec has edge cases: env vars not set, relative paths, multiple data dirs; stdlib has confirmed bug |
| TOML parsing | Manual string parsing | `BurntSushi/toml v1.6.0` | Type coercion, struct tags, error messages, datetime handling |
| CLI subcommand routing | Manual `os.Args` dispatch | `spf13/cobra v1.10.2` | Help generation, flag inheritance, error formatting, shell completion |
| Binary asset embedding | Runtime file reads or base64 literals | stdlib `//go:embed` | Compile-time, goroutine-safe, works with `io/fs` interface, zero overhead |
| Static CGo linking on NixOS | Manual musl linker scripts | `pkgsStatic.callPackage` | Nix handles transitive C dep rebuilds automatically; manual approach misses indirect deps |

**Key insight:** The XDG and static-linking problems have subtle edge cases that will cause silent failures in production. Both `adrg/xdg` and the musl/pkgsStatic strategy exist precisely because the naive solutions fail in real-world environments.

---

## Common Pitfalls

### Pitfall 1: CGo Produces Dynamic Binary (Critical)
**What goes wrong:** Default `go build` with CGo produces a binary linked against glibc and libportaudio.so. `ldd ./yap` shows dynamic dependencies. Binary fails on systems with different glibc versions.
**Why it happens:** CGo links against the system C compiler and libc by default.
**How to avoid:** Must use `CC=musl-gcc` and `-ldflags="-linkmode external -extldflags '-static'"` with `-tags netgo,osusergo`. Verify immediately with `ldd ./yap`.
**Warning signs:** `ldd ./yap` shows anything other than `not a dynamic executable`. Build succeeds on dev machine but fails on clean system.

### Pitfall 2: Nix Build Fails — CGo Headers Not Found
**What goes wrong:** `# cgo pkg-config: exit status 1` during `nix build`. PortAudio headers not on `PKG_CONFIG_PATH` in Nix sandbox.
**Why it happens:** NixOS does not keep headers in a global search path; pkg-config must be explicitly wired.
**How to avoid:** Nix derivation MUST have both `buildInputs = [ pkgs.portaudio ]` AND `nativeBuildInputs = [ pkgs.pkg-config ]`. Both are required — portaudio alone is insufficient.
**Warning signs:** `cgo: C compiler "gcc" not found` or `pkg-config: portaudio-2.0 not found`.

### Pitfall 3: `-tags netgo,osusergo` Missing
**What goes wrong:** Even with musl-gcc, omitting `netgo,osusergo` build tags causes stdlib `net` and `os/user` packages to use CGo paths that link against glibc.
**Why it happens:** By default, Go uses CGo for DNS and user lookup for performance reasons.
**How to avoid:** Always pass `-tags netgo,osusergo` alongside the musl-gcc flags.
**Warning signs:** `ldd ./yap` shows `libnss_*.so` or `libpthread.so` even with musl-gcc.

### Pitfall 4: PortAudio CGo Pointer Crash
**What goes wrong:** Older `gordonklaus/portaudio` versions pass Go pointers through CGo callbacks, causing a runtime panic under Go 1.6+ CGo pointer rules.
**Why it happens:** Pre-2018 versions of the library violated CGo pointer-passing rules.
**How to avoid:** Pin to a post-2018 commit or latest tagged version. The fix uses `cgo.Handle` correctly for callbacks.
**Warning signs:** Panic with `cgo argument has Go pointer to Go pointer` or `unexpected signal during runtime execution`.

### Pitfall 5: Missing File Is Not a Config Error
**What goes wrong:** If `config.Load()` returns an error when the config file doesn't exist, `yap start` crashes on first run before any config is written.
**Why it happens:** `toml.DecodeFile` returns an error for non-existent file.
**How to avoid:** Check `os.IsNotExist(err)` before calling `DecodeFile`. A missing config file must return defaults, not an error.
**Warning signs:** `yap start` exits with `open /home/user/.config/yap/config.toml: no such file or directory`.

### Pitfall 6: Chime WAV Files Too Large
**What goes wrong:** Embedding unoptimized WAV chimes (e.g., 44.1kHz stereo) bloats the binary by 1MB+ per file.
**Why it happens:** Default audio export settings produce high-quality files unnecessary for UI feedback sounds.
**How to avoid:** Pre-encode chimes with `ffmpeg -i input.wav -ar 16000 -ac 1 -sample_fmt s16 output.wav`. Target < 100KB per file (< 3 seconds at 16kHz mono = ~96KB).
**Warning signs:** `ls -lh internal/assets/*.wav` shows files > 100KB.

---

## Code Examples

Verified patterns from official sources:

### go.mod Initialization
```bash
# Source: go modules documentation
go mod init github.com/username/yap
# Resulting go.mod requires Go 1.21 minimum for slog
```

### XDG Config Path Resolution
```go
// Source: pkg.go.dev/github.com/adrg/xdg v0.5.3
import "github.com/adrg/xdg"

// Returns $XDG_CONFIG_HOME/yap/config.toml
// Creates parent directories if they do not exist
configPath, err := xdg.ConfigFile("yap/config.toml")

// Returns $XDG_DATA_HOME/yap/yap.pid (used in Phase 3)
dataPath, err := xdg.DataFile("yap/yap.pid")
```

### TOML Config Decode
```go
// Source: pkg.go.dev/github.com/BurntSushi/toml v1.6.0
import "github.com/BurntSushi/toml"

type Config struct {
    APIKey         string `toml:"api_key"`
    Hotkey         string `toml:"hotkey"`
    Language       string `toml:"language"`
    MicDevice      string `toml:"mic_device"`
    TimeoutSeconds int    `toml:"timeout_seconds"`
}

// Load from file — returns MetaData for checking undefined keys
md, err := toml.DecodeFile(configPath, &cfg)
if err != nil {
    return cfg, fmt.Errorf("parse config: %w", err)
}
// Check for unrecognized keys (optional, useful for user feedback)
for _, key := range md.Undecoded() {
    log.Printf("warning: unknown config key: %s", key)
}
```

### Embed.FS Asset Declaration
```go
// Source: pkg.go.dev/embed (Go stdlib)
package assets

import "embed"

//go:embed start.wav stop.wav
var FS embed.FS
```

### WAV Size Verification (ffmpeg command for chime encoding)
```bash
# Source: ffmpeg documentation — encode chime to 16kHz mono PCM WAV
ffmpeg -i source.wav -ar 16000 -ac 1 -sample_fmt s16 start.wav
ffmpeg -i source.wav -ar 16000 -ac 1 -sample_fmt s16 stop.wav
# Verify size:
ls -lh internal/assets/*.wav  # each must be < 100KB
```

### Static Build Verification
```bash
# Source: NFR-01, NFR-02 requirements + pitfalls research
CGO_ENABLED=1 CC=musl-gcc \
  go build \
  -tags netgo,osusergo \
  -ldflags="-linkmode external -extldflags '-static'" \
  -o yap \
  ./cmd/yap

# Must output: "not a dynamic executable"
ldd ./yap
```

### Cobra Subcommand Tree
```go
// Source: pkg.go.dev/github.com/spf13/cobra v1.10.2
// Produces: yap start | stop | status | toggle | config

func init() {
    rootCmd.AddCommand(startCmd)
    rootCmd.AddCommand(stopCmd)
    rootCmd.AddCommand(statusCmd)
    rootCmd.AddCommand(toggleCmd)
    rootCmd.AddCommand(configCmd)
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `gvalkov/golang-evdev` (CGo) | `holoplot/go-evdev` (pure Go) | ~2019 | Removes CGo boundary; go-evdev actively maintained |
| `os.UserConfigDir()` for XDG | `adrg/xdg` | Go issue #76320 (open) | Stdlib has confirmed bug; library required |
| Manual musl linker flags | `pkgs.pkgsStatic` in Nix | nixpkgs ~2022 | Handles transitive C dep rebuilds; cleaner flake |
| `embed.go` build constraints | `//go:embed` directive | Go 1.16 (2021) | Compile-time, no runtime overhead, goroutine-safe |
| cobra v0.x | cobra v1.10.2 (Dec 2025) | v1.0 2021 | Stable API; `PersistentPreRunE` for error-returning hooks |
| BurntSushi/toml v0.x | v1.6.0 (Dec 2025) | v1.0 2022 | TOML v1.1 support; requires Go 1.18+ |

**Deprecated/outdated:**
- `gvalkov/golang-evdev`: Unmaintained + CGo — do not use
- `os.UserConfigDir()`: Confirmed XDG_CONFIG_HOME bug — do not use for XDG paths
- `golang-design/clipboard`: CGo on Linux — deferred to Phase 4 where `atotto/clipboard` is used instead

---

## Open Questions

1. **vendorHash for Nix flake**
   - What we know: `buildGoModule` requires a `vendorHash` (sha256 of vendor directory)
   - What's unclear: The hash is unknown until `go mod vendor` is run and `nix build` is attempted
   - Recommendation: Set `vendorHash = null` initially; on first `nix build`, capture the hash from the error message and set it explicitly. This is a known bootstrap step, not a blocker.

2. **musl-gcc availability on developer machine**
   - What we know: musl-gcc must be installed (`musl-tools` on Debian/Ubuntu, `musl` on Arch, in devShell on NixOS)
   - What's unclear: The developer's current Linux setup
   - Recommendation: The Nix devShell includes `pkgs.musl`; non-Nix developers need `musl-tools` (Ubuntu/Debian) or `musl` (Arch). The Makefile `build-static` target should emit a clear error if `musl-gcc` is not found.

3. **WAV chime source files**
   - What we know: Chimes must be encoded at 16kHz mono PCM, under 100KB each
   - What's unclear: Source WAV files do not yet exist in the repository
   - Recommendation: Wave 0 task should generate minimal placeholder chimes using `ffmpeg` or a Go test utility, OR use freely-licensed short beep sounds from a public domain source. The `--list-assets` debug flag verifies they are present in the binary.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | stdlib `testing` (Go built-in) |
| Config file | none — `go test` discovers tests by convention |
| Quick run command | `go test ./internal/config/... ./internal/assets/...` |
| Full suite command | `go test ./...` |

### Phase Requirements to Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONFIG-01 | `xdg.ConfigFile("yap/config.toml")` returns XDG-compliant path | unit | `go test ./internal/config/... -run TestConfigPath -v` | Wave 0 |
| CONFIG-02 | `toml.DecodeFile` populates Config struct from valid TOML | unit | `go test ./internal/config/... -run TestConfigLoad -v` | Wave 0 |
| CONFIG-03 | All 5 config keys decoded correctly | unit | `go test ./internal/config/... -run TestConfigKeys -v` | Wave 0 |
| CONFIG-04 | `GROQ_API_KEY` and `YAP_HOTKEY` env vars override TOML values | unit | `go test ./internal/config/... -run TestEnvOverrides -v` | Wave 0 |
| CONFIG-05 | Config struct passed through cobra command without global mutation | unit | `go test ./internal/config/... -run TestNonGlobal -v` | Wave 0 |
| ASSETS-01 | `embed.FS` contains start.wav and stop.wav at compile time | unit | `go test ./internal/assets/... -run TestAssetsPresent -v` | Wave 0 |
| ASSETS-02 | Each WAV file < 100KB | unit | `go test ./internal/assets/... -run TestAssetsSize -v` | Wave 0 |
| NFR-01 | `ldd ./yap` outputs `not a dynamic executable` | smoke | `make build-check` (calls `ldd`) | Wave 0 |
| NFR-02 | Build succeeds with musl-gcc flags | smoke | `make build-static` | Wave 0 |
| NFR-05 | Binary < 20MB after strip | smoke | `ls -lh ./yap \| awk '{print $5}'` | Wave 0 |
| DIST-01 | `nix build` completes and produces runnable binary | integration | `nix build && ./result/bin/yap --help` | Wave 0 |
| DIST-02 | Nix build uses portaudio buildInputs and CGO_ENABLED=1 | integration | Verified by `nix build` success | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/config/... ./internal/assets/...`
- **Per wave merge:** `go test ./... && make build-check`
- **Phase gate:** Full suite green + `ldd ./yap` confirms static + `nix build` succeeds before Phase 2

### Wave 0 Gaps
- [ ] `internal/config/config_test.go` — covers CONFIG-01 through CONFIG-05
- [ ] `internal/assets/assets_test.go` — covers ASSETS-01, ASSETS-02
- [ ] `internal/assets/start.wav` — placeholder chime (16kHz mono, < 100KB)
- [ ] `internal/assets/stop.wav` — placeholder chime (16kHz mono, < 100KB)
- [ ] `Makefile` — `build-static` and `verify-static` targets for NFR-01, NFR-02 smoke tests

---

## Sources

### Primary (HIGH confidence)
- `pkg.go.dev/github.com/spf13/cobra` — v1.10.2 verified, command lifecycle, AddCommand pattern
- `pkg.go.dev/github.com/adrg/xdg` — v0.5.3 verified, ConfigFile/DataFile/ConfigHome API
- `pkg.go.dev/github.com/BurntSushi/toml` — v1.6.0 verified, DecodeFile/Decode/MetaData API
- `pkg.go.dev/embed` — stdlib, //go:embed directive, embed.FS usage
- `wiki.nixos.org/wiki/Go` — buildGoModule with CGO, nativeBuildInputs, buildInputs pattern
- `.planning/research/STACK.md` — stack decisions with rationale (2026-03-07)
- `.planning/research/ARCHITECTURE.md` — component structure, static linking commands (2026-03-07)
- `.planning/research/PITFALLS.md` — pitfalls #1, #10, #14, #15 with prevention steps (2026-03-07)

### Secondary (MEDIUM confidence)
- `kokada.dev/blog/building-static-binaries-in-nix/` — pkgsStatic pattern for CGo static builds; verified against NixOS Wiki
- `github.com/spf13/cobra` README — subcommand directory structure example; cross-verified with pkg.go.dev
- WebSearch: musl-gcc + Go static linking; cross-verified with eli.thegreenplace.net and NixOS Wiki

### Tertiary (LOW confidence — flagged for validation)
- `github.com/adrg/xdg` issue #76320 cross-reference: Go issue number from STACK.md; not independently verified against Go issue tracker in this session

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all 4 library versions verified against pkg.go.dev
- Architecture: HIGH — patterns derived from official docs + prior project research
- Nix flake: MEDIUM-HIGH — pkgsStatic pattern verified from two independent sources; vendorHash bootstrap step is a known unknown
- Pitfalls: HIGH — pitfalls #1/#10/#14/#15 all confirmed by official docs and multiple sources
- Test map: HIGH — standard Go testing patterns; no external framework needed

**Research date:** 2026-03-07
**Valid until:** 2026-04-07 (stable ecosystem; cobra/toml/xdg release cadence is slow)
