package yamllint

import (
	"fmt"
	"slices"

	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// lineLengthRule is yamllint's line-length.
type lineLengthRule struct {
	max                    int
	allowNonBreakableWords bool
	allowInlineMappings    bool
}

func (lineLengthRule) id() string { return "line-length" }

func (r lineLengthRule) check(buf []rune, l line) []Problem {
	length := l.end - l.start
	if length <= r.max {
		return nil
	}
	// The inline-mapping option implies the word one, as it does upstream.
	if (r.allowNonBreakableWords || r.allowInlineMappings) && r.exempt(buf, l) {
		return nil
	}
	return []Problem{{
		Line: l.no, Column: r.max + 1,
		Desc: fmt.Sprintf("line too long (%d > %d characters)", length, r.max),
		Args: []any{length, r.max},
	}}
}

// exempt reports whether an over-long line escapes the limit: it is one
// unbreakable word (after optional indentation and a leading `#...` or `- `
// marker), or, when the inline-mapping option is on, a mapping whose value is
// itself unbreakable.
func (r lineLengthRule) exempt(buf []rune, l line) bool {
	start := l.start
	for start < l.end && buf[start] == ' ' {
		start++
	}
	if start == l.end {
		return false
	}
	switch buf[start] {
	case '#':
		for start < l.end && buf[start] == '#' {
			start++
		}
		start++
	case '-':
		start += 2
	}
	hasSpace := false
	for i := start; i < l.end && i >= 0; i++ {
		if buf[i] == ' ' {
			hasSpace = true
			break
		}
	}
	if !hasSpace {
		return true
	}
	return r.allowInlineMappings && inlineMappingIsUnbreakable(buf[l.start:l.end])
}

// inlineMappingIsUnbreakable reports whether a line is a mapping whose first
// value is a scalar with no space in it, yamllint's check_inline_mapping. The
// line is scanned on its own, so a broken remainder simply yields no verdict.
func inlineMappingIsUnbreakable(content []rune) bool {
	toks := yamlscan.Tokens(string(content))
	for i, tok := range toks {
		if tok.Kind != yamlscan.BlockMappingStart {
			continue
		}
		for j := i + 1; j+1 < len(toks); j++ {
			if toks[j].Kind != yamlscan.Value {
				continue
			}
			value := toks[j+1]
			if value.Kind != yamlscan.Scalar {
				return false
			}
			if value.Start.Column >= len(content) {
				return true
			}
			return !slices.Contains(content[value.Start.Column:], ' ')
		}
		return false
	}
	return false
}

// trailingSpacesRule is yamllint's trailing-spaces.
type trailingSpacesRule struct{}

func (trailingSpacesRule) id() string { return "trailing-spaces" }

func (trailingSpacesRule) check(buf []rune, l line) []Problem {
	if l.end == 0 {
		return nil
	}
	// YAML recognizes two whitespace characters: space and tab.
	pos := l.end
	for pos > l.start && pyWhitespace(buf[pos-1]) {
		pos--
	}
	if pos != l.end && (buf[pos] == ' ' || buf[pos] == '\t') {
		return []Problem{{Line: l.no, Column: pos - l.start + 1, Desc: "trailing spaces"}}
	}
	return nil
}

// emptyLinesRule is yamllint's empty-lines. The \r\n branches of the original
// are gone: input is universal-newline normalized before linting.
type emptyLinesRule struct {
	max      int
	maxStart int
	maxEnd   int
}

func (emptyLinesRule) id() string { return "empty-lines" }

func (r emptyLinesRule) check(buf []rune, l line) []Problem {
	if l.start != l.end || l.end >= len(buf) {
		return nil
	}
	// Only alert on the last blank line of a series.
	if l.end+2 <= len(buf) && buf[l.end] == '\n' && buf[l.end+1] == '\n' {
		return nil
	}

	blankLines := 0
	start := l.start
	for start >= 1 && buf[start-1] == '\n' {
		blankLines++
		start--
	}

	maxAllowed := r.max
	// Special case: start of document.
	if start == 0 {
		blankLines++ // first line doesn't have a preceding \n
		maxAllowed = r.maxStart
	}
	// Special case: end of document. POSIX wants the last line to end with a
	// new line; the one-byte file containing '\n' is allowed through.
	if l.end == len(buf)-1 && buf[l.end] == '\n' {
		if l.end == 0 {
			return nil
		}
		maxAllowed = r.maxEnd
	}

	if blankLines > maxAllowed {
		return []Problem{{
			Line: l.no, Column: 1,
			Desc: fmt.Sprintf("too many blank lines (%d > %d)", blankLines, maxAllowed),
			Args: []any{blankLines, maxAllowed},
		}}
	}
	return nil
}

// newLineAtEndOfFileRule is yamllint's new-line-at-end-of-file.
type newLineAtEndOfFileRule struct{}

func (newLineAtEndOfFileRule) id() string { return "new-line-at-end-of-file" }

func (newLineAtEndOfFileRule) check(buf []rune, l line) []Problem {
	if l.end == len(buf) && l.end > l.start {
		return []Problem{{
			Line: l.no, Column: l.end - l.start + 1,
			Desc: "no new line character at the end of file",
		}}
	}
	return nil
}
