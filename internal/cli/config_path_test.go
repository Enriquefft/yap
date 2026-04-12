package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/internal/config"
)

// configPathTestMinimalConfig is the smallest valid nested payload
// config-path tests need to put on disk. It is intentionally tiny
// because these tests care about the candidate table, not decoded
// values.
const configPathTestMinimalConfig = `[general]
  hotkey = "KEY_F5"

[transcription]
  backend = "whisperlocal"
  language = "en"
`

func TestConfigPath_OnlyUser_MarksUserActive(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	// Point the system path at a TempDir file that does NOT
	// exist, so the system row shows "(missing)".
	sysDir := t.TempDir()
	restore := config.SetSystemConfigPathForTest(filepath.Join(sysDir, "nope.toml"))
	t.Cleanup(restore)

	userPath := filepath.Join(xdgDir, "yap", "config.toml")
	writeConfigFile(t, userPath, configPathTestMinimalConfig)

	out, errOut, err := runCLI(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: err=%v stderr=%s", err, errOut)
	}

	if !strings.Contains(out, "resolved: "+userPath) {
		t.Errorf("missing resolved line for user path; got:\n%s", out)
	}
	if !strings.Contains(out, "candidates:") {
		t.Errorf("missing candidates header; got:\n%s", out)
	}
	if !strings.Contains(out, "$YAP_CONFIG") {
		t.Errorf("missing $YAP_CONFIG row; got:\n%s", out)
	}
	if !strings.Contains(out, "(unset)") {
		t.Errorf("env row should show (unset); got:\n%s", out)
	}
	if !strings.Contains(out, userPath+"  ") || !strings.Contains(out, "(exists, ACTIVE)") {
		t.Errorf("user row missing or not marked ACTIVE; got:\n%s", out)
	}
	if !strings.Contains(out, "(missing)") {
		t.Errorf("system row should be (missing); got:\n%s", out)
	}
}

func TestConfigPath_BothExist_ShowsShadowed(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	sysDir := t.TempDir()
	sysPath := filepath.Join(sysDir, "system-config.toml")
	if err := os.WriteFile(sysPath, []byte(configPathTestMinimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := config.SetSystemConfigPathForTest(sysPath)
	t.Cleanup(restore)

	userPath := filepath.Join(xdgDir, "yap", "config.toml")
	writeConfigFile(t, userPath, configPathTestMinimalConfig)

	// Reset the shadow warning once guard so LoadWithNotices
	// — even if called elsewhere in the same test binary —
	// doesn't interfere with this invocation's assertions.
	config.ResetShadowWarningForTest()
	t.Cleanup(config.ResetShadowWarningForTest)

	out, errOut, err := runCLI(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: err=%v stderr=%s", err, errOut)
	}

	if !strings.Contains(out, "resolved: "+userPath) {
		t.Errorf("missing resolved line for user path; got:\n%s", out)
	}
	// The user file must show ACTIVE.
	if !strings.Contains(out, "(exists, ACTIVE)") {
		t.Errorf("user row missing (exists, ACTIVE); got:\n%s", out)
	}
	// The system file must show shadowed.
	if !strings.Contains(out, "(exists, shadowed)") {
		t.Errorf("system row missing (exists, shadowed); got:\n%s", out)
	}
	// Both paths must appear in the output.
	if !strings.Contains(out, userPath) {
		t.Errorf("user path missing from output; got:\n%s", out)
	}
	if !strings.Contains(out, sysPath) {
		t.Errorf("system path missing from output; got:\n%s", out)
	}
}

func TestConfigPath_NoFiles_UserIsFirstRunTarget(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("YAP_CONFIG", "")

	sysDir := t.TempDir()
	restore := config.SetSystemConfigPathForTest(filepath.Join(sysDir, "nope.toml"))
	t.Cleanup(restore)

	out, errOut, err := runCLI(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: err=%v stderr=%s", err, errOut)
	}

	if !strings.Contains(out, "resolved: ") {
		t.Errorf("missing resolved line; got:\n%s", out)
	}
	if !strings.Contains(out, "first-run Save target") {
		t.Errorf("expected first-run Save target label on user row; got:\n%s", out)
	}
}

func TestConfigPath_YAPConfigSet_ShowsEnvActive(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	envDir := t.TempDir()
	envFile := filepath.Join(envDir, "env.toml")
	if err := os.WriteFile(envFile, []byte(configPathTestMinimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAP_CONFIG", envFile)

	sysDir := t.TempDir()
	restore := config.SetSystemConfigPathForTest(filepath.Join(sysDir, "nope.toml"))
	t.Cleanup(restore)

	out, errOut, err := runCLI(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: err=%v stderr=%s", err, errOut)
	}

	if !strings.Contains(out, "resolved: "+envFile) {
		t.Errorf("resolved should be env file; got:\n%s", out)
	}
	if !strings.Contains(out, "$YAP_CONFIG="+envFile) {
		t.Errorf("env row missing $YAP_CONFIG=<path>; got:\n%s", out)
	}
	if !strings.Contains(out, "(exists, ACTIVE)") {
		t.Errorf("env row missing ACTIVE status; got:\n%s", out)
	}
}
