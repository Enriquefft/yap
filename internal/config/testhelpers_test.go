// internal/config/testhelpers_test.go
package config_test

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
)

func LoadHelper(t *testing.T) (config.Config, error) {
	t.Helper()
	return config.Load()
}

func ConfigPathHelper(t *testing.T) (string, error) {
	t.Helper()
	return config.ConfigPath()
}
