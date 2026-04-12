package local

import "github.com/Enriquefft/yap/pkg/yap/transform"

// init registers the local (Ollama native) backend under the name
// "local" in the transform registry. Side-effect imports
// (`import _ "github.com/Enriquefft/yap/pkg/yap/transform/local"`) are
// the intended entry point.
func init() {
	transform.Register("local", NewFactory)
}
