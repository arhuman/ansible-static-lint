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

// idPair is one row of the equivalence table: the two identifiers for a rule
// tag, and astl's own one-line description of the defect it reports.
//
// desc is native text, never upstream's. Nothing compares astl's SARIF to
// ansible-lint's, so reproducing upstream's rule prose there would be verbatim
// copying with no compatibility asked of it (ADR 0007). It is the same
// sentence docs/rules.md publishes, and TestDescriptionsMatchTheRuleTable
// holds the two together.
type idPair struct{ upstream, native, desc string }

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
	{"name", "name", "every naming check"},
	{"name[missing]", "name.task-missing", "a task has no name"},
	{"name[play]", "name.play-missing", "a play has no name"},
	{"name[casing]", "name.casing", "a name does not start with an uppercase letter"},
	{"name[template]", "name.template-position", "a Jinja template sits somewhere other than the end of the name"},
	// name[prefix] is computed but never emitted: upstream raises it only when
	// a prefix policy is configured. The alias is reserved so enabling it later
	// needs no table change.
	{"name[prefix]", "name.prefix", "a name lacks its configured prefix (reserved, never emitted)"},

	// task: what a task does.
	{"no-changed-when", "task.unguarded-change", "a command can change the host with no `changed_when` guard"},
	{"command-instead-of-module", "task.use-module", "a command runs what a dedicated module already does"},
	{"command-instead-of-shell", "task.use-command", "`shell` is used where `command` would do"},
	{"package-latest", "task.unpinned-package", "a package is installed with `state: latest`"},
	{"partial-become", "task.partial-become", "every `become_user` check"},
	{"partial-become[play]", "task.partial-become[play]", "a play sets `become_user` without `become`"},
	{"partial-become[task]", "task.partial-become[task]", "a task sets `become_user` without `become`"},
	{"key-order", "task.key-order", "every key ordering check"},
	{"key-order[play]", "task.key-order[play]", "a play's keys are out of the recommended order"},
	{"key-order[task]", "task.key-order[task]", "a task's keys are out of the recommended order"},
	{"ignore-errors", "task.ignored-errors", "a task ignores every error without registering the result"},
	{"no-tabs", "task.tab-character", "a task carries a literal tab where one is not meaningful"},
	{"risky-file-permissions", "task.unset-permissions", "a task creates a file without setting its `mode`"},
	{"risky-octal", "task.ambiguous-octal", "a numeric `mode` has no leading zero and reads as decimal"},
	{"risky-shell-pipe", "task.unguarded-pipe", "a shell pipeline runs without `set -o pipefail`"},
	{"no-handler", "task.handler-candidate", "a task conditioned only on a change should be a handler"},
	{"no-jinja-when", "task.templated-condition", "a `when` wraps its expression in redundant `{{ }}`"},
	{"no-log-password", "task.logged-password", "a looped task passes a password without `no_log` (opt-in)"},
	{"no-relative-paths", "task.relative-src", "a `src` climbs out of the role with `../files` or `../templates`"},
	{"literal-compare", "task.literal-compare", "a `when` compares against a literal `True` or `False`"},
	{"empty-string-compare", "task.empty-string-compare", "a `when` compares against an empty string (opt-in)"},
	{"inline-env-var", "task.inline-env-var", "a `command` sets an environment variable inline"},
	{"avoid-implicit", "task.implicit-template", "`copy` is given structured `content` instead of using `template`"},
	{"jinja-template-extension", "task.template-extension", "a template `src` does not end in `.j2` (opt-in)"},
	{"loop-var-prefix", "task.loop-var-prefix", "every role loop variable check (inert until `loop_var_prefix` is set)"},
	{"loop-var-prefix[wrong]", "task.loop-var-prefix[wrong]", "a role's `loop_var` does not match the configured pattern"},
	{"loop-var-prefix[missing]", "task.loop-var-prefix[missing]", "a role loop relies on the implicit `item` variable"},

	// play: what a play declares, above the tasks it runs.
	{"no-prompting", "play.prompting", "a play uses `vars_prompt` or an unbounded `pause` (opt-in)"},
	{"run-once", "play.run-once-strategy", "every `run_once` and strategy check"},
	{"run-once[play]", "play.run-once-strategy[play]", "a play uses `strategy: free`"},
	{"run-once[task]", "play.run-once-strategy[task]", "a task uses `run_once`, which `strategy: free` changes the meaning of"},
	{"complexity", "play.complexity", "every size and nesting check"},
	{"complexity[play]", "play.complexity[play]", "a play holds more than `max_tasks` tasks"},
	{"complexity[nesting]", "play.complexity[nesting]", "blocks nest deeper than `max_block_depth`"},
	{"complexity[tasks]", "play.complexity[tasks]", "a task file holds more than `max_tasks` tasks"},

	// file: what a file is named or holds, independent of its content model.
	{"playbook-extension", "file.playbook-extension", "a playbook is not named `.yml` or `.yaml`"},
	{"sanity", "file.sanity-ignore", "every sanity ignore list check"},
	{"sanity[cannot-ignore]", "file.sanity-ignore[cannot-ignore]", "a sanity ignore names a test that may not be ignored"},
	{"sanity[bad-ignore]", "file.sanity-ignore[bad-ignore]", "a sanity ignore entry is malformed"},

	// deprecated: syntax ansible has superseded.
	{"deprecated-local-action", "deprecated.local-action", "`local_action` instead of `delegate_to: localhost`"},
	{"deprecated-bare-vars", "deprecated.bare-vars", "a loop takes a bare variable name"},

	// role: how a role is named or referenced.
	{"role-name", "role.name", "a role directory name is not snake case"},
	{"role-name[path]", "role.name[path]", "a role is imported by path instead of by name"},

	// meta: role metadata under meta/.
	{"meta-no-tags", "meta.tags-format", "`meta/main.yml` tags are declared wrongly or malformed"},
	{"meta-incorrect", "meta.placeholder-values", "`meta/main.yml` still carries scaffolded placeholder values"},
	{"meta-video-links", "meta.video-links", "a `video_links` entry is malformed or points at an unsupported host"},
	{"meta-runtime", "meta.runtime-version", "every `requires_ansible` check"},
	{"meta-runtime[invalid-version]", "meta.runtime-version[invalid-version]", "`requires_ansible` is not a valid requirement specifier"},
	{"meta-runtime[unsupported-version]", "meta.runtime-version[unsupported-version]", "`requires_ansible` targets an unsupported ansible-core series"},

	// yaml: how the YAML source is written, independent of what it declares.
	// The subtags are yamllint's rule ids, embedded by ansible-lint as-is.
	{"yaml", "yaml", "every yamllint-derived source check"},
	{"yaml[anchors]", "yaml.undeclared-alias", "an alias refers to an anchor declared nowhere above it"},
	{"yaml[braces]", "yaml.brace-spacing", "a flow mapping is padded with more than one inner space"},
	{"yaml[brackets]", "yaml.bracket-spacing", "a flow sequence carries inner spaces"},
	{"yaml[colons]", "yaml.colon-spacing", "spaces sit before a colon, or more than one after it"},
	{"yaml[commas]", "yaml.comma-spacing", "a comma is padded before, or not followed by exactly one space"},
	{"yaml[comments]", "yaml.comment-spacing", "a comment touches its content or lacks a space after `#`"},
	{"yaml[comments-indentation]", "yaml.comment-indentation", "a comment is not indented like the content around it"},
	{"yaml[document-start]", "yaml.document-start", "a document does not open with `---` (or does when forbidden)"},
	{"yaml[empty-lines]", "yaml.blank-lines", "more than two consecutive blank lines, or any at file edges"},
	{"yaml[hyphens]", "yaml.hyphen-spacing", "more than one space follows a list hyphen"},
	{"yaml[indentation]", "yaml.indentation", "indentation breaks the structure yamllint infers"},
	{"yaml[key-duplicates]", "yaml.duplicate-key", "a mapping declares the same key twice"},
	{"yaml[line-length]", "yaml.long-line", "a line exceeds 160 characters"},
	{"yaml[new-line-at-end-of-file]", "yaml.missing-final-newline", "the file does not end with a newline"},
	{"yaml[octal-values]", "yaml.octal-literal", "an unquoted scalar reads as an octal number"},
	{"yaml[trailing-spaces]", "yaml.trailing-whitespace", "whitespace hangs past the end of a line"},
	{"yaml[truthy]", "yaml.ambiguous-truthy", "a bare YAML 1.1 boolean other than `true`/`false`"},

	// var: how variables are named. var-naming[no-jinja] is dead code in the
	// reference version (a jinja name returns early) and gets no row.
	{"var-naming", "var.naming", "every variable naming check"},
	{"var-naming[pattern]", "var.naming[pattern]", "a variable name does not match the naming pattern"},
	{"var-naming[no-keyword]", "var.naming[no-keyword]", "a variable name is a Python keyword"},
	{"var-naming[non-ascii]", "var.naming[non-ascii]", "a variable name carries non-ASCII characters"},
	{"var-naming[no-reserved]", "var.naming[no-reserved]", "a variable name is reserved by Ansible"},
	{"var-naming[read-only]", "var.naming[read-only]", "a variable name is a read-only special variable"},
	{"var-naming[non-string]", "var.naming[non-string]", "a variable name is not a string"},
	{"var-naming[no-role-prefix]", "var.naming[no-role-prefix]", "a role variable lacks the role's `<role>_` prefix"},

	// galaxy: collection metadata in galaxy.yml.
	{"galaxy", "galaxy", "every `galaxy.yml` check"},
	{"galaxy[no-changelog]", "galaxy.changelog-missing", "the collection ships no changelog file"},
	{"galaxy[no-license]", "galaxy.license-missing", "`galaxy.yml` has neither `license` nor `license_file`"},
	{"galaxy[no-repository]", "galaxy.repository-missing", "`galaxy.yml` has no `repository` key"},
	{"galaxy[no-runtime]", "galaxy.runtime-file-missing", "`meta/runtime.yml` is absent"},
	{"galaxy[version-missing]", "galaxy.version-missing", "`galaxy.yml` has no `version` key"},
	{"galaxy[invalid-dependency-version]", "galaxy.invalid-dependency-version", "a dependency declares an empty version range"},
	{"galaxy[tags]", "galaxy.required-tag-missing", "`galaxy.yml` carries none of the required certification tags"},
	{"galaxy[tags-format]", "galaxy.tags-format", "a `galaxy.yml` tag is not lowercase alphanumeric"},
	{"galaxy[tags-length]", "galaxy.tags-too-long", "a `galaxy.yml` tag exceeds 64 characters"},
	{"galaxy[tags-count]", "galaxy.tags-too-many", "`galaxy.yml` declares more than 20 tags"},
	{"galaxy-version-incorrect", "galaxy.version-too-low", "the collection version is below 1.0.0 (opt-in)"},
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
