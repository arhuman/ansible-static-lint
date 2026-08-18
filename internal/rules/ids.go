package rules

import "strings"

// IDStyle selects the rule-identifier taxonomy used in output.
type IDStyle string

const (
	// IDUpstream is the ansible-lint taxonomy. It is the default, and the one
	// the pep8 compatibility contract is written against.
	IDUpstream IDStyle = "upstream"
	// IDNative is astl's own `domain.rule[tag]` taxonomy.
	IDNative IDStyle = "native"
)

// idPair is one row of the equivalence table.
type idPair struct{ upstream, native string }

// equivalence is the compatibility equivalence table. It is the single source
// of truth relating every ansible-lint rule identifier astl emits or accepts
// to its native counterpart, and both lookup directions are derived from it at
// init so they cannot drift.
//
// native ids read `domain.rule[tag]`, where the rule slug names the defect
// rather than the check that finds it. Upstream ids are permanent aliases:
// suppression surfaces accept either form, and a row is never removed once
// published. Rows carrying no subtag are the rule ids themselves, which
// suppression surfaces accept to silence every subtag of that rule at once;
// `name` and `galaxy` are their own native domain, so those two rows are
// identities.
var equivalence = []idPair{
	// name: how a play or task is called.
	{"name", "name"},
	{"name[missing]", "name.task-missing"},
	{"name[play]", "name.play-missing"},
	{"name[casing]", "name.casing"},
	{"name[template]", "name.template-position"},
	// name[prefix] is computed but never emitted: upstream raises it only when
	// a prefix policy is configured. The alias is reserved so enabling it later
	// needs no table change.
	{"name[prefix]", "name.prefix"},

	// task: what a task does.
	{"no-changed-when", "task.unguarded-change"},
	{"command-instead-of-module", "task.use-module"},
	{"command-instead-of-shell", "task.use-command"},
	{"package-latest", "task.unpinned-package"},
	{"partial-become", "task.partial-become"},
	{"partial-become[play]", "task.partial-become[play]"},
	{"partial-become[task]", "task.partial-become[task]"},
	{"key-order", "task.key-order"},
	{"key-order[play]", "task.key-order[play]"},
	{"key-order[task]", "task.key-order[task]"},
	{"ignore-errors", "task.ignored-errors"},
	{"no-tabs", "task.tab-character"},
	{"risky-file-permissions", "task.unset-permissions"},
	{"risky-octal", "task.ambiguous-octal"},
	{"risky-shell-pipe", "task.unguarded-pipe"},
	{"no-handler", "task.handler-candidate"},
	{"no-jinja-when", "task.templated-condition"},
	{"no-log-password", "task.logged-password"},
	{"no-relative-paths", "task.relative-src"},
	{"literal-compare", "task.literal-compare"},
	{"empty-string-compare", "task.empty-string-compare"},
	{"inline-env-var", "task.inline-env-var"},
	{"avoid-implicit", "task.implicit-template"},
	{"jinja-template-extension", "task.template-extension"},
	{"loop-var-prefix", "task.loop-var-prefix"},
	{"loop-var-prefix[wrong]", "task.loop-var-prefix[wrong]"},
	{"loop-var-prefix[missing]", "task.loop-var-prefix[missing]"},

	// play: what a play declares, above the tasks it runs.
	{"no-prompting", "play.prompting"},
	{"run-once", "play.run-once-strategy"},
	{"run-once[play]", "play.run-once-strategy[play]"},
	{"run-once[task]", "play.run-once-strategy[task]"},
	{"complexity", "play.complexity"},
	{"complexity[play]", "play.complexity[play]"},
	{"complexity[nesting]", "play.complexity[nesting]"},
	{"complexity[tasks]", "play.complexity[tasks]"},

	// file: what a file is named or holds, independent of its content model.
	{"playbook-extension", "file.playbook-extension"},
	{"sanity", "file.sanity-ignore"},
	{"sanity[cannot-ignore]", "file.sanity-ignore[cannot-ignore]"},
	{"sanity[bad-ignore]", "file.sanity-ignore[bad-ignore]"},

	// deprecated: syntax ansible has superseded.
	{"deprecated-local-action", "deprecated.local-action"},
	{"deprecated-bare-vars", "deprecated.bare-vars"},

	// role: how a role is named or referenced.
	{"role-name", "role.name"},
	{"role-name[path]", "role.name[path]"},

	// meta: role metadata under meta/.
	{"meta-no-tags", "meta.tags-format"},
	{"meta-incorrect", "meta.placeholder-values"},
	{"meta-video-links", "meta.video-links"},
	{"meta-runtime", "meta.runtime-version"},
	{"meta-runtime[invalid-version]", "meta.runtime-version[invalid-version]"},
	{"meta-runtime[unsupported-version]", "meta.runtime-version[unsupported-version]"},

	// yaml: how the YAML source is written, independent of what it declares.
	// The subtags are yamllint's rule ids, embedded by ansible-lint as-is.
	{"yaml", "yaml"},
	{"yaml[anchors]", "yaml.undeclared-alias"},
	{"yaml[braces]", "yaml.brace-spacing"},
	{"yaml[brackets]", "yaml.bracket-spacing"},
	{"yaml[colons]", "yaml.colon-spacing"},
	{"yaml[commas]", "yaml.comma-spacing"},
	{"yaml[comments]", "yaml.comment-spacing"},
	{"yaml[comments-indentation]", "yaml.comment-indentation"},
	{"yaml[document-start]", "yaml.document-start"},
	{"yaml[empty-lines]", "yaml.blank-lines"},
	{"yaml[hyphens]", "yaml.hyphen-spacing"},
	{"yaml[indentation]", "yaml.indentation"},
	{"yaml[key-duplicates]", "yaml.duplicate-key"},
	{"yaml[line-length]", "yaml.long-line"},
	{"yaml[new-line-at-end-of-file]", "yaml.missing-final-newline"},
	{"yaml[octal-values]", "yaml.octal-literal"},
	{"yaml[trailing-spaces]", "yaml.trailing-whitespace"},
	{"yaml[truthy]", "yaml.ambiguous-truthy"},

	// var: how variables are named. var-naming[no-jinja] is dead code in the
	// reference version (a jinja name returns early) and gets no row.
	{"var-naming", "var.naming"},
	{"var-naming[pattern]", "var.naming[pattern]"},
	{"var-naming[no-keyword]", "var.naming[no-keyword]"},
	{"var-naming[non-ascii]", "var.naming[non-ascii]"},
	{"var-naming[no-reserved]", "var.naming[no-reserved]"},
	{"var-naming[read-only]", "var.naming[read-only]"},
	{"var-naming[non-string]", "var.naming[non-string]"},
	{"var-naming[no-role-prefix]", "var.naming[no-role-prefix]"},

	// galaxy: collection metadata in galaxy.yml.
	{"galaxy", "galaxy"},
	{"galaxy[no-changelog]", "galaxy.changelog-missing"},
	{"galaxy[no-license]", "galaxy.license-missing"},
	{"galaxy[no-repository]", "galaxy.repository-missing"},
	{"galaxy[no-runtime]", "galaxy.runtime-file-missing"},
	{"galaxy[version-missing]", "galaxy.version-missing"},
	{"galaxy[invalid-dependency-version]", "galaxy.invalid-dependency-version"},
	{"galaxy[tags]", "galaxy.required-tag-missing"},
	{"galaxy[tags-format]", "galaxy.tags-format"},
	{"galaxy[tags-length]", "galaxy.tags-too-long"},
	{"galaxy[tags-count]", "galaxy.tags-too-many"},
	{"galaxy-version-incorrect", "galaxy.version-too-low"},
}

// retired maps ansible-lint's historical rule identifiers, the numeric ids and
// the pre-rename slugs, to the names they were replaced by. They appear only in
// suppression surfaces, in files written against an older ansible-lint, so they
// resolve in Canonical and never in TagFor: astl emits no finding under a
// retired id. Entries naming a rule astl does not implement are kept so that
// a suppression written for one stays inert rather than being misread.
var retired = map[string]string{
	"102": "no-jinja-when", "104": "deprecated-bare-vars", "105": "deprecated-module",
	"106": "role-name", "202": "risky-octal", "203": "no-tabs",
	"205": "playbook-extension", "206": "jinja[spacing]", "207": "jinja[invalid]",
	"208": "risky-file-permissions", "301": "no-changed-when",
	"302": "deprecated-command-syntax", "303": "command-instead-of-module",
	"304": "inline-env-var", "305": "command-instead-of-shell", "306": "risky-shell-pipe",
	"401": "latest[git]", "402": "latest[hg]", "403": "package-latest",
	"404": "no-relative-paths", "501": "partial-become", "502": "name[missing]",
	"503": "no-handler", "504": "deprecated-local-action", "505": "missing-import",
	"601": "literal-compare", "602": "empty-string-compare", "702": "meta-no-tags",
	"703": "meta-incorrect", "704": "meta-video-links", "911": "syntax-check",
	"deprecated-command-syntax": "no-free-form",
	"fqcn-builtins":             "fqcn[action-core]",
	"git-latest":                "latest[git]",
	"hg-latest":                 "latest[hg]",
	"no-jinja-nesting":          "jinja[invalid]",
	"no-loop-var-prefix":        "loop-var-prefix",
	"unnamed-task":              "name[missing]",
	"var-spacing":               "jinja[spacing]",
}

var (
	upstreamToNative = make(map[string]string, len(equivalence))
	nativeToUpstream = make(map[string]string, len(equivalence))
)

func init() {
	for _, p := range equivalence {
		if _, dup := upstreamToNative[p.upstream]; dup {
			panic("rules: duplicate upstream id in equivalence table: " + p.upstream)
		}
		if _, dup := nativeToUpstream[p.native]; dup {
			panic("rules: duplicate native id in equivalence table: " + p.native)
		}
		upstreamToNative[p.upstream] = p.native
		nativeToUpstream[p.native] = p.upstream
	}
}

// TagFor renders an upstream rule tag in the requested taxonomy. A tag absent
// from the equivalence table is returned unchanged; a test asserts the table
// covers every tag the engine can emit, so that case means a missing row.
func TagFor(tag string, style IDStyle) string {
	if style != IDNative {
		return tag
	}
	if native, ok := upstreamToNative[tag]; ok {
		return native
	}
	return tag
}

// Canonical normalizes a suppression token written in either taxonomy to the
// upstream form findings carry, so `skip_list` entries and `# noqa` comments
// accept both, as well as the retired ansible-lint ids. Tokens outside the
// tables, including the `*` wildcard, pass through untouched.
func Canonical(token string) string {
	token = strings.TrimSpace(token)
	if current, ok := retired[token]; ok {
		token = current
	}
	if upstream, ok := nativeToUpstream[token]; ok {
		return upstream
	}
	return token
}
