package models

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// ggmlMagic is the 4-byte prefix of every whisper.cpp model file.
// The bytes are the little-endian uint32 0x67676d6c — "ggml" in
// ASCII. whisper.cpp source pins this as GGML_FILE_MAGIC and rejects
// any model whose first uint32 differs; we mirror the check at
// install and resolve time so users who drop a 404 HTML body or a
// renamed ZIP into the cache get a clear error up front instead of
// a cryptic "subprocess exited during startup" thirty seconds later.
//
// The byte order (lmgg) is the on-disk representation on
// little-endian hosts, which covers every platform yap targets.
const ggmlMagic = "lmgg"

// VerifyGGMLMagic is the single source of truth for ggml model file
// validation. It opens path, reads the first four bytes, and returns
// nil if they match the ggml magic prefix. Any other state (read
// error, short file, wrong magic) returns an error that tells the
// user exactly how to recover.
//
// The check is cheap (one open, one 4-byte read, one close) so it
// runs on every Installed / List / resolveModel call rather than
// being cached. Callers across the whisperlocal stack — the models
// package itself (Installed, List) and the whisperlocal backend
// (resolveModel) — funnel through this function so the accepted
// byte sequence stays consistent with whisper.cpp's own file-format
// check.
func VerifyGGMLMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("models: open %s: %w", path, err)
	}
	defer f.Close()
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf(
			"models: %s is not a whisper.cpp model file (file too short to read magic bytes); redownload via yap models download <name>",
			path)
	}
	if !bytes.Equal(head[:], []byte(ggmlMagic)) {
		return fmt.Errorf(
			"models: %s is not a whisper.cpp model file (expected ggml magic bytes); redownload via yap models download <name>",
			path)
	}
	return nil
}
