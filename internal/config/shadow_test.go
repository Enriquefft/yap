package config_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/config"
)

// shadowMinimalNestedConfig is a valid nested TOML payload used by
// shadow-detection tests that need Load() to actually read and
// decode a file. It intentionally sets one non-default value so
// tests can assert which file won.
const shadowMinimalNestedConfig = `[general]
  hotkey = "KEY_F5"

[transcription]
  backend = "whisperlocal"
  language = "en"
`

// installCaptureLogger swaps slog.Default() for a JSON handler
// writing to buf and returns a cleanup that restores the original.
// Shadow-warning tests use this to assert that the expected slog
// record was emitted without coupling to stderr.
func installCaptureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return buf
}

// findShadowWarning parses the captured JSON-log buffer and returns
// the first record whose message contains the shadow-warning
// substring. Tests that expect the warning assert the returned map
// has the right fields; tests that expect silence assert the map is
// nil.
func findShadowWarning(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	dec := json.NewDecoder(buf)
	for {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			return nil
		}
		msg, _ := rec["msg"].(string)
		if strings.Contains(msg, "config file shadowed") {
			return rec
		}
	}
}

// writeShadowSetup creates a user XDG config at $XDG_CONFIG_HOME/yap
// and a fake system config in a TempDir, points the package-level
// systemConfigPath at the fake file via SetSystemConfigPathForTest,
// resets the shadow-warning once guard, and returns the user and
// system paths. t.Cleanup handles restoration.
func writeShadowSetup(t *testing.T, writeUser, writeSystem bool) (userPath, sysPath string) {
	t.Helper()

	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	userDir := filepath.Join(xdgDir, "yap")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userPath = filepath.Join(userDir, "config.toml")
	if writeUser {
		if err := os.WriteFile(userPath, []byte(shadowMinimalNestedConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sysDir := t.TempDir()
	sysPath = filepath.Join(sysDir, "system-config.toml")
	if writeSystem {
		if err := os.WriteFile(sysPath, []byte(shadowMinimalNestedConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	restoreSys := config.SetSystemConfigPathForTest(sysPath)
	t.Cleanup(restoreSys)

	config.ResetShadowWarningForTest()
	t.Cleanup(config.ResetShadowWarningForTest)

	return userPath, sysPath
}

func TestShadowWarning_BothFilesExist_Emits(t *testing.T) {
	userPath, sysPath := writeShadowSetup(t, true, true)
	buf := installCaptureLogger(t)

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	rec := findShadowWarning(t, buf)
	if rec == nil {
		t.Fatalf("expected shadow warning in captured log, got:\n%s", buf.String())
	}
	if level, _ := rec["level"].(string); level != "WARN" {
		t.Errorf("shadow warning level: got %q, want WARN", level)
	}
	if got, _ := rec["user_path"].(string); got != userPath {
		t.Errorf("shadow warning user_path: got %q, want %q", got, userPath)
	}
	if got, _ := rec["system_path"].(string); got != sysPath {
		t.Errorf("shadow warning system_path: got %q, want %q", got, sysPath)
	}
	if got, _ := rec["active"].(string); got != "user" {
		t.Errorf("shadow warning active: got %q, want %q", got, "user")
	}
}

func TestShadowWarning_OnlyUserExists_Silent(t *testing.T) {
	writeShadowSetup(t, true, false)
	buf := installCaptureLogger(t)

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	if rec := findShadowWarning(t, buf); rec != nil {
		t.Errorf("expected no shadow warning, got: %+v", rec)
	}
}

func TestShadowWarning_OnlySystemExists_Silent(t *testing.T) {
	writeShadowSetup(t, false, true)
	buf := installCaptureLogger(t)

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	if rec := findShadowWarning(t, buf); rec != nil {
		t.Errorf("expected no shadow warning, got: %+v", rec)
	}
}

func TestShadowWarning_NeitherExists_Silent(t *testing.T) {
	writeShadowSetup(t, false, false)
	buf := installCaptureLogger(t)

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	if rec := findShadowWarning(t, buf); rec != nil {
		t.Errorf("expected no shadow warning, got: %+v", rec)
	}
}

func TestShadowWarning_YAPConfigSet_Silent(t *testing.T) {
	// Both disk files exist, but the env override is set — shadow
	// detection is suppressed because the env path is explicit
	// intent and neither disk file is being "used".
	userPath, _ := writeShadowSetup(t, true, true)
	t.Setenv("YAP_CONFIG", userPath)

	buf := installCaptureLogger(t)

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	if rec := findShadowWarning(t, buf); rec != nil {
		t.Errorf("expected no shadow warning with YAP_CONFIG set, got: %+v", rec)
	}
}

func TestShadowWarning_FiresAtMostOncePerProcess(t *testing.T) {
	writeShadowSetup(t, true, true)
	buf := installCaptureLogger(t)

	for i := 0; i < 3; i++ {
		var notices bytes.Buffer
		if _, err := config.LoadWithNotices(&notices); err != nil {
			t.Fatalf("LoadWithNotices call %d: %v", i, err)
		}
	}

	// Count warning records — should be exactly one.
	dec := json.NewDecoder(buf)
	count := 0
	for {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if msg, _ := rec["msg"].(string); strings.Contains(msg, "config file shadowed") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shadow warning fired %d times across 3 Load() calls, want 1", count)
	}
}

func TestCandidatePaths_UserWinsOverSystem(t *testing.T) {
	userPath, sysPath := writeShadowSetup(t, true, true)

	cands, err := config.CandidatePaths()
	if err != nil {
		t.Fatalf("CandidatePaths: %v", err)
	}

	// $YAP_CONFIG is unset, so the slice is [user, system].
	if len(cands) != 2 {
		t.Fatalf("CandidatePaths len: got %d, want 2", len(cands))
	}
	if cands[0].Name != config.CandidateUser {
		t.Errorf("cands[0].Name: got %q, want %q", cands[0].Name, config.CandidateUser)
	}
	if cands[0].Path != userPath {
		t.Errorf("cands[0].Path: got %q, want %q", cands[0].Path, userPath)
	}
	if !cands[0].Exists || !cands[0].Active {
		t.Errorf("cands[0] should be exists+active: %+v", cands[0])
	}
	if cands[1].Name != config.CandidateSystem {
		t.Errorf("cands[1].Name: got %q, want %q", cands[1].Name, config.CandidateSystem)
	}
	if cands[1].Path != sysPath {
		t.Errorf("cands[1].Path: got %q, want %q", cands[1].Path, sysPath)
	}
	if !cands[1].Exists || cands[1].Active {
		t.Errorf("cands[1] should be exists but not active: %+v", cands[1])
	}
}

func TestCandidatePaths_SystemFallback(t *testing.T) {
	_, sysPath := writeShadowSetup(t, false, true)

	cands, err := config.CandidatePaths()
	if err != nil {
		t.Fatalf("CandidatePaths: %v", err)
	}

	// User missing, system present — system wins.
	for _, c := range cands {
		if c.Name == config.CandidateSystem {
			if !c.Active {
				t.Errorf("system candidate should be active: %+v", c)
			}
			if c.Path != sysPath {
				t.Errorf("system candidate path: got %q, want %q", c.Path, sysPath)
			}
		}
		if c.Name == config.CandidateUser && c.Active {
			t.Errorf("user candidate should not be active when missing: %+v", c)
		}
	}
}

func TestCandidatePaths_EnvOverrideWins(t *testing.T) {
	// Set up both disk files so the only way env can win is via
	// the Active flag.
	writeShadowSetup(t, true, true)

	envFile := filepath.Join(t.TempDir(), "env.toml")
	if err := os.WriteFile(envFile, []byte(shadowMinimalNestedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAP_CONFIG", envFile)

	cands, err := config.CandidatePaths()
	if err != nil {
		t.Fatalf("CandidatePaths: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("CandidatePaths len: got %d, want 3", len(cands))
	}
	if cands[0].Name != config.CandidateEnv {
		t.Errorf("cands[0].Name: got %q, want %q", cands[0].Name, config.CandidateEnv)
	}
	if cands[0].Path != envFile {
		t.Errorf("cands[0].Path: got %q, want %q", cands[0].Path, envFile)
	}
	if !cands[0].Active {
		t.Errorf("env candidate should be active: %+v", cands[0])
	}
	// The disk candidates must not be active when env is set.
	for _, c := range cands[1:] {
		if c.Active {
			t.Errorf("non-env candidate active with YAP_CONFIG set: %+v", c)
		}
	}
}

func TestCandidatePaths_NoneExistUserIsFirstRunTarget(t *testing.T) {
	userPath, _ := writeShadowSetup(t, false, false)

	cands, err := config.CandidatePaths()
	if err != nil {
		t.Fatalf("CandidatePaths: %v", err)
	}
	// User XDG is active as the first-run Save target.
	var userActive bool
	for _, c := range cands {
		if c.Name == config.CandidateUser && c.Active {
			userActive = true
			if c.Path != userPath {
				t.Errorf("first-run user path: got %q, want %q", c.Path, userPath)
			}
			if c.Exists {
				t.Errorf("first-run user should not exist: %+v", c)
			}
		}
	}
	if !userActive {
		t.Error("user XDG should be active as first-run Save target when no file exists")
	}
}

// TestCandidatePaths_NoSideEffect_DoesNotCreateDir is the L7
// regression guard: CandidatePaths must be a pure query with no
// filesystem side effects. Prior versions called xdg.ConfigFile(...)
// which MkdirAll'd $XDG_CONFIG_HOME/yap/ just to resolve the user
// path, surprising users running `yap config path` on a fresh host.
//
// This test points XDG_CONFIG_HOME at an empty TempDir, calls
// CandidatePaths, and asserts that $TMP/yap was NOT created. The
// per-user candidate path is still resolved correctly — it is just
// a string from xdg.ConfigHome, with no underlying directory work.
func TestCandidatePaths_NoSideEffect_DoesNotCreateDir(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	// Point the system path at a non-existent file in a different
	// TempDir so the system row is "(missing)" and cannot
	// accidentally pull in our XDG tempdir.
	sysDir := t.TempDir()
	restore := config.SetSystemConfigPathForTest(filepath.Join(sysDir, "nope.toml"))
	t.Cleanup(restore)

	yapDir := filepath.Join(xdgDir, "yap")
	// Sanity: the scratch directory is empty to start with.
	if _, err := os.Stat(yapDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist yet (err=%v)", yapDir, err)
	}

	cands, err := config.CandidatePaths()
	if err != nil {
		t.Fatalf("CandidatePaths: %v", err)
	}

	// The user candidate path must still resolve to the expected
	// XDG location — the fix must not break the happy path.
	wantUserPath := filepath.Join(yapDir, "config.toml")
	var sawUser bool
	for _, c := range cands {
		if c.Name == config.CandidateUser {
			sawUser = true
			if c.Path != wantUserPath {
				t.Errorf("user candidate path: got %q, want %q", c.Path, wantUserPath)
			}
			if c.Exists {
				t.Errorf("user candidate should not exist: %+v", c)
			}
		}
	}
	if !sawUser {
		t.Fatal("CandidatePaths did not include a user candidate")
	}

	// The load-bearing assertion: the XDG yap directory must NOT
	// have been created as a side effect of the query.
	if info, err := os.Stat(yapDir); err == nil {
		t.Errorf("CandidatePaths created %s (%v) — query-only API must not have filesystem side effects",
			yapDir, info.Mode())
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected error probing %s: %v", yapDir, err)
	}
}

// TestSave_CreatesDirOnWrite complements the side-effect-free
// L7 regression test: the write path (Save) must still create the
// XDG directory when it does not yet exist. This guards against a
// regression where the L7 fix would accidentally drop Save's
// MkdirAll along with CandidatePaths' implicit one.
func TestSave_CreatesDirOnWrite(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	// Precondition: the XDG yap directory does not exist.
	yapDir := filepath.Join(xdgDir, "yap")
	if _, err := os.Stat(yapDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist (err=%v)", yapDir, err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(yapDir)
	if err != nil {
		t.Fatalf("Save did not create %s: %v", yapDir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory: mode=%v", yapDir, info.Mode())
	}
	if _, err := os.Stat(filepath.Join(yapDir, "config.toml")); err != nil {
		t.Errorf("Save did not write config.toml: %v", err)
	}
}
