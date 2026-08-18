package rules

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// node parses one YAML value and returns the node the rules would see.
func node(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	if len(doc.Content) == 0 {
		t.Fatalf("parse %q: no content", src)
	}
	return doc.Content[0]
}

// TestPyRepr pins how a value is rendered into the messages astl reproduces
// verbatim, which puts these two functions on the byte-exactness surface even
// though no rule calls them directly.
//
// The boolean rows are the reason this exists. ansible parses YAML 1.1 through
// PyYAML, so a bare `no` inside a container renders as Python's False, while
// the parser astl uses calls it the string "no" and would quote it. Quoting in
// the source is what makes PyYAML keep a string, so `"no"` renders quoted.
func TestPyRepr(t *testing.T) {
	tests := map[string]string{
		"x":                   "'x'",
		"~":                   "None",
		"5":                   "5",
		"1.5":                 "1.5",
		"true":                "True",
		"false":               "False",
		"no":                  "False",
		"yes":                 "True",
		"off":                 "False",
		"ON":                  "True",
		`"no"`:                "'no'",
		"[my_list, no]":       "['my_list', False]",
		"{a: no, b: yes}":     "{'a': False, 'b': True}",
		"[]":                  "[]",
		"it's":                `"it's"`,
		`['a', {b: [no]}]`:    "['a', {'b': [False]}]",
		"unquoted words here": "'unquoted words here'",
	}
	for src, want := range tests {
		t.Run(src, func(t *testing.T) {
			if got := pyRepr(node(t, src)); got != want {
				t.Fatalf("pyRepr(%s) = %s, want %s", src, got, want)
			}
		})
	}
	if got := pyRepr(nil); got != "None" {
		t.Errorf("pyRepr(nil) = %s, want None", got)
	}
}

// TestPyStr covers the other renderer, which ansible-lint uses where it
// interpolates a value into a message that supplies its own quoting.
func TestPyStr(t *testing.T) {
	tests := map[string]string{
		"x":     "x",
		"~":     "None",
		"true":  "True",
		"false": "False",
		"no":    "False",
		"YES":   "True",
		`"no"`:  "no",
		"5":     "5",
	}
	for src, want := range tests {
		t.Run(src, func(t *testing.T) {
			if got := pyStr(node(t, src)); got != want {
				t.Fatalf("pyStr(%s) = %s, want %s", src, got, want)
			}
		})
	}
	if got := pyStr(nil); got != "None" {
		t.Errorf("pyStr(nil) = %s, want None", got)
	}
}

// TestPyQuote pins Python's repr quoting: single quotes normally, double quotes
// when the value holds a single quote and no double one, and an escape when it
// holds both.
func TestPyQuote(t *testing.T) {
	tests := map[string]string{
		"plain":     "'plain'",
		"it's":      `"it's"`,
		`say "hi"`:  `'say "hi"'`,
		`it's "hi"`: `'it\'s "hi"'`,
		"":          "''",
	}
	for in, want := range tests {
		if got := pyQuote(in); got != want {
			t.Errorf("pyQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
