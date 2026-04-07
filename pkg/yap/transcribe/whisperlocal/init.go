package whisperlocal

import "github.com/hybridz/yap/pkg/yap/transcribe"

// init registers the whisperlocal backend under the name
// "whisperlocal" so that the daemon (and any third-party consumer) can
// select it via transcribe.Get("whisperlocal"). Side-effect imports
// (`import _ "github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal"`)
// are the intended entry point.
func init() {
	transcribe.Register("whisperlocal", NewFactory)
}
