package config_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/config"
)

// editCase drives the golden-fixture tests. Each case has a fixture
// basename that resolves to testdata/edit/<name>.in.toml and the
// expected output testdata/edit/<name>.out.toml, plus the SetKey
// arguments. Cases that expect a refusal set wantKind and omit the
// out fixture.
type editCase struct {
	name     string
	basename string // fixture stem (<stem>.in.toml / <stem>.out.toml)
	dotPath  string
	literal  string
	// wantKind is the expected EditErrorKind for refusal cases. Zero
	// means the case should succeed against the <basename>.out.toml
	// fixture.
	wantKind config.EditErrorKind
}

func editCases() []editCase {
	return []editCase{
		{
			name:     "comment_heavy_preserves_every_byte",
			basename: "comment_heavy",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
		},
		{
			name:     "missing_key_existing_section_appends_with_indent",
			basename: "missing_key_existing_section",
			dotPath:  "general.stream_partials",
			literal:  `true`,
		},
		{
			name:     "missing_section_appends_section_at_eof",
			basename: "missing_section",
			dotPath:  "tray.enabled",
			literal:  `true`,
		},
		{
			name:     "crlf_endings_preserved",
			basename: "crlf",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
		},
		{
			name:     "inline_comment_preserved",
			basename: "inline_comment_preserved",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
		},
		{
			name:     "bare_int",
			basename: "bare_int",
			dotPath:  "general.max_duration",
			literal:  `120`,
		},
		{
			name:     "bare_float",
			basename: "bare_float",
			dotPath:  "general.silence_threshold",
			literal:  `0.05`,
		},
		{
			name:     "bare_bool",
			basename: "bare_bool",
			dotPath:  "general.audio_feedback",
			literal:  `false`,
		},
		{
			name:     "escaped_string_replaces_escapes",
			basename: "escaped_string",
			dotPath:  "transcription.prompt",
			literal:  `"say \"ok\" twice"`,
		},
		{
			name:     "multiline_string_refused",
			basename: "multiline_string_refused",
			dotPath:  "transform.system_prompt",
			literal:  `"short"`,
			wantKind: config.ErrMultilineString,
		},
		{
			name:     "array_value_refused",
			basename: "array_value_refused",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
			wantKind: config.ErrArrayValue,
		},
		{
			name:     "inline_table_refused",
			basename: "inline_table_refused",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
			wantKind: config.ErrInlineTable,
		},
		{
			name:     "bom_refused",
			basename: "bom_refused",
			dotPath:  "general.hotkey",
			literal:  `"KEY_F24"`,
			wantKind: config.ErrBOM,
		},
		{
			// Quoted section headers are intentionally refused:
			// yap's schema does not use them and adding a
			// second walker pass for them would expand the
			// bounded scope. The editor reports the refusal as
			// ErrInvalidDotPath because there is no way to
			// address the key via a two-segment bare path.
			name:     "quoted_section_refused",
			basename: "quoted_key_refused",
			dotPath:  "foo.bar",
			literal:  `"new"`,
			wantKind: config.ErrInvalidDotPath,
		},
	}
}

// readFile is a test helper that calls t.Fatalf on error so tests
// read like the contract: "open this path, give me the bytes".
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestSetKey_GoldenFixtures(t *testing.T) {
	for _, tc := range editCases() {
		t.Run(tc.name, func(t *testing.T) {
			inPath := filepath.Join("testdata", "edit", tc.basename+".in.toml")
			input := readFile(t, inPath)

			got, err := config.SetKey(input, tc.dotPath, tc.literal)

			if tc.wantKind != 0 {
				if err == nil {
					t.Fatalf("SetKey(%q,%q): expected refusal kind %s, got nil error; output=\n%s",
						tc.dotPath, tc.literal, tc.wantKind, string(got))
				}
				var editErr *config.EditError
				if !errors.As(err, &editErr) {
					t.Fatalf("SetKey error is not *EditError: %v", err)
				}
				if editErr.Kind != tc.wantKind {
					t.Fatalf("SetKey error kind = %s, want %s (err=%v)",
						editErr.Kind, tc.wantKind, err)
				}
				// Refusal cases must return nil output so the
				// caller cannot accidentally write partial
				// bytes.
				if got != nil {
					t.Errorf("refusal case returned non-nil output (%d bytes) — must be nil", len(got))
				}
				return
			}

			if err != nil {
				t.Fatalf("SetKey(%q,%q): unexpected error: %v", tc.dotPath, tc.literal, err)
			}

			outPath := filepath.Join("testdata", "edit", tc.basename+".out.toml")
			want := readFile(t, outPath)
			if !bytes.Equal(got, want) {
				t.Fatalf("SetKey(%q,%q) output mismatch:\n--- got ---\n%s\n--- want ---\n%s",
					tc.dotPath, tc.literal, visualize(got), visualize(want))
			}
		})
	}
}

// visualize returns data with invisible bytes escaped so test
// failures show line-ending differences (CRLF vs LF) clearly.
func visualize(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		switch c {
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString("\\n\n")
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// TestSetKey_MinimumIntervention asserts that every byte of the
// input file outside the target line is byte-identical in the
// output. This is the load-bearing correctness property of the
// editor — any edit that drops or rewrites an unrelated line is a
// bug regardless of what the output looks like.
//
// Each non-refusal fixture declares the 1-based line that is
// expected to differ. Append-path fixtures (missing_key and
// missing_section) use targetLine equal to the new line's 1-based
// index in the output; every other output line must match the
// input line at the same index.
func TestSetKey_MinimumIntervention(t *testing.T) {
	type miCase struct {
		name string
		// fixture stem
		basename string
		dotPath  string
		literal  string
		// targetLine is the 1-based line that may differ. For
		// rewrite cases this is the line number in both input
		// and output. For append cases this is the new line's
		// 1-based index in the OUTPUT; the test compares input
		// lines to output lines at matching indices and skips
		// the append position entirely.
		targetLine int
		// appended is true for cases where the target line is a
		// newly-inserted line rather than a rewritten one.
		appended bool
		// appendCount is the number of new lines the append path
		// inserts into the output. For a single appended key
		// this is 1; for a missing-section append it is 3
		// (blank + header + key).
		appendCount int
	}

	// Lines to skip in the comment_heavy fixture: the target key
	// is at 1-based line 7 (after 3 header comments + blank +
	// `[general]` + `  # comment`).
	cases := []miCase{
		{
			name:       "comment_heavy",
			basename:   "comment_heavy",
			dotPath:    "general.hotkey",
			literal:    `"KEY_F24"`,
			targetLine: 7,
		},
		{
			name:       "crlf",
			basename:   "crlf",
			dotPath:    "general.hotkey",
			literal:    `"KEY_F24"`,
			targetLine: 2,
		},
		{
			name:       "inline_comment_preserved",
			basename:   "inline_comment_preserved",
			dotPath:    "general.hotkey",
			literal:    `"KEY_F24"`,
			targetLine: 2,
		},
		{
			name:       "bare_int",
			basename:   "bare_int",
			dotPath:    "general.max_duration",
			literal:    `120`,
			targetLine: 4,
		},
		{
			name:       "bare_float",
			basename:   "bare_float",
			dotPath:    "general.silence_threshold",
			literal:    `0.05`,
			targetLine: 2,
		},
		{
			name:       "bare_bool",
			basename:   "bare_bool",
			dotPath:    "general.audio_feedback",
			literal:    `false`,
			targetLine: 2,
		},
		{
			name:       "escaped_string",
			basename:   "escaped_string",
			dotPath:    "transcription.prompt",
			literal:    `"say \"ok\" twice"`,
			targetLine: 2,
		},
		{
			name:        "missing_key_existing_section",
			basename:    "missing_key_existing_section",
			dotPath:     "general.stream_partials",
			literal:     `true`,
			targetLine:  11,
			appended:    true,
			appendCount: 1,
		},
		{
			name:        "missing_section",
			basename:    "missing_section",
			dotPath:     "tray.enabled",
			literal:     `true`,
			targetLine:  30, // first of the three appended lines
			appended:    true,
			appendCount: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inPath := filepath.Join("testdata", "edit", tc.basename+".in.toml")
			input := readFile(t, inPath)
			output, err := config.SetKey(input, tc.dotPath, tc.literal)
			if err != nil {
				t.Fatalf("SetKey: %v", err)
			}

			inLines := splitLinesPreserving(input)
			outLines := splitLinesPreserving(output)

			if !tc.appended {
				// Rewrite case: line counts must match, every
				// non-target line must be byte-identical.
				if len(inLines) != len(outLines) {
					t.Fatalf("rewrite changed line count: in=%d out=%d",
						len(inLines), len(outLines))
				}
				for i := range inLines {
					if i+1 == tc.targetLine {
						continue
					}
					if !bytes.Equal(inLines[i], outLines[i]) {
						t.Errorf("line %d differs:\n in: %q\nout: %q",
							i+1, inLines[i], outLines[i])
					}
				}
				return
			}

			// Append case: output has appendCount more lines
			// than input. Lines before targetLine must match
			// verbatim; lines at and after (targetLine +
			// appendCount) in the output must match input
			// lines starting at (targetLine) of the input.
			if len(outLines) != len(inLines)+tc.appendCount {
				t.Fatalf("append changed line count by %d, want %d (in=%d out=%d)",
					len(outLines)-len(inLines), tc.appendCount,
					len(inLines), len(outLines))
			}
			// Prefix lines [1, targetLine) must be identical.
			for i := 0; i < tc.targetLine-1; i++ {
				if !bytes.Equal(inLines[i], outLines[i]) {
					t.Errorf("prefix line %d differs:\n in: %q\nout: %q",
						i+1, inLines[i], outLines[i])
				}
			}
			// Suffix: input line (targetLine-1)+0 must match
			// output line (targetLine-1)+appendCount.
			for i := tc.targetLine - 1; i < len(inLines); i++ {
				ji := i + tc.appendCount
				if !bytes.Equal(inLines[i], outLines[ji]) {
					t.Errorf("suffix line in=%d out=%d differs:\n in: %q\nout: %q",
						i+1, ji+1, inLines[i], outLines[ji])
				}
			}
		})
	}
}

// splitLinesPreserving breaks data into lines, keeping each line's
// original line-ending bytes attached. A trailing fragment without
// EOL becomes its own line. An empty input yields an empty slice.
func splitLinesPreserving(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// TestSetKey_InvalidDotPaths exercises the dot-path validator
// surface. Each case is expected to return ErrInvalidDotPath with
// no output.
func TestSetKey_InvalidDotPaths(t *testing.T) {
	input := []byte("[general]\nhotkey = \"KEY_F1\"\n")
	cases := []string{
		"",
		"general",
		"general.hotkey.extra",
		".hotkey",
		"general.",
		`"general".hotkey`,
		"general hotkey",
		"general.hot key",
	}
	for _, dp := range cases {
		t.Run(dp, func(t *testing.T) {
			got, err := config.SetKey(input, dp, `"x"`)
			if err == nil {
				t.Fatalf("expected error for dot path %q, got nil; output=%s", dp, string(got))
			}
			var editErr *config.EditError
			if !errors.As(err, &editErr) {
				t.Fatalf("error is not *EditError: %v", err)
			}
			if editErr.Kind != config.ErrInvalidDotPath {
				t.Fatalf("wrong kind: got %s, want %s", editErr.Kind, config.ErrInvalidDotPath)
			}
		})
	}
}

// TestSetKey_PreservesOtherSections asserts the classic bug the
// editor exists to fix: setting a value in one section must not
// disturb any other section on disk. A single test that edits
// [general].hotkey and asserts [transcription] and [injection] are
// byte-identical covers the common mental model of "just preserve
// everything".
func TestSetKey_PreservesOtherSections(t *testing.T) {
	input := readFile(t, filepath.Join("testdata", "edit", "comment_heavy.in.toml"))
	got, err := config.SetKey(input, "general.hotkey", `"KEY_F24"`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	// Check every line of the input outside the `general.hotkey`
	// line is present byte-for-byte in the output.
	in := splitLinesPreserving(input)
	out := splitLinesPreserving(got)
	if len(in) != len(out) {
		t.Fatalf("line count diverged: in=%d out=%d", len(in), len(out))
	}
}

// TestSetKey_AppendToFileWithoutTrailingNewline covers the edge
// case where the input file does not end with a newline. The
// append path must emit a separating EOL before the new content
// so the new section header lands on its own line.
func TestSetKey_AppendToFileWithoutTrailingNewline(t *testing.T) {
	// Build an input without a trailing newline.
	input := []byte(`[general]
hotkey = "KEY_RIGHTCTRL"`)
	got, err := config.SetKey(input, "tray.enabled", `true`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	want := "[general]\nhotkey = \"KEY_RIGHTCTRL\"\n\n[tray]\nenabled = true\n"
	if string(got) != want {
		t.Errorf("append without trailing newline:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestSetKey_NestedTargetKeyInMultilineString asserts the
// tokenizer does not mis-identify a line inside a multi-line
// string as a key = value row. If it did, a hand-edited file
// with `key = "..."` lines inside a multi-line prompt could
// trigger false-positive matches and corrupt the payload.
func TestSetKey_NestedTargetKeyInMultilineString(t *testing.T) {
	// max_duration appears inside the prompt text as a false
	// positive candidate. The scanner should treat the multi-line
	// string as one opaque span and the walker should find the
	// *real* max_duration line instead.
	input := []byte(`[general]
max_duration = 60

[transcription]
prompt = """
Ignore this line: max_duration = 999
"""
backend = "whisperlocal"
`)
	got, err := config.SetKey(input, "general.max_duration", `120`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if !bytes.Contains(got, []byte("max_duration = 120")) {
		t.Errorf("real max_duration not updated: %s", string(got))
	}
	if !bytes.Contains(got, []byte("max_duration = 999")) {
		t.Errorf("multi-line string content was corrupted: %s", string(got))
	}
}

// TestSetKey_IdempotentNoop asserts that setting a value to its
// current value is a byte-identical no-op. This is a secondary
// guarantee of the minimum-intervention contract: the rewrite
// path must not introduce whitespace, quoting, or escape
// differences for a trivial set.
func TestSetKey_IdempotentNoop(t *testing.T) {
	input := []byte(`[general]
hotkey = "KEY_RIGHTCTRL"
mode = "hold"
max_duration = 60
`)
	got, err := config.SetKey(input, "general.hotkey", `"KEY_RIGHTCTRL"`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if !bytes.Equal(input, got) {
		t.Errorf("idempotent rewrite mutated bytes:\n in: %q\nout: %q", input, got)
	}
}

// TestSetKey_AdjacentSections covers the case where two sections
// share no blank-line separator. The appender for a missing key
// in the first section must insert the new line BEFORE the next
// section header, not inside it.
func TestSetKey_AdjacentSections(t *testing.T) {
	input := []byte(`[general]
hotkey = "KEY_RIGHTCTRL"
mode = "hold"
[transcription]
backend = "whisperlocal"
`)
	got, err := config.SetKey(input, "general.max_duration", `60`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	want := `[general]
hotkey = "KEY_RIGHTCTRL"
mode = "hold"
max_duration = 60
[transcription]
backend = "whisperlocal"
`
	if string(got) != want {
		t.Errorf("adjacent-section append mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestSetKey_FirstRunEmptyFile covers the degenerate "empty file"
// case: SetKey against an empty byte slice must append a new
// section + key pair.
func TestSetKey_FirstRunEmptyFile(t *testing.T) {
	got, err := config.SetKey([]byte{}, "general.hotkey", `"KEY_F1"`)
	if err != nil {
		t.Fatalf("SetKey empty: %v", err)
	}
	want := "[general]\nhotkey = \"KEY_F1\"\n"
	if string(got) != want {
		t.Errorf("empty-file append mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestSetKey_UnquotedStringInComment covers a tricky edge case: a
// comment line whose text contains `=` must not be mistaken for a
// key = value line.
func TestSetKey_UnquotedStringInComment(t *testing.T) {
	input := []byte(`[general]
# hotkey = "KEY_F99" — disabled
hotkey = "KEY_RIGHTCTRL"
`)
	got, err := config.SetKey(input, "general.hotkey", `"KEY_F24"`)
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	want := `[general]
# hotkey = "KEY_F99" — disabled
hotkey = "KEY_F24"
`
	if string(got) != want {
		t.Errorf("comment-with-equals mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestTOMLLiteralFor exercises the schema-aware serializer. Each
// case covers a representative scalar kind with both valid and
// invalid raw inputs.
func TestTOMLLiteralFor(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		got, err := config.TOMLLiteralFor("general.hotkey", "KEY_F24")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != `"KEY_F24"` {
			t.Errorf("got %q, want %q", got, `"KEY_F24"`)
		}
	})
	t.Run("string_escapes", func(t *testing.T) {
		got, err := config.TOMLLiteralFor("transcription.prompt", `say "hi"\nthere`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `"say \"hi\"\\nthere"`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("bool_true", func(t *testing.T) {
		got, err := config.TOMLLiteralFor("general.audio_feedback", "true")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "true" {
			t.Errorf("got %q, want true", got)
		}
	})
	t.Run("bool_invalid", func(t *testing.T) {
		if _, err := config.TOMLLiteralFor("general.audio_feedback", "maybe"); err == nil {
			t.Error("expected error for invalid bool")
		}
	})
	t.Run("int_valid", func(t *testing.T) {
		got, err := config.TOMLLiteralFor("general.max_duration", "120")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "120" {
			t.Errorf("got %q, want 120", got)
		}
	})
	t.Run("int_invalid", func(t *testing.T) {
		if _, err := config.TOMLLiteralFor("general.max_duration", "soon"); err == nil {
			t.Error("expected error for invalid int")
		}
	})
	t.Run("float_valid", func(t *testing.T) {
		got, err := config.TOMLLiteralFor("general.silence_threshold", "0.05")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "0.05" {
			t.Errorf("got %q, want 0.05", got)
		}
	})
	t.Run("float_invalid", func(t *testing.T) {
		if _, err := config.TOMLLiteralFor("general.silence_threshold", "loud"); err == nil {
			t.Error("expected error for invalid float")
		}
	})
	t.Run("unknown_section", func(t *testing.T) {
		if _, err := config.TOMLLiteralFor("bogus.foo", "x"); err == nil {
			t.Error("expected error for unknown section")
		}
	})
	t.Run("unknown_key", func(t *testing.T) {
		if _, err := config.TOMLLiteralFor("general.nothing", "x"); err == nil {
			t.Error("expected error for unknown key")
		}
	})
	t.Run("slice_field_refused", func(t *testing.T) {
		// injection.app_overrides is a []AppOverride —
		// structured, not a scalar. The serializer refuses with
		// a clear message.
		if _, err := config.TOMLLiteralFor("injection.app_overrides", "x"); err == nil {
			t.Error("expected error for slice field")
		}
	})
}
