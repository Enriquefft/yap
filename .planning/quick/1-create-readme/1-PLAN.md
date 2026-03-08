---
phase: quick
plan: 1
type: execute
wave: 1
depends_on: []
files_modified: [README.md]
autonomous: true
requirements: []
user_setup: []
must_haves:
  truths:
    - "User can quickly understand what yap is and why they might need it"
    - "User can install yap with a single command"
    - "User can configure yap with their API key"
    - "User can see status badges showing active development"
    - "User knows how to get help or contribute"
  artifacts:
    - path: "README.md"
      provides: "Project documentation, installation guide, usage examples"
      min_lines: 100
  key_links:
    - from: "README.md"
      to: "https://yap.sh/install"
      via: "curl -fsSL https://yap.sh/install | bash"
      pattern: "curl.*install"
    - from: "README.md"
      to: "internal/cmd/"
      via: "CLI commands documented"
      pattern: "yap (start|stop|status|config)"
---

<objective>
Create a viral, professional README.md for yap that quickly communicates value, enables easy installation, and builds trust through active development signals.

Purpose: First impression is critical for viral growth — README must convert visitors into users in under 30 seconds
Output: Professional README.md with clear value prop, quick install, usage examples, and trust signals
</objective>

<execution_context>
@/home/hybridz/.claude/get-shit-done/workflows/execute-plan.md
@/home/hybridz/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@PRD.md
@.planning/STATE.md
@internal/cmd/start.go
@internal/cmd/stop.go
@internal/cmd/status.go
@internal/cmd/toggle.go
@internal/cmd/config_set.go
@internal/cmd/config_get.go
@internal/cmd/config_path.go
@internal/config/config.go
@.github/workflows/install.sh
@flake.nix

<interfaces>
<!-- Key CLI commands and config structure from codebase -->

CLI Commands (from internal/cmd/):
- yap start    — Start daemon in background (runs first-run wizard if no config)
- yap stop     — Stop daemon (idempotent, exits 0 if not running)
- yap status   — Query daemon state via IPC, returns JSON
- yap toggle   — Toggle recording (start if idle, stop if recording)
- yap config get <key>     — Get config value (api_key, hotkey, language, mic_device, timeout_seconds)
- yap config set <key> <value> — Set config value
- yap config path          — Print config file path

Config Keys (from internal/config/Config):
- api_key         — Groq API key (can use GROQ_API_KEY env var)
- hotkey          — Keyboard key (default: KEY_RIGHTCTRL)
- language        — Language code (default: en)
- mic_device      — Specific microphone device (optional)
- timeout_seconds — Recording timeout 1-300s (default: 60)

Install Script (from .github/workflows/install.sh):
- curl -fsSL https://yap.sh/install | bash
- Downloads from GitHub Releases
- Installs to ~/.local/bin (Linux)
- Detects OS/arch (linux_amd64, linux_arm64)
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create README.md with all sections</name>
  <files>README.md</files>
  <action>
Create README.md with the following structure and content:

## Header with Logos
- Use wordmark from /home/hybridz/Projects/yap-landing/public/images/wordmark.png as header image
- Reference logomark from same directory for favicon/avatar context

## Status Badges (top of file)
- Add standard GitHub status badges: Build status, License (AGPL-3.0), Go version, Release version
- Use shields.io format for maximum compatibility

## Value Proposition (first screen)
- Single sentence summary: "yap — hold-to-talk voice dictation for your desktop. Hold key, speak, text appears at cursor."
- 3 bullet points highlighting: zero idle footprint, fast transcription (~1-2s), single static binary

## Quick Install (prominent section)
- Primary: curl install script: `curl -fsSL https://yap.sh/install | bash`
- Nix: `nix profile install github:hybridz/yap`
- Manual: Download from GitHub Releases

## Getting Started
1. First-run wizard automatically runs on first start
2. Set Groq API key (get from console.groq.com)
3. Hold configured hotkey (default: Right Ctrl)
4. Speak, release, text appears

## Usage Examples
Code blocks showing:
- yap start (with wizard explanation)
- yap status
- yap stop
- yap config get/set examples
- Environment variable usage (GROQ_API_KEY)

## Features List
From PRD features:
- Daemon mode with global hotkey
- CLI mode for external keybinds
- Hold-to-talk workflow
- Audio feedback (chimes)
- Clipboard preservation
- Configurable timeout
- Multiple language support

## Configuration
Show config file location: `~/.config/yap/config.toml`
Example TOML with all options documented
Environment variable overrides (GROQ_API_KEY, YAP_HOTKEY)

## Platform Status
- Linux: Full support (current)
- macOS: Planned
- Windows: Planned

## Development
- Build instructions: `make build`, `make build-static`, `make test`
- Nix dev shell: `nix develop`
- Link to LICENSE (AGPL-3.0)
- Contribution guidelines section

## Links Section
- GitHub Issues
- Discord/Community (if any, otherwise placeholder)
- Roadmap link

Styling guidelines:
- Use clear section headers with ## and ###
- Use code blocks with ```bash and ```toml language hints
- Keep paragraphs short (2-3 sentences max)
- Use bold for emphasis on key commands
- Add horizontal rules (---) between major sections
- Include "Get your API key" link to console.groq.com
</action>
  <verify>
    <automated>grep -q "yap.*hold-to-talk.*voice dictation" README.md && grep -q "curl.*yap.sh/install" README.md && grep -q "AGPL-3.0" README.md</automated>
  </verify>
  <done>README.md exists with all required sections, install commands work, status badges included, AGPL-3.0 license mentioned</done>
</task>

</tasks>

<verification>
- README.md contains project description and value prop
- Install instructions present (curl + Nix + manual)
- First-run wizard explained
- CLI commands documented (start, stop, status, toggle, config)
- Config file location and format shown
- Environment variables documented
- License (AGPL-3.0) mentioned
- Status badges present
- Development/build instructions included
</verification>

<success_criteria>
- README.md is created at project root
- README.md is scannable with clear section hierarchy
- README.md includes all 6 viral/trust elements: clear value prop, quick start, status badges, config docs, usage examples, license
- README.md is under 300 lines (keep it focused)
</success_criteria>

<output>
After completion, create `.planning/quick/1-create-readme/1-SUMMARY.md`
</output>
