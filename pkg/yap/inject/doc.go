// Package inject declares the text-injection contract. It exposes the
// interfaces (Injector, Strategy), the Target data type, and the
// AppType classification enum.
//
// Phase 3 ships only the interfaces. Phase 4 (Text Injection Overhaul)
// adds concrete Linux strategies — OSC52, bracketed paste, tmux
// passthrough, Electron clipboard + synthesized paste, and a generic
// GUI fallback — plus active-window detection and app classification.
// Those implementations live under internal/platform/linux/inject/.
package inject
