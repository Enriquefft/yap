package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Get resolves a dot-notation key against cfg and returns the value as
// a string suitable for CLI output. Slice elements are addressable via
// a numeric index, e.g. "injection.app_overrides.0.match".
func Get(cfg *Config, key string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config: Get on nil *Config")
	}
	if key == "" {
		return "", fmt.Errorf("config: empty key")
	}
	v, err := walk(reflect.ValueOf(cfg).Elem(), strings.Split(key, "."))
	if err != nil {
		return "", err
	}
	return formatValue(v), nil
}

// Set parses value into the type of the field at key and assigns it.
// It does NOT validate — callers should invoke cfg.Validate afterwards.
func Set(cfg *Config, key, value string) error {
	if cfg == nil {
		return fmt.Errorf("config: Set on nil *Config")
	}
	if key == "" {
		return fmt.Errorf("config: empty key")
	}
	v, err := walk(reflect.ValueOf(cfg).Elem(), strings.Split(key, "."))
	if err != nil {
		return err
	}
	if !v.CanSet() {
		return fmt.Errorf("config: field %q is not settable", key)
	}
	return assign(v, value)
}

// walk descends through cfg following parts. Slice indices are
// integers. Struct fields are matched by their toml tag name.
func walk(v reflect.Value, parts []string) (reflect.Value, error) {
	original := strings.Join(parts, ".")
	for i, part := range parts {
		switch v.Kind() {
		case reflect.Struct:
			f, ok := fieldByTomlTag(v, part)
			if !ok {
				return reflect.Value{}, fmt.Errorf("config: unknown key %q (segment %d: %q)", original, i, part)
			}
			v = f
		case reflect.Slice:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("config: slice index must be a number, got %q", part)
			}
			if idx < 0 || idx >= v.Len() {
				return reflect.Value{}, fmt.Errorf("config: slice index %d out of range (len=%d)", idx, v.Len())
			}
			v = v.Index(idx)
		default:
			return reflect.Value{}, fmt.Errorf("config: cannot descend into %s at segment %d (%q)", v.Kind(), i, part)
		}
	}
	return v, nil
}

// fieldByTomlTag returns the struct field whose toml tag name matches
// tag. Embedded structs are not supported because the schema doesn't
// use them.
func fieldByTomlTag(v reflect.Value, tag string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if tomlName(t.Field(i).Tag.Get("toml")) == tag {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// tomlName returns the leading name from a toml struct tag, ignoring
// any options after the first comma.
func tomlName(tag string) string {
	if i := strings.Index(tag, ","); i >= 0 {
		return tag[:i]
	}
	return tag
}

// assign parses value into the kind of v and assigns it.
func assign(v reflect.Value, value string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("config: invalid bool %q: %w", value, err)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("config: invalid integer %q: %w", value, err)
		}
		v.SetInt(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("config: invalid float %q: %w", value, err)
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("config: cannot assign string to %s", v.Kind())
	}
	return nil
}

// formatValue stringifies v for CLI output. Slices report their
// length so users know to drill in with an index.
func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Slice:
		return fmt.Sprintf("<%d items>", v.Len())
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}
