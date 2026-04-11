// Package config — schema-aware TOML literal serializer.
//
// edit_serialize.go converts raw user input (a string from argv)
// into a properly-formatted TOML literal whose type matches the
// Go field at a dot-notation path in the Config schema.
//
// The serializer is driven by reflection over the Config type:
// the single source of truth for field types is the struct
// definitions in config.go. No hand-maintained type tables — a
// schema addition or rename automatically flows here because the
// walker reads `toml:"..."` struct tags.

package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// TOMLLiteralFor returns the TOML literal representation of
// rawValue interpreted according to the Go type of the Config
// field at dotPath.
//
// Supported field kinds:
//
//	string  → basic-string literal with escapes applied
//	bool    → "true" or "false"
//	int*    → decimal integer literal
//	float*  → decimal float literal
//
// Unsupported kinds (struct, slice, map) return an error so the
// CLI refuses the set before touching the file.
//
// dotPath uses the same two-segment form SetKey accepts:
// "general.hotkey", "transcription.model", etc. A dot path that
// does not resolve to a known scalar field is an error.
func TOMLLiteralFor(dotPath, rawValue string) (string, error) {
	parts := strings.Split(dotPath, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("config: dot path must be section.key, got %q", dotPath)
	}
	cfg := DefaultConfig()
	v := reflect.ValueOf(cfg)
	t := v.Type()

	// Locate the section by toml tag.
	sectionField, ok := fieldByTomlTagStatic(t, parts[0])
	if !ok {
		return "", fmt.Errorf("config: unknown section %q", parts[0])
	}
	if sectionField.Type.Kind() != reflect.Struct {
		return "", fmt.Errorf("config: section %q is not a struct (kind=%s)", parts[0], sectionField.Type.Kind())
	}

	// Locate the key within the section.
	keyField, ok := fieldByTomlTagStatic(sectionField.Type, parts[1])
	if !ok {
		return "", fmt.Errorf("config: unknown key %q in section %q", parts[1], parts[0])
	}

	switch keyField.Type.Kind() {
	case reflect.String:
		return tomlStringLiteral(rawValue), nil
	case reflect.Bool:
		b, err := strconv.ParseBool(rawValue)
		if err != nil {
			return "", fmt.Errorf("config: %s expects a bool, got %q", dotPath, rawValue)
		}
		return strconv.FormatBool(b), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(rawValue, 10, 64)
		if err != nil {
			return "", fmt.Errorf("config: %s expects an integer, got %q", dotPath, rawValue)
		}
		return strconv.FormatInt(n, 10), nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(rawValue, 64)
		if err != nil {
			return "", fmt.Errorf("config: %s expects a float, got %q", dotPath, rawValue)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	case reflect.Slice, reflect.Struct, reflect.Map:
		return "", fmt.Errorf("config: %s is a %s; use a dedicated subcommand to edit structured values", dotPath, keyField.Type.Kind())
	default:
		return "", fmt.Errorf("config: %s has unsupported kind %s", dotPath, keyField.Type.Kind())
	}
}

// fieldByTomlTagStatic returns the reflect.StructField whose toml
// tag name matches tag. Unlike fieldByTomlTag in path.go it works
// against a reflect.Type rather than a reflect.Value, so it can be
// called on a section's field type without needing a non-zero
// instance.
func fieldByTomlTagStatic(t reflect.Type, tag string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tomlName(f.Tag.Get("toml")) == tag {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// tomlStringLiteral quotes s as a TOML basic string. Backslashes
// and double quotes are escaped; control characters below 0x20
// become standard TOML escape sequences. Matches the TOML 1.0
// basic-string grammar; see https://toml.io/en/v1.0.0#string.
func tomlStringLiteral(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
