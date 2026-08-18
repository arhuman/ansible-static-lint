package yamllint

import (
	"fmt"
	"maps"
	"sort"
)

// Config is an effective yamllint configuration: one entry per rule id, each
// either disabled or carrying a complete option set. It is the Go expression
// of the same policy yamllint's bundled configurations encode, not a copy of
// their text (ADR 0005).
type Config struct {
	rules map[string]*ruleConf
	// ignore is the top-level pattern list. A file it matches produces no
	// yaml[*] findings at all, while every other rule still lints it: yamllint
	// `ignore` is not ansible-lint `exclude_paths`.
	ignore *ignoreSpec
}

type ruleConf struct {
	enabled bool
	opts    map[string]any
	// ignore is the rule's own pattern list, which excludes files from this
	// rule alone and is independent of the top-level one.
	ignore *ignoreSpec
}

// ruleDefaults is every yamllint rule's option set with its stock values,
// taken from the pinned yamllint 1.38.0 (each rule module's DEFAULT). The key
// set doubles as the allowed-option list a config file is validated against.
// Rules astl does not implement are listed too: a config may legitimately
// configure them, and validation must not reject it.
var ruleDefaults = map[string]map[string]any{
	"anchors": {
		"forbid-duplicated-anchors": false,
		"forbid-undeclared-aliases": true,
		"forbid-unused-anchors":     false,
	},
	"braces": {
		"forbid":                  false,
		"max-spaces-inside":       0,
		"max-spaces-inside-empty": -1,
		"min-spaces-inside":       0,
		"min-spaces-inside-empty": -1,
	},
	"brackets": {
		"forbid":                  false,
		"max-spaces-inside":       0,
		"max-spaces-inside-empty": -1,
		"min-spaces-inside":       0,
		"min-spaces-inside-empty": -1,
	},
	"colons": {"max-spaces-after": 1, "max-spaces-before": 0},
	"commas": {"max-spaces-after": 1, "max-spaces-before": 0, "min-spaces-after": 1},
	"comments": {
		"ignore-shebangs":         true,
		"min-spaces-from-content": 2,
		"require-starting-space":  true,
	},
	"comments-indentation": {},
	"document-end":         {"present": true},
	"document-start":       {"present": true},
	"empty-lines":          {"max": 2, "max-end": 0, "max-start": 0},
	"empty-values": {
		"forbid-in-block-mappings":  true,
		"forbid-in-block-sequences": true,
		"forbid-in-flow-mappings":   true,
	},
	"float-values": {
		"forbid-inf":                     false,
		"forbid-nan":                     false,
		"forbid-scientific-notation":     false,
		"require-numeral-before-decimal": false,
	},
	"hyphens": {"max-spaces-after": 1},
	"indentation": {
		"check-multi-line-strings": false,
		"indent-sequences":         true,
		"spaces":                   "consistent",
	},
	"key-duplicates": {"forbid-duplicated-merge-keys": false},
	"key-ordering":   {"ignored-keys": []string{}},
	"line-length": {
		"allow-non-breakable-inline-mappings": false,
		"allow-non-breakable-words":           true,
		"max":                                 80,
	},
	"new-line-at-end-of-file": {},
	"new-lines":               {"type": "unix"},
	"octal-values":            {"forbid-explicit-octal": true, "forbid-implicit-octal": true},
	"quoted-strings": {
		"allow-quoted-quotes": false,
		"check-keys":          false,
		"extra-allowed":       []string{},
		"extra-required":      []string{},
		"quote-type":          "any",
		"required":            true,
	},
	"trailing-spaces": {},
	"truthy":          {"allowed-values": []string{"true", "false"}, "check-keys": true},
}

// implemented lists the rules astl can actually run. new-lines counts as
// implemented and inert: it cannot fire on the universal-newline text both
// tools lint, so a config enabling it loses nothing.
var implemented = map[string]bool{
	"anchors": true, "braces": true, "brackets": true, "colons": true,
	"commas": true, "comments": true, "comments-indentation": true,
	"document-start": true, "empty-lines": true, "hyphens": true,
	"indentation": true, "key-duplicates": true, "line-length": true,
	"new-line-at-end-of-file": true, "new-lines": true, "octal-values": true,
	"trailing-spaces": true, "truthy": true,
}

// disabledInYamllintDefault are the rules yamllint's own `default`
// configuration ships switched off.
var disabledInYamllintDefault = []string{
	"document-end", "empty-values", "float-values", "key-ordering",
	"octal-values", "quoted-strings",
}

// newConfig builds a config where every rule carries its stock options and is
// enabled, which is the starting point yamllint's `default` refines.
func newConfig() *Config {
	c := &Config{rules: make(map[string]*ruleConf, len(ruleDefaults))}
	for id, defaults := range ruleDefaults {
		c.rules[id] = &ruleConf{enabled: true, opts: maps.Clone(defaults)}
	}
	return c
}

// yamllintDefault is yamllint's bundled `default` configuration.
func yamllintDefault() *Config {
	c := newConfig()
	for _, id := range disabledInYamllintDefault {
		c.rules[id].enabled = false
	}
	return c
}

// yamllintRelaxed is yamllint's bundled `relaxed` configuration, which
// extends `default`. Only the option changes matter here: the level changes
// it also makes do not affect which findings are reported.
func yamllintRelaxed() *Config {
	c := yamllintDefault()
	c.rules["braces"].opts["max-spaces-inside"] = 1
	c.rules["brackets"].opts["max-spaces-inside"] = 1
	c.rules["comments"].enabled = false
	c.rules["comments-indentation"].enabled = false
	c.rules["document-start"].enabled = false
	c.rules["indentation"].opts["indent-sequences"] = "consistent"
	c.rules["line-length"].opts["allow-non-breakable-inline-mappings"] = true
	c.rules["truthy"].enabled = false
	return c
}

// AnsibleLintDefaults is the configuration ansible-lint applies when a
// repository ships none of its own: yamllint's `default` with the overrides
// from ansible-lint's bundled .yamllint.
func AnsibleLintDefaults() *Config {
	c := yamllintDefault()
	c.rules["comments"].opts["min-spaces-from-content"] = 1
	c.rules["comments-indentation"].enabled = false
	c.rules["document-start"].enabled = false
	c.rules["line-length"].opts["max"] = 160
	c.rules["braces"].opts["min-spaces-inside"] = 0
	c.rules["braces"].opts["max-spaces-inside"] = 1
	c.rules["octal-values"].enabled = true
	c.rules["octal-values"].opts["forbid-implicit-octal"] = true
	c.rules["octal-values"].opts["forbid-explicit-octal"] = true
	return c
}

// extend layers c over base and returns the result, reproducing yamllint's
// YamlLintConfig.extend: a rule configured as a mapping on both sides merges
// option by option with c winning, anything else replaces base's entry
// outright. Rules base knows and c does not are carried over untouched, which
// is why a repository config saying `extends: default` silently reinstates
// yamllint's stock answer for every rule it does not mention.
func (c *Config) extend(base *Config) *Config {
	for id, conf := range c.rules {
		baseConf, known := base.rules[id]
		if conf.enabled && known && baseConf.enabled {
			maps.Copy(baseConf.opts, conf.opts)
			if conf.ignore != nil {
				baseConf.ignore = conf.ignore
			}
			continue
		}
		base.rules[id] = conf
	}
	// The top-level ignore does not merge and does not follow the usual
	// direction: yamllint's extend keeps the *extended* file's list whenever it
	// has one, so a parent's ignore wins over the child's rather than the other
	// way round. It only reaches the child when the parent set none, which is
	// the ordinary case since neither `default` nor `relaxed` ships one.
	if base.ignore == nil {
		base.ignore = c.ignore
	}
	return base
}

// fillDefaults completes every enabled rule's option set from the stock
// values, which is what yamllint's validate() does to a freshly parsed
// configuration. Without it a rule the file merely switches on, or replaces
// outright, would reach the checks with no options at all.
func (c *Config) fillDefaults() *Config {
	for id, conf := range c.rules {
		if !conf.enabled {
			continue
		}
		for key, def := range ruleDefaults[id] {
			if _, set := conf.opts[key]; !set {
				conf.opts[key] = def
			}
		}
	}
	return c
}

// Enabled reports whether a rule runs under this configuration.
func (c *Config) Enabled(id string) bool {
	conf, ok := c.rules[id]
	return ok && conf.enabled
}

// ForFile returns the configuration that applies to path, or **nil** when the
// top-level `ignore` patterns exclude the file. A nil result means "report
// nothing for the yaml family here", reproducing yamllint's `run()`, which
// returns an empty generator for an ignored path before linting anything.
//
// Callers must pass the path as it will be printed, relative to the directory
// the run started in, because that is what ansible-lint hands to yamllint and
// therefore what the patterns are written against.
//
// When a rule carries its own `ignore` that matches, the returned config has
// that one rule switched off and the rest untouched.
func (c *Config) ForFile(path string) *Config {
	if c.ignore.match(path) {
		return nil
	}
	var scoped *Config
	for id, conf := range c.rules {
		if !conf.enabled || !conf.ignore.match(path) {
			continue
		}
		if scoped == nil {
			scoped = &Config{rules: maps.Clone(c.rules), ignore: c.ignore}
		}
		// Clone the entry before disabling it: the map is shallow-copied, so
		// the pointers are still the ones every other file is linted through.
		off := *conf
		off.enabled = false
		scoped.rules[id] = &off
	}
	if scoped == nil {
		return c
	}
	return scoped
}

// EnabledUnimplemented lists the rules this configuration switches on that
// astl cannot run, sorted. Callers surface it: silently reporting nothing for
// a rule the operator asked for would read as a clean file.
func (c *Config) EnabledUnimplemented() []string {
	var out []string
	for id, conf := range c.rules {
		if conf.enabled && !implemented[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// opt returns a typed option. A rule only ever asks for options its own
// defaults declare, so a miss, or a type that disagrees with the declared
// default, means the tables and the rule have drifted apart.
func opt[T any](c *Config, id, key string) T {
	v, ok := c.rules[id].opts[key]
	if !ok {
		panic(fmt.Sprintf("yamllint: rule %q has no option %q", id, key))
	}
	typed, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("yamllint: option %q of %q is %T, want %T", key, id, v, typed))
	}
	return typed
}

func (c *Config) optInt(id, key string) int { return opt[int](c, id, key) }

func (c *Config) optBool(id, key string) bool { return opt[bool](c, id, key) }

func (c *Config) optStrings(id, key string) []string { return opt[[]string](c, id, key) }

// optAny returns an option that yamllint allows to hold more than one type,
// such as indentation's `spaces` (an int or "consistent").
func (c *Config) optAny(id, key string) any {
	v, ok := c.rules[id].opts[key]
	if !ok {
		panic(fmt.Sprintf("yamllint: rule %q has no option %q", id, key))
	}
	return v
}
