package yamllint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The .problems goldens were generated with yamllint 1.38.0 running under
// ansible-lint's effective configuration, via the reference driver described
// in docs/design/static-yaml-and-var-naming.md. Fixtures are original; the
// goldens pin problem-level parity with the reference implementation.
func TestLintMatchesYamllintGoldens(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.yml"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".yml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join("testdata", name+".problems"))
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for _, p := range Lint(string(src), AnsibleLintDefaults()) {
				fmt.Fprintf(&b, "%d:%d %s %s\n", p.Line, p.Column, p.Rule, p.Desc)
			}
			if b.String() != string(golden) {
				t.Errorf("problems diverge from yamllint golden\ngot:\n%s\nwant:\n%s", b.String(), golden)
			}
		})
	}
}

// A DOS-newline input must behave as its LF equivalent: upstream reads files
// with universal newlines, which is also why new-lines can never fire.
func TestLintNormalizesNewlines(t *testing.T) {
	got := Lint("---\r\nkey: yes\r\n", AnsibleLintDefaults())
	if len(got) != 1 || got[0].Rule != "truthy" || got[0].Line != 2 {
		t.Errorf("want one truthy problem on line 2, got %v", got)
	}
}

func TestLintEmptyInput(t *testing.T) {
	if got := Lint("", AnsibleLintDefaults()); len(got) != 0 {
		t.Errorf("empty input must be clean, got %v", got)
	}
}
