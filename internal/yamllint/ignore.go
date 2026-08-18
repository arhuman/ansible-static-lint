package yamllint

import (
	"regexp"
	"strings"
)

// ignoreSpec is a yamllint `ignore:` pattern list, compiled. yamllint hands the
// list to pathspec's GitIgnoreSpec, so the syntax is git's, not filepath.Match's:
// a pattern without a slash matches at any depth, one with a slash is anchored
// to the run's working directory, a trailing slash matches a directory and
// everything under it, `**` spans path segments, and a leading `!` re-includes.
//
// Only whether a file is skipped matters here, never why, so the spec compiles
// to one regexp per pattern and nothing else is retained.
type ignoreSpec struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	re     *regexp.Regexp
	negate bool
}

// parseIgnore compiles a pattern list. Lines it cannot make sense of are
// dropped rather than reported: yamllint accepts the same list through pathspec
// without complaint, and refusing to lint a repository over one odd line in an
// ignore list would be a worse outcome than ignoring the line.
func parseIgnore(lines []string) *ignoreSpec {
	spec := &ignoreSpec{}
	for _, line := range lines {
		p, ok := compileIgnore(line)
		if !ok {
			continue
		}
		spec.patterns = append(spec.patterns, p)
	}
	if len(spec.patterns) == 0 {
		return nil
	}
	return spec
}

// match reports whether path is ignored. Later patterns win, which is what
// makes `!` re-inclusion work.
func (s *ignoreSpec) match(path string) bool {
	if s == nil {
		return false
	}
	path = strings.TrimPrefix(strings.TrimPrefix(path, "./"), "/")
	ignored := false
	for _, p := range s.patterns {
		if p.re.MatchString(path) {
			ignored = !p.negate
		}
	}
	return ignored
}

// compileIgnore turns one gitignore-style line into a matcher.
func compileIgnore(line string) (ignorePattern, bool) {
	// Trailing whitespace is not part of a pattern unless escaped, and a
	// leading `#` is a comment. Both are git's rules, which pathspec keeps.
	line = trimUnescapedTrailingSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ignorePattern{}, false
	}
	negate := false
	if strings.HasPrefix(line, "!") {
		negate, line = true, line[1:]
	} else if strings.HasPrefix(line, `\`) && len(line) > 1 {
		// `\#` and `\!` are how a pattern starts with those characters.
		line = line[1:]
	}
	if line == "" {
		return ignorePattern{}, false
	}

	dirOnly := strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	if line == "" {
		return ignorePattern{}, false
	}
	// A slash anywhere but the end anchors the pattern to the working
	// directory; without one it matches a name at any depth.
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return ignorePattern{}, false
	}

	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	segments := strings.Split(line, "/")
	for i, seg := range segments {
		if seg == "**" {
			// Zero or more whole segments. Written as a repeated group rather
			// than `.*` so it cannot swallow half a name.
			b.WriteString("(?:[^/]+/)*")
			continue
		}
		b.WriteString(translateSegment(seg))
		if i < len(segments)-1 {
			b.WriteString("/")
		}
	}
	if dirOnly {
		// The spec only ever matches file paths, so a directory pattern has to
		// match something inside it.
		b.WriteString("/.*")
	} else {
		// A name matches the file itself and, when it names a directory,
		// everything beneath it.
		b.WriteString("(?:/.*)?")
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return ignorePattern{}, false
	}
	return ignorePattern{re: re, negate: negate}, true
}

// translateSegment renders one path segment's glob as a regexp fragment. `*`
// and `?` stop at a separator, which is what distinguishes them from `**`.
func translateSegment(seg string) string {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		switch c := seg[i]; c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			class, next, ok := translateClass(seg, i)
			if !ok {
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			b.WriteString(class)
			i = next
		case '\\':
			if i+1 < len(seg) {
				i++
				b.WriteString(regexp.QuoteMeta(string(seg[i])))
				continue
			}
			b.WriteString(regexp.QuoteMeta(string(c)))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// translateClass renders a `[...]` character class starting at seg[i], and
// returns the index of its closing bracket. An unterminated class is not one:
// the caller then treats the bracket as a literal, as git does.
func translateClass(seg string, i int) (class string, end int, ok bool) {
	j := i + 1
	var negated bool
	if j < len(seg) && (seg[j] == '!' || seg[j] == '^') {
		negated, j = true, j+1
	}
	// A `]` immediately after the opening bracket is a literal member.
	if j < len(seg) && seg[j] == ']' {
		j++
	}
	for j < len(seg) && seg[j] != ']' {
		j++
	}
	if j >= len(seg) {
		return "", i, false
	}
	body := seg[i+1 : j]
	if negated {
		// RE2 has no class intersection, so a negated class keeps globs
		// segment-local by naming the separator among the excluded characters.
		// A positive class is left as written: it would have to spell `/` out
		// to cross a segment, which no real pattern does.
		return "[^" + strings.TrimLeft(body, "!^") + "/]", j, true
	}
	return "[" + body + "]", j, true
}

// trimUnescapedTrailingSpace drops trailing whitespace that is not backslash
// escaped, so a pattern may end in a deliberate space.
func trimUnescapedTrailingSpace(line string) string {
	end := len(line)
	for end > 0 && (line[end-1] == ' ' || line[end-1] == '\t') {
		backslashes := 0
		for k := end - 2; k >= 0 && line[k] == '\\'; k-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			break
		}
		end--
	}
	return line[:end]
}
