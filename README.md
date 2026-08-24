# ansible-static-lint

Static Ansible linting with no Ansible runtime, no environment setup, and
near-instant feedback. The command is `astl`.

astl is a Go reimplementation of the [ansible-lint](https://github.com/ansible/ansible-lint)
rules that can be decided from the YAML source alone: 38 of the 51 default
rules, reproducing ansible-lint's `-f pep8` output byte for byte within that
scope. It is **not** a drop-in replacement for ansible-lint and does not try to
become one. Use astl where feedback latency matters: editor-on-save,
pre-commit, the inner loop of CI. Keep ansible-lint where its runtime matters:
syntax check, collection resolution, schema validation, `--fix`.

## Quickstart

From nothing to a verified binary in three commands. No Python, no
ansible-lint, no test corpus:

```sh
git clone https://github.com/arhuman/ansible-static-lint
cd ansible-static-lint
make check
```

`make check` builds `bin/astl`, lints the deliberately broken playbook in
`examples/`, and asserts the run against `examples/expected.txt`. A working
build ends on:

```
==> ./bin/astl examples
examples/playbook.yml:4:3: name[play][/]: All plays should be named.
examples/playbook.yml:6: command-instead-of-shell: Use shell only when shell functionality is required.
examples/playbook.yml:6: name[missing][/]: All tasks should be named.
examples/playbook.yml:6: no-changed-when: Commands should not change things if nothing needs doing.
examples/playbook.yml:7:13: name[casing][/]: All names should start with an uppercase letter.
examples/playbook.yml:7: package-latest: Package installs should not use latest.
examples/playbook.yml:11: risky-file-permissions: File permissions unset or incorrect.
OK: 7 findings, exit code 2, output matches examples/expected.txt
```

Anything else is a failure with a diff, because a linter that runs and reports
nothing looks exactly like a linter that runs and is broken. The findings and
the exit code are both asserted, so neither can drift unnoticed.

Now point it at your own repository:

```sh
./bin/astl path/to/playbooks
```

That is the whole loop. `examples/playbook.yml` is 11 lines of YAML and trips
seven rules across five families, so it also doubles as the shortest tour of
what astl catches.

Prefer not to clone? `go install github.com/arhuman/ansible-static-lint/cmd/astl@latest`
puts `astl` on your PATH, then `astl --version` and `astl path/to/playbooks`.

Requirements: Go 1.26 or newer for either path. Nothing else.

## How fast?

Measured on Apple Silicon macOS against ansible-lint 26.8.0
(Python 3.14, `--offline`), on ansible-lint's own examples corpus:

| Metric | ansible-lint | astl (38 rules) |
|---|---|---|
| Cold start (`--version`) | 0.52 s | 2.2 ms |
| One 6-line playbook | 2.1 s | 2.5 ms |
| 478-file corpus | 46.8 s | 37 ms |
| Max RSS on the corpus | 123 MiB | 42 MiB |

Read the ratios with care: the comparison is asymmetric, since ansible-lint is
also running its syntax-check subprocess and the 13 rules astl excludes. The
honest headline numbers are cold start and the single playbook, where the gap
is interpreter and import overhead that exists before any rule runs; even
`ansible-lint --version` costs half a second.

The number that matters architecturally: the 36 rules that work off the parsed
document produce no measurable slowdown between them, because a rule is a
predicate over an already-parsed document and its marginal cost is
microseconds. The one rule family that does cost something is `yaml[*]`, which
runs a second, token-level scan of every file for the yamllint checks; after
streaming that scan through a fixed four-token window and adopting a
lazy-GC-under-a-memory-ceiling posture
suited to a lint-and-exit process (`GOGC`/`GOMEMLIMIT` in the environment
still win), the corpus sits at 37 ms against 31 ms before the family existed.
The memory ceiling keeps the trade bounded: 42 MiB on the corpus, under
100 MiB on a 4000-file monorepo. astl's cost remains startup, I/O and the two
parses, and stays nearly independent of how many static checks run on top. That property is guarded,
not assumed: `make bench` fails the build if linting the reference corpus
exceeds 150 ms, roughly five times the current time.

## Running it on your own repository

If the repo already has an ansible-lint config file, astl reads it as is:
`profile`, `skip_list`, `enable_list`, `warn_list` and `exclude_paths` work
unchanged, so trying astl costs nothing in configuration (see
[Configuration](#configuration)). `-c FILE` lints against a named config
instead of searching for one.

astl follows `include_tasks`, `import_tasks`, `include` and `import_playbook`
to a fixpoint, so a task list is linted as tasks even when it does not sit
under a `tasks/` directory. A file reached only through an include is linted; a
file reached both by the directory walk and through an include is linted under
both kinds, and the duplicate findings that produces are removed. Templated
targets such as `{{ role_path }}/x.yml` are left alone, since resolving them
needs the Ansible runtime astl deliberately does not have.

`-f sarif` emits a minimal SARIF 2.1.0 document instead of pep8 lines.

| Exit code | Meaning |
|---|---|
| 0 | clean, no violations |
| 1 | usage or runtime error, for example an input path that does not exist |
| 2 | violations were found |
| 3 | the run could not check every file it was given |

An input path that cannot be read is fatal. A file or directory that becomes
unreadable during the walk is reported on stderr and skipped, leaving the rest
of the run intact.

Exit code 3 covers the files astl was given but could not examine: one that is
not readable, or one that is not valid YAML. Each is named on stderr, the files
around it are still linted, and their findings are still written to stdout. It
takes precedence over 2 because a violation is a result you can read, while an
unchecked file means there is no result for it, and an exit code is the only
place that can say so. Treat 3 as "fix the broken file, then trust the run".

Files that were never YAML in the first place, such as Jinja2 templates and
Python plugins, do not count: astl reads the ones it has rules for as text, so
failing to parse them as YAML is expected. A multi-document YAML file does not
count either, since the `yaml[*]` rules still lint it.

## Using astl in CI

astl is not a drop-in replacement for ansible-lint, so the pipeline that gets
the most out of it runs both, at different frequencies:

| Tier | When | What runs | Why |
|---|---|---|---|
| Fast | every push and pull request | `astl` | seconds, and it covers the 38 rules that block most pipelines |
| Deep | merge to the default branch, or nightly | `ansible-lint` | the 13 runtime-dependent rules astl cannot decide ([docs/scope.md](docs/scope.md)) |

Adopting the fast tier costs no configuration. The default `--ids upstream`
keeps ansible-lint's rule identifiers, so an existing `.ansible-lint`
`skip_list` and existing `# noqa` comments keep working unchanged.

### GitHub Actions

```yaml
name: lint
on: [push, pull_request]

jobs:
  ansible:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: arhuman/ansible-static-lint@v0.1.0
```

The action downloads a release binary and checks it against the published
checksums, so the job needs neither Go nor Python. `paths`, `config`, `format`,
`ids`, `working-directory` and `fail-on-findings` are inputs; see
[action.yml](action.yml). Pin it to a commit SHA if your policy requires it, as
this repository does for its own workflows.

Without the action, install the binary yourself:

```yaml
      - uses: actions/setup-go@v7
        with:
          go-version: stable
      - run: go install github.com/arhuman/ansible-static-lint/cmd/astl@latest
      - run: astl .
```

### Exit codes are the job status

astl exits 2 when it finds violations and 3 when it could not check a file, and
both fail the step. Exit 3 failing is the point: an unchecked file produces no
findings, so a run that let it pass would read as clean. Exit 1 is astl itself
failing, on a bad flag or a path that does not exist, and always fails the step
regardless of the settings below.

### Uploading to GitHub code scanning

`-f sarif` emits SARIF 2.1.0, which is what code scanning consumes. One thing
to get right: astl still exits 2 when it finds something, so in the obvious job
the lint step fails, the upload step never runs, and code scanning stays empty
while the log looks like an ordinary lint failure. Let the upload run, then
decide:

```yaml
      - uses: arhuman/ansible-static-lint@v0.1.0
        id: astl
        with:
          format: sarif
          output: astl.sarif
          fail-on-findings: false   # let the upload step run

      - uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: astl.sarif

      - name: Fail on findings
        if: steps.astl.outputs.exit-code != '0'
        run: exit 1
```

`fail-on-findings: false` hands the verdict to the last step, so the findings
reach code scanning and the job still goes red. Drop either that input or the
`if: always()` and you lose one of the two.

### pre-commit

```yaml
repos:
  - repo: https://github.com/arhuman/ansible-static-lint
    rev: v0.1.0
    hooks:
      - id: astl
```

The hook builds astl from source at the pinned revision, so it needs a Go
toolchain and no Python. It lints the whole repository rather than the staged
files, because discovery decides a file's kind from its path and follows
`include_tasks` from the files it is given, so a narrowed list changes what is
reported. At tens of milliseconds there is nothing to gain by narrowing it.

With the binary already on PATH, skip the toolchain:

```yaml
repos:
  - repo: local
    hooks:
      - id: astl
        name: astl
        entry: astl .
        language: system
        types: [yaml]
        pass_filenames: false
```

### Verifying a downloaded release

Releases ship `checksums.txt` signed with cosign in keyless mode. The checksum
alone only proves the archive arrived intact, since it travels from the same
release; the signature is what ties it to this repository's release workflow:

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/arhuman/ansible-static-lint/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The bundle carries the signature and the certificate together. Releases up to
v0.1.0 shipped them as separate `checksums.txt.sig` and `checksums.txt.pem`
files; verify those with `--signature` and `--certificate` instead.

## Early feedback wanted

astl is early. I am looking for real-world Ansible repositories to test
whether the static-only trade-off is useful outside the compatibility corpus.
If you try it, please [open an issue](https://github.com/arhuman/ansible-static-lint/issues)
with anything surprising: missing findings, extra findings, unsupported
workflows, or simply why you would not use it.

## Why static?

astl has no Ansible runtime and never shells out. It therefore does not:

- run `ansible-playbook --syntax-check`;
- resolve collections, roles or module argument specs;
- evaluate Jinja templates;
- validate content against JSON schemas;
- apply `--fix` transforms;
- load custom rule plugins or profiles.

That is the trade-off: astl handles every check that can be decided from the
source alone and leaves runtime-dependent validation to ansible-lint. The
boundary is measured, not rhetorical: 90% of the corpus findings astl does
not report require capabilities it deliberately does not have, and the full
per-rule accounting is in [docs/scope.md](docs/scope.md).

The flip side of having no runtime: astl keeps linting files whose references
Ansible cannot resolve, where ansible-lint abandons the file and reports only
the load failure.

## Compatibility

Two numbers, and they measure different things:

| Metric | Value |
|---|---|
| Conformance within the supported scope | **2370 / 2370** golden findings reproduced (100%) |
| Coverage of all ansible-lint findings on the corpus | 2370 / 2648 (89.5%) |

Within its scope, astl agrees with ansible-lint on every finding: same file,
same line, same column, same message, byte for byte. It also emits 46 findings
ansible-lint does not, all on files ansible-lint abandons because its embedded
runtime rejects them. Those extras are pinned line for line as an exact set:
the harness fails if the set changes in either direction, so a new false
positive cannot hide among them.

The corpus, the frozen golden output, and the assertion live in a separate
repository,
[astl-compatibility-check](https://github.com/arhuman/astl-compatibility-check),
kept as a sibling directory. It is licensed GPL-3.0-or-later because it contains
ansible-lint test data: the corpus and the golden output. astl carries neither
into its own tree and stays MIT. What astl does carry, under its default
output mode, is ansible-lint's diagnostic message text itself, reproduced
verbatim for compatibility; see ADR 0004 for the reasoning and its limits.

## Configuration

astl looks for a config file in the working directory under ansible-lint's own
names, in ansible-lint's order: `.ansible-lint`, `.ansible-lint.yml`,
`.ansible-lint.yaml`, `.config/ansible-lint.yml`, `.config/ansible-lint.yaml`.
The first one that exists is the whole policy; the rest are not merged into it.
`-c FILE` (or `--config FILE`) skips the search and reads FILE, which must then
exist: naming a file that is not there is an error, not an empty config.

Keys are spelled as ansible-lint spells them, so one file drives both linters.

| Key | Effect |
|---|---|
| `profile` | `min`, `basic`, `moderate`, `safety`, `shared` or `production`: runs that profile's rules and no others |
| `skip_list` | rule ids or tags to silence everywhere |
| `enable_list` | opt-in rule ids to switch on, and rules to keep that `profile` would drop |
| `warn_list` | rule ids or tags to demote to warning: still printed, with a trailing ` (warning)`, but they do not fail the run |
| `exclude_paths` | path substrings to skip during discovery |
| `loop_var_prefix` | regexp a role `loop_var` must match, `{role}` expanded; unset leaves `loop-var-prefix` inert |
| `max_tasks` | tasks allowed in a play or task file, default 100 |
| `max_block_depth` | block nesting allowed, default 20 |
| `var_naming_pattern` | regexp variable names must match, default `^[a-z_][a-z0-9_]*$` |

The four selection keys resolve in ansible-lint's order: `profile` picks a rule
set, `enable_list` adds back to it, `skip_list` subtracts, and `warn_list`
demotes what survives. A profile name astl does not recognise runs every rule
and says so on stderr, rather than silently linting nothing.

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

Inline `# noqa: <rule>` comments and `tags: [skip_ansible_lint]` are honoured.
`# noqa` and `skip_list` accept either identifier taxonomy, the retired
ansible-lint identifiers such as `102` or `unnamed-task`, and the forms can be
mixed freely in the same file.

### Ignoring rules for whole files

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
[docs/adr/0006-ignore-file-semantics.md](docs/adr/0006-ignore-file-semantics.md)
lists.

## Rules

38 static rules are supported; the full equivalence table between astl's
`domain.rule[tag]` identifiers and ansible-lint's is in
[docs/rules.md](docs/rules.md). `internal/rules/ids.go` is the single source
both taxonomies are derived from.

`--ids native` switches output to the native vocabulary: both the rule
identifier and the message wording, in pep8 and SARIF alike. The default is
`--ids upstream`, which keeps output byte for byte identical to ansible-lint's.

```
# default, ansible-lint's identifier and wording
playbooks/demo.yml:11: no-changed-when: Commands should not change things if nothing needs doing.

# --ids native
playbooks/demo.yml:11: task.unguarded-change: This command always reports changed. Add changed_when, or a creates guard.
```

The native messages state the defect, then the fix. They are original text,
never a reworded copy of upstream's, and they stay under 100 characters so
that the fix survives truncation in CI annotations and editor panels.

## Development

```sh
make check    # build + lint examples/, asserted against examples/expected.txt
make test     # go test -race ./...
make cover    # tests with coverage, fails below COVER_MIN
make audit    # coverage gate + go mod verify + golangci-lint + govulncheck
make parity   # pep8 output parity against the frozen ansible-lint corpus
make bench    # both speed guards: noqa shape, then the corpus under 150 ms
make ci       # tidy + audit + check + parity + bench, the gate before any commit
make tidy     # gofmt + go mod tidy
```

`make test` cannot see a compatibility regression: it asserts behaviour, not
byte-exact output. Anything touching a rule, a message, or a finding's line or
column needs `make parity`, which needs the sibling repository described above.
`make ci` runs both. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT, see `LICENSE`.
