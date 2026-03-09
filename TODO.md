# TODO

## Hotkey combo support
Support multi-key hotkey combos (e.g. Shift+K, Ctrl+Shift+Space).
Requires changes across the full stack:
- **Config**: parse `hotkey = "KEY_LEFTSHIFT+KEY_K"` (plus-delimited)
- **Daemon listener**: track held keys, fire onPress only when all combo keys are held simultaneously
- **Wizard detection**: track held keys via evdev, return combo string on release
- **Terminal fallback**: detect modifier+key combos via escape sequences
