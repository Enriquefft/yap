package inject

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeExec returns an ExecCommand stub that maps a command name to a
// pre-recorded stdout body. The fake reuses the test binary itself as
// the helper process: see TestHelperProcess for the implementation.
//
// Each call records the (name, args) pair into calls so tests can
// assert what the strategy invoked.
type fakeExec struct {
	stdout map[string]string
	stderr map[string]string
	exit   map[string]int
	calls  []fakeCall
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeExec) command(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	cs := []string{"-test.run=TestHelperProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	env := []string{
		"GO_WANT_HELPER_PROCESS=1",
		"HELPER_STDOUT=" + f.stdout[name],
		"HELPER_STDERR=" + f.stderr[name],
	}
	if code, ok := f.exit[name]; ok {
		env = append(env, "HELPER_EXIT="+itoa(code))
	}
	cmd.Env = env
	return cmd
}

// commandContext lifts the (name, args) fake into the ExecCommandContext
// shape used by Deps. The fake does not honour ctx — strategy callers
// pass ctx through for cancellation in production, and tests rely on
// ctx-Done in dedicated tests rather than this fake.
func (f *fakeExec) commandContext(_ context.Context, name string, args ...string) *exec.Cmd {
	return f.command(name, args...)
}

// TestHelperProcess is the worker side of fakeExec. It is invoked as a
// child process via os.Args[0] -test.run=TestHelperProcess. When
// GO_WANT_HELPER_PROCESS=1 it writes the canned stdout/stderr and
// exits with the requested code; otherwise it returns immediately so
// `go test` ignores it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if s := os.Getenv("HELPER_STDOUT"); s != "" {
		_, _ = os.Stdout.WriteString(s)
	}
	if s := os.Getenv("HELPER_STDERR"); s != "" {
		_, _ = os.Stderr.WriteString(s)
	}
	code := 0
	if s := os.Getenv("HELPER_EXIT"); s != "" {
		var err error
		code, err = atoi(s)
		if err != nil {
			code = 1
		}
	}
	os.Exit(code)
}

// itoa / atoi avoid pulling strconv into the test wiring path; the
// helper process is already minimal.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func atoi(s string) (int, error) {
	n := 0
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

// envFunc returns an EnvGet stub backed by a map.
func envFunc(env map[string]string) func(string) string {
	return func(k string) string {
		return env[k]
	}
}

func TestDetectDisplayServer(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"wayland wins when both set", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "DISPLAY": ":0"}, "wayland"},
		{"x11 only", map[string]string{"DISPLAY": ":0"}, "x11"},
		{"wayland only", map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, "wayland"},
		{"neither", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := Deps{EnvGet: envFunc(tc.env)}
			if got := detectDisplayServer(deps); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectNoDisplayReturnsErrNoDisplay(t *testing.T) {
	deps := Deps{EnvGet: envFunc(map[string]string{})}
	_, err := Detect(context.Background(), deps)
	if !errors.Is(err, ErrNoDisplay) {
		t.Errorf("err = %v, want ErrNoDisplay", err)
	}
}

func TestDetectSwayParsesFocusedNode(t *testing.T) {
	const tree = `{
		"focused": false,
		"nodes": [
			{
				"focused": false,
				"nodes": [
					{
						"focused": true,
						"app_id": "kitty",
						"pid": 4242
					}
				]
			}
		]
	}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet:      envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "SWAYSOCK": "/run/sway"}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.DisplayServer != "wayland" {
		t.Errorf("display = %q, want wayland", tgt.DisplayServer)
	}
	if tgt.AppClass != "kitty" {
		t.Errorf("class = %q, want kitty", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppTerminal {
		t.Errorf("type = %v, want AppTerminal", tgt.AppType)
	}
	if tgt.WindowID != "4242" {
		t.Errorf("window id = %q, want 4242", tgt.WindowID)
	}
}

func TestDetectSwayXWaylandWindowProperties(t *testing.T) {
	const tree = `{
		"nodes": [
			{
				"focused": true,
				"app_id": "",
				"pid": 17,
				"window_properties": {"class": "Firefox"}
			}
		]
	}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet:      envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "SWAYSOCK": "/run/sway"}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.AppClass != "firefox" {
		t.Errorf("class = %q, want firefox", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppBrowser {
		t.Errorf("type = %v, want AppBrowser", tgt.AppType)
	}
}

func TestDetectHyprland(t *testing.T) {
	const json = `{"class":"Code","pid":99}`
	fe := &fakeExec{stdout: map[string]string{"hyprctl": json}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY":             "wayland-1",
			"HYPRLAND_INSTANCE_SIGNATURE": "abc",
		}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.AppClass != "code" {
		t.Errorf("class = %q, want code", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppElectron {
		t.Errorf("type = %v, want AppElectron", tgt.AppType)
	}
	if tgt.WindowID != "99" {
		t.Errorf("window id = %q, want 99", tgt.WindowID)
	}
}

func TestDetectGenericWaylandFallsThrough(t *testing.T) {
	deps := Deps{
		ExecCommandContext: func(context.Context, string, ...string) *exec.Cmd { return exec.Command("false") },
		EnvGet:             envFunc(map[string]string{"WAYLAND_DISPLAY": "wayland-1"}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.DisplayServer != "wayland" {
		t.Errorf("display = %q, want wayland", tgt.DisplayServer)
	}
	if tgt.AppType != yinject.AppGeneric {
		t.Errorf("type = %v, want AppGeneric", tgt.AppType)
	}
	if tgt.AppClass != "" {
		t.Errorf("class = %q, want empty", tgt.AppClass)
	}
}

func TestDetectX11(t *testing.T) {
	stdout := map[string]string{
		"xdotool": "12345\n",
		"xprop":   "WM_CLASS(STRING) = \"foot\", \"foot\"\n_NET_WM_PID(CARDINAL) = 8888\n",
	}
	fe := &fakeExec{stdout: stdout}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet:      envFunc(map[string]string{"DISPLAY": ":0"}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.DisplayServer != "x11" {
		t.Errorf("display = %q, want x11", tgt.DisplayServer)
	}
	if tgt.AppClass != "foot" {
		t.Errorf("class = %q, want foot", tgt.AppClass)
	}
	if tgt.AppType != yinject.AppTerminal {
		t.Errorf("type = %v, want AppTerminal", tgt.AppType)
	}
	if tgt.WindowID != "8888" {
		t.Errorf("window id = %q, want 8888", tgt.WindowID)
	}
}

func TestDetectAnnotateTmuxOnTerminal(t *testing.T) {
	const tree = `{"focused":true,"app_id":"alacritty","pid":1}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
			"TMUX":            "/tmp/tmux-1000/default,1234,0",
		}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !tgt.Tmux {
		t.Error("Tmux annotation should be set")
	}
}

func TestDetectAnnotateTmuxNotSetForGenericApp(t *testing.T) {
	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"TMUX":            "/tmp/tmux-1000/default,1234,0",
		}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if tgt.Tmux {
		t.Error("Tmux must not be set on a generic-GUI target")
	}
}

func TestDetectAnnotateSSHRemote(t *testing.T) {
	deps := Deps{
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SSH_TTY":         "/dev/pts/2",
		}),
	}
	tgt, err := Detect(context.Background(), deps)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !tgt.SSHRemote {
		t.Error("SSHRemote should be set when SSH_TTY is present")
	}
}

func TestParseXpropWMClass(t *testing.T) {
	cases := map[string]string{
		`WM_CLASS(STRING) = "kitty", "kitty"`:                    "kitty",
		`WM_CLASS(STRING) = "code", "Code"`:                      "Code",
		`WM_CLASS(STRING) = "navigator", "Firefox"`:              "Firefox",
		`WM_CLASS(STRING) = "Navigator", "Firefox-Esr"`:          "Firefox-Esr",
		`WM_CLASS:  not set`:                                     "",
		`WM_CLASS(STRING) =`:                                     "",
		`WM_CLASS(STRING) = "alacritty", "Alacritty"        `:    "Alacritty",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := parseXpropWMClass(in); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestParseXpropPID(t *testing.T) {
	cases := map[string]int{
		`_NET_WM_PID(CARDINAL) = 12345`: 12345,
		`_NET_WM_PID(CARDINAL) = 0`:     0,
		`_NET_WM_PID:  not found`:       0,
		`_NET_WM_PID(CARDINAL) = abc`:   0,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := parseXpropPID(in); got != want {
				t.Errorf("got %d, want %d", got, want)
			}
		})
	}
}
