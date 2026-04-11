// Package config — line-oriented TOML editor.
//
// edit.go implements SetKey: a minimum-intervention scalar editor
// that rewrites one value in an existing TOML document while
// preserving every byte outside the target line. The editor does not
// depend on any TOML library; it is its own tokenizer. Library-based
// parsing is reserved for post-write validation in callers.
//
// Scope is bounded by design:
//
//   - Only scalar values (string, int, float, bool) in nested
//     sections at one level depth (yap's schema shape).
//   - No array, inline-table, or multi-line-string edits.
//   - No array-of-tables indexing.
//
// Refusal cases return a structured EditError so callers can render
// a specific message and leave the user's file untouched.
//
// The minimum-intervention property is load-bearing: every byte of
// the input outside the target line (or the appended line when the
// key was missing) is byte-identical in the output. Tests assert
// this contract line-by-line; see edit_test.go.

package config

import (
	"bytes"
	"fmt"
	"strings"
)

// EditErrorKind categorizes SetKey refusal cases. Callers switch on
// Kind to render user-facing messages specific to each failure mode.
type EditErrorKind int

// EditErrorKind values. New values append to the end of the iota
// block so existing switch statements keep compiling.
const (
	// ErrInvalidDotPath means dotPath is empty, has the wrong
	// number of segments, or names a key the tokenizer cannot
	// represent (e.g. a dotted or quoted key not supported by the
	// editor's bounded scope).
	ErrInvalidDotPath EditErrorKind = iota + 1
	// ErrBOM means the input file starts with a UTF-8 byte-order
	// mark. SetKey refuses rather than silently decide whether to
	// preserve or strip it.
	ErrBOM
	// ErrMultilineString means the target value is a triple-quoted
	// (""" or ''') multi-line string. The line editor cannot span
	// lines safely.
	ErrMultilineString
	// ErrArrayValue means the target value is a TOML array
	// (starts with '[').
	ErrArrayValue
	// ErrInlineTable means the target value is a TOML inline table
	// (starts with '{').
	ErrInlineTable
	// ErrMalformedLine means the tokenizer failed to parse the
	// target line — malformed quoted string, missing =, etc. The
	// editor refuses rather than guess.
	ErrMalformedLine
	// ErrInternal is an invariant-violation sentinel. It should
	// never fire in production; if it does, there is a bug in the
	// tokenizer or walker.
	ErrInternal
)

// String returns a stable human-readable label for the kind, used
// in error messages.
func (k EditErrorKind) String() string {
	switch k {
	case ErrInvalidDotPath:
		return "invalid dot path"
	case ErrBOM:
		return "UTF-8 BOM not supported"
	case ErrMultilineString:
		return "multi-line string value"
	case ErrArrayValue:
		return "array value"
	case ErrInlineTable:
		return "inline table value"
	case ErrMalformedLine:
		return "malformed TOML line"
	case ErrInternal:
		return "internal editor error"
	}
	return fmt.Sprintf("EditErrorKind(%d)", int(k))
}

// EditError is the sentinel returned by SetKey. Callers may type-
// assert to map Kind onto user-visible messaging. Line is 1-based
// or 0 when the failure is not tied to a specific line (e.g. BOM).
type EditError struct {
	Kind EditErrorKind
	Line int
	Msg  string
}

func (e *EditError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("config edit: %s at line %d: %s", e.Kind, e.Line, e.Msg)
	}
	return fmt.Sprintf("config edit: %s: %s", e.Kind, e.Msg)
}

// newEditError is a terse constructor for the common case.
func newEditError(kind EditErrorKind, line int, msg string) *EditError {
	return &EditError{Kind: kind, Line: line, Msg: msg}
}

// SetKey returns a copy of data with the value at dotPath replaced
// by tomlLiteral. Every byte of data outside the target line (or
// the appended line when the key was missing) is byte-identical in
// the output.
//
// tomlLiteral is the caller-serialized TOML form of the new value
// (e.g. `"KEY_F24"`, `60`, `true`, `0.05`). SetKey does not infer
// types from Go values; a schema-aware serializer such as
// TOMLLiteralFor runs upstream.
//
// dotPath is the section.key form: "general.hotkey" or
// "transcription.model". Exactly two segments are supported; yap's
// schema has one level of nesting.
//
// Refusal cases return an *EditError. The input is not partially
// edited on error — the caller receives only the original bytes
// back via data, which it already owns.
func SetKey(data []byte, dotPath, tomlLiteral string) ([]byte, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return nil, newEditError(ErrBOM, 0, "input starts with UTF-8 byte-order mark")
	}

	section, key, err := splitDotPath(dotPath)
	if err != nil {
		return nil, err
	}

	// Detect line ending: CRLF if the first newline in the file is
	// preceded by CR, otherwise LF. Files with no newline default
	// to LF; the appender respects this for any new lines it emits.
	eol := detectEOL(data)

	// Scan the file into a list of logical lines. Each line carries
	// its raw bytes (with EOL), the line number (1-based), and the
	// normalized text used for classification. Multi-line strings
	// cause a refusal only if they are the target value; otherwise
	// they are opaque groups of lines the walker skips over so we
	// do not mis-classify a `key = "..."` inside a multi-line string
	// as a bare key.
	lines, err := scanLines(data)
	if err != nil {
		return nil, err
	}

	// Before walking, refuse any file that contains a dotted or
	// quoted section header whose name would be ambiguous with
	// our target dotPath. yap's schema never uses such headers,
	// so a hand-edited file with them is out of the editor's
	// bounded scope — safer to refuse than to guess.
	//
	// Specifically we refuse when the dotted header's raw name
	// equals the target section, equals the full dotPath, or
	// begins with "<section>." — any of which would leave the
	// walker with no safe way to distinguish the user's intent
	// from a silent append that could produce a TOML file with
	// duplicate / conflicting section blocks.
	sectionDotPrefix := section + "."
	for _, ln := range lines {
		if ln.kind != lineKindSection || !ln.sectionDotted {
			continue
		}
		if ln.sectionName == section ||
			ln.sectionName == dotPath ||
			strings.HasPrefix(ln.sectionName, sectionDotPrefix) {
			return nil, newEditError(ErrInvalidDotPath, ln.lineNum,
				fmt.Sprintf("file contains a dotted or quoted section header %q that is ambiguous with dotPath %q", ln.sectionName, dotPath))
		}
	}

	// Locate the target section's [header] line and its first-line /
	// last-line range. One level of nesting is supported; a sibling
	// section header terminates the range.
	sectionStart, sectionEnd, sectionFound := locateSection(lines, section)

	if !sectionFound {
		// Append a fresh [section] block at EOF with the new key.
		return appendNewSection(data, eol, section, key, tomlLiteral)
	}

	// Walk the section's key lines looking for an existing entry.
	// Multi-line-string groups are treated as key-bearing lines
	// here so the walker finds them and rewriteValueOnLine can
	// refuse with ErrMultilineString instead of silently
	// appending a new key.
	targetIdx := -1
	for i := sectionStart + 1; i < sectionEnd; i++ {
		ln := lines[i]
		if ln.kind != lineKindKeyValue && ln.kind != lineKindMultilineStringGroup {
			continue
		}
		if ln.key == key {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		// Missing key in existing section: append at the end of
		// the section's contiguous block, matching the indent
		// style of the surrounding keys.
		return appendKeyInSection(data, eol, lines, sectionStart, sectionEnd, key, tomlLiteral)
	}

	// Existing key: rewrite its value span, preserving everything
	// else on the line (indent, key, `=`, surrounding whitespace,
	// inline comment). The value-span extractor handles all the
	// subtle TOML cases (quoted strings, bare scalars, refusal
	// sentinels for arrays / inline tables / multi-line strings).
	target := lines[targetIdx]
	rewritten, err := rewriteValueOnLine(target, tomlLiteral)
	if err != nil {
		return nil, err
	}

	// Splice the rewritten line into the original bytes. Every
	// other byte of the file is untouched — the minimum-intervention
	// property reduces to "overwrite target.offset..target.offset+
	// len(target.raw) with rewritten".
	out := make([]byte, 0, len(data)+len(rewritten)-len(target.raw))
	out = append(out, data[:target.offset]...)
	out = append(out, rewritten...)
	out = append(out, data[target.offset+len(target.raw):]...)
	return out, nil
}

// splitDotPath validates dotPath and returns its two segments.
// Exactly two segments are required; yap's schema has one level of
// nesting and the editor is deliberately bounded to match.
//
// Empty segments, dotted keys (more than two segments), and quoted
// segments are rejected with ErrInvalidDotPath.
func splitDotPath(dotPath string) (section, key string, err error) {
	if dotPath == "" {
		return "", "", newEditError(ErrInvalidDotPath, 0, "empty dot path")
	}
	// Disallow quoted segments: yap's schema has no quoted keys,
	// and handling them would require a second tokenizer.
	if strings.ContainsAny(dotPath, `"'`) {
		return "", "", newEditError(ErrInvalidDotPath, 0, fmt.Sprintf("quoted segments not supported: %q", dotPath))
	}
	parts := strings.Split(dotPath, ".")
	if len(parts) != 2 {
		return "", "", newEditError(ErrInvalidDotPath, 0, fmt.Sprintf("expected section.key (two segments), got %q", dotPath))
	}
	for i, p := range parts {
		if p == "" {
			return "", "", newEditError(ErrInvalidDotPath, 0, fmt.Sprintf("empty segment at position %d in %q", i, dotPath))
		}
		if !isBareKeyName(p) {
			return "", "", newEditError(ErrInvalidDotPath, 0, fmt.Sprintf("segment %q is not a bare TOML key", p))
		}
	}
	return parts[0], parts[1], nil
}

// isBareKeyName reports whether s is a valid TOML bare key: non-
// empty and composed only of A-Z, a-z, 0-9, `_`, `-`. Matches
// TOML 1.0 bare-key grammar.
func isBareKeyName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// eolKind captures the first line ending found in the input. This
// determines what byte sequence the appender uses for any new lines.
type eolKind int

const (
	eolLF eolKind = iota
	eolCRLF
)

func (e eolKind) bytes() []byte {
	if e == eolCRLF {
		return []byte("\r\n")
	}
	return []byte("\n")
}

// detectEOL returns eolCRLF if the first newline in data is
// immediately preceded by a CR, otherwise eolLF. A file with no
// newline at all defaults to LF.
func detectEOL(data []byte) eolKind {
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		return eolLF
	}
	if idx > 0 && data[idx-1] == '\r' {
		return eolCRLF
	}
	return eolLF
}

// lineKind classifies each scanned line. The classifier is
// deliberately coarse — it only cares about what the walker needs
// to do: find the target section header and the target key row.
// Unknown lines are treated as opaque and preserved verbatim.
type lineKind int

const (
	lineKindBlank lineKind = iota
	lineKindComment
	lineKindSection
	lineKindKeyValue
	// lineKindMultilineStringGroup represents a multi-line-string
	// assignment that spans multiple physical lines. scanLines
	// coalesces every physical line in the group into one logical
	// line entry; the walker treats it as opaque unless the walker
	// is asked to target its key, in which case rewriteValueOnLine
	// returns ErrMultilineString.
	lineKindMultilineStringGroup
	lineKindUnknown
)

// scannedLine is one logical line from the input file. offset+len(raw)
// gives the byte range this line occupies in the original data. For
// multi-line-string groups raw spans every physical line of the
// group so splicing remains a single contiguous range replace.
type scannedLine struct {
	offset  int
	raw     []byte   // original bytes including EOL
	lineNum int      // 1-based line number of the first physical line
	kind    lineKind
	// sectionName is populated when kind == lineKindSection. One
	// level of nesting is supported; dotted headers produce
	// ErrInvalidDotPath when they are the target.
	sectionName string
	// sectionDotted is true if the header used a dotted name like
	// `[foo.bar]`. The walker refuses to match such sections to
	// its two-segment dot path.
	sectionDotted bool
	// key and value-related fields are populated when
	// kind == lineKindKeyValue.
	key          string
	keyQuoted    bool // true if the key uses a quoted form ("a" or 'a')
	// valueStart/valueEnd are byte offsets within raw (not data)
	// bounding the value region — exclusive of any surrounding
	// whitespace or inline comment suffix.
	valueStart int
	valueEnd   int
	// valueLeadKind is the first non-whitespace byte of the value
	// region, used to detect refusal cases: `[` → array,
	// `{` → inline table, `"` / `'` → string (possibly multi-line).
	valueLeadKind byte
}

// scanLines tokenizes data into a flat list of scannedLines. The
// scanner is intentionally minimal: it recognizes section headers,
// bare comments, blank lines, bare key = value rows, and multi-
// line-string assignments (""" or '''). Everything else becomes
// lineKindUnknown — harmless for the walker because the walker only
// acts on section + key-value lines.
func scanLines(data []byte) ([]scannedLine, error) {
	var out []scannedLine
	offset := 0
	lineNum := 0

	for offset < len(data) {
		lineNum++
		lineStart := offset
		// Advance to the next newline (inclusive) or EOF.
		eolIdx := bytes.IndexByte(data[offset:], '\n')
		var lineEnd int
		if eolIdx < 0 {
			lineEnd = len(data)
		} else {
			lineEnd = offset + eolIdx + 1 // include the \n
		}
		raw := data[lineStart:lineEnd]

		// Classify.
		content := trimEOL(raw)
		trimmed := strings.TrimLeft(string(content), " \t")

		switch {
		case trimmed == "":
			out = append(out, scannedLine{
				offset:  lineStart,
				raw:     raw,
				lineNum: lineNum,
				kind:    lineKindBlank,
			})
			offset = lineEnd
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, scannedLine{
				offset:  lineStart,
				raw:     raw,
				lineNum: lineNum,
				kind:    lineKindComment,
			})
			offset = lineEnd
		case strings.HasPrefix(trimmed, "[["):
			// Array of tables — yap's schema does not use this.
			// We treat it as an unknown/opaque line; if the
			// walker encounters one it simply will not match
			// any section name and the walker refuses via the
			// "section not found" path.
			out = append(out, scannedLine{
				offset:  lineStart,
				raw:     raw,
				lineNum: lineNum,
				kind:    lineKindUnknown,
			})
			offset = lineEnd
		case strings.HasPrefix(trimmed, "["):
			sec, dotted, ok := parseSectionHeader(trimmed)
			if !ok {
				// Malformed section header: preserve as unknown
				// so splicing still works for unrelated keys.
				out = append(out, scannedLine{
					offset:  lineStart,
					raw:     raw,
					lineNum: lineNum,
					kind:    lineKindUnknown,
				})
				offset = lineEnd
				break
			}
			out = append(out, scannedLine{
				offset:        lineStart,
				raw:           raw,
				lineNum:       lineNum,
				kind:          lineKindSection,
				sectionName:   sec,
				sectionDotted: dotted,
			})
			offset = lineEnd
		default:
			// Probable key = value line. Try to parse it; if
			// the parse fails we fall back to lineKindUnknown
			// and the walker skips it. A parse error is only
			// a hard failure when the walker is actually asked
			// to target this line.
			ln, consumed, err := parseKeyValueLine(data, lineStart, lineNum)
			if err != nil {
				// Record the line as unknown with no key info.
				// The error is deferred until the walker tries
				// to use it as the target. consumed is the
				// single-line length in this case.
				out = append(out, scannedLine{
					offset:  lineStart,
					raw:     raw,
					lineNum: lineNum,
					kind:    lineKindUnknown,
				})
				offset = lineEnd
				break
			}
			out = append(out, ln)
			// parseKeyValueLine may consume multiple physical
			// lines (multi-line strings); the number of newlines
			// consumed tells us how many lineNum entries to skip.
			if consumed > len(raw) {
				// Adjust lineNum by counting newlines in the
				// consumed span.
				extraNL := bytes.Count(data[lineStart+len(raw):lineStart+consumed], []byte("\n"))
				lineNum += extraNL
			}
			offset = lineStart + consumed
		}
	}

	return out, nil
}

// trimEOL returns raw without a trailing LF or CRLF. Used when
// classifying lines by their content without worrying about line-
// ending variants.
func trimEOL(raw []byte) []byte {
	n := len(raw)
	if n > 0 && raw[n-1] == '\n' {
		n--
	}
	if n > 0 && raw[n-1] == '\r' {
		n--
	}
	return raw[:n]
}

// parseSectionHeader parses a `[section]` header line (already
// trimmed of leading whitespace). The returned dotted flag is true
// when the header uses a dotted name like `[foo.bar]`; the walker
// rejects such sections as unsupported if they are the target. A
// malformed header returns ok=false.
func parseSectionHeader(s string) (name string, dotted bool, ok bool) {
	if len(s) < 2 || s[0] != '[' {
		return "", false, false
	}
	// Find the closing bracket, ignoring content inside quoted
	// strings. yap's schema has no quoted section names, but being
	// defensive here means a hand-edited file with a quoted name
	// is at least tokenized correctly.
	inStr := byte(0)
	end := -1
	for i := 1; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' && inStr == '"' && i+1 < len(s) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			continue
		}
		if c == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return "", false, false
	}
	raw := strings.TrimSpace(s[1:end])
	if raw == "" {
		return "", false, false
	}
	// Quoted section names: strip one layer of surrounding
	// quotes so the walker can compare against a bare dotPath.
	// Such headers are always classified as "dotted" so the
	// ambiguity guard catches them before any edit happens.
	if (strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2) ||
		(strings.HasPrefix(raw, `'`) && strings.HasSuffix(raw, `'`) && len(raw) >= 2) {
		return raw[1 : len(raw)-1], true, true
	}
	// Non-quoted section names containing a dot are dotted
	// (e.g. [foo.bar]).
	if strings.Contains(raw, ".") {
		return raw, true, true
	}
	// Validate the bare name.
	if !isBareKeyName(raw) {
		return raw, false, false
	}
	// Trailing content after the `]` is allowed (inline comment),
	// as long as the only thing between `]` and EOL is whitespace
	// or a `# comment`. Malformed trailing content downgrades to
	// unknown by returning ok=false.
	tail := strings.TrimLeft(s[end+1:], " \t")
	if tail != "" && !strings.HasPrefix(tail, "#") {
		return raw, false, false
	}
	return raw, false, true
}

// parseKeyValueLine parses a single physical line starting at
// data[lineStart] as a TOML `key = value` assignment. It returns
// the populated scannedLine and the number of bytes consumed (which
// equals the physical line length except when the value is a multi-
// line string, in which case consumed spans every physical line of
// the group). Malformed lines return an error.
func parseKeyValueLine(data []byte, lineStart, lineNum int) (scannedLine, int, error) {
	// Find the physical line end for the FIRST line of the entry.
	// The tokenizer walks forward from lineStart to find the key,
	// `=`, and value. Multi-line strings extend consumed past the
	// first line end.
	firstLineEnd := lineStart
	if nl := bytes.IndexByte(data[lineStart:], '\n'); nl >= 0 {
		firstLineEnd = lineStart + nl + 1
	} else {
		firstLineEnd = len(data)
	}
	rawFirst := data[lineStart:firstLineEnd]

	// Walk the first line to find the key and `=`.
	i := lineStart
	end := firstLineEnd

	// Skip leading whitespace.
	for i < end && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	// Parse the key. Bare key only for yap's schema; quoted keys
	// are detected and surfaced via keyQuoted for caller awareness
	// but would require a second walker pass to actually target.
	var key string
	var keyQuoted bool
	switch {
	case i < end && data[i] == '"':
		// "quoted key"
		keyQuoted = true
		j := i + 1
		for j < end && data[j] != '"' {
			if data[j] == '\\' && j+1 < end {
				j += 2
				continue
			}
			j++
		}
		if j >= end || data[j] != '"' {
			return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated quoted key at line %d", lineNum)
		}
		key = string(data[i+1 : j])
		i = j + 1
	case i < end && data[i] == '\'':
		keyQuoted = true
		j := i + 1
		for j < end && data[j] != '\'' {
			j++
		}
		if j >= end || data[j] != '\'' {
			return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated literal-quoted key at line %d", lineNum)
		}
		key = string(data[i+1 : j])
		i = j + 1
	default:
		// Bare key: read while the char is in [A-Za-z0-9_-].
		keyStart := i
		for i < end {
			c := data[i]
			if !(c >= 'A' && c <= 'Z' ||
				c >= 'a' && c <= 'z' ||
				c >= '0' && c <= '9' ||
				c == '_' || c == '-') {
				break
			}
			i++
		}
		if i == keyStart {
			return scannedLine{}, len(rawFirst), fmt.Errorf("no key found at line %d", lineNum)
		}
		key = string(data[keyStart:i])
	}

	// Dotted keys: yap's schema doesn't use them. If we see a `.`
	// after the first key segment we refuse at walker time, but
	// the scanner treats the line as key = value with the first
	// segment as the key. This means a dotted key named
	// `general.hotkey = ...` (inside no section) would be matched
	// as key="general"; walker scoping prevents false positives
	// because the target section header `[general]` must be on a
	// prior line for the match.
	//
	// For safety we mark dotted keys as unknown by bailing out
	// with an error — simpler to reject than to handle.
	for i < end && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	if i < end && data[i] == '.' {
		return scannedLine{}, len(rawFirst), fmt.Errorf("dotted keys not supported at line %d", lineNum)
	}

	// Expect `=`.
	if i >= end || data[i] != '=' {
		return scannedLine{}, len(rawFirst), fmt.Errorf("expected `=` at line %d", lineNum)
	}
	i++ // past the `=`

	// Skip whitespace before the value.
	for i < end && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	if i >= end || data[i] == '\n' || data[i] == '\r' || data[i] == '#' {
		return scannedLine{}, len(rawFirst), fmt.Errorf("missing value at line %d", lineNum)
	}

	// i is now at the first byte of the value region. Classify the
	// value: the leading byte tells us which scalar form we are
	// looking at.
	valueStart := i - lineStart // relative to raw
	lead := data[i]
	var valueEnd int
	var consumed int // total bytes consumed from lineStart

	// Detect multi-line strings by looking for """ or '''.
	if lead == '"' && i+2 < end && data[i+1] == '"' && data[i+2] == '"' {
		// Multi-line string: find the closing """. Consume every
		// physical line up to and including the closing line.
		closeIdx := findMultilineClose(data, i+3, []byte(`"""`))
		if closeIdx < 0 {
			return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated multi-line string at line %d", lineNum)
		}
		// Expand consumed to the end of the physical line that
		// contains the closing delimiter.
		groupEnd := closeIdx + 3
		if nl := bytes.IndexByte(data[groupEnd:], '\n'); nl >= 0 {
			groupEnd += nl + 1
		} else {
			groupEnd = len(data)
		}
		consumed = groupEnd - lineStart
		return scannedLine{
			offset:        lineStart,
			raw:           data[lineStart:groupEnd],
			lineNum:       lineNum,
			kind:          lineKindMultilineStringGroup,
			key:           key,
			keyQuoted:     keyQuoted,
			valueStart:    valueStart,
			valueEnd:      valueStart + (groupEnd - i),
			valueLeadKind: lead,
		}, consumed, nil
	}
	if lead == '\'' && i+2 < end && data[i+1] == '\'' && data[i+2] == '\'' {
		closeIdx := findMultilineClose(data, i+3, []byte(`'''`))
		if closeIdx < 0 {
			return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated multi-line literal string at line %d", lineNum)
		}
		groupEnd := closeIdx + 3
		if nl := bytes.IndexByte(data[groupEnd:], '\n'); nl >= 0 {
			groupEnd += nl + 1
		} else {
			groupEnd = len(data)
		}
		consumed = groupEnd - lineStart
		return scannedLine{
			offset:        lineStart,
			raw:           data[lineStart:groupEnd],
			lineNum:       lineNum,
			kind:          lineKindMultilineStringGroup,
			key:           key,
			keyQuoted:     keyQuoted,
			valueStart:    valueStart,
			valueEnd:      valueStart + (groupEnd - i),
			valueLeadKind: lead,
		}, consumed, nil
	}

	// Single-line value. Walk to the end of the value region,
	// stopping at an inline comment (`#`) or EOL. Strings need
	// quote-aware walking so a `#` inside a quoted string does
	// not terminate the value.
	switch lead {
	case '"':
		// Basic string. Walk until matching `"`, honoring escapes.
		j := i + 1
		for j < end {
			c := data[j]
			if c == '\\' && j+1 < end {
				j += 2
				continue
			}
			if c == '"' {
				j++
				break
			}
			if c == '\n' {
				return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated basic string at line %d", lineNum)
			}
			j++
		}
		// The value ends at j. After j we may have whitespace and
		// an inline comment that the editor preserves verbatim.
		valueEnd = j - lineStart
	case '\'':
		// Literal string: no escapes.
		j := i + 1
		for j < end && data[j] != '\'' {
			if data[j] == '\n' {
				return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated literal string at line %d", lineNum)
			}
			j++
		}
		if j >= end || data[j] != '\'' {
			return scannedLine{}, len(rawFirst), fmt.Errorf("unterminated literal string at line %d", lineNum)
		}
		valueEnd = (j + 1) - lineStart
	case '[':
		// Array. Inline arrays that fit on one line end at the
		// matching `]`; multi-line arrays are out of scope and
		// we refuse when the walker targets this key. For the
		// scanner we walk to the matching `]` if it fits on one
		// line, otherwise set valueEnd to the physical line end
		// and let the refusal path surface the right error.
		j := findMatchingBracket(data, i, end, '[', ']')
		if j < 0 {
			// Multi-line array — set to line end; the walker
			// will refuse with ErrArrayValue.
			valueEnd = physicalLineContentEnd(data, lineStart, firstLineEnd) - lineStart
		} else {
			valueEnd = (j + 1) - lineStart
		}
	case '{':
		j := findMatchingBracket(data, i, end, '{', '}')
		if j < 0 {
			valueEnd = physicalLineContentEnd(data, lineStart, firstLineEnd) - lineStart
		} else {
			valueEnd = (j + 1) - lineStart
		}
	default:
		// Bare scalar (number, bool, date, etc.). Walk until
		// whitespace, `#`, or EOL.
		j := i
		for j < end {
			c := data[j]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '#' {
				break
			}
			j++
		}
		valueEnd = j - lineStart
	}

	consumed = len(rawFirst)
	return scannedLine{
		offset:        lineStart,
		raw:           rawFirst,
		lineNum:       lineNum,
		kind:          lineKindKeyValue,
		key:           key,
		keyQuoted:     keyQuoted,
		valueStart:    valueStart,
		valueEnd:      valueEnd,
		valueLeadKind: lead,
	}, consumed, nil
}

// findMultilineClose searches data starting at from for the next
// occurrence of delim (either """ or '''). Returns the byte offset
// of the first delim byte, or -1 if not found. Multi-line strings
// can contain escape sequences in the basic-string form; this
// minimal implementation does NOT honor escapes when scanning for
// the close because yap's schema never uses multi-line strings and
// the scanner only needs to correctly identify the terminating
// delimiter for refusal purposes.
func findMultilineClose(data []byte, from int, delim []byte) int {
	rel := bytes.Index(data[from:], delim)
	if rel < 0 {
		return -1
	}
	return rel + from
}

// findMatchingBracket returns the offset of the byte matching open
// at data[from], stepping across quoted strings and nested brackets.
// Returns -1 if no match is found before end. Used for inline arrays
// and inline tables on a single line.
func findMatchingBracket(data []byte, from, end int, open, close byte) int {
	depth := 0
	i := from
	for i < end {
		c := data[i]
		switch c {
		case '"':
			// Skip over basic string.
			i++
			for i < end && data[i] != '"' {
				if data[i] == '\\' && i+1 < end {
					i += 2
					continue
				}
				if data[i] == '\n' {
					return -1
				}
				i++
			}
			if i < end {
				i++
			}
			continue
		case '\'':
			i++
			for i < end && data[i] != '\'' {
				if data[i] == '\n' {
					return -1
				}
				i++
			}
			if i < end {
				i++
			}
			continue
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		case '\n':
			return -1
		}
		i++
	}
	return -1
}

// physicalLineContentEnd returns the offset of the last content
// byte on the physical line (before the trailing LF/CRLF).
func physicalLineContentEnd(data []byte, lineStart, lineEnd int) int {
	end := lineEnd
	if end > lineStart && data[end-1] == '\n' {
		end--
	}
	if end > lineStart && data[end-1] == '\r' {
		end--
	}
	return end
}

// locateSection returns the index range [startIdx, endIdx) of the
// target section in lines. startIdx is the line index of the
// section header; endIdx is the line index of the next section
// header (exclusive) or len(lines) if the target is the last
// section. Returns found=false when no matching header exists.
func locateSection(lines []scannedLine, name string) (startIdx, endIdx int, found bool) {
	startIdx = -1
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if ln.kind != lineKindSection {
			continue
		}
		if startIdx < 0 {
			if !ln.sectionDotted && ln.sectionName == name {
				startIdx = i
			}
			continue
		}
		// Already found start; terminate at the next section
		// header.
		return startIdx, i, true
	}
	if startIdx < 0 {
		return 0, 0, false
	}
	return startIdx, len(lines), true
}

// rewriteValueOnLine rewrites the value region of ln with literal,
// preserving everything else on the line. Refuses for multi-line
// strings, arrays, and inline tables.
func rewriteValueOnLine(ln scannedLine, literal string) ([]byte, error) {
	if ln.kind == lineKindMultilineStringGroup {
		return nil, newEditError(ErrMultilineString, ln.lineNum, fmt.Sprintf("key %q has a multi-line string value", ln.key))
	}
	if ln.kind != lineKindKeyValue {
		return nil, newEditError(ErrInternal, ln.lineNum, fmt.Sprintf("rewriteValueOnLine called on non-key-value line (kind=%d)", ln.kind))
	}
	switch ln.valueLeadKind {
	case '[':
		return nil, newEditError(ErrArrayValue, ln.lineNum, fmt.Sprintf("key %q has an array value", ln.key))
	case '{':
		return nil, newEditError(ErrInlineTable, ln.lineNum, fmt.Sprintf("key %q has an inline-table value", ln.key))
	}
	if ln.valueStart < 0 || ln.valueEnd > len(ln.raw) || ln.valueStart >= ln.valueEnd {
		return nil, newEditError(ErrMalformedLine, ln.lineNum, fmt.Sprintf("invalid value span on key %q", ln.key))
	}
	// Splice: prefix (indent, key, `=`, whitespace before value)
	// + literal + suffix (whitespace after value, inline comment,
	// EOL). Every byte outside the [valueStart,valueEnd) range is
	// preserved unchanged.
	prefix := ln.raw[:ln.valueStart]
	suffix := ln.raw[ln.valueEnd:]
	out := make([]byte, 0, len(prefix)+len(literal)+len(suffix))
	out = append(out, prefix...)
	out = append(out, literal...)
	out = append(out, suffix...)
	return out, nil
}

// appendKeyInSection builds a new key = value line in the indent
// style of the section's existing keys and inserts it at the end
// of the section's contiguous block — immediately before the next
// section header, or at EOF if this is the last section.
//
// Indent style: if any existing key in the section has a leading
// indent, the new line inherits the first one observed (exactly
// the same byte sequence). Otherwise the new line uses no indent.
func appendKeyInSection(data []byte, eol eolKind, lines []scannedLine, sectionStart, sectionEnd int, key, literal string) ([]byte, error) {
	// Determine indent from the first key-value line in the
	// section.
	indent := ""
	for i := sectionStart + 1; i < sectionEnd; i++ {
		ln := lines[i]
		if ln.kind == lineKindKeyValue {
			// Scan raw for leading spaces/tabs up to the first
			// non-whitespace byte.
			end := 0
			for end < len(ln.raw) && (ln.raw[end] == ' ' || ln.raw[end] == '\t') {
				end++
			}
			indent = string(ln.raw[:end])
			break
		}
	}

	// Find the insertion offset: the byte offset of the first
	// trailing blank/EOF line within the section, walking
	// backwards from sectionEnd. Empty trailing lines inside the
	// section block should not be disturbed — the new key goes
	// just before them so the blank separator to the next section
	// remains visually identical.
	insertIdx := sectionEnd
	for insertIdx > sectionStart+1 {
		prev := lines[insertIdx-1]
		if prev.kind == lineKindBlank {
			insertIdx--
			continue
		}
		break
	}

	// Convert insertIdx to a byte offset in data.
	var insertOffset int
	if insertIdx >= len(lines) {
		insertOffset = len(data)
	} else {
		insertOffset = lines[insertIdx].offset
	}

	// Build the new line. Use the file's EOL.
	newLine := indent + key + " = " + literal + string(eol.bytes())

	// If the previous physical byte is not a newline we need to
	// insert one before the new line so the new key does not glue
	// to the preceding content. This only matters when the target
	// section is the final section and the file does not end with
	// a newline.
	var prefix []byte
	if insertOffset > 0 && data[insertOffset-1] != '\n' {
		prefix = eol.bytes()
	}

	out := make([]byte, 0, len(data)+len(prefix)+len(newLine))
	out = append(out, data[:insertOffset]...)
	out = append(out, prefix...)
	out = append(out, newLine...)
	out = append(out, data[insertOffset:]...)
	return out, nil
}

// appendNewSection builds `[section]` + key line at EOF, separated
// from any prior content by a single blank line (unless the file
// already ends with one).
func appendNewSection(data []byte, eol eolKind, section, key, literal string) ([]byte, error) {
	eolBytes := eol.bytes()
	var buf bytes.Buffer
	buf.Write(data)

	// Ensure the existing content ends with a newline so the new
	// section header lands on its own line.
	needsTrailingNL := len(data) > 0 && data[len(data)-1] != '\n'
	if needsTrailingNL {
		buf.Write(eolBytes)
	}

	// Ensure there is exactly one blank line between the prior
	// content and the new section header. The blank-line count is
	// measured by walking back from the end of buf.
	if len(data) > 0 && !endsWithBlankLine(buf.Bytes()) {
		buf.Write(eolBytes)
	}

	buf.WriteString("[")
	buf.WriteString(section)
	buf.WriteString("]")
	buf.Write(eolBytes)
	buf.WriteString(key)
	buf.WriteString(" = ")
	buf.WriteString(literal)
	buf.Write(eolBytes)

	return buf.Bytes(), nil
}

// endsWithBlankLine reports whether data ends with at least one
// blank line. A blank line is an EOL preceded by either another
// EOL or the start of the file. Examples:
//
//	"foo\n"           → false (one nonempty trailing line)
//	"foo\n\n"         → true  (trailing blank line)
//	"foo\r\n\r\n"     → true  (CRLF trailing blank line)
//	""                → false (empty file has no lines at all)
func endsWithBlankLine(data []byte) bool {
	n := len(data)
	if n == 0 || data[n-1] != '\n' {
		return false
	}
	// Strip exactly one trailing EOL. If what remains still ends
	// with a newline (or is now empty), the original ended with a
	// blank line.
	n--
	if n > 0 && data[n-1] == '\r' {
		n--
	}
	if n == 0 {
		return true
	}
	return data[n-1] == '\n'
}
