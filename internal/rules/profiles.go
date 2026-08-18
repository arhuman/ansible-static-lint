package rules

import "strings"

// Profiles are ansible-lint's named rule sets. Setting `profile:` in
// `.ansible-lint` does not tune a rule, it selects which rules run at all:
// upstream enables the profile's rules and disables everything else, so a rule
// belonging to no profile (astl's `complexity` and `run-once`) is unreachable
// whenever any profile is set.
//
// Upstream defines them in `data/profiles.yml` as a chain of `extends`, from
// `min` up to `production`. profileRules stores each chain already flattened,
// and only the rule ids astl can emit: an upstream rule astl does not
// implement cannot change what astl prints, and listing it would invite the
// table to be read as a rule inventory. The counts in each comment are the
// full upstream figures, so a chain that grows upstream is visible as a
// mismatch rather than as a silently short list.
//
// The compatibility corpus checks this table against upstream's own file; see
// astl-compatibility-check. Nothing here is derived from upstream's code, only
// from which rule names it groups together.
var profileRules = map[string][]string{
	// min: 4 upstream rules, 0 implemented by astl. Its rules are
	// internal-error, load-failure, parser-error and syntax-check, none of
	// which astl reports, so this profile silences astl entirely.
	"min": {},
	// basic: 23 upstream rules, 15 implemented by astl.
	"basic": {
		"command-instead-of-module", "command-instead-of-shell",
		"deprecated-bare-vars", "deprecated-local-action", "inline-env-var",
		"key-order", "literal-compare", "name", "no-jinja-when", "no-tabs",
		"partial-become", "playbook-extension", "role-name", "var-naming",
		"yaml",
	},
	// moderate: 24 upstream rules, 15 implemented by astl. The rule it adds
	// over basic (name[casing] and friends) is a subtag of one astl already
	// has, so the flattened set is unchanged.
	"moderate": {
		"command-instead-of-module", "command-instead-of-shell",
		"deprecated-bare-vars", "deprecated-local-action", "inline-env-var",
		"key-order", "literal-compare", "name", "no-jinja-when", "no-tabs",
		"partial-become", "playbook-extension", "role-name", "var-naming",
		"yaml",
	},
	// safety: 30 upstream rules, 20 implemented by astl.
	"safety": {
		"avoid-implicit", "command-instead-of-module",
		"command-instead-of-shell", "deprecated-bare-vars",
		"deprecated-local-action", "inline-env-var", "key-order",
		"literal-compare", "name", "no-jinja-when", "no-tabs", "package-latest",
		"partial-become", "playbook-extension", "risky-file-permissions",
		"risky-octal", "risky-shell-pipe", "role-name", "var-naming", "yaml",
	},
	// shared: 45 upstream rules, 29 implemented by astl.
	"shared": {
		"avoid-implicit", "command-instead-of-module",
		"command-instead-of-shell", "deprecated-bare-vars",
		"deprecated-local-action", "galaxy", "ignore-errors", "inline-env-var",
		"key-order", "literal-compare", "meta-incorrect", "meta-no-tags",
		"meta-runtime", "meta-video-links", "name", "no-changed-when",
		"no-handler", "no-jinja-when", "no-relative-paths", "no-tabs",
		"package-latest", "partial-become", "playbook-extension",
		"risky-file-permissions", "risky-octal", "risky-shell-pipe",
		"role-name", "var-naming", "yaml",
	},
	// production: 52 upstream rules, 30 implemented by astl. The strictest
	// profile, and still not a superset of astl: complexity and run-once
	// belong to no profile at all.
	"production": {
		"avoid-implicit", "command-instead-of-module",
		"command-instead-of-shell", "deprecated-bare-vars",
		"deprecated-local-action", "galaxy", "ignore-errors", "inline-env-var",
		"key-order", "literal-compare", "meta-incorrect", "meta-no-tags",
		"meta-runtime", "meta-video-links", "name", "no-changed-when",
		"no-handler", "no-jinja-when", "no-relative-paths", "no-tabs",
		"package-latest", "partial-become", "playbook-extension",
		"risky-file-permissions", "risky-octal", "risky-shell-pipe",
		"role-name", "sanity", "var-naming", "yaml",
	},
}

// profileSets is profileRules indexed for lookup, built once at init so the
// literal above stays the single place a chain is written down.
var profileSets = func() map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(profileRules))
	for name, ids := range profileRules {
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		out[name] = set
	}
	return out
}()

// KnownProfile reports whether name is one of ansible-lint's profiles. Callers
// warn on an unknown name rather than failing: upstream may add one, and a
// config astl cannot fully honour is still a config it should lint under.
func KnownProfile(name string) bool {
	_, ok := profileSets[strings.TrimSpace(name)]
	return ok
}

// ProfileNames lists the profiles in increasing strictness, for diagnostics.
func ProfileNames() []string {
	return []string{"min", "basic", "moderate", "safety", "shared", "production"}
}

// inProfile reports whether ruleID runs under the named profile. An empty or
// unrecognised profile selects nothing, which callers read as "no profile
// restriction" so that an upstream addition cannot silently mute astl.
func inProfile(profile, ruleID string) bool {
	set, ok := profileSets[strings.TrimSpace(profile)]
	if !ok {
		return true
	}
	return set[ruleID]
}
