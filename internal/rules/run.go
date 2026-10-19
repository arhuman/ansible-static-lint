package rules

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
)

// IDs lists every rule astl implements, in the order documented in the README.
var IDs = []string{
	"avoid-implicit", "command-instead-of-module", "command-instead-of-shell",
	"complexity", "deprecated-bare-vars", "deprecated-local-action",
	"empty-string-compare", "galaxy", "galaxy-version-incorrect", "ignore-errors",
	"inline-env-var", "jinja-template-extension", "key-order", "latest",
	"literal-compare",
	"loop-var-prefix", "meta-incorrect", "meta-no-tags", "meta-runtime",
	"meta-video-links", "name", "no-changed-when", "no-free-form", "no-handler",
	"no-jinja-when",
	"no-log-password", "no-prompting", "no-relative-paths", "no-tabs",
	"package-latest", "partial-become", "playbook-extension",
	"risky-file-permissions", "risky-octal", "risky-shell-pipe", "role-name",
	"run-once", "sanity", "var-naming", "yaml",
}

// File runs every rule applicable to one loaded file.
func File(f *parse.File, opt Options) []Finding {
	// A sanity ignore list is not YAML, so it is checked from the raw text
	// before the YAML preconditions below could reject it.
	if f.Kind == parse.KindSanityIgnoreFile {
		return applySkips(f, sanityRules(f))
	}
	// yaml[*] sees every well-formed file, as upstream's yamllint does, including
	// multi-document and unparsable-task files the rules below must still skip.
	var out []Finding
	if f.Err == nil || parse.IsMultiDocument(f.Err) {
		out = append(out, yamlRules(f, opt)...)
	}
	if f.Err != nil || f.Root == nil {
		return applySkips(f, out)
	}
	if hasUnparsableTask(f) {
		return applySkips(f, out)
	}
	out = append(out, taskRules(f, opt)...)
	out = append(out, varNamingRules(f, opt)...)
	out = append(out, playRules(f, opt)...)
	out = append(out, metaRules(f)...)
	out = append(out, roleNameMetaDeps(f)...)
	out = append(out, metaRuntimeRules(f)...)
	out = append(out, galaxyRules(f)...)
	out = append(out, lintableRules(f, opt)...)
	return applySkips(f, out)
}

// hasUnparsableTask reports whether any task would make ansible's
// ModuleArgsParser fail outright: two competing action keys, or an action key
// sitting alongside a block. Upstream aborts the whole file in that case, so
// astl reports nothing for it either.
func hasUnparsableTask(f *parse.File) bool {
	for _, t := range f.Tasks() {
		actionKeys := parse.CountActionKeys(t.Node)
		if t.IsBlock && actionKeys > 0 {
			return true
		}
		if !t.IsBlock && actionKeys > 1 {
			return true
		}
	}
	return false
}

// applySkips drops findings silenced by an inline `# noqa` comment or by a
// `skip_ansible_lint` tag.
func applySkips(f *parse.File, findings []Finding) []Finding {
	if len(f.Noqa) == 0 && !hasSkipTag(f) {
		return findings
	}
	fileSkips := map[string]bool{}
	if isMetadataKind(f.Kind) {
		fileSkips = canonicalSkips(f.AllSkips())
	}
	scopes := skipScopes{file: fileSkips, task: taskSkipRanges(f)}
	// Upstream's per-line filter: a same-line noqa silences that exact tag in
	// any file kind; it alone suppresses yaml[*] outside task and metadata scopes.
	scopes.line = make(map[int]map[string]bool, len(f.Noqa))
	for line, set := range f.Noqa {
		scopes.line[line] = canonicalSkips(set)
	}

	out := findings[:0]
	for _, fd := range findings {
		if !scopes.silence(fd) {
			out = append(out, fd)
		}
	}
	return out
}

// skipScopes are the skip sets a file's findings are filtered through: the
// whole file (metadata kinds only), the task covering a line, and the line
// itself.
type skipScopes struct {
	file map[string]bool
	task map[int]map[string]bool
	line map[int]map[string]bool
}

// silence reports whether any scope suppresses fd.
func (s skipScopes) silence(fd Finding) bool {
	if s.line[fd.Line][fd.Tag] {
		return true
	}
	// A line-scoped finding takes its rule's own same-line filter, which
	// also accepts the bare rule id, and nothing else.
	if fd.lineScoped {
		return skipped(s.line[fd.Line], fd)
	}
	// Task and metadata skip scopes never reach upstream's yamllint rule; only
	// the same-line tag-exact noqa above silences yaml[*].
	return fd.RuleID() != "yaml" && (skipped(s.file, fd) || skipped(s.task[fd.Line], fd))
}

// taskSkipRanges maps every line covered by a task to the skips collected
// anywhere inside that task, matching ansible-lint's per-task skip
// aggregation. Covering the whole range, not just the first line, is what
// silences rules that anchor their finding on a sub-node (a when, a
// loop_var) rather than on the task itself.
func taskSkipRanges(f *parse.File) map[int]map[string]bool {
	out := map[int]map[string]bool{}
	for _, t := range f.Tasks() {
		end := parse.EndLine(t.Node)
		// A block's span covers its child tasks, so collecting over it lets one
		// child's noqa suppress siblings; upstream skips each flattened task alone.
		collectTo := end
		if t.IsBlock {
			collectTo = parse.BlockHeaderEnd(t.Node)
		}
		set := canonicalSkips(f.SkipsInRange(t.Pos.Line, collectTo))
		if hasSkipAnsibleLint(t) {
			set["*"] = true
		}
		addSkipRange(out, set, t.Pos.Line, end)
	}
	for _, play := range f.Plays() {
		line := parse.NodePos(play).Line
		addSkipRange(out, canonicalSkips(f.SkipsInRange(line, line)), line, line)
	}
	return out
}

// addSkipRange merges set into every line of [from, to].
func addSkipRange(out map[int]map[string]bool, set map[string]bool, from, to int) {
	if len(set) == 0 {
		return
	}
	for line := from; line <= to; line++ {
		if out[line] == nil {
			out[line] = make(map[string]bool, len(set))
		}
		for k := range set {
			out[line][k] = true
		}
	}
}

// canonicalSkips normalizes a set of suppression tokens, which may be written
// in either taxonomy, to the upstream ids findings carry.
func canonicalSkips(set map[string]bool) map[string]bool {
	out := make(map[string]bool, len(set))
	for k := range set {
		out[Canonical(k)] = true
	}
	return out
}

func hasSkipAnsibleLint(t *parse.Task) bool {
	return skipTagIn(t.RawGet("tags"))
}

// skipTagIn reports whether a tags node names skip_ansible_lint.
func skipTagIn(tags *yaml.Node) bool {
	if tags == nil {
		return false
	}
	if parse.IsScalar(tags) {
		return tags.Value == "skip_ansible_lint"
	}
	for _, v := range parse.StrList(tags) {
		if v == "skip_ansible_lint" {
			return true
		}
	}
	return false
}

func hasSkipTag(f *parse.File) bool {
	for _, t := range f.Tasks() {
		if hasSkipAnsibleLint(t) {
			return true
		}
	}
	return false
}

func skipped(set map[string]bool, fd Finding) bool {
	if len(set) == 0 {
		return false
	}
	return set["*"] || set[fd.Tag] || set[fd.RuleID()]
}

func isMetadataKind(kind string) bool {
	switch kind {
	case parse.KindYAML, parse.KindRequirements, parse.KindVars, parse.KindMeta, parse.KindReno, parse.KindTestMeta, parse.KindGalaxy, parse.KindMetaRuntime:
		return true
	}
	return false
}

// Filter drops findings whose rule id or tag appears in skipList, written in
// either taxonomy.
func Filter(findings []Finding, skipList []string) []Finding {
	if len(skipList) == 0 {
		return findings
	}
	skip := make(map[string]bool, len(skipList))
	for _, s := range skipList {
		skip[Canonical(s)] = true
	}
	out := findings[:0]
	for _, f := range findings {
		if skip[f.Tag] || skip[f.RuleID()] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Selection is the `.ansible-lint` keys that decide which findings a run
// reports and at what level. Ids may be written in either taxonomy, and a bare
// rule id covers every subtag of that rule.
type Selection struct {
	// Profile names the ansible-lint profile whose rule set to run. Empty, or
	// a name upstream added after this table was written, means no restriction.
	Profile string
	// EnableList adds rules back that Profile drops, and switches on the rules
	// upstream ships as opt-in.
	EnableList []string
	// SkipList removes findings outright.
	SkipList []string
	// WarnList demotes findings to warning level instead of removing them:
	// they still print, with pep8's trailing ` (warning)`, but they do not
	// make the run fail.
	WarnList []string
}

// Select applies a Selection to findings, resolving the keys in ansible-lint's
// own order: the profile picks a rule set, enable_list adds back to it,
// skip_list subtracts, and warn_list demotes whatever survives.
//
// Order is not cosmetic. skip_list runs after enable_list so a repository can
// enable a rule and still silence one of its subtags, and warn_list runs last
// so demoting a rule that is also skipped cannot resurrect it.
//
// Findings already carrying Warning keep it: upstream's default warn_list holds
// the `experimental` tag, which is what makes complexity a warning with no
// configuration at all, and a user's warn_list adds to that rather than
// replacing it.
func Select(findings []Finding, sel Selection) []Finding {
	enable := canonicalSet(sel.EnableList)
	warn := canonicalSet(sel.WarnList)

	findings = Filter(findings, sel.SkipList)
	out := findings[:0]
	for _, f := range findings {
		if !selects(sel.Profile, enable, f) {
			continue
		}
		if warn["*"] || warn[f.Tag] || warn[f.RuleID()] {
			f.Warning = true
		}
		out = append(out, f)
	}
	return out
}

// EnabledRules lists the rules a run under sel can report, as bare upstream ids
// in IDs order. It answers a question the findings cannot: a rule that fired
// nothing and a rule that never ran are both absent from the results.
//
// A rule is enabled when skip_list does not name it, and either enable_list
// names it or the profile keeps it and it is not one of the opt-in rules.
// skip_list beating enable_list mirrors Select, where Filter runs first.
// WarnList is not consulted: it sets a finding's level, not whether it runs.
func EnabledRules(sel Selection) []string {
	skip := canonicalSet(sel.SkipList)
	enable := canonicalSet(sel.EnableList)

	out := make([]string, 0, len(IDs))
	for _, id := range IDs {
		if skip[id] {
			continue
		}
		if enable[id] || (inProfile(sel.Profile, id) && !optIn[id]) {
			out = append(out, id)
		}
	}
	return out
}

// selects reports whether the profile keeps f, with enable_list overriding it.
func selects(profile string, enable map[string]bool, f Finding) bool {
	if enable[f.Tag] || enable[f.RuleID()] {
		return true
	}
	return inProfile(profile, f.RuleID())
}

// canonicalSet indexes ids in either taxonomy under their upstream spelling.
func canonicalSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[Canonical(strings.TrimSpace(id))] = true
	}
	return set
}
