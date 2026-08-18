package yamllint

import (
	"fmt"

	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// indentationRule is yamllint's indentation with ansible-lint's settings:
// spaces consistent, indent-sequences true, check-multi-line-strings false
// (so the original's check_scalar_indentation never runs and is not ported).
// The transcription keeps the original's parent-stack shape; the assertions
// of the Python code become errAssert, reported as its except branch does.
type indentationRule struct {
	// indentSequences is true, false, or "consistent"/"whatever" while the
	// document has not settled it yet; a "consistent" setting mutates as the
	// file reveals its style, so the configured value is kept for reset.
	indentSequences     any
	initIndentSequences any
	checkMultiLine      bool
	stack               []indentParent
	curLine             int
	curLineIndent       int
	spaces              int
	spacesSet           bool // false while spaces is still "consistent"
	initSpacesSet       bool
	started             bool
}

// newIndentationRule resolves the two options that decide the state machine's
// starting point: `spaces`, an integer or "consistent", and
// `indent-sequences`, a boolean or "consistent"/"whatever".
func newIndentationRule(cfg *Config) *indentationRule {
	r := &indentationRule{
		indentSequences: cfg.optAny("indentation", "indent-sequences"),
		checkMultiLine:  cfg.optBool("indentation", "check-multi-line-strings"),
	}
	if n, ok := cfg.optAny("indentation", "spaces").(int); ok {
		r.spaces = n
		r.spacesSet = true
	}
	r.initIndentSequences = r.indentSequences
	r.initSpacesSet = r.spacesSet
	return r
}

// resetState rewinds the per-file state; checkToken rebuilds the stack on its
// first call, so only the settings a "consistent" document mutates need
// restoring.
func (r *indentationRule) resetState() {
	r.started = false
	r.stack = r.stack[:0]
	r.indentSequences = r.initIndentSequences
	r.spacesSet = r.initSpacesSet
}

type indentParentType int

const (
	pROOT indentParentType = iota
	pBMAP
	pFMAP
	pBSEQ
	pFSEQ
	pBENT
	pKEY
	pVAL
)

type indentParent struct {
	typ              indentParentType
	indent           int
	lineIndent       int
	explicitKey      bool
	implicitBlockSeq bool
}

func (*indentationRule) id() string { return "indentation" }

// errAssert stands for the Python assertions; raising one turns into the
// "cannot infer indentation" problem, as upstream's except clause does.
type errAssert struct{}

func (errAssert) Error() string { return "assertion failed" }

func (r *indentationRule) check(buf []rune, t *token) []Problem {
	problems, err := r.checkToken(buf, t)
	if err != nil {
		return append(problems, Problem{
			Line: t.curr.Start.Line + 1, Column: t.curr.Start.Column + 1,
			Desc: "cannot infer indentation: unexpected token",
		})
	}
	return problems
}

func (r *indentationRule) detectIndent(base int, next *yamlscan.Token) int {
	if !r.spacesSet {
		r.spaces = next.Start.Column - base
		r.spacesSet = true
	}
	return base + r.spaces
}

func (r *indentationRule) top() *indentParent { return &r.stack[len(r.stack)-1] }

func (r *indentationRule) pop() { r.stack = r.stack[:len(r.stack)-1] }

// pushBlockStart opens a block mapping or sequence, whose first inner token
// the scanner guarantees on the same line (upstream asserts it).
func (r *indentationRule) pushBlockStart(curr, next *yamlscan.Token, wantNext yamlscan.Kind, typ indentParentType) error {
	if !is(next, wantNext) || next.Start.Line != curr.Start.Line {
		return errAssert{}
	}
	r.stack = append(r.stack, indentParent{typ: typ, indent: curr.Start.Column})
	return nil
}

// sequenceIndent decides what indentation a block sequence under a key must
// have, under whichever `indent-sequences` setting is in force. Under
// "consistent" the first sequence in the document settles it into true or
// false for the rest of the file; "whatever" takes the same two branches
// without settling anything.
func (r *indentationRule) sequenceIndent(next *yamlscan.Token) int {
	parent := r.top().indent
	if v, ok := r.indentSequences.(bool); ok {
		if !v {
			return parent
		}
		// An unset `spaces` plus a sequence at the parent's own column leaves
		// the step size unknowable, so only "at least one more" can be asked.
		if !r.spacesSet && next.Start.Column-parent == 0 {
			return -1
		}
		return r.detectIndent(parent, next)
	}
	consistent := r.indentSequences == "consistent"
	if next.Start.Column == parent {
		if consistent {
			r.indentSequences = false
		}
		return parent
	}
	if consistent {
		r.indentSequences = true
	}
	return r.detectIndent(parent, next)
}

// pushFlowStart opens a flow mapping or sequence.
func (r *indentationRule) pushFlowStart(curr, next *yamlscan.Token, typ indentParentType) {
	indent := 0
	if next.Start.Line == curr.Start.Line {
		indent = next.Start.Column
	} else {
		indent = r.detectIndent(r.curLineIndent, next)
	}
	r.stack = append(r.stack, indentParent{typ: typ, indent: indent, lineIndent: r.curLineIndent})
}

func is(tok *yamlscan.Token, kinds ...yamlscan.Kind) bool {
	if tok == nil {
		return false
	}
	for _, k := range kinds {
		if tok.Kind == k {
			return true
		}
	}
	return false
}

//nolint:gocognit,gocyclo,funlen,nestif // 1:1 transcription of yamllint's _check; splitting it would blur the mapping to the reference implementation.
func (r *indentationRule) checkToken(buf []rune, t *token) ([]Problem, error) {
	if !r.started {
		r.started = true
		r.stack = append(r.stack[:0], indentParent{typ: pROOT})
		r.curLine = -1
	}
	curr, prev, next, nextnext := t.curr, t.prev, t.next, t.nextnext
	// A nil next occurs only on the last token of an error-truncated stream,
	// which pyyaml never yields (and astl never lints); bail like an assert
	// instead of dereferencing it below.
	if next == nil && is(curr, yamlscan.FlowMappingStart, yamlscan.FlowSequenceStart,
		yamlscan.BlockEntry, yamlscan.Value) {
		return nil, errAssert{}
	}

	isVisible := !is(curr, yamlscan.StreamStart, yamlscan.StreamEnd, yamlscan.BlockEnd) &&
		(curr.Kind != yamlscan.Scalar || curr.Value != "")
	firstInLine := isVisible && curr.Start.Line+1 > r.curLine

	var problems []Problem
	if firstInLine {
		found := curr.Start.Column
		expected := r.top().indent
		if is(curr, yamlscan.FlowMappingEnd, yamlscan.FlowSequenceEnd) {
			expected = r.top().lineIndent
		} else if r.top().typ == pKEY && r.top().explicitKey && !is(curr, yamlscan.Value) {
			expected = r.detectIndent(expected, curr)
		}
		if found != expected {
			p := Problem{Line: curr.Start.Line + 1, Column: found + 1}
			if expected < 0 {
				p.Desc = fmt.Sprintf("wrong indentation: expected at least %d", found+1)
				p.Args = []any{found + 1}
			} else {
				p.Desc = fmt.Sprintf("wrong indentation: expected %d but found %d", expected, found)
				p.Args = []any{expected, found}
			}
			problems = append(problems, p)
		}

		r.curLineIndent = found
	}
	if isVisible {
		r.curLine = realEndLine(buf, curr)
	}

	switch {
	case curr.Kind == yamlscan.BlockMappingStart:
		if err := r.pushBlockStart(curr, next, yamlscan.Key, pBMAP); err != nil {
			return problems, err
		}

	case curr.Kind == yamlscan.FlowMappingStart:
		r.pushFlowStart(curr, next, pFMAP)

	case curr.Kind == yamlscan.BlockSequenceStart:
		if err := r.pushBlockStart(curr, next, yamlscan.BlockEntry, pBSEQ); err != nil {
			return problems, err
		}

	case curr.Kind == yamlscan.BlockEntry && !is(next, yamlscan.BlockEntry, yamlscan.BlockEnd):
		if r.top().typ != pBSEQ {
			r.stack = append(r.stack, indentParent{typ: pBSEQ, indent: curr.Start.Column, implicitBlockSeq: true})
		}
		var indent int
		switch {
		case next.Start.Line == curr.End.Line:
			indent = next.Start.Column
		case next.Start.Column == curr.Start.Column:
			indent = next.Start.Column
		default:
			indent = r.detectIndent(curr.Start.Column, next)
		}
		r.stack = append(r.stack, indentParent{typ: pBENT, indent: indent})

	case curr.Kind == yamlscan.FlowSequenceStart:
		r.pushFlowStart(curr, next, pFSEQ)

	case curr.Kind == yamlscan.Key:
		r.stack = append(r.stack, indentParent{
			typ: pKEY, indent: r.top().indent,
			explicitKey: isExplicitKey(buf, curr),
		})

	case curr.Kind == yamlscan.Value:
		if r.top().typ != pKEY {
			return problems, errAssert{}
		}
		if is(next, yamlscan.Anchor, yamlscan.Tag) &&
			prev != nil && nextnext != nil &&
			next.Start.Line == prev.Start.Line && next.Start.Line < nextnext.Start.Line {
			next = nextnext
		}
		if !is(next, yamlscan.BlockEnd, yamlscan.FlowMappingEnd, yamlscan.FlowSequenceEnd, yamlscan.Key) {
			var indent int
			switch {
			case r.top().explicitKey:
				indent = r.detectIndent(r.top().indent, next)
			case prev != nil && next.Start.Line == prev.Start.Line:
				indent = next.Start.Column
			case is(next, yamlscan.BlockSequenceStart, yamlscan.BlockEntry):
				indent = r.sequenceIndent(next)
			default:
				indent = r.detectIndent(r.top().indent, next)
			}
			r.stack = append(r.stack, indentParent{typ: pVAL, indent: indent})
		}
	}

	consumed := false
	for {
		top := r.top()
		switch {
		case top.typ == pFSEQ && is(curr, yamlscan.FlowSequenceEnd) && !consumed:
			r.pop()
			consumed = true
		case top.typ == pFMAP && is(curr, yamlscan.FlowMappingEnd) && !consumed:
			r.pop()
			consumed = true
		case (top.typ == pBMAP || top.typ == pBSEQ) && is(curr, yamlscan.BlockEnd) &&
			!top.implicitBlockSeq && !consumed:
			r.pop()
			consumed = true
		case top.typ == pBENT && !is(curr, yamlscan.BlockEntry) &&
			r.stack[len(r.stack)-2].implicitBlockSeq &&
			!is(curr, yamlscan.Anchor, yamlscan.Tag) && !is(next, yamlscan.BlockEntry):
			r.pop()
			r.pop()
		case top.typ == pBENT && is(next, yamlscan.BlockEntry, yamlscan.BlockEnd):
			r.pop()
		case top.typ == pVAL && !is(curr, yamlscan.Value) && !is(curr, yamlscan.Anchor, yamlscan.Tag):
			if r.stack[len(r.stack)-2].typ != pKEY {
				return problems, errAssert{}
			}
			r.pop()
			r.pop()
		case top.typ == pKEY && is(next, yamlscan.BlockEnd, yamlscan.FlowMappingEnd, yamlscan.FlowSequenceEnd, yamlscan.Key):
			r.pop()
		default:
			return problems, nil
		}
	}
}
