package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
)

// downloadClient is the HTTP client used by Download. It is the only
// package-level mutable state in the models package and is whitelisted
// by name in noglobals_test.go. Tests substitute it via
// SetDownloadClientForTest before exercising the download path.
//
// The default timeout is generous (15 minutes) because medium.en is
// 1.5 GB and a slow connection is still a working connection.
var downloadClient = &http.Client{Timeout: 15 * time.Minute}

// SetDownloadClientForTest replaces the package's HTTP client. Only
// _test.go files should call this. The function returns the previous
// client so the test can defer-restore it.
func SetDownloadClientForTest(c *http.Client) *http.Client {
	prev := downloadClient
	downloadClient = c
	return prev
}

// OverrideManifestForTest replaces the pinned manifest with override
// for the duration of a test. It returns a restore function the test
// must defer to put the production manifest back. Only _test.go files
// should call this; production code never mutates the manifest.
//
// The function lives in production code (not _test.go) so packages
// outside `models` — for example internal/cli's models_test.go — can
// import it via the public API.
func OverrideManifestForTest(override []Manifest) func() {
	prev := known
	known = override
	return func() { known = prev }
}

// Model is the surface returned by List. It pairs a manifest entry
// with its on-disk state at the moment of inspection.
type Model struct {
	Manifest
	// Installed reports whether the file exists in the cache.
	Installed bool
	// Path is the absolute on-disk path the file would live at,
	// regardless of whether it currently exists.
	Path string
}

// CacheDir returns the absolute path to the model cache directory,
// creating it if necessary. The directory is created with 0o755 so the
// user owns it and other users can read it (the model files are not
// secrets).
//
// xdg.Reload is called on every invocation so the function honors
// XDG_CACHE_HOME changes made after process start. The adrg/xdg
// library caches resolved directories at init time, which is the wrong
// shape for tests (each subtest installs a fresh temp cache).
func CacheDir() (string, error) {
	xdg.Reload()
	// xdg.CacheFile returns the absolute path of a file under the
	// XDG cache directory and creates the parent directory chain.
	// Pass a sentinel filename so we get the directory back via
	// filepath.Dir.
	p, err := xdg.CacheFile("yap/models/.keep")
	if err != nil {
		return "", fmt.Errorf("models: resolve cache dir: %w", err)
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("models: create cache dir %s: %w", dir, err)
	}
	return dir, nil
}

// Path returns the absolute path where the named model would live in
// the cache. The file may or may not exist; use Installed to check.
// Returns an error if name is not a pinned model.
func Path(name string) (string, error) {
	m, ok := lookupManifest(name)
	if !ok {
		return "", ErrUnknownModel(name)
	}
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, m.Filename()), nil
}

// Installed reports whether the named model file exists in the cache.
// A non-nil error is returned only if the cache directory cannot be
// resolved or stat fails for a reason other than "file does not
// exist".
func Installed(name string) (bool, error) {
	p, err := Path(name)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("models: stat %s: %w", p, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("models: cache path %s is a directory", p)
	}
	return true, nil
}

// List returns every pinned model with its current install state. The
// returned slice is freshly allocated each call.
func List() ([]Model, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(known))
	for _, m := range known {
		full := filepath.Join(dir, m.Filename())
		installed := false
		if info, statErr := os.Stat(full); statErr == nil && !info.IsDir() {
			installed = true
		}
		out = append(out, Model{
			Manifest:  m,
			Installed: installed,
			Path:      full,
		})
	}
	return out, nil
}

// Download fetches the named model into the cache directory and
// verifies the SHA256 against the manifest. The download is atomic:
// bytes are streamed to a sibling temp file, fsync'd, the SHA256 is
// validated, then the file is renamed into place. A failed verification
// removes the temp file so the cache is never left with a half-written
// or wrong-hash file.
//
// progress is an optional writer that receives one human-readable line
// per ~1% step (or per chunk for tiny files). Pass nil for silent
// downloads. Library callers usually pass nil; the CLI passes os.Stdout.
//
// Download returns nil on success. The model file is then resolvable
// via Path(name).
func Download(ctx context.Context, name string, progress io.Writer) error {
	m, ok := lookupManifest(name)
	if !ok {
		return ErrUnknownModel(name)
	}
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, m.Filename())

	// Atomic temp-file in the same directory so the rename is
	// guaranteed to be on the same filesystem.
	tmp, err := os.CreateTemp(dir, "."+m.Filename()+".*.part")
	if err != nil {
		return fmt.Errorf("models: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Cleanup on every failure path. Success removes tmpPath via
	// rename, after which os.Remove is a no-op.
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("models: build request: %w", err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("models: GET %s: %w", m.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("models: GET %s: HTTP %d", m.URL, resp.StatusCode)
	}

	hasher := sha256.New()
	expected := int64(m.SizeMB) * 1024 * 1024
	if resp.ContentLength > 0 {
		expected = resp.ContentLength
	}

	pw := newProgressWriter(progress, m.Name, expected)
	written, err := io.Copy(io.MultiWriter(tmp, hasher, pw), resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("models: stream %s: %w", m.URL, err)
	}
	pw.finish(written)

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("models: sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("models: close %s: %w", tmpPath, err)
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if gotHash != m.SHA256 {
		return fmt.Errorf(
			"models: SHA256 mismatch for %s: got %s, expected %s "+
				"(file rejected; cache is unchanged)",
			m.Name, gotHash, m.SHA256)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("models: rename %s -> %s: %w", tmpPath, finalPath, err)
	}
	return nil
}

// progressWriter writes percent-step progress lines to a sink. It is
// allocation-light: one struct, no buffering, one line per percent.
// When sink is nil the writer is a no-op so library callers pay
// nothing.
type progressWriter struct {
	sink     io.Writer
	name     string
	total    int64
	count    int64
	lastPct  int
	announce bool
}

func newProgressWriter(sink io.Writer, name string, total int64) *progressWriter {
	return &progressWriter{
		sink:     sink,
		name:     name,
		total:    total,
		lastPct:  -1,
		announce: sink != nil,
	}
}

// Write satisfies io.Writer. It tracks bytes written and emits a line
// when the percent-of-total advances by at least one whole percent.
func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	if !p.announce {
		return n, nil
	}
	p.count += int64(n)
	if p.total <= 0 {
		return n, nil
	}
	pct := int(float64(p.count) * 100 / float64(p.total))
	if pct > 100 {
		pct = 100
	}
	if pct != p.lastPct {
		p.lastPct = pct
		fmt.Fprintf(p.sink, "downloading %s: %3d%% (%d/%d bytes)\n",
			p.name, pct, p.count, p.total)
	}
	return n, nil
}

// finish prints a final 100% line if the loop did not naturally hit it
// (small file with no progress steps).
func (p *progressWriter) finish(written int64) {
	if !p.announce {
		return
	}
	if p.lastPct < 100 {
		fmt.Fprintf(p.sink, "downloading %s: 100%% (%d bytes)\n", p.name, written)
	}
}
