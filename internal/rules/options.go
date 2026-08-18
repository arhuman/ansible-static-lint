package rules

import (
	"strings"
	"sync"

	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// Default rule option values, taken from ansible-lint's own defaults.
const (
	defaultMaxTasks      = 100
	defaultMaxBlockDepth = 20
)

// LoopVarPrefixDefault is the pattern ansible-lint documents for
// `loop_var_prefix`. It is not applied unless the option is set, because
// upstream leaves the option unset by default and the rule is inert then.
const LoopVarPrefixDefault = `^(__|{role}_)`

// Options carries the rule settings a user can tune. The zero value means
// "upstream defaults", so callers that tune nothing pass Options{}.
type Options struct {
	// EnableList holds rule ids or tags, in either taxonomy, that switch on
	// rules ansible-lint ships as opt-in. Selection carries it too, because it
	// also overrides the profile; see Select.
	EnableList []string
	// LoopVarPrefix is the regexp a role's `loop_var` must match. Empty leaves
	// the loop-var-prefix rule inert, as upstream does.
	LoopVarPrefix string
	// MaxTasks caps the tasks in a play or task file. Zero means the default.
	MaxTasks int
	// MaxBlockDepth caps how deeply blocks may nest. Zero means the default.
	MaxBlockDepth int
	// VarNamingPattern replaces var-naming's default pattern. It must be a
	// valid regexp; config.Load validates it. Empty means the default.
	VarNamingPattern string
	// Yamllint is the configuration the yaml[*] family runs under, resolved
	// once per run from the repository's own .yamllint when it has one. A nil
	// value means ansible-lint's bundled policy.
	Yamllint *yamllint.Config
}

// bundledYamllint is the shared read-only fallback configuration. It must
// never be mutated: Load builds its own instance for anything it extends.
var bundledYamllint = sync.OnceValue(yamllint.AnsibleLintDefaults)

// yamllintConfig returns the configuration to lint with, defaulting to
// ansible-lint's bundled policy so that Options{} stays usable.
func (o Options) yamllintConfig() *yamllint.Config {
	if o.Yamllint == nil {
		return bundledYamllint()
	}
	return o.Yamllint
}

func (o Options) maxTasks() int {
	if o.MaxTasks <= 0 {
		return defaultMaxTasks
	}
	return o.MaxTasks
}

func (o Options) maxBlockDepth() int {
	if o.MaxBlockDepth <= 0 {
		return defaultMaxBlockDepth
	}
	return o.MaxBlockDepth
}

// optIn lists the rules ansible-lint registers only when the user names them,
// because they encode a policy rather than a defect. astl keeps them off by
// default for the same reason, and so that its output stays comparable.
var optIn = map[string]bool{
	"no-log-password":          true,
	"no-prompting":             true,
	"empty-string-compare":     true,
	"jinja-template-extension": true,
	"galaxy-version-incorrect": true,
}

// enabled reports whether an opt-in rule should run. Every rule outside the
// opt-in set always runs; an opt-in rule runs only when EnableList names it,
// in either taxonomy.
//
// Profile is deliberately not consulted here. Only opt-in rules call this, so
// a profile check would cover six rules and silently miss the other thirty
// two. Selecting on the profile happens once, over the findings, in Select.
func (o Options) enabled(ruleID string) bool {
	if !optIn[ruleID] {
		return true
	}
	for _, want := range o.EnableList {
		if Canonical(strings.TrimSpace(want)) == ruleID {
			return true
		}
	}
	return false
}
