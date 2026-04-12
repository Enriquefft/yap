package passthrough

import "github.com/Enriquefft/yap/pkg/yap/transform"

// init registers the passthrough backend under the name "passthrough"
// in the transform registry. Side-effect imports
// (`import _ "github.com/Enriquefft/yap/pkg/yap/transform/passthrough"`)
// are the intended entry point.
func init() {
	transform.Register("passthrough", NewFactory)
}
