package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// reTagLiteral matches the shape of a rule tag, `rule` or `rule[subtag]`.
var reTagLiteral = regexp.MustCompile(`^[a-z][a-z0-9-]*(\[[a-z0-9-]+\])?$`)

// TestEquivalenceRoundTrips asserts each upstream id resolves to exactly one
// native id and back, and that neither derived map has gained or lost a key.
func TestEquivalenceRoundTrips(t *testing.T) {
	for _, p := range equivalence {
		native, ok := upstreamToNative[p.upstream]
		switch {
		case !ok:
			t.Errorf("%q: no native id", p.upstream)
		case native != p.native:
			t.Errorf("%q maps to %q, want %q", p.upstream, native, p.native)
		}
		if back := nativeToUpstream[p.native]; back != p.upstream {
			t.Errorf("%q round-trips to %q, want %q", p.native, back, p.upstream)
		}
	}
	if len(upstreamToNative) != len(equivalence) || len(nativeToUpstream) != len(equivalence) {
		t.Errorf("table has %d rows but %d upstream and %d native keys",
			len(equivalence), len(upstreamToNative), len(nativeToUpstream))
	}
}

// TestTagForAndCanonicalAgreeWithTheTable checks the two public entry points
// against every row, so neither can drift from the declaration.
func TestTagForAndCanonicalAgreeWithTheTable(t *testing.T) {
	for _, p := range equivalence {
		if got := TagFor(p.upstream, IDNative); got != p.native {
			t.Errorf("TagFor(%q, native) = %q, want %q", p.upstream, got, p.native)
		}
		if got := TagFor(p.upstream, IDUpstream); got != p.upstream {
			t.Errorf("TagFor(%q, upstream) = %q, want it unchanged", p.upstream, got)
		}
		if got := Canonical(p.native); got != p.upstream {
			t.Errorf("Canonical(%q) = %q, want %q", p.native, got, p.upstream)
		}
		if got := Canonical(p.upstream); got != p.upstream {
			t.Errorf("Canonical(%q) = %q, want it unchanged", p.upstream, got)
		}
	}
}

// TestEquivalenceGrammar pins the `domain.rule[tag]` shape and the invariant
// that makes rule-level suppression work in both taxonomies: an native id
// carrying a subtag must share its base row with the upstream id's base.
func TestEquivalenceGrammar(t *testing.T) {
	domains := map[string]bool{
		"name": true, "task": true, "deprecated": true, "role": true,
		"meta": true, "galaxy": true, "play": true, "file": true, "yaml": true,
		"var": true,
	}
	for _, p := range equivalence {
		base, sub := splitTag(p.native)
		domain, _, qualified := strings.Cut(base, ".")
		if !domains[domain] {
			t.Errorf("%q: unknown domain %q", p.native, domain)
		}
		if !qualified && sub != "" {
			t.Errorf("%q: a bare domain carries no subtag", p.native)
		}
		if sub == "" {
			continue
		}
		upstreamBase, upstreamSub := splitTag(p.upstream)
		if upstreamSub != sub {
			t.Errorf("%q: subtag %q does not carry over from %q", p.native, sub, p.upstream)
		}
		if got := nativeToUpstream[base]; got != upstreamBase {
			t.Errorf("%q: base %q maps to %q, want %q", p.native, base, got, upstreamBase)
		}
	}
}

// TestEquivalenceCoversEveryEmittableTag scans the rule sources for the tag
// literals the engine can emit and asserts the table covers each one. Scanning
// keeps the assertion honest: a new tag literal fails this test until the
// table gains a row, and no second list of ids is maintained by hand.
func TestEquivalenceCoversEveryEmittableTag(t *testing.T) {
	tags := tagLiteralsInSources(t)
	if len(tags) < len(IDs) {
		t.Fatalf("found only %d tag literals, expected at least the %d rule ids", len(tags), len(IDs))
	}
	for _, tag := range tags {
		if _, ok := upstreamToNative[tag]; !ok {
			t.Errorf("rule sources emit %q but the equivalence table has no row for it", tag)
		}
	}
}

// TestEquivalenceHasNoStrayRows is the converse: every upstream row belongs to
// a rule the registry declares.
func TestEquivalenceHasNoStrayRows(t *testing.T) {
	known := ruleIDSet()
	for _, p := range equivalence {
		if base, _ := splitTag(p.upstream); !known[base] {
			t.Errorf("%q: rule %q is not in the rules registry", p.upstream, base)
		}
	}
}

func TestCanonicalNormalizesTokens(t *testing.T) {
	tests := map[string]string{
		"*":                  "*",
		"not-a-rule":         "not-a-rule",
		"  name.casing  ":    "name[casing]",
		"role.name":          "role-name",
		"task.key-order":     "key-order",
		"name[casing]":       "name[casing]",
		"galaxy.tags-format": "galaxy[tags-format]",
		"meta.tags-format":   "meta-no-tags",
	}
	for in, want := range tests {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

// splitTag separates a tag into its rule id and its subtag, if any.
func splitTag(tag string) (base, sub string) {
	i := strings.IndexByte(tag, '[')
	if i < 0 {
		return tag, ""
	}
	return tag[:i], tag[i:]
}

func ruleIDSet() map[string]bool {
	out := make(map[string]bool, len(IDs))
	for _, id := range IDs {
		out[id] = true
	}
	return out
}

// tagLiteralsInSources returns every string literal in the package's own
// non-test sources that is shaped like a tag of a registered rule.
func tagLiteralsInSources(t *testing.T) []string {
	t.Helper()
	known := ruleIDSet()
	seen := map[string]bool{}
	var out []string
	for _, name := range ruleSourceFiles(t) {
		for _, tag := range tagLiteralsInFile(t, name) {
			base, _ := splitTag(tag)
			if !known[base] || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// ruleSourceFiles lists the package sources to scan. ids.go is left out so the
// equivalence table cannot vouch for itself.
func ruleSourceFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading rule sources: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && name != "ids.go" && !strings.HasSuffix(name, "_test.go") {
			out = append(out, name)
		}
	}
	return out
}

func tagLiteralsInFile(t *testing.T, name string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil && reTagLiteral.MatchString(v) {
			out = append(out, v)
		}
		return true
	})
	return out
}
