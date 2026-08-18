// Package rules implements the static ansible-lint rules covered by astl.
package rules

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// Finding is a single rule violation.
type Finding struct {
	// Path is the file (or role directory) the finding belongs to.
	Path string
	// Line is 1-based. Column is 0 when the rule reports no column.
	Line   int
	Column int
	// Tag is the full rule tag, e.g. "name[casing]" or "no-changed-when".
	Tag string
	// Message is ansible-lint's own wording, reproduced verbatim. It is what the
	// default output prints, and the byte-for-byte parity contract is written
	// against it, so it is never edited to read better.
	Message string
	// NativeMessage describes the same defect in astl's own vocabulary and is
	// printed under `--ids native`. It carries the same interpolated values as
	// Message, and a test asserts the two never coincide.
	NativeMessage string
	// Warning marks a finding ansible-lint reports at warning level rather than
	// error level, which its pep8 output flags with a trailing `(warning)`.
	// Only rules upstream tags `experimental` are warnings here.
	Warning bool
	// lineScoped marks findings whose upstream path filters suppressions on
	// the finding's own line only (var-naming's play and vars-file passes):
	// the task and metadata skip scopes must not touch them.
	lineScoped bool
}

// MessageFor returns the wording matching a rule-identifier taxonomy: upstream's
// verbatim text by default, astl's own under IDNative. An unset NativeMessage
// falls back to the upstream text rather than printing an empty finding; a test
// asserts no emittable finding leaves it unset.
func (f Finding) MessageFor(style IDStyle) string {
	if style == IDNative && f.NativeMessage != "" {
		return f.NativeMessage
	}
	return f.Message
}

// RuleID returns the tag without its subtag.
func (f Finding) RuleID() string {
	if i := strings.IndexByte(f.Tag, '['); i > 0 {
		return f.Tag[:i]
	}
	return f.Tag
}

// Sort orders findings the way ansible-lint orders matches: by path, line,
// rule id, upstream message, then column with unset columns first (its
// MatchError._hash_key, minus the details field astl does not carry). The
// upstream message stays the sort key under --ids native too, so both
// taxonomies print in the same order.
func Sort(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		switch {
		case a.Path != b.Path:
			return a.Path < b.Path
		case a.Line != b.Line:
			return a.Line < b.Line
		case a.RuleID() != b.RuleID():
			return a.RuleID() < b.RuleID()
		case a.Message != b.Message:
			return a.Message < b.Message
		default:
			return sortColumn(a) < sortColumn(b)
		}
	})
}

// sortColumn maps an unset column to -1, upstream's "sort before all others".
func sortColumn(f Finding) int {
	if f.Column == 0 {
		return -1
	}
	return f.Column
}

// Dedupe drops findings identical under the sort key, mirroring upstream's
// sorted(set(matches)): yamllint can report one defect twice on a line (both
// sides of over-padded brackets) and ansible-lint prints it once. The input
// must already be sorted.
func Dedupe(findings []Finding) []Finding {
	out := findings[:0]
	for i, f := range findings {
		if i > 0 {
			prev := findings[i-1]
			if f.Path == prev.Path && f.Line == prev.Line && f.Tag == prev.Tag &&
				f.Message == prev.Message && f.Column == prev.Column {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// Every constructor below takes the two wordings of one defect side by side:
// msg is ansible-lint's verbatim text, nativeMsg is astl's own. Keeping them
// adjacent at the call site is what stops the pair from drifting, and lets one
// AST test prove every emittable tag carries both.

// at builds a finding positioned on a YAML node, including its column.
func at(f *parse.File, node *yaml.Node, tag, msg, nativeMsg string) Finding {
	p := parse.NodePos(node)
	if p.Line == 0 {
		p.Line = 1
	}
	return Finding{Path: f.Path, Line: p.Line, Column: p.Column, Tag: tag, Message: msg, NativeMessage: nativeMsg}
}

// onLine builds a finding with a line but no column, matching rules that pass
// only a line number upstream.
func onLine(f *parse.File, line int, tag, msg, nativeMsg string) Finding {
	if line == 0 {
		line = 1
	}
	return Finding{Path: f.Path, Line: line, Tag: tag, Message: msg, NativeMessage: nativeMsg}
}

// warnOnLine is onLine for a rule ansible-lint reports at warning level.
func warnOnLine(f *parse.File, line int, tag, msg, nativeMsg string) Finding {
	fd := onLine(f, line, tag, msg, nativeMsg)
	fd.Warning = true
	return fd
}

// warnAt is at for a rule ansible-lint reports at warning level.
func warnAt(f *parse.File, node *yaml.Node, tag, msg, nativeMsg string) Finding {
	fd := at(f, node, tag, msg, nativeMsg)
	fd.Warning = true
	return fd
}

// yamlAt builds one yaml[*] finding from a yamllint problem: its line, no
// column, which is what upstream's YamllintRule passes through.
func yamlAt(f *parse.File, p yamllint.Problem, tag, msg, nativeMsg string) Finding {
	return onLine(f, p.Line, tag, msg, nativeMsg)
}

// whole builds a finding for a file as a whole, which upstream reports at line 1.
func whole(f *parse.File, tag, msg, nativeMsg string) Finding {
	return Finding{Path: f.Path, Line: 1, Tag: tag, Message: msg, NativeMessage: nativeMsg}
}

// pyRepr renders a YAML node the way Python's repr() would render the value
// ansible-lint interpolates into some messages.
func pyRepr(n *yaml.Node) string {
	if n == nil {
		return "None"
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return "None"
		case "!!bool":
			if strings.EqualFold(n.Value, "true") || n.Value == "yes" || n.Value == "on" {
				return "True"
			}
			return "False"
		case "!!int", "!!float":
			return n.Value
		default:
			// A plain `no` is a string to yaml.v3 and False to the PyYAML
			// ansible parses with, so it has to render as Python's False here
			// rather than as the quoted word.
			if v, ok := parse.PyBool(n); ok {
				return pyBoolLiteral(v)
			}
			return pyQuote(n.Value)
		}
	case yaml.SequenceNode:
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			parts = append(parts, pyRepr(c))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case yaml.MappingNode:
		parts := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			parts = append(parts, fmt.Sprintf("%s: %s", pyRepr(n.Content[i]), pyRepr(n.Content[i+1])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return pyQuote(n.Value)
	}
}

// pyBoolLiteral spells a bool the way Python prints one.
func pyBoolLiteral(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// pyStr renders a node the way Python's str() would, used where ansible-lint
// interpolates a value into a message with its own quoting.
func pyStr(n *yaml.Node) string {
	if n == nil {
		return "None"
	}
	if n.Kind == yaml.ScalarNode {
		switch n.Tag {
		case "!!null":
			return "None"
		case "!!bool":
			if strings.EqualFold(n.Value, "true") || n.Value == "yes" || n.Value == "on" {
				return "True"
			}
			return "False"
		default:
			if v, ok := parse.PyBool(n); ok {
				return pyBoolLiteral(v)
			}
			return n.Value
		}
	}
	return pyRepr(n)
}

func pyQuote(s string) string {
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}
