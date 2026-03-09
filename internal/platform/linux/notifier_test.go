package linux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifier_Notify(t *testing.T) {
	var gotTitle, gotMessage string
	n := newNotifierWithFn(func(title, message string, icon any) error {
		gotTitle = title
		gotMessage = message
		return nil
	})

	n.Notify("test title", "test message")

	require.Equal(t, "yap: test title", gotTitle)
	require.Equal(t, "test message", gotMessage)
}

func TestNotifier_PrefixesTitle(t *testing.T) {
	var gotTitle string
	n := newNotifierWithFn(func(title, message string, icon any) error {
		gotTitle = title
		return nil
	})

	n.Notify("error", "details")
	assert.Equal(t, "yap: error", gotTitle)
}

func TestNotifier_ErrorIsSilentlyDropped(t *testing.T) {
	n := newNotifierWithFn(func(title, message string, icon any) error {
		return assert.AnError
	})
	// Must not panic
	n.Notify("title", "message")
}
