package openai

import "github.com/Enriquefft/yap/pkg/yap/transform"

// init registers the OpenAI-compatible backend under the name
// "openai" in the transform registry. Side-effect imports
// (`import _ "github.com/Enriquefft/yap/pkg/yap/transform/openai"`) are
// the intended entry point.
func init() {
	transform.Register("openai", NewFactory)
}
