package ipc

const (
	CmdStop   = "stop"
	CmdStatus = "status"
	CmdToggle = "toggle"
)

// Request is a client → daemon command.
type Request struct {
	Cmd string `json:"cmd"`
	// Exec is an optional command name for the exec output mode.
	// When non-empty on a CmdToggle request, the daemon pipes the
	// transcript to this command via stdin instead of injecting into
	// the focused application. Ignored for non-toggle commands.
	Exec string `json:"exec,omitempty"`
}

// Response is a daemon → client response.
//
// Ok indicates success; State is present for status, Error for
// failures. The remaining fields are populated only by CmdStatus and
// only when the daemon has them — every status field is omitempty so
// non-status responses (toggle, stop) round-trip as the original
// {ok,state,error} triple they always emitted.
//
// The Phase 7 status extension exposes ConfigPath, Version, PID,
// Backend, Model, and Mode so `yap status` can replace the previous
// hand-rolled bash invocations operators used to discover what the
// running daemon is configured for. Every new field is optional on
// the wire.
type Response struct {
	Ok         bool   `json:"ok"`
	State      string `json:"state,omitempty"`
	Mode       string `json:"mode,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
	Version    string `json:"version,omitempty"`
	PID        int    `json:"pid,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Model      string `json:"model,omitempty"`
	Error      string `json:"error,omitempty"`
}
