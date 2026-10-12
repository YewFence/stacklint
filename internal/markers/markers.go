// Package markers parses stacklint's @env-* comment markers in Compose
// source files. Marker lines look like `# @env-example NAME: text`; yaml.v3
// comment fields are fed in line by line.
package markers

import "strings"

// Marker kinds, longest first so Parse does not swallow a longer marker with
// the @env prefix.
const (
	KindPreamble = "@env-preamble"
	KindExample  = "@env-example"
	KindSource   = "@env-source"
	KindEnv      = "@env"
	KindInvalid  = "invalid"
)

var ordered = []string{KindPreamble, KindExample, KindSource, KindEnv}

// Parse extracts a stacklint marker from one comment line, tolerating an
// optional leading "#". ok is false for non-marker lines. kind is KindInvalid
// for malformed markers: unknown @env suffix, missing colon, invalid variable
// name, or empty text (only @env-preamble allows empty text).
func Parse(line string) (kind, name, text string, ok bool) {
	content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if !strings.HasPrefix(content, KindEnv) {
		return "", "", "", false
	}
	kind, rest := KindInvalid, ""
	for _, marker := range ordered {
		trimmed, found := strings.CutPrefix(content, marker)
		if found && (trimmed == "" || trimmed[0] == ' ' || trimmed[0] == '\t') {
			kind, rest = marker, trimmed
			break
		}
	}
	name, text, hasColon := strings.Cut(strings.TrimSpace(rest), ":")
	name, text = strings.TrimSpace(name), strings.TrimSpace(text)
	if kind != KindInvalid && (!hasColon || !ValidName(name) || text == "" && kind != KindPreamble) {
		kind = KindInvalid
	}
	if kind == KindInvalid {
		return KindInvalid, "", "", true
	}
	return kind, name, text, true
}

// ValidName reports whether name is a usable environment variable name.
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for i, char := range name {
		if char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}
