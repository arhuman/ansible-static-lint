package rules

import (
	"slices"
	"testing"
)

// finding builds a minimal Finding carrying only what Select reads.
func finding(tag string) Finding {
	return Finding{Tag: tag, Path: "p.yml", Line: 1, Message: tag}
}

func tags(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Tag)
	}
	return out
}

// TestSelectProfileDropsRulesOutsideIt is issue 0005's regression test. The
// exact case is insippo/proxmox-infra: `profile: production` in .ansible-lint,
// two run_once post_tasks, upstream silent because run-once belongs to no
// profile, astl reporting both until this landed.
func TestSelectProfileDropsRulesOutsideIt(t *testing.T) {
	in := []Finding{finding("run-once[task]"), finding("no-handler"), finding("complexity[tasks]")}

	got := tags(Select(in, Selection{Profile: "production"}))
	want := []string{"no-handler"}
	if !slices.Equal(got, want) {
		t.Errorf("under profile production got %v, want %v: run-once and complexity are in no profile", got, want)
	}
}

func TestSelectWithoutProfileKeepsEveryRule(t *testing.T) {
	in := []Finding{finding("run-once[task]"), finding("no-handler")}

	got := tags(Select(in, Selection{}))
	if len(got) != 2 {
		t.Errorf("with no profile got %v, want both: an absent key must not restrict anything", got)
	}
}

// TestSelectUnknownProfileKeepsEveryRule pins the safe direction of the
// failure. Upstream can add a profile this table predates; muting astl on a
// name it merely does not recognise would turn a stale table into a silent
// pass, which is the one outcome a linter must not have.
func TestSelectUnknownProfileKeepsEveryRule(t *testing.T) {
	in := []Finding{finding("run-once[task]"), finding("no-handler")}

	got := tags(Select(in, Selection{Profile: "hardened"}))
	if len(got) != 2 {
		t.Errorf("under an unknown profile got %v, want every rule to keep running", got)
	}
}

func TestSelectProfileScalesWithStrictness(t *testing.T) {
	// risky-file-permissions enters at safety, no-handler at shared.
	in := []Finding{finding("risky-file-permissions"), finding("no-handler"), finding("yaml[trailing-spaces]")}

	cases := map[string][]string{
		"min":        nil,
		"basic":      {"yaml[trailing-spaces]"},
		"safety":     {"risky-file-permissions", "yaml[trailing-spaces]"},
		"shared":     {"risky-file-permissions", "no-handler", "yaml[trailing-spaces]"},
		"production": {"risky-file-permissions", "no-handler", "yaml[trailing-spaces]"},
	}
	for profile, want := range cases {
		got := tags(Select(slices.Clone(in), Selection{Profile: profile}))
		if len(got) != len(want) {
			t.Errorf("profile %s: got %v, want %v", profile, got, want)
			continue
		}
		for _, w := range want {
			if !slices.Contains(got, w) {
				t.Errorf("profile %s: got %v, missing %s", profile, got, w)
			}
		}
	}
}

// TestSelectEnableListOverridesProfile covers the escape hatch: upstream
// resolves the profile first and lets enable_list add back to it, so a
// repository can keep one rule its profile drops.
func TestSelectEnableListOverridesProfile(t *testing.T) {
	in := []Finding{finding("run-once[task]"), finding("no-handler")}

	got := tags(Select(in, Selection{Profile: "production", EnableList: []string{"run-once"}}))
	if !slices.Contains(got, "run-once[task]") {
		t.Errorf("got %v, want enable_list to add run-once back under a profile that drops it", got)
	}
}

// TestSelectSkipListBeatsEnableList pins the order the other way. skip_list
// runs after enable_list so a repository can enable a rule and still silence
// one of its subtags.
func TestSelectSkipListBeatsEnableList(t *testing.T) {
	in := []Finding{finding("run-once[task]"), finding("no-handler")}

	got := tags(Select(in, Selection{
		Profile:    "production",
		EnableList: []string{"run-once"},
		SkipList:   []string{"run-once[task]"},
	}))
	if slices.Contains(got, "run-once[task]") {
		t.Errorf("got %v, want skip_list to win over enable_list", got)
	}
}

// TestSelectWarnListDemotesRatherThanDrops is issue 0004's regression test.
// The distinction is the whole point of the key: a repository uses warn_list
// precisely to keep seeing a finding without failing on it, so dropping it
// would be as wrong as failing.
func TestSelectWarnListDemotesRatherThanDrops(t *testing.T) {
	out := Select([]Finding{finding("no-handler")}, Selection{WarnList: []string{"no-handler"}})

	if len(out) != 1 {
		t.Fatalf("got %d findings, want warn_list to demote rather than drop", len(out))
	}
	if !out[0].Warning {
		t.Error("no-handler is in warn_list but the finding is still error level")
	}
}

// TestSelectWarnListMatchesByRuleID covers the subtag case: upstream's
// warn_list resolves ids and tags the same way skip_list does, so a bare `yaml`
// demotes every yaml[*] subtag.
func TestSelectWarnListMatchesByRuleID(t *testing.T) {
	out := Select([]Finding{finding("yaml[trailing-spaces]")}, Selection{WarnList: []string{"yaml"}})

	if len(out) != 1 || !out[0].Warning {
		t.Errorf("got %+v, want a bare rule id in warn_list to cover its subtags", tags(out))
	}
}

func TestSelectSkippedRuleStaysSkippedWhenAlsoWarned(t *testing.T) {
	out := Select([]Finding{finding("no-handler")}, Selection{
		SkipList: []string{"no-handler"},
		WarnList: []string{"no-handler"},
	})

	if len(out) != 0 {
		t.Errorf("got %v, want skip_list to win: demoting a skipped rule must not resurrect it", tags(out))
	}
}

// TestSelectKeepsAnExistingWarning guards the default warn_list astl already
// emulates: upstream ships `experimental` in warn_list, which is what makes
// complexity a warning with no configuration at all. A user's warn_list adds
// to that rather than replacing it.
func TestSelectKeepsAnExistingWarning(t *testing.T) {
	f := finding("complexity[tasks]")
	f.Warning = true

	out := Select([]Finding{f}, Selection{})
	if len(out) != 1 || !out[0].Warning {
		t.Error("a finding built as a warning must stay one when no warn_list is configured")
	}
}

// TestProfileTableNamesOnlyKnownRules keeps the vendored table honest against
// astl's own rule set. A typo in a profile list would silently drop the rule it
// meant to name, which reads as a false negative rather than a broken table.
func TestProfileTableNamesOnlyKnownRules(t *testing.T) {
	known := make(map[string]bool, len(IDs))
	for _, id := range IDs {
		known[id] = true
	}
	for profile, ids := range profileRules {
		for _, id := range ids {
			if !known[id] {
				t.Errorf("profile %s names %q, which is not one of astl's rules", profile, id)
			}
		}
	}
}

// TestProfileChainsAreCumulative pins the shape upstream's `extends` gives
// them: each profile is a superset of the one below, so a stricter profile can
// never run fewer rules.
func TestProfileChainsAreCumulative(t *testing.T) {
	order := ProfileNames()
	for i := 1; i < len(order); i++ {
		lower, higher := profileSets[order[i-1]], profileSets[order[i]]
		for id := range lower {
			if !higher[id] {
				t.Errorf("%s runs %s but %s does not, breaking the extends chain",
					order[i-1], id, order[i])
			}
		}
	}
}
