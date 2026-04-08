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

// known is the pinned manifest of every English-only whisper.cpp model
// yap supports out of the box. Each entry's SHA256 was computed live
// against the canonical Hugging Face download; mismatches are rejected
// at install time. Users who want a model not on this list — for
// example a multilingual variant or a custom fine-tune — can point
// transcription.model_path at a hand-downloaded ggml-*.bin file and
// bypass the manifest entirely.
//
// Reproduction recipe (run from any throwaway directory):
//
//	for name in tiny.en base.en small.en medium.en; do
//	  curl -fL -o "ggml-${name}.bin" \
//	    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${name}.bin"
//	  sha256sum "ggml-${name}.bin"
//	done
//
// The four hashes below are the lowercase hex SHA256 of those files
// and the SizeMB values are round(bytes / 1024 / 1024).
var known = []Manifest{
	{
		Name:   "tiny.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
		SHA256: "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f",
		SizeMB: 74,
	},
	{
		Name:   "base.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
		SHA256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		SizeMB: 142,
	},
	{
		Name:   "small.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
		SHA256: "c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d",
		SizeMB: 465,
	},
	{
		Name:   "medium.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin",
		SHA256: "cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356",
		SizeMB: 1463,
	},
}

// lookupManifest returns the pinned manifest for name. The second
// return value is a boolean for "found"; callers usually do:
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

// ErrUnknownModel returns the error for an unrecognised model name. It
// is a function rather than a sentinel because the message embeds the
// requested name and the list of currently-pinned models so the user
// sees an actionable hint instead of a bare "unknown".
func ErrUnknownModel(name string) error {
	pinned := make([]string, 0, len(known))
	for _, m := range known {
		pinned = append(pinned, m.Name)
	}
	return fmt.Errorf(
		"models: unknown model %q (pinned: %v). Set transcription.model_path "+
			"to a hand-downloaded ggml-*.bin file to use a model outside the manifest",
		name, pinned)
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
