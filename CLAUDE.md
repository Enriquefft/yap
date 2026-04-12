yap is a lightweight voice-to-text tool that runs as a daemon with near-zero idle footprint. Hold a hotkey to record, release to transcribe, and watch your text appear exactly where you're typing.

yap uses context-aware transcription: domain terms from project docs (this file, README.md) are extracted and passed to Whisper's prompt parameter to bias recognition toward project vocabulary. Configure via `[hint]` in `~/.config/yap/config.toml` or per-project in `.yap.toml`.

## Quality Bar

- Robust, long-term correct, scalable implementations only.
- Single source of truth, always.
- Zero workarounds, bandaids, hacks, or "fix later" TODOs.
- Production and enterprise ready is the floor, not the ceiling.
- If something is hard to do right, do it right anyway.
