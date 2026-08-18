package yamllint

import (
	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// spacesAfter checks the gap between tok's end and next's start on the same
// line. min or max set to -1 disables that bound, like yamllint's helpers.
func spacesAfter(tok, next *yamlscan.Token, minSpaces, maxSpaces int, minDesc, maxDesc string) *Problem {
	if next == nil || tok.End.Line != next.Start.Line {
		return nil
	}
	spaces := next.Start.Pointer - tok.End.Pointer
	if maxSpaces != -1 && spaces > maxSpaces {
		return &Problem{Line: tok.Start.Line + 1, Column: next.Start.Column, Desc: maxDesc}
	}
	if minSpaces != -1 && spaces < minSpaces {
		return &Problem{Line: tok.Start.Line + 1, Column: next.Start.Column + 1, Desc: minDesc}
	}
	return nil
}

// spacesBefore checks the gap between prev's end and tok's start on the same
// line, skipping tokens whose previous token ends at a line break.
func spacesBefore(buf []rune, tok, prev *yamlscan.Token, minSpaces, maxSpaces int, minDesc, maxDesc string) *Problem {
	if prev == nil || prev.End.Line != tok.Start.Line {
		return nil
	}
	// Discard tokens (only scalars?) that end at the start of next line.
	if prev.End.Pointer != 0 && buf[prev.End.Pointer-1] == '\n' {
		return nil
	}
	spaces := tok.Start.Pointer - prev.End.Pointer
	if maxSpaces != -1 && spaces > maxSpaces {
		return &Problem{Line: tok.Start.Line + 1, Column: tok.Start.Column, Desc: maxDesc}
	}
	if minSpaces != -1 && spaces < minSpaces {
		return &Problem{Line: tok.Start.Line + 1, Column: tok.Start.Column + 1, Desc: minDesc}
	}
	return nil
}

// pyWhitespace reports what Python's string.whitespace contains.
func pyWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// realEndLine is the 1-based line a token really ends on: pyyaml scalar
// tokens often carry an end mark on the next line, which this walks back.
func realEndLine(buf []rune, tok *yamlscan.Token) int {
	endLine := tok.End.Line + 1
	if tok.Kind != yamlscan.Scalar {
		return endLine
	}
	pos := tok.End.Pointer - 1
	for pos >= tok.Start.Pointer-1 && pos >= 0 && pos < len(buf) && pyWhitespace(buf[pos]) {
		if buf[pos] == '\n' {
			endLine--
		}
		pos--
	}
	return endLine
}

// isExplicitKey reports whether a Key token is the explicit `? key` form.
func isExplicitKey(buf []rune, tok *yamlscan.Token) bool {
	return tok.Start.Pointer < tok.End.Pointer &&
		tok.Start.Pointer < len(buf) &&
		buf[tok.Start.Pointer] == '?'
}
