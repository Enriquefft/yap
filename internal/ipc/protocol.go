package ipc

const (
	CmdStop   = "stop"
	CmdStatus = "status"
	CmdToggle = "toggle"
)

// Request is a client → daemon command.
type Request struct {
	Cmd string `json:"cmd"`
}

// Response is a daemon → client response.
// Ok indicates success; State is present for status, Error for failures.
type Response struct {
	Ok    bool   `json:"ok"`
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
}
