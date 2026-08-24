# Configuration

astl reads ansible-lint's own config file, so one file drives both linters and
trying astl costs nothing in configuration.

## Where the config comes from

astl looks for a config file in the working directory under ansible-lint's own
names, in ansible-lint's order: `.ansible-lint`, `.ansible-lint.yml`,
`.ansible-lint.yaml`, `.config/ansible-lint.yml`, `.config/ansible-lint.yaml`.
The first one that exists is the whole policy; the rest are not merged into it.
`-c FILE` (or `--config FILE`) skips the search and reads FILE, which must then
exist: naming a file that is not there is an error, not an empty config.

## Keys

Keys are spelled as ansible-lint spells them.

| Key | Effect |
|---|---|
| `profile` | `min`, `basic`, `moderate`, `safety`, `shared` or `production`: runs that profile's rules and no others |
| `skip_list` | rule ids or tags to silence everywhere |
| `enable_list` | opt-in rule ids to switch on, and rules to keep that `profile` would drop |
| `warn_list` | rule ids or tags to demote to warning: still printed, with a trailing ` (warning)`, but they do not fail the run |
| `exclude_paths` | path substrings to skip during discovery |
| `ignore_file` | path to the ignore file, overridden by `-i` |
| `loop_var_prefix` | regexp a role `loop_var` must match, `{role}` expanded; unset leaves `loop-var-prefix` inert |
| `max_tasks` | tasks allowed in a play or task file, default 100 |
| `max_block_depth` | block nesting allowed, default 20 |
| `var_naming_pattern` | regexp variable names must match, default `^[a-z_][a-z0-9_]*$` |

The four selection keys resolve in ansible-lint's order: `profile` picks a rule
set, `enable_list` adds back to it, `skip_list` subtracts, and `warn_list`
demotes what survives. A profile name astl does not recognise runs every rule
and says so on stderr, rather than silently linting nothing.

## yamllint

A repository's own `.yamllint` (or `.yamllint.yaml`/`.yamllint.yml`,
`$YAMLLINT_CONFIG_FILE`, `~/.config/yamllint/config`) is read for the `yaml[*]`
family, layered over ansible-lint's bundled yamllint policy exactly as
ansible-lint layers it, `extends: default`/`relaxed` included. yamllint's
`# yamllint disable` comment directives work too, as do `ignore` and
`ignore-from-file`, with git's pattern semantics and per-rule `ignore:` blocks.
An ignored file produces no `yaml[*]` findings and is still linted by every
other rule, which is what separates `ignore` from `exclude_paths`.

One limit is reported on stderr rather than passed over in silence: a config
that switches on a yamllint rule astl does not implement (`quoted-strings`,
`key-ordering`, `empty-values`, `float-values`, `document-end`) says so.

## Inline suppression

Inline `# noqa: <rule>` comments and `tags: [skip_ansible_lint]` are honoured.
`# noqa` and `skip_list` accept either identifier taxonomy, the retired
ansible-lint identifiers such as `102` or `unnamed-task`, and the forms can be
mixed freely in the same file.

## Ignoring rules for whole files

`.ansible-lint-ignore` is read from the working directory, then
`.config/ansible-lint-ignore.txt`; the first one found wins. `-i` /
`--ignore-file` names one directly, and the `ignore_file` config key does the
same with the flag taking precedence. One entry per line, `<path> <rule>`, `#`
starting a comment:

```
playbooks/legacy.yml risky-file-permissions
playbooks/legacy.yml package-latest skip
```

The two forms differ, and the difference is the point of the file. A bare entry
**keeps reporting** the finding, at warning level and ahead of every other
finding, but stops it failing the run. Add `skip` and the finding disappears
entirely. So a repository can adopt astl on a red tree, keep what it owes in
view, and still get a green build.

Matching is exact on both columns: the path is compared verbatim, so `./x.yml`
matches nothing, and `yaml` does not cover `yaml[indentation]`. The rule column
accepts either identifier taxonomy, as `# noqa` does. astl reproduces
ansible-lint's parser here down to its quirks, which
[adr/0006-ignore-file-semantics.md](adr/0006-ignore-file-semantics.md) lists.
