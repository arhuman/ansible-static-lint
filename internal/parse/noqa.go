package parse

import (
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// reNoqa matches an inline `# noqa` comment and captures the rule list.
var reNoqa = regexp.MustCompile(`#\s*noqa\b[\s:]*(.*)$`)

// reNoqaOnly matches a line that holds nothing but a noqa comment; such a
// comment also applies to the next non-empty line.
var reNoqaOnly = regexp.MustCompile(`^\s*#\s*noqa\b`)

// parseNoqa returns, per 1-based line, the rule ids or tags to skip. Lines
// are walked in place; most files carry no noqa at all, and a fast substring
// check keeps those allocation-free.
func parseNoqa(content string) map[int]map[string]bool {
	if !strings.Contains(content, "noqa") {
		return nil
	}
	skips := map[int]map[string]bool{}
	remaining := content
	for lineNo := 1; ; lineNo++ {
		line, rest, more := strings.Cut(remaining, "\n")
		recordNoqa(skips, lineNo, line, rest)
		if !more {
			break
		}
		remaining = rest
	}
	if len(skips) == 0 {
		return nil
	}
	return skips
}

// recordNoqa adds one line's noqa skips, and, for a line holding nothing but
// the comment, extends them to the next non-empty line.
func recordNoqa(skips map[int]map[string]bool, lineNo int, line, rest string) {
	set := noqaSet(line)
	if set == nil {
		return
	}
	skips[lineNo] = set
	if reNoqaOnly.MatchString(line) {
		if target := nextContentLine(rest, lineNo); target > 0 {
			merge(skips, target, set)
		}
	}
}

// noqaSet parses one line's noqa comment into a skip set, nil when the line
// carries none. A bare `# noqa` becomes the wildcard.
func noqaSet(line string) map[string]bool {
	if !strings.Contains(line, "noqa") {
		return nil
	}
	m := reNoqa.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	set := map[string]bool{}
	for _, tag := range strings.Fields(m[1]) {
		set[tag] = true
	}
	if len(set) == 0 {
		set["*"] = true
	}
	return set
}

// nextContentLine returns the 1-based number of the first non-empty line in
// remaining, which starts at line from+1; zero when only blank lines remain.
func nextContentLine(remaining string, from int) int {
	for no := from + 1; ; no++ {
		line, rest, more := strings.Cut(remaining, "\n")
		if strings.TrimSpace(line) != "" {
			return no
		}
		if !more {
			return 0
		}
		remaining = rest
	}
}

func merge(skips map[int]map[string]bool, line int, set map[string]bool) {
	if skips[line] == nil {
		skips[line] = map[string]bool{}
	}
	for k := range set {
		skips[line][k] = true
	}
}

// sortedNoqaLines returns the lines carrying a noqa comment, ascending, built
// once per file. Memoized rather than computed per query because the callers
// ask one range per task and per play, so rebuilding it would restore the very
// scan the index exists to remove. Safe without a lock for the same reason
// tasks and plays are: one goroutine owns a File for its whole lifetime.
func (f *File) sortedNoqaLines() []int {
	if f.noqaLinesDone {
		return f.noqaLines
	}
	f.noqaLinesDone = true
	if len(f.Noqa) == 0 {
		return nil
	}
	f.noqaLines = make([]int, 0, len(f.Noqa))
	for line := range f.Noqa {
		f.noqaLines = append(f.noqaLines, line)
	}
	slices.Sort(f.noqaLines)
	return f.noqaLines
}

// SkipsInRange collects every noqa entry between start and end (inclusive), or
// nil when the range carries none. Every caller feeds the result through a
// function that copies it, so nil costs them nothing and saves an allocation on
// the common task that is not suppressed.
func (f *File) SkipsInRange(start, end int) map[string]bool {
	lines := f.sortedNoqaLines()
	i, _ := slices.BinarySearch(lines, start)

	var out map[string]bool
	for ; i < len(lines) && lines[i] <= end; i++ {
		for k := range f.Noqa[lines[i]] {
			if out == nil {
				out = map[string]bool{}
			}
			out[k] = true
		}
	}
	return out
}

// AllSkips collects every noqa entry in the file.
func (f *File) AllSkips() map[string]bool {
	out := map[string]bool{}
	for _, set := range f.Noqa {
		for k := range set {
			out[k] = true
		}
	}
	return out
}

// EndLine returns the last source line covered by a node's subtree.
func EndLine(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	last := n.Line
	for _, c := range n.Content {
		if l := EndLine(c); l > last {
			last = l
		}
	}
	return last
}
