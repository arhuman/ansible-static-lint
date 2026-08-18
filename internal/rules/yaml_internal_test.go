package rules

import (
	"strings"
	"testing"
	"unicode"

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// pyCapitalize reproduces Python's str.capitalize(), which is how upstream
// renders yamllint descriptions: first rune title-cased, everything else
// lowercased, interpolated values included.
func pyCapitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(unicode.ToTitle(r[0])) + strings.ToLower(string(r[1:]))
}

// yamlTorture exercises every wording branch of yamlFinding except
// cannot-infer indentation, whose trigger needs a token mix the scanner
// rejects earlier.
const yamlTorture = `---
FOO: 1
FOO: 2
anchored: &OK 1
alias: *BAD
braces_pad: {  a: 1  }
braces_empty: {  }
brackets_pad: [ 1,2,  3 ]
brackets_empty: [  ]
colon_before : x
colon_after:  x
?  explicit
: v
octal1: 0777
octal2: 0o777
truthy: yes
#nospace
qc: "v"# close comment
hyphen:
  -  spaced
blanks:



after: 1
ind:
   a: 1
seq:
- x
long: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa b"
trail: "x"` + "   " + `
last: no_newline`

// TestYamlMessagesMatchPythonCapitalize pins the manual capitalization in
// yamlFinding's literals to the real upstream transformation: for every
// problem the port can produce, the upstream message must equal Python's
// str.capitalize() of the yamllint description.
func TestYamlMessagesMatchPythonCapitalize(t *testing.T) {
	f := &parse.File{Path: "torture.yml"}
	problems := yamllint.Lint(yamlTorture, yamllint.AnsibleLintDefaults())
	if len(problems) < 18 {
		t.Fatalf("torture input yields only %d problems, branches are untested: %v", len(problems), problems)
	}
	seen := map[string]bool{}
	for _, p := range problems {
		fd := yamlFinding(f, p)
		if want := pyCapitalize(p.Desc); fd.Message != want {
			t.Errorf("yaml[%s] wording drifted: got %q, want %q", p.Rule, fd.Message, want)
		}
		if fd.Tag != "yaml["+p.Rule+"]" {
			t.Errorf("problem of rule %s got tag %s", p.Rule, fd.Tag)
		}
		if fd.Line != p.Line || fd.Column != 0 {
			t.Errorf("yaml[%s] position: got %d:%d, want %d with no column", p.Rule, fd.Line, fd.Column, p.Line)
		}
		seen[p.Rule+"|"+p.Desc] = true
	}
	for _, want := range []string{
		"key-duplicates", "anchors", "braces", "brackets", "colons", "commas",
		"comments", "empty-lines", "hyphens", "indentation", "line-length",
		"new-line-at-end-of-file", "octal-values", "trailing-spaces", "truthy",
	} {
		found := false
		for k := range seen {
			if strings.HasPrefix(k, want+"|") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("torture input never triggered %s", want)
		}
	}
	// The interpolated key and alias must come out lowercased, exactly like
	// Python's capitalize() leaves them.
	for k := range seen {
		if strings.HasPrefix(k, "key-duplicates|") && !strings.Contains(k, `"FOO"`) {
			t.Errorf("torture dup key changed: %s", k)
		}
	}
}

// TestYamlCannotInferWording covers the wording branch the torture input
// cannot reach through the scanner.
func TestYamlCannotInferWording(t *testing.T) {
	f := &parse.File{Path: "x.yml"}
	fd := yamlFinding(f, yamllint.Problem{Line: 3, Rule: "indentation", Desc: "cannot infer indentation: unexpected token"})
	if fd.Message != "Cannot infer indentation: unexpected token" || fd.Tag != "yaml[indentation]" {
		t.Errorf("unexpected wording: %+v", fd)
	}
}
