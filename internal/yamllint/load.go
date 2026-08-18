package yamllint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

// loadError is a failure to read or parse one configuration file, carrying two
// renderings of it: cause has the detail, kind names the category in words that
// contain nothing read out of the file. Which one a caller may show depends on
// who chose the path, so the two cannot be collapsed. See safeExtendsError.
type loadError struct {
	kind  string
	cause error
}

func (e *loadError) Error() string { return e.cause.Error() }
func (e *loadError) Unwrap() error { return e.cause }

// safeExtendsError reduces a failure to its content-free category when it comes
// from a file named by `extends`.
//
// The path in an `extends` is chosen by the repository being linted, which astl
// does not control, and the message ends up in a CI log. Reporting the detail
// would echo back whatever was read at that path: yaml.v3 quotes a fragment of
// the offending scalar, and an unrecognised rule is reported by name. Either
// turns the configuration loader into a file-disclosure channel, so only the
// category survives the boundary.
//
// Errors carrying no file content pass through unchanged: a cycle, the depth
// bound, and a nested `extends` failure already reduced by this function on its
// way up, so a chain keeps naming every hop that led to the failure.
func safeExtendsError(err error) error {
	var le *loadError
	if errors.As(err, &le) {
		return errors.New(le.kind)
	}
	return err
}

// configNames are the repository-local file names ansible-lint looks for, in
// order; the first that exists wins.
var configNames = []string{".yamllint", ".yamllint.yaml", ".yamllint.yml"}

// maxExtendsDepth bounds how long an `extends` chain may be. yamllint imposes
// no limit, but astl reads configuration out of repositories the operator does
// not control, so the chain has to terminate on adversarial input as well as on
// honest input. Real chains are one or two links; 32 is far above anything a
// human writes and far below anything that costs time to reject.
const maxExtendsDepth = 32

// canonicalPath returns a stable identity for a configuration file so that a
// cycle is caught however each hop spells the path. Symlinks are resolved
// because two names for one file must compare equal; when that fails the file
// does not exist, the read in parseFile reports it, and the cleaned absolute
// path is a good enough key until then.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// Load resolves the yamllint configuration that applies to dir, the way
// ansible-lint does: its own bundled defaults, with a repository config
// layered over them when one is found. Warnings name limits the operator
// should know about, each already carrying the config path.
//
// The search order is ansible-lint's: .yamllint, .yamllint.yaml and
// .yamllint.yml under dir, then $YAMLLINT_CONFIG_FILE, then
// ${XDG_CONFIG_HOME:-~/.config}/yamllint/config.
func Load(dir string) (cfg *Config, warnings []string, err error) {
	path := findConfig(dir)
	if path == "" {
		return AnsibleLintDefaults(), nil, nil
	}
	repo, warnings, err := parseFile(path, dir, map[string]struct{}{}, 0)
	if err != nil {
		return nil, nil, err
	}
	cfg = repo.extend(AnsibleLintDefaults())
	for _, id := range cfg.EnabledUnimplemented() {
		warnings = append(warnings, fmt.Sprintf(
			"%s enables yamllint rule %q, which astl does not implement: its findings will be missing",
			path, id))
	}
	return cfg, warnings, nil
}

func findConfig(dir string) string {
	for _, name := range configNames {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if env := os.Getenv("YAMLLINT_CONFIG_FILE"); env != "" {
		if fi, err := os.Stat(env); err == nil && !fi.IsDir() {
			return env
		}
	}
	home := os.Getenv("XDG_CONFIG_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".config")
	}
	p := filepath.Join(home, "yamllint", "config")
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// rawConfig is a yamllint configuration file as written.
type rawConfig struct {
	Extends        string               `yaml:"extends"`
	Rules          map[string]yaml.Node `yaml:"rules"`
	Ignore         yaml.Node            `yaml:"ignore"`
	IgnoreFromFile yaml.Node            `yaml:"ignore-from-file"`
}

// parseFile reads one configuration file and resolves its `extends` chain.
// base is the directory a relative `extends` path is resolved against. seen
// carries the canonical paths already visited on this chain and depth how many
// links deep it is, so that a chain which loops or never ends is rejected
// rather than followed; callers starting a fresh chain pass an empty set and 0.
func parseFile(path, base string, seen map[string]struct{}, depth int) (*Config, []string, error) {
	if depth > maxExtendsDepth {
		return nil, nil, fmt.Errorf("extends chain is longer than %d links", maxExtendsDepth)
	}
	key := canonicalPath(path)
	if _, ok := seen[key]; ok {
		return nil, nil, fmt.Errorf("extends cycle: %s already appears in this chain", path)
	}
	seen[key] = struct{}{}

	data, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return nil, nil, &loadError{
			kind:  "could not be read",
			cause: fmt.Errorf("yamllint config: %w", err),
		}
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, &loadError{
			kind:  "is not valid YAML",
			cause: fmt.Errorf("yamllint config %s: %w", path, err),
		}
	}

	cfg := &Config{rules: map[string]*ruleConf{}}
	var warnings []string
	for id, node := range raw.Rules {
		conf, err := parseRule(id, node)
		if err != nil {
			return nil, nil, &loadError{
				kind:  "is not a valid yamllint configuration",
				cause: fmt.Errorf("yamllint config %s: %w", path, err),
			}
		}
		cfg.rules[id] = conf
	}
	if !raw.Ignore.IsZero() && !raw.IgnoreFromFile.IsZero() {
		return nil, nil, &loadError{
			kind:  "is not a valid yamllint configuration",
			cause: fmt.Errorf("yamllint config %s: ignore and ignore-from-file cannot both be set", path),
		}
	}
	switch {
	case !raw.Ignore.IsZero():
		lines, err := ignoreLines(raw.Ignore)
		if err != nil {
			return nil, nil, &loadError{
				kind:  "is not a valid yamllint configuration",
				cause: fmt.Errorf("yamllint config %s: ignore: %w", path, err),
			}
		}
		cfg.ignore = parseIgnore(lines)
	case !raw.IgnoreFromFile.IsZero():
		lines, warn, err := ignoreFromFiles(raw.IgnoreFromFile, filepath.Dir(path))
		if err != nil {
			return nil, nil, &loadError{
				kind:  "is not a valid yamllint configuration",
				cause: fmt.Errorf("yamllint config %s: ignore-from-file: %w", path, err),
			}
		}
		warnings = append(warnings, warn...)
		cfg.ignore = parseIgnore(lines)
	}

	if raw.Extends != "" {
		parent, parentWarnings, err := parseExtends(raw.Extends, base, seen, depth+1)
		if err != nil {
			return nil, nil, fmt.Errorf("yamllint config %s: %w", path, err)
		}
		warnings = append(warnings, parentWarnings...)
		cfg = cfg.extend(parent)
	}
	return cfg.fillDefaults(), warnings, nil
}

// parseExtends resolves an `extends` value: one of yamllint's bundled
// configuration names, or a path to another file. seen and depth are the chain
// state described on parseFile, which this hop extends rather than restarts.
func parseExtends(name, base string, seen map[string]struct{}, depth int) (*Config, []string, error) {
	if !strings.ContainsAny(name, "/\\") {
		switch name {
		case "default":
			return yamllintDefault(), nil, nil
		case "relaxed":
			return yamllintRelaxed(), nil, nil
		}
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	cfg, warnings, err := parseFile(path, base, seen, depth)
	if err != nil {
		return nil, nil, fmt.Errorf("extends %q: %w", name, safeExtendsError(err))
	}
	// A file reached through `extends` is a complete configuration in its own
	// right, so its unset options fall back to yamllint's stock values rather
	// than to whatever layer it is later applied over.
	return cfg.extend(newConfig()), warnings, nil
}

// ignoreLines reads an `ignore` value, which yamllint accepts either as one
// block string of newline-separated patterns or as a list of them.
func ignoreLines(node yaml.Node) ([]string, error) {
	var block string
	if err := node.Decode(&block); err == nil {
		return strings.Split(block, "\n"), nil
	}
	var list []string
	if err := node.Decode(&list); err == nil {
		return list, nil
	}
	return nil, errors.New("should be a string or a list of strings")
}

// ignoreFromFiles reads patterns out of the files an `ignore-from-file` names,
// resolved against the configuration's own directory. A file that cannot be
// read is a warning rather than an error: the repository chose the path, the
// list is advisory, and refusing to lint over a missing ignore file would be a
// worse outcome than linting a few files yamllint would have skipped.
//
// The read goes through safeio so that a path pointing at something enormous
// cannot cost more than a configuration is allowed to. Nothing read here is
// ever printed, so unlike `extends` there is no disclosure channel to close.
func ignoreFromFiles(node yaml.Node, base string) (lines, warnings []string, err error) {
	var names []string
	var one string
	switch {
	case node.Decode(&one) == nil:
		names = []string{one}
	case node.Decode(&names) == nil:
	default:
		return nil, nil, errors.New("should be a string or a list of strings")
	}
	for _, name := range names {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		data, readErr := safeio.ReadFile(p, safeio.MaxConfigBytes)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf(
				"ignore-from-file names %s, which could not be read: its patterns are not applied", name))
			continue
		}
		lines = append(lines, strings.Split(string(data), "\n")...)
	}
	return lines, warnings, nil
}

// nonOptionKeys are per-rule keys that configure reporting rather than the
// check itself. `level` only sets a severity ansible-lint's pep8 output never
// prints, so it is validated and then ignored. `ignore` is handled separately
// in parseRule, since it does change which findings are reported.
var nonOptionKeys = []string{"level", "ignore", "ignore-from-file"}

// parseRule reads one entry of the `rules` mapping: the shorthand strings
// `enable` and `disable`, or a mapping of options.
func parseRule(id string, node yaml.Node) (*ruleConf, error) {
	defaults, known := ruleDefaults[id]
	if !known {
		return nil, fmt.Errorf("unknown rule %q", id)
	}
	if node.Kind == yaml.ScalarNode {
		return parseRuleShorthand(id, node)
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("rule %q: should be \"enable\", \"disable\" or a mapping", id)
	}

	conf := &ruleConf{enabled: true, opts: map[string]any{}}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if err := parseRuleKey(id, defaults, node.Content[i].Value, node.Content[i+1], conf); err != nil {
			return nil, err
		}
	}
	return conf, nil
}

// parseRuleShorthand reads the scalar forms of a rule entry.
func parseRuleShorthand(id string, node yaml.Node) (*ruleConf, error) {
	switch node.Value {
	case "enable":
		return &ruleConf{enabled: true, opts: map[string]any{}}, nil
	case "disable":
		return &ruleConf{enabled: false, opts: map[string]any{}}, nil
	}
	// A plain `false` switches a rule off too: yamllint only rewrites the two
	// keywords, and its validation then reads any remaining false as
	// "disabled". A `true` is not symmetric, it is an error there.
	var b bool
	if err := node.Decode(&b); err == nil && !b {
		return &ruleConf{enabled: false, opts: map[string]any{}}, nil
	}
	return nil, fmt.Errorf("rule %q: should be \"enable\", \"disable\" or a mapping", id)
}

// parseRuleKey reads one key of a rule's option mapping into conf. The three
// keys that are not options are handled here rather than filtered out ahead of
// time, because `level` still has to be validated and `ignore` still has to be
// compiled.
func parseRuleKey(id string, defaults map[string]any, key string, value *yaml.Node, conf *ruleConf) error {
	switch {
	case key == "level":
		if value.Value != "error" && value.Value != "warning" {
			return fmt.Errorf("rule %q: level should be \"error\" or \"warning\"", id)
		}
		return nil
	case key == "ignore":
		lines, err := ignoreLines(*value)
		if err != nil {
			return fmt.Errorf("rule %q: ignore: %w", id, err)
		}
		conf.ignore = parseIgnore(lines)
		return nil
	case slices.Contains(nonOptionKeys, key):
		return nil
	}
	def, ok := defaults[key]
	if !ok {
		return fmt.Errorf("unknown option %q for rule %q", key, id)
	}
	v, err := decodeOption(value, def)
	if err != nil {
		return fmt.Errorf("rule %q: option %q: %w", id, key, err)
	}
	conf.opts[key] = v
	return nil
}

// decodeOption reads an option value, taking the type it should have from the
// stock value of the same option. Options yamllint lets hold more than one
// type (indentation's `spaces`, braces' `forbid`) decode to whichever of int,
// bool or string the document carries.
func decodeOption(node *yaml.Node, def any) (any, error) {
	switch def.(type) {
	case []string:
		var v []string
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("should be a list of strings")
		}
		return v, nil
	case int:
		var v int
		if err := node.Decode(&v); err != nil {
			return nil, fmt.Errorf("should be an integer")
		}
		return v, nil
	case bool:
		var b bool
		if err := node.Decode(&b); err == nil {
			return b, nil
		}
		// `forbid` accepts the string "non-empty" alongside a boolean.
		return node.Value, nil
	default:
		var i int
		if err := node.Decode(&i); err == nil {
			return i, nil
		}
		var b bool
		if err := node.Decode(&b); err == nil {
			return b, nil
		}
		return node.Value, nil
	}
}

// Describe renders the effective configuration as sorted `rule: disabled` or
// `rule: opt=value` lines. It exists so the loader can be compared against
// ansible-lint's own resolution in tests.
func (c *Config) Describe() string {
	ids := make([]string, 0, len(c.rules))
	for id := range c.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		conf := c.rules[id]
		if !conf.enabled {
			fmt.Fprintf(&b, "%s: disabled\n", id)
			continue
		}
		keys := make([]string, 0, len(conf.opts))
		for k := range conf.opts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			v := conf.opts[k]
			if list, ok := v.([]string); ok {
				parts = append(parts, fmt.Sprintf("%s=[%s]", k, strings.Join(list, ",")))
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		fmt.Fprintf(&b, "%s: %s\n", id, strings.Join(parts, " "))
	}
	return b.String()
}
