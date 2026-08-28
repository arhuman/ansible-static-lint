# ansible-static-lint

[![CI](https://github.com/arhuman/ansible-static-lint/actions/workflows/ci.yml/badge.svg)](https://github.com/arhuman/ansible-static-lint/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/arhuman/ansible-static-lint)](https://github.com/arhuman/ansible-static-lint/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/arhuman/ansible-static-lint.svg)](https://pkg.go.dev/github.com/arhuman/ansible-static-lint)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A fast, offline static companion to ansible-lint, for editor-on-save,
pre-commit, or a CI pre-check.** No Ansible runtime, no Python, no environment
setup, near-instant feedback. The command is `astl`.

**Same config file. Same rule ids. Same `# noqa` comments. Same output.**

astl is a Go reimplementation of the [ansible-lint](https://github.com/ansible/ansible-lint)
rules that can be decided from the YAML source alone: 38 of the 51 default
rules, reproducing ansible-lint's `-f pep8` output byte for byte within that
scope. It is **not** a drop-in replacement for ansible-lint and does not try to
become one: keep ansible-lint where its runtime matters, for syntax check,
collection resolution, schema validation and `--fix`.

| | astl |
|---|---|
| Runtime dependencies | none, one static binary |
| ansible-lint rules supported | 38 of 51 |
| Output conformance within that scope | 2370 / 2370 findings, byte for byte |
| 478-file corpus | 37 ms, against 46.8 s |

## Try it on your repository

**Download the binary.** No Go, no Python, no Ansible:

```sh
VERSION=0.4.0
OS=$(uname -s | tr 'A-Z' 'a-z')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -sSfL "https://github.com/arhuman/ansible-static-lint/releases/download/v${VERSION}/ansible-static-lint_${VERSION}_${OS}_${ARCH}.tar.gz" | tar -xz astl
./astl path/to/playbooks
```

Linux, macOS and Windows, amd64 and arm64, are on the
[releases page](https://github.com/arhuman/ansible-static-lint/releases). Each
release ships checksums signed with cosign; see
[docs/supply-chain.md](docs/supply-chain.md) to verify one.

**In GitHub Actions.** Two lines, and the action fetches and checksum-verifies
the binary for you:

```yaml
      - uses: actions/checkout@v7
      - uses: arhuman/ansible-static-lint@v0.4.0
```

**As a pre-commit hook.** pre-commit installs astl in an isolated environment
and caches it. With pre-commit 3.0 or newer, neither astl nor Go needs to be
installed beforehand: `language: golang` bootstraps the toolchain itself. The
first run downloads Go and compiles astl once; every run after that uses the
cached binary.

```yaml
repos:
  - repo: https://github.com/arhuman/ansible-static-lint
    rev: v0.4.0
    hooks:
      - id: astl
```

**With Go already installed**, skip the download:
`go install github.com/arhuman/ansible-static-lint/cmd/astl@latest`. Go 1.26 or
newer.

None of these ask you to change your repository. If it already has an
ansible-lint config file, astl reads it as is: `profile`, `skip_list`,
`enable_list`, `warn_list` and `exclude_paths` work unchanged, and so do your
existing `# noqa` comments. More in
[docs/configuration.md](docs/configuration.md).

## Why static?

astl has no Ansible runtime and never shells out. It therefore does not:

- run `ansible-playbook --syntax-check`;
- resolve collections, roles or module argument specs;
- evaluate Jinja templates;
- validate content against JSON schemas;
- apply `--fix` transforms;
- load custom rule plugins or profiles.

| | astl | ansible-lint |
|---|---|---|
| The 38 statically decidable rules | yes | yes |
| The 13 runtime-dependent rules | no | yes |
| `--fix` | no | yes |
| Needs Python and an Ansible install | no | yes |
| Cold start | 2.2 ms | 0.52 s |

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
ansible-lint test data. astl carries neither the corpus nor the golden output
into its own tree and stays MIT. What astl does carry, under its default output
mode, is ansible-lint's diagnostic message text itself, reproduced verbatim for
compatibility; see ADR 0004 for the reasoning and its limits.

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

`make bench` fails the build if linting the reference corpus exceeds 150 ms,
roughly five times the current time, so the property is guarded rather than
assumed. Why the numbers look like this is in
[docs/performance.md](docs/performance.md).

## Using astl in CI

astl is not a drop-in replacement for ansible-lint, so the pipeline that gets
the most out of it runs both, at different frequencies:

| Tier | When | What runs | Why |
|---|---|---|---|
| Fast | every push and pull request | `astl` | seconds, and it covers the 38 rules that block most pipelines |
| Deep | merge to the default branch, or nightly | `ansible-lint` | the 13 runtime-dependent rules astl cannot decide |

Adopting the fast tier costs no configuration, since `--ids upstream` is the
default. [docs/ci.md](docs/ci.md) has the full workflows: action inputs, SARIF
upload to GitHub code scanning, and the pre-commit variants.

## Usage

```sh
astl path/to/playbooks     # pep8 lines on stdout
astl -f sarif .            # SARIF 2.1.0 instead, see docs/sarif.md
astl -c ci.ansible-lint .  # lint against a named config
```

astl follows `include_tasks`, `import_tasks`, `include` and `import_playbook`
to a fixpoint, so a task list is linted as tasks even when it does not sit
under a `tasks/` directory. A file reached only through an include is linted; a
file reached both by the directory walk and through an include is linted under
both kinds, and the duplicate findings that produces are removed. Templated
targets such as `{{ role_path }}/x.yml` are left alone, since resolving them
needs the Ansible runtime astl deliberately does not have.

| Exit code | Meaning |
|---|---|
| 0 | clean, no violations |
| 1 | usage or runtime error, for example an input path that does not exist |
| 2 | violations were found |
| 3 | the run could not check every file it was given |

Exit 3 says a file astl was given could not be examined, so there is no result
for it. It takes precedence over 2, because a violation is a result you can
read while an unchecked file is not, and an exit code is the only place that
can say so. [docs/exit-codes.md](docs/exit-codes.md) has the details, including
what does not count as unchecked.

## Rules

38 static rules are supported; the full equivalence table between astl's
`domain.rule[tag]` identifiers and ansible-lint's is in
[docs/rules.md](docs/rules.md). `internal/rules/ids.go` is the single source
both taxonomies are derived from.

`--ids native` switches output to the native vocabulary: both the rule
identifier and the message wording, in pep8 and SARIF alike. The default is
`--ids upstream`, which keeps the pep8 output byte for byte identical to
ansible-lint's. The SARIF document is not held to that comparison and does not
try to match upstream's own SARIF, for the reasons in
[ADR 0007](docs/adr/0007-sarif-outside-the-compatibility-contract.md).

```
# default, ansible-lint's identifier and wording
playbooks/demo.yml:11: no-changed-when: Commands should not change things if nothing needs doing.

# --ids native
playbooks/demo.yml:11: task.unguarded-change: This command always reports changed. Add changed_when, or a creates guard.
```

The native messages state the defect, then the fix. They are original text,
never a reworded copy of upstream's, and they stay under 100 characters so
that the fix survives truncation in CI annotations and editor panels.

## Early feedback wanted

astl is early. I am looking for real-world Ansible repositories to test
whether the static-only trade-off is useful outside the compatibility corpus.
If you try it, please [open an issue](https://github.com/arhuman/ansible-static-lint/issues)
with anything surprising: missing findings, extra findings, unsupported
workflows, or simply why you would not use it.

## Verifying the implementation yourself

From nothing to a verified binary in three commands, needing neither Python,
ansible-lint, nor the test corpus:

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
`examples/playbook.yml` is 11 lines of YAML and trips seven rules across five
families, so it also doubles as the shortest tour of what astl catches.

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
