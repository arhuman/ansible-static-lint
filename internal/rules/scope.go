package rules

import "strings"

// OutOfScopeRule names one ansible-lint rule astl does not implement, and what
// reproducing it would require. It exists so that a consumer of astl's output
// can tell "this rule found nothing" from "this rule was never evaluated": a
// SARIF report declares both lists, and an editor reading it can say which
// diagnostics it is entitled to claim coverage for.
type OutOfScopeRule struct {
	// ID is the ansible-lint rule id. These rules have no native counterpart,
	// astl having nothing to name.
	ID string
	// Requires is the capability astl would need, phrased so that "nothing"
	// marks the rules held back by implementation effort rather than by the
	// static boundary. docs/scope.md quantifies what each one costs.
	Requires string
}

// OutOfScope is every rule in ansible-lint 26.8.0's inventory that astl does
// not implement: `ansible-lint --list-rules` reports 53 rules, IDs covers 38,
// and these are the remaining 15. Ordered by what they require, then by id.
//
// Keeping the list here rather than deriving it means it can go stale against
// a newer ansible-lint. That is the trade: a wrong entry is visible in the
// output and fixable, whereas deriving it would need the runtime astl exists
// to avoid. TestOutOfScopeDisjointFromIDs guards the half that can be checked.
var OutOfScope = []OutOfScopeRule{
	{"args", "validating task arguments against module argument specs"},
	{"fqcn", "resolving module names through Ansible's plugin loader"},
	{"only-builtins", "resolving whether an action is a builtin, through that same loader"},
	{"syntax-check", "running ansible-playbook --syntax-check as a subprocess"},

	{"internal-error", "Ansible's own loader failures, surfaced as findings"},
	{"load-failure", "Ansible's own loader failures, surfaced as findings"},
	{"parser-error", "Ansible's parser failures, surfaced as findings"},
	{"warning", "ansible-lint's own runtime warnings, which have no static equivalent"},

	{"jinja", "evaluating Jinja templates with Ansible's filter set"},
	{"schema", "upstream's JSON Schema bundle"},
	{"no-free-form", "Ansible's argument splitter semantics as data"},
	{"deprecated-module", "upstream's deprecated-module inventory as data"},

	{"latest", "nothing: static, not implemented yet"},
	{"no-same-owner", "nothing: static, not implemented yet"},
	{"role-argument-spec", "nothing: static, not implemented yet"},
}

// Descriptor names one rule astl can report, in both taxonomies at once, so a
// consumer can map between them without carrying its own copy of the table.
type Descriptor struct {
	// ID is the rule tag in the requested taxonomy, matching the ids findings
	// carry under that same taxonomy.
	ID string
	// Upstream and Native are the same tag in each taxonomy, always populated
	// whichever style ID was rendered in.
	Upstream string
	Native   string
	// Base is the upstream rule the tag belongs to, `name` for `name[play]`.
	// It is the rule ansible-lint documents, subtags having no page of their
	// own.
	Base string
	// Description is astl's own one-line statement of the defect, in the
	// project's words rather than upstream's (ADR 0007). It is markdown source
	// as docs/rules.md publishes it, so a renderer emitting plain text has to
	// account for its code spans.
	Description string
}

// Descriptors lists every rule tag astl can report, in the equivalence table's
// order, which groups them by domain. Rows whose base rule astl does not
// implement are left out, so the result stays a description of this build
// rather than of the equivalence table's reach.
func Descriptors(style IDStyle) []Descriptor {
	implemented := make(map[string]bool, len(IDs))
	for _, id := range IDs {
		implemented[id] = true
	}
	out := make([]Descriptor, 0, len(equivalence))
	for _, p := range equivalence {
		base := BaseRule(p.upstream)
		if !implemented[base] {
			continue
		}
		out = append(out, Descriptor{
			ID:          TagFor(p.upstream, style),
			Upstream:    p.upstream,
			Native:      p.native,
			Base:        base,
			Description: p.desc,
		})
	}
	return out
}

// BaseRule strips a subtag: `name[play]` is reported by the `name` rule. A tag
// carrying no subtag is its own base.
func BaseRule(tag string) string {
	if i := strings.IndexByte(tag, '['); i >= 0 {
		return tag[:i]
	}
	return tag
}
