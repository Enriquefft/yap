package models

import (
	"fmt"
	"strings"
)

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

// known is the pinned manifest of whisper.cpp models yap supports out
// of the box. Each entry's SHA256 was computed live against the
// canonical Hugging Face download; mismatches are rejected at install
// time. Users who want a model not on this list — for example a
// custom fine-tune — can point transcription.model_path at a
// hand-downloaded ggml-*.bin file and bypass the manifest entirely.
//
// The manifest includes both English-only (.en suffix) and multilingual
// variants. English-only models are slightly faster and smaller;
// multilingual models support transcription.language and auto-detect.
//
// Reproduction recipe (run from any throwaway directory):
//
//	for name in tiny tiny.en base base.en small small.en medium medium.en; do
//	  curl -fL -o "ggml-${name}.bin" \
//	    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${name}.bin"
//	  sha256sum "ggml-${name}.bin"
//	done
//
// The hashes below are the lowercase hex SHA256 of those files and the
// SizeMB values are round(bytes / 1024 / 1024).
//
// known is the production manifest. Tests construct their own Manager
// via NewManager(WithManifest(...)) and never touch this slice.
var known = []Manifest{
	{
		Name:   "tiny",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin",
		SHA256: "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21",
		SizeMB: 74,
	},
	{
		Name:   "tiny.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
		SHA256: "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f",
		SizeMB: 74,
	},
	{
		Name:   "base",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin",
		SHA256: "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe",
		SizeMB: 141,
	},
	{
		Name:   "base.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
		SHA256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		SizeMB: 142,
	},
	{
		Name:   "small",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
		SHA256: "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
		SizeMB: 465,
	},
	{
		Name:   "small.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
		SHA256: "c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d",
		SizeMB: 465,
	},
	{
		Name:   "medium",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin",
		SHA256: "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208",
		SizeMB: 1463,
	},
	{
		Name:   "medium.en",
		URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin",
		SHA256: "cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356",
		SizeMB: 1463,
	},
}

// knownCopy returns a fresh copy of the production manifest. The
// Manager constructor uses it so per-process Manager instances do not
// share the underlying slice with each other or with the package
// global.
func knownCopy() []Manifest {
	out := make([]Manifest, len(known))
	copy(out, known)
	return out
}

// lookupManifestIn does the case-insensitive name match against the
// supplied manifest slice. The case normalization (lowercase) is the
// single source of truth: every lookup path goes through this
// function so users can write "Base.EN" in their config and have it
// resolve to "base.en".
func lookupManifestIn(manifest []Manifest, name string) (Manifest, bool) {
	want := strings.ToLower(name)
	for _, m := range manifest {
		if strings.ToLower(m.Name) == want {
			return m, true
		}
	}
	return Manifest{}, false
}

// ErrUnknownModel returns the error for an unrecognised model name
// against the production manifest. It is a function rather than a
// sentinel because the message embeds the requested name and the list
// of currently-pinned models so the user sees an actionable hint
// instead of a bare "unknown".
func ErrUnknownModel(name string) error {
	return ErrUnknownModelFromManifest(name, known)
}

// ErrUnknownModelFromManifest is the Manager-aware variant. It returns
// an actionable error message embedding the names from the supplied
// manifest, so a Manager constructed with a fixture manifest produces
// an error that lists its own pinned models rather than the package
// defaults.
func ErrUnknownModelFromManifest(name string, manifest []Manifest) error {
	pinned := make([]string, 0, len(manifest))
	for _, m := range manifest {
		pinned = append(pinned, m.Name)
	}
	return fmt.Errorf(
		"models: unknown model %q (pinned: %v). Set transcription.model_path "+
			"to a hand-downloaded ggml-*.bin file to use a model outside the manifest",
		name, pinned)
}

// LookupByName returns the manifest entry for name and a found bool
// against the production manifest. Exported for callers (CLI, tests)
// that need the manifest metadata without going through Path/Installed.
//
// The match is case-insensitive.
func LookupByName(name string) (Manifest, bool) {
	return lookupManifestIn(known, name)
}

// Known returns a copy of the production manifest list. The returned
// slice is freshly allocated so callers cannot mutate the package
// state.
func Known() []Manifest {
	return knownCopy()
}
