package yamllint

import (
	"slices"
	"strings"

	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// anchorsRule is yamllint's anchors.
type anchorsRule struct {
	forbidUndeclaredAliases bool
	forbidDuplicatedAnchors bool
	forbidUnusedAnchors     bool
	anchors                 map[string]*anchorInfo
	order                   []string
}

type anchorInfo struct {
	line, column int
	used         bool
}

func (*anchorsRule) id() string { return "anchors" }

func (r *anchorsRule) resetState() {
	r.reset()
}

func (r *anchorsRule) reset() {
	if r.anchors == nil {
		r.anchors = map[string]*anchorInfo{}
	} else {
		clear(r.anchors)
	}
	r.order = r.order[:0]
}

func (r *anchorsRule) check(_ []rune, t *token) []Problem {
	c := t.curr
	switch c.Kind {
	case yamlscan.StreamStart, yamlscan.DocumentStart, yamlscan.DocumentEnd:
		r.reset()
	}
	if r.anchors == nil {
		r.reset()
	}

	var out []Problem
	switch {
	case r.forbidUndeclaredAliases && c.Kind == yamlscan.Alias && r.anchors[c.Value] == nil:
		out = append(out, anchorProblem(c, "found undeclared alias"))
	case r.forbidDuplicatedAnchors && c.Kind == yamlscan.Anchor && r.anchors[c.Value] != nil:
		out = append(out, anchorProblem(c, "found duplicated anchor"))
	}
	if r.forbidUnusedAnchors {
		switch {
		case is(t.next, yamlscan.StreamEnd, yamlscan.DocumentStart, yamlscan.DocumentEnd):
			for _, name := range r.order {
				if info := r.anchors[name]; info != nil && !info.used {
					out = append(out, Problem{
						Line: info.line + 1, Column: info.column + 1,
						Desc: `found unused anchor "` + name + `"`, Args: []any{name},
					})
				}
			}
		case c.Kind == yamlscan.Alias:
			if info := r.anchors[c.Value]; info != nil {
				info.used = true
			}
		}
	}
	if c.Kind == yamlscan.Anchor {
		if r.anchors[c.Value] == nil {
			r.order = append(r.order, c.Value)
		}
		r.anchors[c.Value] = &anchorInfo{line: c.Start.Line, column: c.Start.Column}
	}
	return out
}

// anchorProblem reports one anchor defect, all of which name the offending
// anchor the same way.
func anchorProblem(c *yamlscan.Token, what string) Problem {
	return Problem{
		Line: c.Start.Line + 1, Column: c.Start.Column + 1,
		Desc: what + ` "` + c.Value + `"`, Args: []any{c.Value},
	}
}

// flowPairRule implements braces and brackets, which differ only in the
// tokens they watch and the words in their messages.
type flowPairRule struct {
	ruleID     string
	start, end yamlscan.Kind
	forbid     any
	minInside  int
	maxInside  int
	minEmpty   int
	maxEmpty   int
	forbidDesc string
	tooFew     string
	tooMany    string
	tooFewEmp  string
	tooManyEmp string
}

func newFlowPairRule(ruleID string, start, end yamlscan.Kind, word string, cfg *Config) *flowPairRule {
	forbidDesc := "forbidden flow mapping"
	if word == "brackets" {
		forbidDesc = "forbidden flow sequence"
	}
	r := &flowPairRule{
		ruleID: ruleID, start: start, end: end,
		forbid:     cfg.optAny(ruleID, "forbid"),
		minInside:  cfg.optInt(ruleID, "min-spaces-inside"),
		maxInside:  cfg.optInt(ruleID, "max-spaces-inside"),
		minEmpty:   cfg.optInt(ruleID, "min-spaces-inside-empty"),
		maxEmpty:   cfg.optInt(ruleID, "max-spaces-inside-empty"),
		forbidDesc: forbidDesc,
		tooFew:     "too few spaces inside " + word,
		tooMany:    "too many spaces inside " + word,
		tooFewEmp:  "too few spaces inside empty " + word,
		tooManyEmp: "too many spaces inside empty " + word,
	}
	// -1 means "fall back to the non-empty bounds", which yamllint resolves at
	// check time; resolving it here keeps the check itself branchless.
	if r.minEmpty == -1 {
		r.minEmpty = r.minInside
	}
	if r.maxEmpty == -1 {
		r.maxEmpty = r.maxInside
	}
	return r
}

func (r *flowPairRule) id() string { return r.ruleID }

func (r *flowPairRule) check(buf []rune, t *token) []Problem {
	c := t.curr
	empty := t.next != nil && t.next.Kind == r.end
	switch {
	case r.forbids(empty) && c.Kind == r.start:
		return []Problem{{Line: c.Start.Line + 1, Column: c.End.Column + 1, Desc: r.forbidDesc}}
	case c.Kind == r.start && empty:
		if p := spacesAfter(c, t.next, r.minEmpty, r.maxEmpty, r.tooFewEmp, r.tooManyEmp); p != nil {
			return []Problem{*p}
		}
	case c.Kind == r.start:
		if p := spacesAfter(c, t.next, r.minInside, r.maxInside, r.tooFew, r.tooMany); p != nil {
			return []Problem{*p}
		}
	case c.Kind == r.end && (t.prev == nil || t.prev.Kind != r.start):
		if p := spacesBefore(buf, c, t.prev, r.minInside, r.maxInside, r.tooFew, r.tooMany); p != nil {
			return []Problem{*p}
		}
	}
	return nil
}

// forbids reports whether the `forbid` option rejects this flow collection:
// true rejects every one, "non-empty" only those with content.
func (r *flowPairRule) forbids(empty bool) bool {
	switch v := r.forbid.(type) {
	case bool:
		return v
	case string:
		return v == "non-empty" && !empty
	}
	return false
}

// colonsRule is yamllint's colons.
type colonsRule struct {
	maxBefore int
	maxAfter  int
}

func (colonsRule) id() string { return "colons" }

func (r colonsRule) check(buf []rune, t *token) []Problem {
	c := t.curr
	var out []Problem
	if c.Kind == yamlscan.Value &&
		(t.prev == nil || t.prev.Kind != yamlscan.Alias || c.Start.Pointer-t.prev.End.Pointer != 1) {
		if p := spacesBefore(buf, c, t.prev, -1, r.maxBefore, "", "too many spaces before colon"); p != nil {
			out = append(out, *p)
		}
		if p := spacesAfter(c, t.next, -1, r.maxAfter, "", "too many spaces after colon"); p != nil {
			out = append(out, *p)
		}
	}
	if c.Kind == yamlscan.Key && isExplicitKey(buf, c) {
		if p := spacesAfter(c, t.next, -1, r.maxAfter, "", "too many spaces after question mark"); p != nil {
			out = append(out, *p)
		}
	}
	return out
}

// commasRule is yamllint's commas.
type commasRule struct {
	maxBefore int
	minAfter  int
	maxAfter  int
}

func (commasRule) id() string { return "commas" }

func (r commasRule) check(buf []rune, t *token) []Problem {
	c := t.curr
	if c.Kind != yamlscan.FlowEntry {
		return nil
	}
	var out []Problem
	if t.prev != nil && r.maxBefore != -1 && t.prev.End.Line < c.Start.Line {
		out = append(out, Problem{
			Line: c.Start.Line + 1, Column: max(1, c.Start.Column),
			Desc: "too many spaces before comma",
		})
	} else if p := spacesBefore(buf, c, t.prev, -1, r.maxBefore, "", "too many spaces before comma"); p != nil {
		out = append(out, *p)
	}
	if p := spacesAfter(c, t.next, r.minAfter, r.maxAfter,
		"too few spaces after comma", "too many spaces after comma"); p != nil {
		out = append(out, *p)
	}
	return out
}

// hyphensRule is yamllint's hyphens.
type hyphensRule struct{ maxAfter int }

func (hyphensRule) id() string { return "hyphens" }

func (r hyphensRule) check(_ []rune, t *token) []Problem {
	if t.curr.Kind != yamlscan.BlockEntry {
		return nil
	}
	if p := spacesAfter(t.curr, t.next, -1, r.maxAfter, "", "too many spaces after hyphen"); p != nil {
		return []Problem{*p}
	}
	return nil
}

// documentStartRule is yamllint's document-start. ansible-lint disables it,
// but a repository config that says `extends: default` puts it back.
type documentStartRule struct{ present bool }

func (documentStartRule) id() string { return "document-start" }

func (r documentStartRule) check(_ []rune, t *token) []Problem {
	c := t.curr
	if r.present {
		if is(t.prev, yamlscan.StreamStart, yamlscan.DocumentEnd, yamlscan.VersionDirective, yamlscan.TagDirective) &&
			!is(c, yamlscan.DocumentStart, yamlscan.VersionDirective, yamlscan.TagDirective, yamlscan.StreamEnd) {
			return []Problem{{Line: c.Start.Line + 1, Column: 1, Desc: `missing document start "---"`}}
		}
		return nil
	}
	if c.Kind == yamlscan.DocumentStart {
		return []Problem{{
			Line: c.Start.Line + 1, Column: c.Start.Column + 1,
			Desc: `found forbidden document start "---"`,
		}}
	}
	return nil
}

// octalValuesRule is yamllint's octal-values.
type octalValuesRule struct {
	forbidImplicit bool
	forbidExplicit bool
}

// isOctalDigits reports a non-empty all-[0-7] string, the hand-rolled form
// of upstream's ^[0-7]+$: it runs on every plain scalar, where a regexp
// engine's per-match state is measurable.
func isOctalDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return true
}

func (*octalValuesRule) id() string { return "octal-values" }

func (r *octalValuesRule) check(_ []rune, t *token) []Problem {
	c := t.curr
	if c.Kind != yamlscan.Scalar || c.Style != yamlscan.StylePlain {
		return nil
	}
	if t.prev != nil && t.prev.Kind == yamlscan.Tag {
		return nil
	}
	val := c.Value
	if r.forbidImplicit && len(val) > 1 && val[0] == '0' && isOctalDigits(val[1:]) {
		return []Problem{{
			Line: c.Start.Line + 1, Column: c.End.Column + 1,
			Desc: `forbidden implicit octal value "` + val + `"`, Args: []any{val},
		}}
	}
	if r.forbidExplicit && len(val) > 2 && val[:2] == "0o" && isOctalDigits(val[2:]) {
		return []Problem{{
			Line: c.Start.Line + 1, Column: c.End.Column + 1,
			Desc: `forbidden explicit octal value "` + val + `"`, Args: []any{val},
		}}
	}
	return nil
}

// keyDuplicatesRule is yamllint's key-duplicates.
type keyDuplicatesRule struct {
	forbidDuplicatedMergeKeys bool
	stack                     []mapParent
}

// mapParent mirrors upstream's Parent: a list of seen keys searched linearly,
// which allocates nothing for empty mappings and one slice otherwise.
type mapParent struct {
	isMap bool
	keys  []string
}

func (*keyDuplicatesRule) id() string { return "key-duplicates" }

func (r *keyDuplicatesRule) resetState() { r.stack = r.stack[:0] }

func (r *keyDuplicatesRule) check(_ []rune, t *token) []Problem {
	c := t.curr
	switch c.Kind {
	case yamlscan.BlockMappingStart, yamlscan.FlowMappingStart:
		r.push(mapParent{isMap: true})
	case yamlscan.BlockSequenceStart, yamlscan.FlowSequenceStart:
		r.push(mapParent{})
	case yamlscan.BlockEnd, yamlscan.FlowMappingEnd, yamlscan.FlowSequenceEnd:
		if len(r.stack) > 0 {
			r.stack = r.stack[:len(r.stack)-1]
		}
	case yamlscan.Key:
		next := t.next
		if next == nil || next.Kind != yamlscan.Scalar || len(r.stack) == 0 || !r.stack[len(r.stack)-1].isMap {
			return nil
		}
		top := &r.stack[len(r.stack)-1]
		if slices.Contains(top.keys, next.Value) && (next.Value != "<<" || r.forbidDuplicatedMergeKeys) {
			return []Problem{{
				Line: next.Start.Line + 1, Column: next.Start.Column + 1,
				Desc: `duplication of key "` + next.Value + `" in mapping`, Args: []any{next.Value},
			}}
		}
		top.keys = append(top.keys, next.Value)
	}
	return nil
}

// push reuses the popped tail of the stack, so re-entering a nesting depth
// costs no allocation and the retained key slices get recycled.
func (r *keyDuplicatesRule) push(p mapParent) {
	if len(r.stack) < cap(r.stack) {
		r.stack = r.stack[:len(r.stack)+1]
		top := &r.stack[len(r.stack)-1]
		top.isMap = p.isMap
		top.keys = top.keys[:0]
		return
	}
	r.stack = append(r.stack, p)
}

// truthyRule is yamllint's truthy. The YAML 1.1 truthy set applies unless a
// %YAML 1.2 directive governs the document.
type truthyRule struct {
	allowed       map[string]bool
	allowedList   string
	allowedDesc   string
	checkKeys     bool
	specVersion12 bool
	seenDirective bool
	bad11, bad12  map[string]bool
	badValues     map[string]bool
}

var truthy11 = []string{
	"YES", "Yes", "yes", "NO", "No", "no",
	"TRUE", "True", "true", "FALSE", "False", "false",
	"ON", "On", "on", "OFF", "Off", "off",
}

var truthy12 = []string{"TRUE", "True", "true", "FALSE", "False", "false"}

func newTruthyRule(cfg *Config) *truthyRule {
	values := cfg.optStrings("truthy", "allowed-values")
	allowed := make(map[string]bool, len(values))
	for _, v := range values {
		allowed[v] = true
	}
	sorted := slices.Sorted(slices.Values(values))
	badSet := func(set []string) map[string]bool {
		bad := map[string]bool{}
		for _, v := range set {
			if !allowed[v] {
				bad[v] = true
			}
		}
		return bad
	}
	r := &truthyRule{
		allowed:     allowed,
		allowedList: strings.Join(sorted, ", "),
		allowedDesc: "truthy value should be one of [" + strings.Join(sorted, ", ") + "]",
		checkKeys:   cfg.optBool("truthy", "check-keys"),
		bad11:       badSet(truthy11),
		bad12:       badSet(truthy12),
	}
	r.badValues = r.bad11
	return r
}

func (*truthyRule) id() string { return "truthy" }

func (r *truthyRule) resetState() {
	r.seenDirective = false
	r.specVersion12 = false
	r.badValues = r.bad11
}

func (r *truthyRule) check(_ []rune, t *token) []Problem {
	c := t.curr
	switch c.Kind {
	case yamlscan.VersionDirective:
		r.seenDirective = true
		r.specVersion12 = c.Major == 1 && c.Minor == 2
		if r.specVersion12 {
			r.badValues = r.bad12
		} else {
			r.badValues = r.bad11
		}
	case yamlscan.DocumentEnd:
		r.seenDirective = false
		r.specVersion12 = false
		r.badValues = r.bad11
	}

	if t.prev != nil && t.prev.Kind == yamlscan.Tag {
		return nil
	}
	if !r.checkKeys && t.prev != nil && t.prev.Kind == yamlscan.Key && c.Kind == yamlscan.Scalar {
		return nil
	}
	if c.Kind != yamlscan.Scalar || c.Style != yamlscan.StylePlain {
		return nil
	}
	if r.badValues[c.Value] {
		return []Problem{{
			Line: c.Start.Line + 1, Column: c.Start.Column + 1,
			Desc: r.allowedDesc, Args: []any{r.allowedList},
		}}
	}
	return nil
}
