package models

import "fmt"

// Manifest is the pinned description of a known whisper.cpp model. The
// fields are the minimum information needed to reproduce a verified
// download from Hugging Face.
type Manifest struct {
	// Name is the short identifier (e.g. "base.en"). It matches the
	// suffix between "ggml-" and ".bin" on Hugging Face.
	Name string
	// URL is the canonical Hugging Face download URL.
	URL string
	// SHA256 is the lowercase hex SHA256 of the binary file. This is
	// the contract: a download whose SHA256 differs is rejected.
	SHA256 string
	// SizeMB is the approximate file size in MiB, used by `yap models
	// list` to give the user a sense of how big the download is.
	SizeMB int
}

// Filename returns the canonical on-disk filename for the model. It is
// the same shape Hugging Face uses (`ggml-<name>.bin`) so users can
// drop a manually-downloaded file into the cache directory and have it
// resolve.
func (m Manifest) Filename() string { return "ggml-" + m.Name + ".bin" }

// known is the pinned model list. Phase 6 ships exactly one entry —
// `base.en` — because that is the model the default config points at
// and the only file whose SHA256 we have verified live during the
// Phase 6 implementation run. Adding tiny.en / small.en / medium.en is
// a follow-up change that re-downloads each file, computes the hash,
// and appends an entry here. Until then those names return a clear
// "not currently pinned" error from lookupManifest so users see a
// helpful message instead of "unknown".
//
// SHA256 sourced from a live download performed during Phase 6
// implementation:
//
//	curl -L https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin -o /tmp/ggml-base.en.bin
//	sha256sum /tmp/ggml-base.en.bin
//	a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002  /tmp/ggml-base.en.bin
//
// See ROADMAP.md Phase 6 Findings.
var known = []Manifest{
	{
		Name:   "base.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
		SHA256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		SizeMB: 142,
	},
}

// pinnedModelNames is the list of model names that the manifest does
// NOT currently pin but that users might reasonably ask for. The error
// path uses this list to give a helpful "not currently pinned" message.
var pinnedAlternates = []string{"tiny.en", "small.en", "medium.en"}

// lookupManifest returns the pinned manifest for name. The second
// return value is a boolean for "found"; the third is a sentinel error
// describing why a known-but-not-pinned name was rejected. Callers
// usually want one of:
//
//	m, ok := lookupManifest(name)
//	if !ok { return ErrUnknownModel(name) }
//
// where ErrUnknownModel composes the error from this package.
func lookupManifest(name string) (Manifest, bool) {
	for _, m := range known {
		if m.Name == name {
			return m, true
		}
	}
	return Manifest{}, false
}

// isAlternateName reports whether name is one of the well-known
// whisper.cpp models that the Phase 6 manifest deliberately omits. The
// caller uses this to give a tailored error message.
func isAlternateName(name string) bool {
	for _, alt := range pinnedAlternates {
		if alt == name {
			return true
		}
	}
	return false
}

// ErrUnknownModel returns the error for an unrecognised model name. It
// is a function rather than a sentinel because the message embeds the
// requested name and a hint about which models are currently pinned.
func ErrUnknownModel(name string) error {
	if isAlternateName(name) {
		return fmt.Errorf(
			"models: model %q is not currently pinned in the Phase 6 manifest "+
				"(only %q is). Set transcription.model_path to a hand-downloaded "+
				"file or stay on base.en", name, known[0].Name)
	}
	pinned := make([]string, 0, len(known))
	for _, m := range known {
		pinned = append(pinned, m.Name)
	}
	return fmt.Errorf("models: unknown model %q (pinned: %v)", name, pinned)
}

// LookupByName returns the manifest entry for name and a found bool.
// Exported for callers (CLI, tests) that need the manifest metadata
// without going through Path/Installed.
func LookupByName(name string) (Manifest, bool) {
	return lookupManifest(name)
}

// Known returns a copy of the pinned manifest list. The returned slice
// is freshly allocated so callers cannot mutate the package state.
func Known() []Manifest {
	out := make([]Manifest, len(known))
	copy(out, known)
	return out
}
