package config

import (
	"bytes"
	"fmt"
	"io"

	"github.com/BurntSushi/toml"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// LoadBytes decodes data into a Config without any disk I/O. It is
// the in-memory counterpart to Load — used by the `yap config set`
// post-write validation path, which parses the editor's output
// before renaming the temp file into place so a syntactically-valid
// but schema-invalid edit is refused with the user's file still
// intact.
//
// Unlike Load, LoadBytes does not run the legacy-flat migration.
// The custom editor only operates on nested-schema files; a legacy
// file is handled by the first-run Save path in the CLI wiring.
//
// notices receives unknown-key warnings. Production passes
// os.Stderr; tests may pass io.Discard when only validation is
// being asserted.
func LoadBytes(notices io.Writer, data []byte) (Config, error) {
	if notices == nil {
		notices = io.Discard
	}
	cfg := pcfg.DefaultConfig()
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse edited config: %w", err)
	}
	warnUndecoded(notices, "<in-memory>", md)
	return cfg, nil
}
