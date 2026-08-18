# Rule compatibility table

astl gives every rule a canonical identifier of its own, shaped
`domain.rule[tag]`, where the slug names the defect rather than the check that
finds it. The ansible-lint identifier it came from stays a permanent alias.
`internal/rules/ids.go` holds the equivalence table both taxonomies are derived
from; it is the only place either list is written down.

Rows without a subtag are the rule identifiers themselves. Naming one silences
every subtag of that rule at once.

| astl id | ansible-lint id | Meaning |
|---|---|---|
| `name` | `name` | every naming check |
| `name.task-missing` | `name[missing]` | a task has no name |
| `name.play-missing` | `name[play]` | a play has no name |
| `name.casing` | `name[casing]` | a name does not start with an uppercase letter |
| `name.template-position` | `name[template]` | a Jinja template sits somewhere other than the end of the name |
| `name.prefix` | `name[prefix]` | a name lacks its configured prefix (reserved, never emitted) |
| `task.unguarded-change` | `no-changed-when` | a command can change the host with no `changed_when` guard |
| `task.use-module` | `command-instead-of-module` | a command runs what a dedicated module already does |
| `task.use-command` | `command-instead-of-shell` | `shell` is used where `command` would do |
| `task.unpinned-package` | `package-latest` | a package is installed with `state: latest` |
| `task.partial-become` | `partial-become` | every `become_user` check |
| `task.partial-become[play]` | `partial-become[play]` | a play sets `become_user` without `become` |
| `task.partial-become[task]` | `partial-become[task]` | a task sets `become_user` without `become` |
| `task.key-order` | `key-order` | every key ordering check |
| `task.key-order[play]` | `key-order[play]` | a play's keys are out of the recommended order |
| `task.key-order[task]` | `key-order[task]` | a task's keys are out of the recommended order |
| `task.ignored-errors` | `ignore-errors` | a task ignores every error without registering the result |
| `task.tab-character` | `no-tabs` | a task carries a literal tab where one is not meaningful |
| `task.unset-permissions` | `risky-file-permissions` | a task creates a file without setting its `mode` |
| `task.ambiguous-octal` | `risky-octal` | a numeric `mode` has no leading zero and reads as decimal |
| `task.unguarded-pipe` | `risky-shell-pipe` | a shell pipeline runs without `set -o pipefail` |
| `task.handler-candidate` | `no-handler` | a task conditioned only on a change should be a handler |
| `task.templated-condition` | `no-jinja-when` | a `when` wraps its expression in redundant `{{ }}` |
| `task.logged-password` | `no-log-password` | a looped task passes a password without `no_log` (opt-in) |
| `task.relative-src` | `no-relative-paths` | a `src` climbs out of the role with `../files` or `../templates` |
| `task.literal-compare` | `literal-compare` | a `when` compares against a literal `True` or `False` |
| `task.empty-string-compare` | `empty-string-compare` | a `when` compares against an empty string (opt-in) |
| `task.inline-env-var` | `inline-env-var` | a `command` sets an environment variable inline |
| `task.implicit-template` | `avoid-implicit` | `copy` is given structured `content` instead of using `template` |
| `task.template-extension` | `jinja-template-extension` | a template `src` does not end in `.j2` (opt-in) |
| `task.loop-var-prefix` | `loop-var-prefix` | every role loop variable check (inert until `loop_var_prefix` is set) |
| `task.loop-var-prefix[wrong]` | `loop-var-prefix[wrong]` | a role's `loop_var` does not match the configured pattern |
| `task.loop-var-prefix[missing]` | `loop-var-prefix[missing]` | a role loop relies on the implicit `item` variable |
| `play.prompting` | `no-prompting` | a play uses `vars_prompt` or an unbounded `pause` (opt-in) |
| `play.run-once-strategy` | `run-once` | every `run_once` and strategy check |
| `play.run-once-strategy[play]` | `run-once[play]` | a play uses `strategy: free` |
| `play.run-once-strategy[task]` | `run-once[task]` | a task uses `run_once`, which `strategy: free` changes the meaning of |
| `play.complexity` | `complexity` | every size and nesting check |
| `play.complexity[play]` | `complexity[play]` | a play holds more than `max_tasks` tasks |
| `play.complexity[nesting]` | `complexity[nesting]` | blocks nest deeper than `max_block_depth` |
| `play.complexity[tasks]` | `complexity[tasks]` | a task file holds more than `max_tasks` tasks |
| `file.playbook-extension` | `playbook-extension` | a playbook is not named `.yml` or `.yaml` |
| `file.sanity-ignore` | `sanity` | every sanity ignore list check |
| `file.sanity-ignore[cannot-ignore]` | `sanity[cannot-ignore]` | a sanity ignore names a test that may not be ignored |
| `file.sanity-ignore[bad-ignore]` | `sanity[bad-ignore]` | a sanity ignore entry is malformed |
| `galaxy.version-too-low` | `galaxy-version-incorrect` | the collection version is below 1.0.0 (opt-in) |
| `deprecated.local-action` | `deprecated-local-action` | `local_action` instead of `delegate_to: localhost` |
| `deprecated.bare-vars` | `deprecated-bare-vars` | a loop takes a bare variable name |
| `role.name` | `role-name` | a role directory name is not snake case |
| `role.name[path]` | `role-name[path]` | a role is imported by path instead of by name |
| `meta.tags-format` | `meta-no-tags` | `meta/main.yml` tags are declared wrongly or malformed |
| `meta.placeholder-values` | `meta-incorrect` | `meta/main.yml` still carries scaffolded placeholder values |
| `meta.video-links` | `meta-video-links` | a `video_links` entry is malformed or points at an unsupported host |
| `meta.runtime-version` | `meta-runtime` | every `requires_ansible` check |
| `meta.runtime-version[invalid-version]` | `meta-runtime[invalid-version]` | `requires_ansible` is not a valid requirement specifier |
| `meta.runtime-version[unsupported-version]` | `meta-runtime[unsupported-version]` | `requires_ansible` targets an unsupported ansible-core series |
| `galaxy` | `galaxy` | every `galaxy.yml` check |
| `galaxy.changelog-missing` | `galaxy[no-changelog]` | the collection ships no changelog file |
| `galaxy.license-missing` | `galaxy[no-license]` | `galaxy.yml` has neither `license` nor `license_file` |
| `galaxy.repository-missing` | `galaxy[no-repository]` | `galaxy.yml` has no `repository` key |
| `galaxy.runtime-file-missing` | `galaxy[no-runtime]` | `meta/runtime.yml` is absent |
| `galaxy.version-missing` | `galaxy[version-missing]` | `galaxy.yml` has no `version` key |
| `galaxy.invalid-dependency-version` | `galaxy[invalid-dependency-version]` | a dependency declares an empty version range |
| `galaxy.required-tag-missing` | `galaxy[tags]` | `galaxy.yml` carries none of the required certification tags |
| `galaxy.tags-format` | `galaxy[tags-format]` | a `galaxy.yml` tag is not lowercase alphanumeric |
| `galaxy.tags-too-long` | `galaxy[tags-length]` | a `galaxy.yml` tag exceeds 64 characters |
| `galaxy.tags-too-many` | `galaxy[tags-count]` | `galaxy.yml` declares more than 20 tags |
| `yaml` | `yaml` | every yamllint-derived source check |
| `yaml.undeclared-alias` | `yaml[anchors]` | an alias refers to an anchor declared nowhere above it |
| `yaml.brace-spacing` | `yaml[braces]` | a flow mapping is padded with more than one inner space |
| `yaml.bracket-spacing` | `yaml[brackets]` | a flow sequence carries inner spaces |
| `yaml.colon-spacing` | `yaml[colons]` | spaces sit before a colon, or more than one after it |
| `yaml.comma-spacing` | `yaml[commas]` | a comma is padded before, or not followed by exactly one space |
| `yaml.comment-spacing` | `yaml[comments]` | a comment touches its content or lacks a space after `#` |
| `yaml.comment-indentation` | `yaml[comments-indentation]` | a comment is not indented like the content around it |
| `yaml.document-start` | `yaml[document-start]` | a document does not open with `---` (or does when forbidden) |
| `yaml.blank-lines` | `yaml[empty-lines]` | more than two consecutive blank lines, or any at file edges |
| `yaml.hyphen-spacing` | `yaml[hyphens]` | more than one space follows a list hyphen |
| `yaml.indentation` | `yaml[indentation]` | indentation breaks the structure yamllint infers |
| `yaml.duplicate-key` | `yaml[key-duplicates]` | a mapping declares the same key twice |
| `yaml.long-line` | `yaml[line-length]` | a line exceeds 160 characters |
| `yaml.missing-final-newline` | `yaml[new-line-at-end-of-file]` | the file does not end with a newline |
| `yaml.octal-literal` | `yaml[octal-values]` | an unquoted scalar reads as an octal number |
| `yaml.trailing-whitespace` | `yaml[trailing-spaces]` | whitespace hangs past the end of a line |
| `yaml.ambiguous-truthy` | `yaml[truthy]` | a bare YAML 1.1 boolean other than `true`/`false` |
| `var.naming` | `var-naming` | every variable naming check |
| `var.naming[pattern]` | `var-naming[pattern]` | a variable name does not match the naming pattern |
| `var.naming[no-keyword]` | `var-naming[no-keyword]` | a variable name is a Python keyword |
| `var.naming[non-ascii]` | `var-naming[non-ascii]` | a variable name carries non-ASCII characters |
| `var.naming[no-reserved]` | `var-naming[no-reserved]` | a variable name is reserved by Ansible |
| `var.naming[read-only]` | `var-naming[read-only]` | a variable name is a read-only special variable |
| `var.naming[non-string]` | `var-naming[non-string]` | a variable name is not a string |
| `var.naming[no-role-prefix]` | `var-naming[no-role-prefix]` | a role variable lacks the role's `<role>_` prefix |

The rules marked opt-in are the ones ansible-lint registers only when asked, so
astl leaves them off too. Name them in `enable_list` to switch them on.

The `yaml[*]` subtags are yamllint's rule ids, run with ansible-lint's own
embedded yamllint configuration and with the repository's own `.yamllint`
layered over it when there is one, as ansible-lint does. yamllint's
`# yamllint disable` comment directives are honoured.
`comments-indentation` and `document-start` are off under ansible-lint's
policy and only appear when a config puts them back, which any
`extends: default` does. Five yamllint rules are not implemented
(`quoted-strings`, `key-ordering`, `empty-values`, `float-values`,
`document-end`); all five are off in every bundled policy, and a config that
enables one gets a warning on stderr rather than silence.
`var-naming[no-jinja]` is dead code in the reference ansible-lint version and
is deliberately absent.
