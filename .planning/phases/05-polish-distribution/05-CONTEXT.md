# Phase 5: Polish + Distribution - Context

**Gathered:** 2026-03-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 5 transforms the working prototype into a shippable Linux application. The focus is on user experience, configurability, safety, and distribution. This phase completes the core feature set by adding missing configuration management (CONFIG-06, CONFIG-07, CONFIG-08), first-run setup guidance (FIRSTRUN-01, FIRSTRUN-02, FIRSTRUN-03), and production-ready packaging (DIST-03, DIST-04, DIST-05). Critical improvements include recording timeout enforcement (AUDIO-08) and comprehensive NixOS integration.

**What this phase delivers:**
- Config CLI subcommands for runtime configuration management
- Interactive first-run wizard for new user onboarding
- Automatic recording timeout for safety
- Complete NixOS module with proper group management
- GitHub Releases CI for automated publishing
- Curl-based installer script for easy deployment

**What this phase does NOT deliver:**
- Cloud storage integration (future milestone)
- Voice command features (future milestone)
- Windows/macOS support (future milestone)
- Plugin system (future milestone)
</domain>

<decisions>
## Implementation Decisions

### Configuration Management
- **Config CLI**: Cobra-based subcommands (`set`, `get`, `path`) that directly modify config.toml
- **Atomic file operations**: Write to temp file then rename for safety (prevents corruption on write failure)
- **Environment priority**: Runtime env vars override file config (preserves Docker/container use cases)

### First-Run Wizard
- **Interactive TUI**: Simple text-based wizard using bubbletea/tea for better UX than printf/read
- **Validation step**: API key format validation (sk-xxxxxxxxxxxxxxxx) before writing config
- **Skip detection**: Check GROQ_API_KEY env var first (allows container non-interactive setup)
- **Hotkey preview**: Show key name in readable format (e.g., "Ctrl+Shift+P" instead of keycode)

### Recording Timeout
- **Dual chimes**: 50s warning (770Hz beep), 60s cutoff (same tone, longer)
- **Context timeout**: Use context.WithTimeout() for clean cancellation
- **No partial submits**: Auto-stop at 60s even if recording is active

### NixOS Module
- **Auto-group**: Add user to 'input' group via extraGroups (no manual usermod needed)
- **Pipewire enable**: Set services.pipewire.alsa.enable = true for modern audio
- **Activation script**: Include verification step that checks membership

### Distribution
- **GitHub Releases**: CI runs on git tag push, uploads binary to release assets
- **Install script**: Detects OS/arch, downloads from GitHub, places in ~/.local/bin
- **Post-install**: Checks PATH and suggests adding to ~/.config/fish/config.fish if missing
</decisions>

<specifics>
## Specific Ideas

**Config CLI Implementation:**
```go
// internal/cmd/config.go
var configCmd = &cobra.Command{
  Use:   "config",
  Short: "Manage configuration",
}

var setCmd = &cobra.Command{
  Use:   "set <key> <value>",
  Short: "Set config value",
  Args:  cobra.ExactArgs(2),
  Run: func(cmd *cobra.Command, args []string) {
    // Load existing config, update key, save atomically
  },
}

var getCmd = &cobra.Command{
  Use:   "get <key>",
  Short: "Get config value",
  Args:  cobra.ExactArgs(1),
  Run: func(cmd *cobra.Command, args []string) {
    // Load config, print value, or error if missing
  },
}
```

**First-Run Wizard Flow:**
1. Check if config exists at startup
2. If missing and no GROQ_API_KEY env var:
   - Start wizard: "Welcome to yap! Let's set up your configuration..."
   - Prompt: "Enter your Groq API key (sk-...): "
   - Validate format: regex `^sk-[a-zA-Z0-9]{48}$`
   - Prompt: "Choose hotkey [default: Ctrl+Shift+P]: "
   - Parse hotkey name to evdev code
   - Write config and confirm: "Config saved to ~/.config/yap/config.toml"

**Recording Timeout Integration:**
- Modify recording context from Phase 4 to respect timeout_seconds config
- Add 50s warning chime (non-blocking) before 60s cutoff
- Ensure SIGTERM cancels active timeout (inherited context)
</specifics>

<deferred>
## Deferred Ideas

- **Auto-update**: Version checking and binary updating (v1.0+)
- **Systemd service**: Optional user service for autostart (future iteration)
- **Config validation**: Schema validation for config.toml (complex, post-v0.1)
- **Multiple profiles**: Hotkey profiles for different applications (post-v0.1)
- **Audio device switching**: Runtime mic device selection via config (post-v0.1)

None — PRD covers phase scope completely
</deferred>

---

*Phase: 05-polish-distribution*
*Context gathered: 2026-03-08 via direct requirements analysis*
```