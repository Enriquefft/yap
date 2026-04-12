package openai

import "github.com/Enriquefft/yap/pkg/yap/transcribe"

// init registers the OpenAI-compatible backend under the name "openai"
// so that the daemon (and any third-party consumer) can select it via
// transcribe.Get("openai"). Side-effect imports
// (`import _ "github.com/Enriquefft/yap/pkg/yap/transcribe/openai"`) are
// the intended entry point.
func init() {
	transcribe.Register("openai", NewFactory)
}
