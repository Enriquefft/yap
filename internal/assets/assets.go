package assets

import (
	"bytes"
	"embed"
	"io"
)

//go:embed start.wav stop.wav warning.wav
var FS embed.FS

// StartChime returns an io.Reader for the embedded start chime WAV bytes.
// The reader is backed by in-memory bytes — no file I/O at runtime.
func StartChime() (io.Reader, error) {
	data, err := FS.ReadFile("start.wav")
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// StopChime returns an io.Reader for the embedded stop chime WAV bytes.
func StopChime() (io.Reader, error) {
	data, err := FS.ReadFile("stop.wav")
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// WarningChime returns an io.Reader for the embedded 50-second recording warning beep (770Hz).
// Used to alert users when they approach the recording time limit.
func WarningChime() (io.Reader, error) {
	data, err := FS.ReadFile("warning.wav")
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// ListAssets returns the names of all embedded asset files.
// Used by --list-assets debug flag and tests to verify embedding.
func ListAssets() ([]string, error) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
