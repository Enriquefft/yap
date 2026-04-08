// gen-nixos generates nixosModules.nix from the struct tags in
// pkg/yap/config. The generator reads the schema via reflection,
// parses the `yap:"..."` metadata, and renders a Nix module via
// text/template.
//
// Run: go generate ./pkg/yap/config/... (which executes
//      go run ./internal/cmd/gen-nixos -o ./nixosModules.nix).
//
// CI enforces that the committed nixosModules.nix matches this
// tool's output byte-for-byte via internal/cmd/gen-nixos/main_test.go.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/template"

	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// FieldInfo describes one leaf field in the config schema.
type FieldInfo struct {
	Key     string      // toml tag name (e.g. "hotkey")
	Kind    reflect.Kind // underlying kind (bool, string, int, float, slice)
	Default interface{} // default value from DefaultConfig()
	Meta    TagMeta     // parsed yap:"..." metadata
}

// SectionInfo describes one TOML section (one top-level struct field).
type SectionInfo struct {
	Name   string
	Fields []FieldInfo
}

// TemplateData is the payload passed to text/template.
type TemplateData struct {
	Sections []SectionInfo
}

// TagMeta is the parsed content of a `yap:"..."` struct tag.
type TagMeta struct {
	Doc      string
	Enum     []string
	Min      *float64
	Max      *float64
	GreaterThan *float64
	Secret   bool
}

func main() {
	nixosOut := flag.String("o-nixos", "", "output path for nixosModules.nix (required)")
	hmOut := flag.String("o-hm", "", "output path for homeManagerModules.nix (required)")
	flag.Parse()
	if *nixosOut == "" || *hmOut == "" {
		fmt.Fprintln(os.Stderr, "gen-nixos: -o-nixos and -o-hm are required")
		os.Exit(2)
	}

	if err := writeRendered(*nixosOut, RenderNixOS); err != nil {
		fmt.Fprintln(os.Stderr, "gen-nixos: nixos:", err)
		os.Exit(1)
	}
	if err := writeRendered(*hmOut, RenderHomeManager); err != nil {
		fmt.Fprintln(os.Stderr, "gen-nixos: home-manager:", err)
		os.Exit(1)
	}
}

func writeRendered(path string, render func(io.Writer) error) error {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// sharedFuncMap returns the template function map used by both module
// templates.
func sharedFuncMap() template.FuncMap {
	return template.FuncMap{
		"nixType":        nixType,
		"nixDefault":     nixDefault,
		"nixDescription": nixDescription,
		"tomlRender":     tomlRender,
	}
}

// renderModule renders a module template with the shared schema data.
func renderModule(w io.Writer, tmplText string) error {
	data := buildTemplateData(pcfg.DefaultConfig())
	tmpl := template.New("nix").Funcs(sharedFuncMap())
	tmpl, err := tmpl.Parse(tmplText)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	return tmpl.Execute(w, data)
}

// RenderNixOS writes the rendered nixosModules.nix to w.
func RenderNixOS(w io.Writer) error {
	return renderModule(w, nixosModuleTemplate)
}

// RenderHomeManager writes the rendered homeManagerModules.nix to w.
func RenderHomeManager(w io.Writer) error {
	return renderModule(w, homeManagerModuleTemplate)
}

// buildTemplateData walks cfg via reflection and assembles the
// template payload. Only leaf fields on the top-level sections are
// included; nested slices (like AppOverrides) are represented as
// sentinel defaults in Nix (empty list) with a doc string pointing
// users at `yap config overrides`.
func buildTemplateData(cfg pcfg.Config) TemplateData {
	v := reflect.ValueOf(cfg)
	t := v.Type()

	var sections []SectionInfo
	for i := 0; i < t.NumField(); i++ {
		sectionField := t.Field(i)
		sectionName := tomlName(sectionField.Tag.Get("toml"))
		if sectionName == "" || sectionName == "-" {
			continue
		}
		sectionValue := v.Field(i)

		var fields []FieldInfo
		st := sectionValue.Type()
		for j := 0; j < st.NumField(); j++ {
			f := st.Field(j)
			key := tomlName(f.Tag.Get("toml"))
			if key == "" || key == "-" {
				continue
			}
			meta := parseTagMeta(f.Tag.Get("yap"))
			fv := sectionValue.Field(j)

			// Slices are rendered as a comment marker — Nix users
			// manage injection.app_overrides via the CLI or by
			// editing the generated /etc/yap/config.toml directly
			// through services.yap.settings.injection.app_overrides.
			// For Phase 2 we expose the slice as an empty default
			// with an informative description.
			fields = append(fields, FieldInfo{
				Key:     key,
				Kind:    fv.Kind(),
				Default: fv.Interface(),
				Meta:    meta,
			})
		}

		sections = append(sections, SectionInfo{
			Name:   sectionName,
			Fields: fields,
		})
	}
	return TemplateData{Sections: sections}
}

// tomlName returns the leading tag name (before the first comma).
func tomlName(tag string) string {
	if i := strings.Index(tag, ","); i >= 0 {
		return tag[:i]
	}
	return tag
}

// parseTagMeta parses a `yap:"..."` struct tag into TagMeta.
//
// Grammar: semicolon-separated parts. Each part is either the literal
// word "secret" or `key=value`. Because doc strings frequently contain
// semicolons, the `doc=...` key is greedy: if present, it consumes the
// rest of the tag. Therefore `doc=...` MUST be the final part. The
// recognized keys are:
//
//	enum=a,b,c   — comma-separated enum values
//	min=N        — numeric minimum
//	max=N        — numeric maximum
//	gt=N         — strict numeric lower bound
//	doc=TEXT     — documentation (greedy; must be last)
//	secret       — flag: field holds a secret
//
// Unknown keys are ignored so future phases can add metadata without
// breaking the generator.
func parseTagMeta(tag string) TagMeta {
	var m TagMeta
	if tag == "" {
		return m
	}
	rest := tag
	for rest != "" {
		// Look for `doc=` at the start; if found, consume the
		// entire remainder as the doc string.
		if strings.HasPrefix(rest, "doc=") {
			m.Doc = strings.TrimSpace(rest[len("doc="):])
			return m
		}
		// Find the next separator that belongs to the tag (not a
		// user-written one inside doc=, which is handled above).
		sep := strings.IndexByte(rest, ';')
		var part string
		if sep < 0 {
			part = rest
			rest = ""
		} else {
			part = rest[:sep]
			rest = strings.TrimSpace(rest[sep+1:])
		}
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "secret" {
			m.Secret = true
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(part[:eq])
		value := strings.TrimSpace(part[eq+1:])
		switch key {
		case "enum":
			for _, v := range strings.Split(value, ",") {
				m.Enum = append(m.Enum, strings.TrimSpace(v))
			}
		case "min":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				m.Min = &f
			}
		case "max":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				m.Max = &f
			}
		case "gt":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				m.GreaterThan = &f
			}
		}
	}
	return m
}

// nixType returns the Nix type expression for a FieldInfo. Enums are
// rendered as `enum [ "a" "b" ]`; strings, bools, ints and floats map
// to their obvious Nix counterparts.
func nixType(f FieldInfo) string {
	if len(f.Meta.Enum) > 0 {
		sorted := append([]string(nil), f.Meta.Enum...)
		sort.Strings(sorted)
		quoted := make([]string, 0, len(sorted))
		for _, v := range sorted {
			quoted = append(quoted, strconv.Quote(v))
		}
		return fmt.Sprintf("lib.types.enum [ %s ]", strings.Join(quoted, " "))
	}
	switch f.Kind {
	case reflect.Bool:
		return "lib.types.bool"
	case reflect.String:
		return "lib.types.str"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "lib.types.int"
	case reflect.Float32, reflect.Float64:
		return "lib.types.float"
	case reflect.Slice:
		return "lib.types.listOf (lib.types.attrsOf lib.types.str)"
	default:
		return "lib.types.str"
	}
}

// nixDefault renders the default value for a FieldInfo in Nix syntax.
func nixDefault(f FieldInfo) string {
	switch f.Kind {
	case reflect.Bool:
		if f.Default.(bool) {
			return "true"
		}
		return "false"
	case reflect.String:
		return strconv.Quote(f.Default.(string))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(reflect.ValueOf(f.Default).Int(), 10)
	case reflect.Float32, reflect.Float64:
		return formatFloat(reflect.ValueOf(f.Default).Float())
	case reflect.Slice:
		return "[ ]"
	default:
		return `""`
	}
}

// nixDescription quotes the doc string for Nix. Empty doc strings
// fall back to the field key.
func nixDescription(f FieldInfo) string {
	doc := f.Meta.Doc
	if doc == "" {
		doc = f.Key
	}
	return strconv.Quote(doc)
}

// tomlRender converts a FieldInfo's default into a TOML literal
// suitable for the pkgs.writeText blob. Slices render as inline
// arrays; everything else matches the BurntSushi encoder output.
func tomlRender(f FieldInfo) string {
	switch f.Kind {
	case reflect.Bool:
		if f.Default.(bool) {
			return "true"
		}
		return "false"
	case reflect.String:
		return strconv.Quote(f.Default.(string))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(reflect.ValueOf(f.Default).Int(), 10)
	case reflect.Float32, reflect.Float64:
		return formatFloat(reflect.ValueOf(f.Default).Float())
	case reflect.Slice:
		return "[]"
	default:
		return `""`
	}
}

// formatFloat emits floats the way the BurntSushi encoder does: no
// trailing decimals, but always at least one digit after the point
// to signal float-ness (2 -> 2.0, 0.02 -> 0.02).
func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
