package mock

import "github.com/hybridz/yap/pkg/yap/transcribe"

// init registers the mock backend under the name "mock" so that
// daemon and integration tests can select it via transcribe.Get.
// Production callers should not import this package for side effects
// unless they intend the mock backend to be reachable from the
// config file.
func init() {
	transcribe.Register("mock", NewFactory)
}
