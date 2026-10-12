package markers

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name, line, kind, markerName, text string
		ok                                 bool
	}{
		{name: "env", line: "# @env VALUE: description", kind: KindEnv, markerName: "VALUE", text: "description", ok: true},
		{name: "source", line: "# @env-source VALUE: provider", kind: KindSource, markerName: "VALUE", text: "provider", ok: true},
		{name: "preamble", line: "# @env-preamble VALUE: text", kind: KindPreamble, markerName: "VALUE", text: "text", ok: true},
		{name: "example", line: "# @env-example VALUE: 3004", kind: KindExample, markerName: "VALUE", text: "3004", ok: true},
		{name: "no hash", line: "@env VALUE: description", kind: KindEnv, markerName: "VALUE", text: "description", ok: true},
		{name: "preamble empty text", line: "# @env-preamble VALUE:", kind: KindPreamble, markerName: "VALUE", text: "", ok: true},
		{name: "example with colon in value", line: "# @env-example URL: https://a:b", kind: KindExample, markerName: "URL", text: "https://a:b", ok: true},
		{name: "plain comment", line: "# just a comment", ok: false},
		{name: "empty", line: "", ok: false},
		{name: "missing colon", line: "# @env VALUE", kind: KindInvalid, ok: true},
		{name: "empty description", line: "# @env VALUE:", kind: KindInvalid, ok: true},
		{name: "empty example", line: "# @env-example VALUE: ", kind: KindInvalid, ok: true},
		{name: "bad name", line: "# @env NOT VALID: text", kind: KindInvalid, ok: true},
		{name: "unknown suffix", line: "# @environment VALUE: text", kind: KindInvalid, ok: true},
		{name: "glued suffix", line: "# @envVALUE: text", kind: KindInvalid, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, name, text, ok := Parse(tt.line)
			if ok != tt.ok || kind != tt.kind || name != tt.markerName || text != tt.text {
				t.Fatalf("Parse(%q) = %q, %q, %q, %v", tt.line, kind, name, text, ok)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"VALUE", "_PRIVATE", "lower", "A1", "A_1"} {
		if !ValidName(name) {
			t.Fatalf("%s should be valid", name)
		}
	}
	for _, name := range []string{"", "1ST", "NOT VALID", "A-B"} {
		if ValidName(name) {
			t.Fatalf("%s should be invalid", name)
		}
	}
}
