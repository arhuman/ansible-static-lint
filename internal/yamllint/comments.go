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

	if !r.requireStartingSpace {
		return out
	}
	textStart := c.pointer + 1
	for textStart < len(buf) && buf[textStart] == '#' {
		textStart++
	}
	if textStart < len(buf) {
		if r.ignoreShebangs && c.lineNo == 1 && c.columnNo == 1 && buf[textStart] == '!' {
			return out
		}
		if r := buf[textStart]; r != ' ' && r != '\n' && r != '\r' {
			out = append(out, Problem{
				Line: c.lineNo, Column: c.columnNo + textStart - c.pointer,
				Desc: "missing starting space in comment",
			})
		}
	}
	return out
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
