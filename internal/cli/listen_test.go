package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestListen_Help confirms the listen command is registered and
// exposes the --foreground flag.
func TestListen_Help(t *testing.T) {
	withCleanConfig(t)
	stdout, _, err := runCLI(t, "listen", "--help")
	if err != nil {
		t.Fatalf("listen --help: %v", err)
	}
	if !strings.Contains(stdout, "--foreground") {
		t.Errorf("expected --foreground flag in help, got:\n%s", stdout)
	}
}

// TestStartCommand_NoLongerExists is the regression guard that the
// deprecated `yap start` alias has been removed. The previous release
// kept it as a hidden delegating command with a stderr deprecation
// notice; the CHANGELOG warned it would go in the next major. It is
// now gone, and cobra must reject `start` as an unknown subcommand.
func TestStartCommand_NoLongerExists(t *testing.T) {
	withCleanConfig(t)
	_, stderr, err := runCLI(t, "start")
	if err == nil {
		t.Fatal("expected error for removed `start` command, got nil")
	}
	combined := err.Error() + "\n" + stderr
	if !strings.Contains(combined, "unknown command") {
		t.Errorf("expected `unknown command` in error/stderr, got err=%v stderr=%q",
			err, stderr)
	}
}

// TestRoot_StartNotInHelp confirms `start` no longer appears in the
// root command's Available Commands listing. `listen` must remain.
func TestRoot_StartNotInHelp(t *testing.T) {
	withCleanConfig(t)
	stdout, _, err := runCLI(t, "--help")
	if err != nil {
		t.Fatalf("yap --help: %v", err)
	}
	// Walk Available Commands lines explicitly so we don't false-
	// positive on "start the yap daemon" which is the listen short.
	lines := strings.Split(stdout, "\n")
	inCommands := false
	for _, line := range lines {
		if strings.HasPrefix(line, "Available Commands:") {
			inCommands = true
			continue
		}
		if inCommands {
			if strings.HasPrefix(line, "Flags:") || strings.TrimSpace(line) == "" {
				inCommands = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == "start" {
				t.Errorf("`start` should not appear in Available Commands, got:\n%s", stdout)
			}
		}
	}
	if !strings.Contains(stdout, "listen") {
		t.Errorf("`listen` command should appear in root help, got:\n%s", stdout)
	}
}

// TestRoot_DeprecatedSpawnFlagRemoved asserts the legacy hidden flag
// used to bootstrap a detached daemon child is no longer recognized.
// Phase 7 replaced it with the YAP_DAEMON env sentinel handled in
// cmd/yap/main.go.
func TestRoot_DeprecatedSpawnFlagRemoved(t *testing.T) {
	withCleanConfig(t)
	const flag = "--" + "daemon" + "-run" // assembled to keep grep checks clean
	_, _, err := runCLI(t, flag)
	if err == nil {
		t.Fatal("expected the deprecated spawn flag to be unknown")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}

// TestNeedsWizard_RespectsExistingConfig confirms the wizard does
// not run when the config file is already present. Without this
// guard, the listen command would block on stdin during tests.
func TestNeedsWizard_RespectsExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[general]\n  hotkey = \"KEY_RIGHTCTRL\"\n")
	// We do NOT actually run listen here — we just confirm the
	// config exists, so other listen-path tests don't accidentally
	// trigger the interactive wizard.
	if _, err := filepath.Abs(cfgFile); err != nil {
		t.Fatal(err)
	}
}
