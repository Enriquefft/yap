// Package claudecode is a hint provider that reads the most recent
// Claude Code session JSONL for the project's working directory and
// returns recent user/assistant messages as conversation context.
//
// The provider returns only Bundle.Conversation — project-level
// vocabulary (CLAUDE.md, AGENTS.md, README.md) is handled by the
// daemon's base vocabulary layer.
package claudecode
