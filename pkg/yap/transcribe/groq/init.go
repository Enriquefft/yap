package groq

import "github.com/hybridz/yap/pkg/yap/transcribe"

// init registers the Groq backend under the name "groq" so that the
// daemon (and any third-party consumer) can select it via
// transcribe.Get("groq"). Side-effect imports
// (`import _ "github.com/hybridz/yap/pkg/yap/transcribe/groq"`) are
// the intended entry point.
func init() {
	transcribe.Register("groq", NewFactory)
}
