package yamllint

import (
	"fmt"

	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// commentsRule is yamllint's comments.
type commentsRule struct {
	minSpacesFromContent int
	requireStartingSpace bool
	ignoreShebangs       bool
}

func (r *commentsRule) check(buf []rune, c comment) []Problem {
	var out []Problem
	if r.minSpacesFromContent != -1 && c.isInline(buf) &&
		c.pointer-c.beforeEnd.Pointer < r.minSpacesFromContent {
		out = append(out, Problem{
			Line: c.lineNo, Column: c.columnNo,
			Desc: fmt.Sprintf("too few spaces before comment: expected %d", r.minSpacesFromContent),
			Args: []any{r.minSpacesFromContent},
		})
	}
	if r.requireStartingSpace {
		if p, ok := r.missingStartingSpace(buf, c); ok {
			out = append(out, p)
		}
	}
	return out
}

// missingStartingSpace reports a comment whose text, past its run of `#`,
// does not start with a space. A shebang on the first line may be exempt.
func (r *commentsRule) missingStartingSpace(buf []rune, c comment) (Problem, bool) {
	textStart := commentTextStart(buf, c)
	if textStart >= len(buf) {
		return Problem{}, false
	}
	if r.ignoreShebangs && c.lineNo == 1 && c.columnNo == 1 && buf[textStart] == '!' {
		return Problem{}, false
	}
	if r := buf[textStart]; r == ' ' || r == '\n' || r == '\r' {
		return Problem{}, false
	}
	return Problem{
		Line: c.lineNo, Column: c.columnNo + textStart - c.pointer,
		Desc: "missing starting space in comment",
	}, true
}

// commentTextStart returns where the comment's text begins, past its run of
// `#`.
func commentTextStart(buf []rune, c comment) int {
	textStart := c.pointer + 1
	for textStart < len(buf) && buf[textStart] == '#' {
		textStart++
	}
	return textStart
}

// checkCommentsIndentation is yamllint's comments-indentation, which has no
// options. ansible-lint disables it, but a repository config that says
// `extends: default` puts it back.
func checkCommentsIndentation(buf []rune, c comment) []Problem {
	if c.beforeKind != yamlscan.StreamStart && c.beforeEnd.Line+1 == c.lineNo {
		return nil
	}
	nextLineIndent := 0
	if c.afterValid && c.afterKind != yamlscan.StreamEnd {
		nextLineIndent = c.afterStart.Column
	}
	prevLineIndent := 0
	if c.beforeKind != yamlscan.StreamStart {
		prevLineIndent = lineIndent(buf, c.beforeStart.Pointer)
	}
	prevLineIndent = max(prevLineIndent, nextLineIndent)
	if c.prevValid && !c.prevInline {
		prevLineIndent = c.prevColumnNo - 1
	}
	if c.columnNo-1 != prevLineIndent && c.columnNo-1 != nextLineIndent {
		return []Problem{{Line: c.lineNo, Column: c.columnNo, Desc: "comment not indented like content"}}
	}
	return nil
}

// lineIndent returns the indentation of the line the given position sits on,
// mirroring yamllint's get_line_indent.
func lineIndent(buf []rune, pos int) int {
	start := 0
	for i := pos - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			start = i + 1
			break
		}
	}
	content := start
	for content < len(buf) && buf[content] == ' ' {
		content++
	}
	return content - start
}
