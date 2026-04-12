package termscroll

import "github.com/Enriquefft/yap/pkg/yap/hint"

func init() {
	hint.Register("termscroll", NewFactory)
}
