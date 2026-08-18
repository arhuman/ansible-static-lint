// Package yamllint reproduces the yamllint 1.38.0 checks that ansible-lint
// enables, over the token stream of internal/yamlscan. Each rule file is a
// transcription of its yamllint counterpart; the reference sources are the
// yamllint package pinned in the bench venv (see
// docs/design/static-yaml-and-var-naming.md). The package is self-contained:
// it knows nothing about findings, kinds or ansible-lint tags.
package yamllint

import (
	"regexp"
	"strings"
	"sync"

	"github.com/arhuman/ansible-static-lint/internal/yamlscan"
)

// Problem is one yamllint violation.
type Problem struct {
	// Line and Column are 1-based. ansible-lint discards the column, but it
	// still decides output order upstream, so it is kept.
	Line, Column int
	// Rule is the yamllint rule id, e.g. "trailing-spaces".
	Rule string
	// Desc is yamllint's own description, verbatim, in its original lowercase.
	Desc string
	// Args are the values interpolated into Desc, in order, for the rules
	// whose description varies. The consumer rebuilds its own wording from
	// them; fixed descriptions carry none.
	Args []any
}

// Lint checks text against cfg, which callers obtain from Load (or from
// AnsibleLintDefaults for the stock policy). The input is normalized to
// universal newlines first, which is how upstream reads file content (and why
// the new-lines rule can never fire).
func Lint(text string, cfg *Config) []Problem {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.HasPrefix(text, "#") && disableFileRE.MatchString(firstLine(text)) {
		return nil
	}
	return cosmeticProblems(text, []rune(text), cfg)
}

var disableFileRE = regexp.MustCompile(`^#\s*yamllint disable-file\s*$`)

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

// line is one buffer line, newline excluded. start and end are rune offsets.
type line struct {
	no         int // 1-based
	start, end int
}

func lineList(buf []rune) []line {
	n := 1
	for _, r := range buf {
		if r == '\n' {
			n++
		}
	}
	out := make([]line, 0, n)
	no, cur := 1, 0
	for i, r := range buf {
		if r == '\n' {
			out = append(out, line{no: no, start: cur, end: i})
			cur = i + 1
			no++
		}
	}
	return append(out, line{no: no, start: cur, end: len(buf)})
}

// comment is a `#` comment found between two tokens. The neighboring tokens
// and the preceding comment contribute only a few marks, carried by value so
// that streaming over a fixed token window never retains a stale pointer.
type comment struct {
	lineNo, columnNo int // 1-based
	pointer          int // rune offset of the '#'
	beforeKind       yamlscan.Kind
	beforeStart      yamlscan.Mark
	beforeEnd        yamlscan.Mark
	afterValid       bool
	afterKind        yamlscan.Kind
	afterStart       yamlscan.Mark
	prevValid        bool // a comment preceded this one in the same gap
	prevInline       bool
	prevColumnNo     int
}

// text returns the comment from its `#` to the end of its line.
func (c comment) text(buf []rune) string {
	for i := c.pointer; i < len(buf); i++ {
		if buf[i] == '\n' {
			return string(buf[c.pointer:i])
		}
	}
	return string(buf[c.pointer:])
}

// isInline reports whether the comment shares its line with preceding content.
func (c comment) isInline(buf []rune) bool {
	return c.beforeKind != yamlscan.StreamStart &&
		c.lineNo == c.beforeEnd.Line+1 &&
		// sometimes token end marks are on the next line
		(c.beforeEnd.Pointer < 1 || buf[c.beforeEnd.Pointer-1] != '\n')
}

// forEachCommentBetween finds the comments between two consecutive tokens,
// next being nil after the last one. Only whitespace and comments can sit
// there. Yielding through a callback keeps the pass allocation-free.
func forEachCommentBetween(buf []rune, curr, next *yamlscan.Token, yield func(comment)) {
	var to int
	switch {
	case next == nil:
		to = len(buf)
	case curr.End.Line == next.Start.Line &&
		curr.Kind != yamlscan.StreamStart && next.Kind != yamlscan.StreamEnd:
		return
	default:
		to = next.Start.Pointer
	}

	lineNo := curr.End.Line + 1
	columnNo := curr.End.Column + 1
	pointer := curr.End.Pointer
	var prev comment
	prevValid := false
	for pointer <= to {
		lineEnd := to
		for i := pointer; i < to; i++ {
			if buf[i] == '\n' {
				lineEnd = i
				break
			}
		}
		for i := pointer; i < lineEnd; i++ {
			if buf[i] == '#' {
				c := comment{
					lineNo: lineNo, columnNo: columnNo + (i - pointer), pointer: i,
					beforeKind: curr.Kind, beforeStart: curr.Start, beforeEnd: curr.End,
				}
				if next != nil {
					c.afterValid = true
					c.afterKind = next.Kind
					c.afterStart = next.Start
				}
				if prevValid {
					c.prevValid = true
					c.prevInline = prev.isInline(buf)
					c.prevColumnNo = prev.columnNo
				}
				yield(c)
				prev = c
				prevValid = true
				break
			}
		}
		pointer = lineEnd + 1
		lineNo++
		columnNo = 1
	}
}

// token is one scanner token with its neighbors, mirroring yamllint's Token
// element (curr, prev, next, nextnext; any of the neighbors may be nil).
// The pointers address a four-slot ring and are only valid for the duration
// of one rule dispatch.
type token struct {
	lineNo               int // 1-based, from curr's start mark
	curr                 *yamlscan.Token
	prev, next, nextnext *yamlscan.Token
}

// linter is one file's linting pass: yamllint's get_cosmetic_problems loop,
// which runs every rule, caches problems per line, and flushes the cache at
// each line end filtered by the disable directives seen so far.
type linter struct {
	cfg                 *Config
	tokenRules          []tokenRule
	lineRules           []lineRule
	comments            *commentsRule
	commentsIndentation bool
	// enabled is the directive vocabulary for this run: a `# yamllint
	// disable` naming a rule the configuration switched off is inert.
	enabled             map[string]bool
	cache               []Problem
	disabled            *directive
	disabledForLine     *directive
	disabledForNextLine *directive
	out                 []Problem
}

// newLinter instantiates the rules cfg enables, each with its options already
// resolved so the per-token loop never consults the configuration. A disabled
// rule is not instantiated at all, which is what yamllint's enabled_rules
// does.
func newLinter(cfg *Config) *linter {
	ln := &linter{
		cfg:     cfg,
		enabled: map[string]bool{},
	}
	for _, id := range allRuleIDs {
		ln.enabled[id] = cfg.Enabled(id)
	}
	ln.disabled = newDirective(ln.enabled)
	ln.disabledForLine = newDirective(ln.enabled)
	ln.disabledForNextLine = newDirective(ln.enabled)

	ln.addTokenRules(cfg)
	ln.addLineRules(cfg)

	if cfg.Enabled("comments") {
		ln.comments = &commentsRule{
			minSpacesFromContent: cfg.optInt("comments", "min-spaces-from-content"),
			requireStartingSpace: cfg.optBool("comments", "require-starting-space"),
			ignoreShebangs:       cfg.optBool("comments", "ignore-shebangs"),
		}
	}
	ln.commentsIndentation = cfg.Enabled("comments-indentation")
	return ln
}

// addTokenRules instantiates the enabled token rules. Each is built only when
// enabled: a disabled one carries no options to read, exactly as yamllint's
// enabled_rules leaves it out.
func (ln *linter) addTokenRules(cfg *Config) {
	addToken := func(id string, build func() tokenRule) {
		if cfg.Enabled(id) {
			ln.tokenRules = append(ln.tokenRules, build())
		}
	}
	addToken("anchors", func() tokenRule {
		return &anchorsRule{
			forbidUndeclaredAliases: cfg.optBool("anchors", "forbid-undeclared-aliases"),
			forbidDuplicatedAnchors: cfg.optBool("anchors", "forbid-duplicated-anchors"),
			forbidUnusedAnchors:     cfg.optBool("anchors", "forbid-unused-anchors"),
		}
	})
	addToken("braces", func() tokenRule {
		return newFlowPairRule("braces", yamlscan.FlowMappingStart, yamlscan.FlowMappingEnd, "braces", cfg)
	})
	addToken("brackets", func() tokenRule {
		return newFlowPairRule("brackets", yamlscan.FlowSequenceStart, yamlscan.FlowSequenceEnd, "brackets", cfg)
	})
	addToken("colons", func() tokenRule {
		return colonsRule{
			maxBefore: cfg.optInt("colons", "max-spaces-before"),
			maxAfter:  cfg.optInt("colons", "max-spaces-after"),
		}
	})
	addToken("commas", func() tokenRule {
		return commasRule{
			maxBefore: cfg.optInt("commas", "max-spaces-before"),
			minAfter:  cfg.optInt("commas", "min-spaces-after"),
			maxAfter:  cfg.optInt("commas", "max-spaces-after"),
		}
	})
	addToken("document-start", func() tokenRule {
		return documentStartRule{present: cfg.optBool("document-start", "present")}
	})
	addToken("hyphens", func() tokenRule {
		return hyphensRule{maxAfter: cfg.optInt("hyphens", "max-spaces-after")}
	})
	addToken("indentation", func() tokenRule { return newIndentationRule(cfg) })
	addToken("key-duplicates", func() tokenRule {
		return &keyDuplicatesRule{
			forbidDuplicatedMergeKeys: cfg.optBool("key-duplicates", "forbid-duplicated-merge-keys"),
		}
	})
	addToken("octal-values", func() tokenRule {
		return &octalValuesRule{
			forbidImplicit: cfg.optBool("octal-values", "forbid-implicit-octal"),
			forbidExplicit: cfg.optBool("octal-values", "forbid-explicit-octal"),
		}
	})
	addToken("truthy", func() tokenRule { return newTruthyRule(cfg) })
}

// addLineRules instantiates the enabled line rules.
func (ln *linter) addLineRules(cfg *Config) {
	addLine := func(id string, build func() lineRule) {
		if cfg.Enabled(id) {
			ln.lineRules = append(ln.lineRules, build())
		}
	}

	addLine("empty-lines", func() lineRule {
		return emptyLinesRule{
			max:      cfg.optInt("empty-lines", "max"),
			maxStart: cfg.optInt("empty-lines", "max-start"),
			maxEnd:   cfg.optInt("empty-lines", "max-end"),
		}
	})
	addLine("line-length", func() lineRule {
		return lineLengthRule{
			max:                    cfg.optInt("line-length", "max"),
			allowNonBreakableWords: cfg.optBool("line-length", "allow-non-breakable-words"),
			allowInlineMappings:    cfg.optBool("line-length", "allow-non-breakable-inline-mappings"),
		}
	})
	addLine("new-line-at-end-of-file", func() lineRule { return newLineAtEndOfFileRule{} })
	addLine("trailing-spaces", func() lineRule { return trailingSpacesRule{} })
}

// cosmeticProblems drives the streaming pass in yamllint's generator order:
// each token, then the comments trailing it, with lines interleaved strictly
// by line number (tokens and comments first on ties). Tokens live in a
// four-slot ring covering exactly the prev/curr/next/nextnext window the
// rules see, so the stream is never materialized.
// linterPool recycles linters between files of the same run. A pooled linter
// is only reusable under the configuration it was built for; a mismatch (only
// possible if two configurations were ever live at once) builds a fresh one.
var linterPool sync.Pool

func cosmeticProblems(text string, buf []rune, cfg *Config) []Problem {
	ln, _ := linterPool.Get().(*linter)
	if ln == nil || ln.cfg != cfg {
		ln = newLinter(cfg)
	} else {
		ln.reset()
	}
	lines := lineList(buf)
	li := 0
	flushLinesBefore := func(lineNo int) {
		for li < len(lines) && lines[li].no < lineNo {
			ln.onLine(buf, lines[li])
			li++
		}
	}
	onComment := func(c comment) {
		flushLinesBefore(c.lineNo)
		ln.onComment(buf, c)
	}

	sc := yamlscan.NewScanner(text)
	defer sc.Close()
	var ring [4]yamlscan.Token
	loaded, eof := 0, false
	fetch := func() {
		if !eof && sc.Next(&ring[loaded%4]) {
			loaded++
		} else {
			eof = true
		}
	}
	for loaded < 3 && !eof {
		fetch()
	}
	var elem token
	for i := 0; i < loaded; i++ {
		// Top up the lookahead; token i+3 would overwrite slot (i-1)%4, the
		// prev still in use, so the window stops at i+2.
		for loaded <= i+2 && !eof {
			fetch()
		}
		elem = token{lineNo: ring[i%4].Start.Line + 1, curr: &ring[i%4]}
		if i > 0 {
			elem.prev = &ring[(i-1)%4]
		}
		if i+1 < loaded {
			elem.next = &ring[(i+1)%4]
		}
		if i+2 < loaded {
			elem.nextnext = &ring[(i+2)%4]
		}
		flushLinesBefore(elem.lineNo)
		ln.onToken(buf, &elem)
		forEachCommentBetween(buf, elem.curr, elem.next, onComment)
	}
	for li < len(lines) {
		ln.onLine(buf, lines[li])
		li++
	}
	out := ln.out
	ln.out = nil
	linterPool.Put(ln)
	return out
}

// reset returns a pooled linter to its start-of-file state. The result slice
// was detached at hand-out, so only rule and directive state remains.
func (ln *linter) reset() {
	ln.cache = ln.cache[:0]
	ln.disabled.reset()
	ln.disabledForLine.reset()
	ln.disabledForNextLine.reset()
	for _, r := range ln.tokenRules {
		if rr, ok := r.(interface{ resetState() }); ok {
			rr.resetState()
		}
	}
}

func (ln *linter) onToken(buf []rune, t *token) {
	for _, r := range ln.tokenRules {
		for _, p := range r.check(buf, t) {
			p.Rule = r.id()
			ln.cache = append(ln.cache, p)
		}
	}
}

func (ln *linter) onComment(buf []rune, c comment) {
	if ln.comments != nil {
		for _, p := range ln.comments.check(buf, c) {
			p.Rule = "comments"
			ln.cache = append(ln.cache, p)
		}
	}
	if ln.commentsIndentation {
		for _, p := range checkCommentsIndentation(buf, c) {
			p.Rule = "comments-indentation"
			ln.cache = append(ln.cache, p)
		}
	}
	// Only a comment starting `# yamllint` can be a directive; checking that
	// in place avoids extracting a string for every ordinary comment.
	if !hasDirectivePrefix(buf, c.pointer) {
		return
	}
	text := c.text(buf)
	ln.disabled.processComment(text)
	if c.isInline(buf) {
		ln.disabledForLine.processLineComment(text)
	} else {
		ln.disabledForNextLine.processLineComment(text)
	}
}

var directivePrefix = []rune("# yamllint ")

func hasDirectivePrefix(buf []rune, at int) bool {
	if at+len(directivePrefix) > len(buf) {
		return false
	}
	for i, r := range directivePrefix {
		if buf[at+i] != r {
			return false
		}
	}
	return true
}

// onLine runs the line rules, then flushes the cached problems of the line
// just ended, dropping those a directive disables.
func (ln *linter) onLine(buf []rune, l line) {
	for _, r := range ln.lineRules {
		for _, p := range r.check(buf, l) {
			p.Rule = r.id()
			ln.cache = append(ln.cache, p)
		}
	}
	for _, p := range ln.cache {
		if !ln.disabledForLine.isDisabled(p.Rule) && !ln.disabled.isDisabled(p.Rule) {
			ln.out = append(ln.out, p)
		}
	}
	// Rotate rather than allocate: the retiring per-line directive becomes
	// the next line's, reset.
	spare := ln.disabledForLine
	ln.disabledForLine = ln.disabledForNextLine
	spare.reset()
	ln.disabledForNextLine = spare
	ln.cache = ln.cache[:0]
}

type tokenRule interface {
	id() string
	check(buf []rune, t *token) []Problem
}

type lineRule interface {
	id() string
	check(buf []rune, l line) []Problem
}

// allRuleIDs is every rule astl can run. The directive vocabulary of one run
// is the subset the configuration enables, which is what yamllint hands its
// disable directives.
var allRuleIDs = []string{
	"anchors", "braces", "brackets", "colons", "commas", "comments",
	"comments-indentation", "document-start", "empty-lines", "hyphens",
	"indentation", "key-duplicates", "line-length", "new-line-at-end-of-file",
	"new-lines", "octal-values", "trailing-spaces", "truthy",
}

var (
	disableRE     = regexp.MustCompile(`^# yamllint disable( rule:\S+)*\s*$`)
	enableRE      = regexp.MustCompile(`^# yamllint enable( rule:\S+)*\s*$`)
	disableLineRE = regexp.MustCompile(`^# yamllint disable-line( rule:\S+)*\s*$`)
)

// directive tracks which rules a yamllint comment directive silences.
// vocabulary is the enabled-rule set the directive may name. The rules map is
// nil until a directive actually fires, which on most files is never.
type directive struct {
	rules      map[string]bool
	vocabulary map[string]bool
}

func newDirective(vocabulary map[string]bool) *directive {
	return &directive{vocabulary: vocabulary}
}

func (d *directive) reset() {
	if len(d.rules) > 0 {
		clear(d.rules)
	}
}

func (d *directive) set(id string) {
	if d.rules == nil {
		d.rules = map[string]bool{}
	}
	d.rules[id] = true
}

// ruleArgs replicates yamllint's fixed-offset parsing: strip the directive
// prefix, split on single spaces, drop the leading empty item, and keep the
// text after each "rule:" marker.
func ruleArgs(text string, prefixLen int) []string {
	items := strings.Split(strings.TrimRight(text[prefixLen:], " \t\n\v\f\r"), " ")
	out := make([]string, 0, len(items))
	for _, item := range items {
		if len(item) > 5 {
			out = append(out, item[5:])
		} else {
			out = append(out, "")
		}
	}
	return out[1:]
}

func (d *directive) enableAll() {
	for id, on := range d.vocabulary {
		if on {
			d.set(id)
		}
	}
}

func (d *directive) add(ids []string) {
	if len(ids) == 0 {
		d.enableAll()
		return
	}
	for _, id := range ids {
		if d.vocabulary[id] {
			d.set(id)
		}
	}
}

// processComment handles `# yamllint disable` and `# yamllint enable`.
func (d *directive) processComment(text string) {
	switch {
	case disableRE.MatchString(text):
		d.add(ruleArgs(text, len("# yamllint disable")))
	case enableRE.MatchString(text):
		ids := ruleArgs(text, len("# yamllint enable"))
		if len(ids) == 0 {
			d.reset()
			return
		}
		for _, id := range ids {
			delete(d.rules, id)
		}
	}
}

// processLineComment handles `# yamllint disable-line`.
func (d *directive) processLineComment(text string) {
	if disableLineRE.MatchString(text) {
		d.add(ruleArgs(text, len("# yamllint disable-line")))
	}
}

func (d *directive) isDisabled(rule string) bool {
	return d.rules[rule]
}
