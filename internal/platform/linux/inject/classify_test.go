package inject

import (
	"testing"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

func TestClassifyTerminals(t *testing.T) {
	for _, name := range terminalClasses {
		t.Run(name, func(t *testing.T) {
			if got := classify(name); got != yinject.AppTerminal {
				t.Errorf("classify(%q) = %v, want AppTerminal", name, got)
			}
		})
	}
}

func TestClassifyElectron(t *testing.T) {
	for _, name := range electronClasses {
		t.Run(name, func(t *testing.T) {
			if got := classify(name); got != yinject.AppElectron {
				t.Errorf("classify(%q) = %v, want AppElectron", name, got)
			}
		})
	}
}

func TestClassifyBrowsers(t *testing.T) {
	for _, name := range browserClasses {
		t.Run(name, func(t *testing.T) {
			if got := classify(name); got != yinject.AppBrowser {
				t.Errorf("classify(%q) = %v, want AppBrowser", name, got)
			}
		})
	}
}

func TestClassifyCaseInsensitiveAndTrim(t *testing.T) {
	cases := []struct {
		in   string
		want yinject.AppType
	}{
		{"  Kitty  ", yinject.AppTerminal},
		{"FOOT", yinject.AppTerminal},
		{"Code", yinject.AppElectron},
		{"FIREFOX", yinject.AppBrowser},
		{"Mozilla Firefox", yinject.AppBrowser},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := classify(tc.in); got != tc.want {
				t.Errorf("classify(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassifyUnknownReturnsGeneric(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"some-random-app",
		"emacs",
		"libreoffice",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got := classify(in); got != yinject.AppGeneric {
				t.Errorf("classify(%q) = %v, want AppGeneric", in, got)
			}
		})
	}
}

func TestClassifyExhaustiveAllowlistMatchesEnum(t *testing.T) {
	// Sanity check: every entry in every allowlist must produce the
	// AppType the allowlist promises. This guards against a future
	// edit that adds a duplicate entry to the wrong slice.
	for _, name := range terminalClasses {
		if classify(name) != yinject.AppTerminal {
			t.Fatalf("terminal allowlist contains entry %q that does not classify as AppTerminal", name)
		}
	}
	for _, name := range electronClasses {
		if classify(name) != yinject.AppElectron {
			t.Fatalf("electron allowlist contains entry %q that does not classify as AppElectron", name)
		}
	}
	for _, name := range browserClasses {
		if classify(name) != yinject.AppBrowser {
			t.Fatalf("browser allowlist contains entry %q that does not classify as AppBrowser", name)
		}
	}
}
