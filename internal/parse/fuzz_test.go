package parse

import (
	"strings"
	"testing"
)

// noqaSeeds cover the syntax's branches: an inline comment, a comment on its
// own line (which also applies to the next non-empty line), the bare wildcard,
// several ids at once, and the shapes around them that must not be mistaken
// for one.
var noqaSeeds = []string{
	"",
	"- name: t  # noqa no-changed-when\n",
	"# noqa\n- name: t\n",
	"# noqa no-changed-when\n\n\n- name: t\n",
	"# noqa\n",
	"- x  # noqa a b c\n- y\n",
	"- x  # noqa: no-changed-when\n",
	"- x  #noqa\n",
	"- x  # NOQA\n",
	"noqa\n",
	"- x  # noqa\r\n- y\r\n",
	"- x # noqa    \n",
	"# noqa\n# noqa\n- x\n",
	strings.Repeat("# noqa\n", 40),
}

// FuzzNoqaIndex checks the suppression index against a scan of the map it
// indexes.
//
// SkipsInRange used to walk every noqa line in the file for every task, which
// made a playbook cost tasks times suppressions; it now binary-searches a
// sorted key list built once. That is a real optimization over a data structure
// the file's own text decides the shape of, and its failure mode is silence:
// a wrong bound drops a suppression, the finding it should have hidden is
// reported, and nothing crashes. So the property under test is equivalence
// with the obvious implementation, over ranges the fuzzer chooses, including
// inverted and out-of-range ones that the callers do not currently produce but
// nothing prevents.
func FuzzNoqaIndex(f *testing.F) {
	for _, seed := range noqaSeeds {
		f.Add(seed, 1, 10)
	}
	f.Add("- x  # noqa a\n- y  # noqa b\n", 2, 1)
	f.Add("- x  # noqa a\n", -5, 1<<30)

	f.Fuzz(func(t *testing.T, content string, start, end int) {
		noqa := parseNoqa(content)
		checkNoqaKeys(t, noqa, strings.Count(content, "\n")+1)

		file := &File{Noqa: noqa}
		got, want := file.SkipsInRange(start, end), scanSkips(noqa, start, end)
		if !sameSet(got, want) {
			t.Fatalf("range [%d,%d]: got %v, want %v", start, end, got, want)
		}
	})
}

// checkNoqaKeys asserts the index keys point at real lines. SkipsInRange
// binary-searches them against a task's line span, so a key outside the file
// would either never match or match the wrong task.
func checkNoqaKeys(t *testing.T, noqa map[int]map[string]bool, lines int) {
	t.Helper()
	for line, set := range noqa {
		if line < 1 || line > lines {
			t.Fatalf("noqa recorded at line %d, file has %d lines", line, lines)
		}
		if len(set) == 0 {
			t.Fatalf("noqa at line %d holds an empty skip set", line)
		}
	}
}

// scanSkips is SkipsInRange written the obvious way, as the oracle.
func scanSkips(noqa map[int]map[string]bool, start, end int) map[string]bool {
	out := map[string]bool{}
	for line, set := range noqa {
		if line < start || line > end {
			continue
		}
		for k := range set {
			out[k] = true
		}
	}
	return out
}

// sameSet compares skip sets, treating nil and empty as equal: SkipsInRange
// returns nil for an empty result on purpose, to save an allocation on the
// common task that is not suppressed.
func sameSet(got, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}
