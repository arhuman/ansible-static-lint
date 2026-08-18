package parse

import "testing"

func TestParseNoqaForms(t *testing.T) {
	content := "---\n" +
		"a: 1 # noqa: rule-one rule-two\n" +
		"b: 2 # noqa\n" +
		"# noqa: rule-three\n" +
		"\n" +
		"c: 3\n" +
		"plain noqa word without comment marker\n" +
		"d: 4 # noqa: rule-four"
	skips := parseNoqa(content)

	if !skips[2]["rule-one"] || !skips[2]["rule-two"] {
		t.Errorf("line 2 skips = %v, want rule-one and rule-two", skips[2])
	}
	if !skips[3]["*"] {
		t.Errorf("bare noqa must skip everything on its line, got %v", skips[3])
	}
	// A noqa-only line also covers the next non-empty line, across blanks.
	if !skips[4]["rule-three"] || !skips[6]["rule-three"] {
		t.Errorf("noqa-only line must cover itself and line 6, got 4=%v 6=%v", skips[4], skips[6])
	}
	if len(skips[7]) != 0 {
		t.Errorf("a bare 'noqa' word is not a directive, got %v", skips[7])
	}
	// The last line has no trailing newline and must still be seen.
	if !skips[8]["rule-four"] {
		t.Errorf("line 8 skips = %v, want rule-four", skips[8])
	}
}

func TestParseNoqaFastPath(t *testing.T) {
	if got := parseNoqa("---\na: 1\nb: 2\n"); got != nil {
		t.Errorf("content without noqa must parse to nil, got %v", got)
	}
	if got := parseNoqa(""); got != nil {
		t.Errorf("empty content must parse to nil, got %v", got)
	}
}

func TestSkipHelpers(t *testing.T) {
	f := &File{Noqa: parseNoqa("a # noqa: one\nb\nc # noqa: two\n")}
	in := f.SkipsInRange(1, 2)
	if !in["one"] || in["two"] {
		t.Errorf("SkipsInRange(1,2) = %v, want only one", in)
	}
	all := f.AllSkips()
	if !all["one"] || !all["two"] {
		t.Errorf("AllSkips() = %v, want one and two", all)
	}
}

func TestEndLine(t *testing.T) {
	dir := t.TempDir()
	abs := dir + "/x.yml"
	if err := writeFile(abs, "---\na:\n  b:\n    - 1\n    - 2\n"); err != nil {
		t.Fatal(err)
	}
	f := Load("x.yml", abs, "yaml")
	if f.Root == nil {
		t.Fatal("no root")
	}
	if got := EndLine(f.Root); got != 5 {
		t.Errorf("EndLine = %d, want 5", got)
	}
	if got := EndLine(nil); got != 0 {
		t.Errorf("EndLine(nil) = %d, want 0", got)
	}
}
