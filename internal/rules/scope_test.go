package rules

import (
	"os"
	"regexp"
	"testing"
)

// reRuleTableRow matches one row of docs/rules.md: native id, upstream id,
// meaning. The header and separator rows carry no backticks and do not match.
var reRuleTableRow = regexp.MustCompile("(?m)^\\|\\s*`([^`]+)`\\s*\\|\\s*`([^`]+)`\\s*\\|\\s*([^|]+?)\\s*\\|$")

// TestDescriptionsMatchTheRuleTable holds the descriptions the SARIF output
// publishes to the ones docs/rules.md publishes. They are one text with two
// readers, and nothing else would notice them diverging: the doc is prose no
// test reads, and the table feeds a field no parity gate compares.
func TestDescriptionsMatchTheRuleTable(t *testing.T) {
	md, err := os.ReadFile("../../docs/rules.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := make(map[string]string)
	for _, row := range reRuleTableRow.FindAllStringSubmatch(string(md), -1) {
		documented[row[2]] = row[3]
	}
	if len(documented) != len(equivalence) {
		t.Errorf("docs/rules.md has %d rows, the table has %d", len(documented), len(equivalence))
	}
	for _, p := range equivalence {
		switch doc, ok := documented[p.upstream]; {
		case !ok:
			t.Errorf("%q is in the table and not in docs/rules.md", p.upstream)
		case doc != p.desc:
			t.Errorf("%q: table says %q, docs/rules.md says %q", p.upstream, p.desc, doc)
		}
		if p.desc == "" {
			t.Errorf("%q has no description", p.upstream)
		}
	}
}

// TestOutOfScopeDisjointFromIDs is the half of the out-of-scope list that can
// be checked without an Ansible runtime: a rule cannot be both implemented and
// declared unimplemented. The other half, that the list still matches
// upstream's inventory, is a manual step when the pinned ansible-lint moves.
func TestOutOfScopeDisjointFromIDs(t *testing.T) {
	implemented := make(map[string]bool, len(IDs))
	for _, id := range IDs {
		implemented[id] = true
	}
	seen := make(map[string]bool, len(OutOfScope))
	for _, r := range OutOfScope {
		if implemented[r.ID] {
			t.Errorf("%q is in IDs and in OutOfScope", r.ID)
		}
		if seen[r.ID] {
			t.Errorf("%q is listed twice in OutOfScope", r.ID)
		}
		seen[r.ID] = true
		if r.Requires == "" {
			t.Errorf("%q says nothing about what it requires", r.ID)
		}
		if r.ID != BaseRule(r.ID) {
			t.Errorf("%q is a subtag; OutOfScope names whole rules", r.ID)
		}
	}
}

// TestOutOfScopeCoversUpstreamInventory pins the count against ansible-lint
// 26.8.0, whose `--list-rules` reports 53. A newer upstream adding a rule
// makes this fail rather than let the SARIF scope block quietly under-report.
func TestOutOfScopeCoversUpstreamInventory(t *testing.T) {
	const upstreamRules = 53
	if got := len(IDs) + len(OutOfScope); got != upstreamRules {
		t.Errorf("IDs (%d) + OutOfScope (%d) = %d, want %d", len(IDs), len(OutOfScope), got, upstreamRules)
	}
}

// TestDescriptorsCoverEveryImplementedRule asserts the descriptor list is a
// description of this build: every implemented rule contributes its bare id,
// and nothing describes a rule astl cannot report.
func TestDescriptorsCoverEveryImplementedRule(t *testing.T) {
	implemented := make(map[string]bool, len(IDs))
	for _, id := range IDs {
		implemented[id] = true
	}
	bare := make(map[string]bool, len(IDs))
	for _, d := range Descriptors(IDUpstream) {
		if !implemented[d.Base] {
			t.Errorf("descriptor %q belongs to unimplemented rule %q", d.ID, d.Base)
		}
		if d.Upstream == d.Base {
			bare[d.Base] = true
		}
	}
	for _, id := range IDs {
		if !bare[id] {
			t.Errorf("rule %q has no bare descriptor, so the scope block would omit it", id)
		}
	}
}

// TestDescriptorsRenderTheRequestedTaxonomy keeps the id a consumer matches on
// aligned with the one findings carry under the same flag.
func TestDescriptorsRenderTheRequestedTaxonomy(t *testing.T) {
	for _, style := range []IDStyle{IDUpstream, IDNative} {
		for _, d := range Descriptors(style) {
			if want := TagFor(d.Upstream, style); d.ID != want {
				t.Errorf("descriptor id %q under %s, want %q", d.ID, style, want)
			}
			if d.Upstream == "" || d.Native == "" {
				t.Errorf("descriptor %q is missing a taxonomy", d.ID)
			}
		}
	}
}

func TestBaseRule(t *testing.T) {
	tests := map[string]string{
		"name[play]":   "name",
		"name":         "name",
		"yaml[truthy]": "yaml",
		"":             "",
	}
	for in, want := range tests {
		if got := BaseRule(in); got != want {
			t.Errorf("BaseRule(%q) = %q, want %q", in, got, want)
		}
	}
}
