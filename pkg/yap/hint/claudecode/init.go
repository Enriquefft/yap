package claudecode

import "github.com/Enriquefft/yap/pkg/yap/hint"

func init() {
	hint.Register("claudecode", NewFactory)
}
