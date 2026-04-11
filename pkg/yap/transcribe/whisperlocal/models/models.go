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
	"sync"
	"time"

	"github.com/adrg/xdg"
)

// defaultDownloadTimeout is the per-request HTTP timeout used by the
// production Manager. The default is generous (15 minutes) because
// medium.en is 1.5 GB and a slow connection is still a working
// connection.
const defaultDownloadTimeout = 15 * time.Minute

// lockFileName is the sentinel file used by the inter-process advisory
// lock that serializes concurrent Download calls against the same
// cache directory.
const lockFileName = ".lock"

// Manager owns the whisper.cpp model cache for a single yap process.
//
// All methods are safe for concurrent use within a process; the
// inter-process safety of Download is provided by an advisory file
// lock on the cache directory's .lock sentinel.
//
// Tests construct their own Manager (via NewManager) with an httptest
// client and a fixture manifest. Production callers use Default(),
// which returns a lazily-built singleton wired to the real Hugging
// Face URLs and the user's XDG cache.
type Manager struct {
	client   *http.Client
	manifest []Manifest
}

// ManagerOption configures a Manager at construction time.
type ManagerOption func(*Manager)

// WithHTTPClient overrides the default HTTP client used by Download.
// Tests use this to inject an httptest server's client.
func WithHTTPClient(c *http.Client) ManagerOption {
	return func(m *Manager) { m.client = c }
}

// WithManifest overrides the pinned manifest. Tests use this to
// construct a Manager that knows about a fixture model rather than
// the production English-only models.
func WithManifest(manifest []Manifest) ManagerOption {
	return func(m *Manager) {
		m.manifest = make([]Manifest, len(manifest))
		copy(m.manifest, manifest)
	}
}

// NewManager constructs a Manager with the given options applied on
// top of production defaults.
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		client:   &http.Client{Timeout: defaultDownloadTimeout},
		manifest: knownCopy(),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// defaultManager is the lazily-built production singleton. It is
// guarded by defaultOnce so the construction happens exactly once.
//
// The free functions (Path, Installed, Download, List, CacheDir)
// delegate to defaultManager so existing call sites in internal/cli
// and pkg/yap/transcribe/whisperlocal/discover.go continue to work
// without code changes.
//
// cacheDirMu serializes calls to xdg.Reload + CacheFile inside
// CacheDir. The adrg/xdg vendor library writes to its package-level
// globals without synchronisation, so concurrent callers (two
// parallel Download calls, for instance) would race it. The mutex
// lives at the models package level because the races are on xdg's
// globals and every CacheDir path — whether via a Manager or a
// free function wrapper — must funnel through the same lock.
var (
	defaultOnce    sync.Once
	defaultManager *Manager
	cacheDirMu     sync.Mutex
)

// Default returns the package-level singleton Manager wired to the
// real Hugging Face URLs and the production HTTP client. It is
// constructed exactly once on first call.
func Default() *Manager {
	defaultOnce.Do(func() {
		defaultManager = NewManager()
	})
	return defaultManager
}

// Model is the surface returned by List. It pairs a manifest entry
// with its on-disk state at the moment of inspection.
//
// The (Installed, Corrupt) pair partitions the three states a cache
// entry can be in:
//
//   - Installed=true,  Corrupt=false — file exists and passes
//     VerifyGGMLMagic. Ready to use.
//   - Installed=false, Corrupt=false — file does not exist. Run
//     `yap models download <name>` to fetch it.
//   - Installed=false, Corrupt=true  — file exists on disk but is
//     not a valid whisper.cpp model (half-written download, saved
//     404 HTML body, renamed ZIP, truncated file, etc.). The user
//     must `rm` the file before `yap models download` will succeed
//     if the cache directory is writable; if the cache is a read-
//     only link (Nix store, sandboxed Flatpak) they need to clear
//     it out-of-band.
//
// Installed is kept as the positive-only answer so existing callers
// (`if m.Installed { ... }`) stay correct — a corrupt file must not
// count as installed.
type Model struct {
	Manifest
	// Installed reports whether the file exists in the cache AND
	// passes ggml magic-byte validation.
	Installed bool
	// Corrupt reports whether the file exists on disk but fails
	// ggml magic-byte validation. Mutually exclusive with
	// Installed: a file is either ready to use, corrupt, or absent.
	Corrupt bool
	// Path is the absolute on-disk path the file would live at,
	// regardless of whether it currently exists.
	Path string
}

// cacheFileState classifies a single cache-path as one of the three
// disposition states List surfaces. It is the single source of truth
// for the stat + magic-byte check used by List() and Installed(), so
// the two entry points cannot disagree about what "installed" means.
//
// Returns installed=true if the file exists and passes
// VerifyGGMLMagic. Returns corrupt=true if the file exists but fails
// verification. Returns both false if the file is absent. A non-nil
// error is reserved for unexpected stat failures (permission denied,
// I/O error) and for the pathological "cache path is a directory"
// case — not for "file does not exist" or "magic bytes wrong", which
// are normal outcomes a user can recover from.
func cacheFileState(path string) (installed, corrupt bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("models: stat %s: %w", path, statErr)
	}
	if info.IsDir() {
		return false, false, fmt.Errorf("models: cache path %s is a directory", path)
	}
	if VerifyGGMLMagic(path) != nil {
		return false, true, nil
	}
	return true, false, nil
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
//
// cacheDirMu protects the xdg.Reload + CacheFile sequence because
// the adrg/xdg library mutates its own package globals without any
// synchronisation of its own. Two parallel Download calls would
// otherwise race the xdg library; holding the mutex for the duration
// of the two calls funnels them through a single writer.
func CacheDir() (string, error) {
	cacheDirMu.Lock()
	defer cacheDirMu.Unlock()
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
//
// Names are matched case-insensitively against the manifest.
func (m *Manager) Path(name string) (string, error) {
	manifest, ok := m.lookup(name)
	if !ok {
		return "", ErrUnknownModelFromManifest(name, m.manifest)
	}
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, manifest.Filename()), nil
}

// Installed reports whether the named model file exists in the cache
// AND passes ggml magic-byte validation. A file that exists on disk
// but fails verification (e.g. a half-written download, a saved 404
// HTML body, a renamed ZIP) is reported as NOT installed — the same
// answer a fresh cache would give — so `yap models list` and the
// downstream resolveModel check agree on what counts as a real
// model.
//
// Callers who need to distinguish "missing" from "present-but-corrupt"
// should use List() instead, which populates Model.Corrupt alongside
// Model.Installed. Installed() collapses both of those into false
// because every Installed() caller today only needs the ready-to-use
// yes/no, and threading a second return value through every call
// site would be noise without a corresponding benefit.
//
// A non-nil error is returned only if the cache directory cannot be
// resolved or stat fails for a reason other than "file does not
// exist". Magic-byte failures are not errors here; they are simply
// "not installed" because the user's recovery action is identical
// (run `yap models download <name>`).
func (m *Manager) Installed(name string) (bool, error) {
	p, err := m.Path(name)
	if err != nil {
		return false, err
	}
	installed, _, err := cacheFileState(p)
	if err != nil {
		return false, err
	}
	return installed, nil
}

// List returns every pinned model with its current install state. The
// returned slice is freshly allocated each call.
//
// Model.Installed is true only when the file both exists in the cache
// AND passes ggml magic-byte validation — the same contract
// Installed() enforces, so the two entry points give users a
// consistent answer and junk files never show up as "installed".
//
// Model.Corrupt is true when the file exists on disk but fails
// magic-byte verification. A corrupt file is NEITHER installed nor
// missing — List surfaces it as a distinct third state so the CLI
// renderer can tell the user their cache has a bad file that needs
// removing (especially important when the cache directory is a
// read-only Nix store link and a redownload would fail in a
// confusing way).
func (m *Manager) List() ([]Model, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(m.manifest))
	for _, entry := range m.manifest {
		full := filepath.Join(dir, entry.Filename())
		installed, corrupt, stateErr := cacheFileState(full)
		if stateErr != nil {
			return nil, stateErr
		}
		out = append(out, Model{
			Manifest:  entry,
			Installed: installed,
			Corrupt:   corrupt,
			Path:      full,
		})
	}
	return out, nil
}

// Download fetches the named model into the cache directory and
// verifies the SHA256 against the manifest.
//
// The download is atomic: bytes are streamed to a sibling temp file,
// fsync'd, the SHA256 is validated, then the file is renamed into
// place. A failed verification removes the temp file so the cache is
// never left with a half-written or wrong-hash file.
//
// Concurrent downloads from multiple yap processes are serialized via
// an advisory file lock on <cache>/.lock. The second process blocks
// until the first finishes, then notices the file already exists and
// returns immediately without re-downloading.
//
// progress is an optional writer that receives one human-readable line
// per ~1% step (or per chunk for tiny files). Pass nil for silent
// downloads. Library callers usually pass nil; the CLI passes os.Stdout.
//
// Download returns nil on success. The model file is then resolvable
// via Path(name).
func (m *Manager) Download(ctx context.Context, name string, progress io.Writer) error {
	manifest, ok := m.lookup(name)
	if !ok {
		return ErrUnknownModelFromManifest(name, m.manifest)
	}
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, manifest.Filename())

	// Acquire the inter-process advisory lock before doing any work.
	// LOCK_EX blocks until any concurrent downloader finishes; once
	// we hold the lock we re-check whether the file already exists
	// (the other downloader may have finished it for us) and skip
	// the network round-trip in that case.
	unlock, err := acquireCacheLock(dir)
	if err != nil {
		return err
	}
	defer unlock()

	if info, statErr := os.Stat(finalPath); statErr == nil && !info.IsDir() {
		// Another process finished the download while we waited
		// on the lock. Verify the existing file's hash to confirm
		// it matches the pinned manifest before declaring success.
		if hashErr := verifyFileSHA256(finalPath, manifest.SHA256); hashErr != nil {
			return fmt.Errorf("models: existing %s failed verification: %w", finalPath, hashErr)
		}
		return nil
	}

	return m.downloadLocked(ctx, manifest, dir, finalPath, progress)
}

// downloadLocked performs the actual download with the cache lock
// already held. It is split out so the lock-acquire path stays
// readable.
func (m *Manager) downloadLocked(
	ctx context.Context,
	manifest Manifest,
	dir, finalPath string,
	progress io.Writer,
) error {
	// Atomic temp-file in the same directory so the rename is
	// guaranteed to be on the same filesystem.
	tmp, err := os.CreateTemp(dir, "."+manifest.Filename()+".*.part")
	if err != nil {
		return fmt.Errorf("models: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Single-source close + cleanup. The defer-close is a no-op if
	// we already closed before rename (the typical happy path); the
	// defer-remove is a no-op if rename succeeded. Both are safe to
	// run regardless of success or failure path, which makes future
	// early-return additions trivially correct.
	closed := false
	closeOnce := func() error {
		if closed {
			return nil
		}
		closed = true
		return tmp.Close()
	}
	defer func() { _ = closeOnce() }()
	defer func() { _ = os.Remove(tmpPath) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.URL, nil)
	if err != nil {
		return fmt.Errorf("models: build request: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("models: GET %s: %w", manifest.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models: GET %s: HTTP %d", manifest.URL, resp.StatusCode)
	}

	hasher := sha256.New()
	expected := int64(manifest.SizeMB) * 1024 * 1024
	if resp.ContentLength > 0 {
		expected = resp.ContentLength
	}

	pw := newProgressWriter(progress, manifest.Name, expected)
	written, err := io.Copy(io.MultiWriter(tmp, hasher, pw), resp.Body)
	if err != nil {
		return fmt.Errorf("models: stream %s: %w", manifest.URL, err)
	}
	pw.finish(written)

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("models: sync %s: %w", tmpPath, err)
	}
	// Some filesystems require the file to be closed before rename.
	// closeOnce makes the deferred close above a no-op so we can
	// surface a meaningful error here without double-close panics.
	if err := closeOnce(); err != nil {
		return fmt.Errorf("models: close %s: %w", tmpPath, err)
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if gotHash != manifest.SHA256 {
		return fmt.Errorf(
			"models: SHA256 mismatch for %s: got %s, expected %s "+
				"(file rejected; cache is unchanged)",
			manifest.Name, gotHash, manifest.SHA256)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf(
			"models: download verified, rename %s -> %s failed: %w",
			tmpPath, finalPath, err)
	}
	return nil
}

// lookup is the case-insensitive manifest match against the
// Manager's pinned slice.
func (m *Manager) lookup(name string) (Manifest, bool) {
	return lookupManifestIn(m.manifest, name)
}

// Manifest returns a copy of the Manager's pinned manifest. The
// returned slice is freshly allocated so callers cannot mutate the
// Manager's state.
func (m *Manager) Manifest() []Manifest {
	out := make([]Manifest, len(m.manifest))
	copy(out, m.manifest)
	return out
}

// acquireCacheLock is defined in lock_unix.go / lock_windows.go so
// Download can use an advisory file lock without pulling in
// platform-specific syscall code at the call site.

// verifyFileSHA256 streams a file through SHA256 and compares the
// hex digest to expected. Used to validate that an
// already-on-disk file (downloaded by a sibling process) really
// matches the pinned hash before reporting success.
func verifyFileSHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expected {
		return fmt.Errorf("SHA256 mismatch for %s: got %s, expected %s", path, got, expected)
	}
	return nil
}

// Path is the package-level wrapper for Manager.Path that delegates
// to the production singleton. Existing call sites in the CLI and
// the discover layer continue to work unchanged.
func Path(name string) (string, error) { return Default().Path(name) }

// Installed is the package-level wrapper for Manager.Installed.
func Installed(name string) (bool, error) { return Default().Installed(name) }

// List is the package-level wrapper for Manager.List.
func List() ([]Model, error) { return Default().List() }

// Download is the package-level wrapper for Manager.Download.
func Download(ctx context.Context, name string, progress io.Writer) error {
	return Default().Download(ctx, name, progress)
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
