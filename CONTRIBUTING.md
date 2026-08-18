# Contributing

## Setup

astl has no runtime dependencies beyond the Go toolchain.

```sh
git clone https://github.com/arhuman/ansible-static-lint
cd ansible-static-lint
make check     # build, then confirm the binary lints examples/ as expected
make tools     # install the pinned golangci-lint and govulncheck
```

Output parity is asserted against a corpus that lives in a separate repository,
because that corpus carries ansible-lint test data under GPL-3.0-or-later and
astl itself stays MIT. Clone it beside this one to run `make parity` locally:

```sh
git clone https://github.com/arhuman/astl-compatibility-check ../astl-compatibility-check
```

## Make targets

| Target | Purpose |
| ------ | ------- |
| `make build` | Compile `bin/astl`, version-stamped via `-ldflags`. |
| `make check` | Build, lint `examples/`, and assert the findings and the exit code against `examples/expected.txt`. The quickstart, and the cheapest end-to-end proof the binary works. |
| `make test` | Unit tests with the race detector. |
| `make cover` | Tests with coverage; fails below `COVER_MIN`. |
| `make audit` | Coverage gate + `go mod verify` + `golangci-lint` + `govulncheck`. The same command runs in CI. |
| `make parity` | Assert pep8 output against the frozen ansible-lint corpus. Skips loudly when the sibling repo is absent. |
| `make bench` | Speed regression guard: the corpus must lint under 150 ms (best of 5 runs). Skips loudly when the sibling repo is absent. |
| `make fuzz` | Run each fuzz target for `FUZZTIME` (default 60s). Their seed corpora already run under `make test`. |
| `make ci` | Full local pipeline: `tidy` + `audit` + `check` + `parity` + `bench`. |
| `make tidy` | `gofmt` + `go mod tidy`. |
| `make release` | Derive the next semver from Conventional Commits, gate on `make ci`, tag and push. |

## The invariant that governs every change

Under the default `--ids upstream`, astl's pep8 output must stay **byte for
byte identical** to ansible-lint's. That is the whole point of the port, and it
is why `make parity` exists. Any change that touches a rule, a message, or a
finding's line or column must pass `make parity`, not just `make test`.

Two consequences that regularly surprise newcomers:

- The stray `[/]` in pep8 rule tags is deliberate. It reproduces an artifact of
  ansible-lint's own rich-markup rendering. See `internal/format/format.go`.
- `internal/rules/ids.go` is the single source of truth for both identifier
  taxonomies. A new rule needs a row there, or a test fails.
- `examples/expected.txt` is generated, not written. When a change legitimately
  alters what `examples/playbook.yml` reports, regenerate it with
  `./bin/astl examples > examples/expected.txt` and read the diff before
  committing it: an unexplained line there is a regression, not a chore.

## Adding a rule

1. Implement it in `internal/rules/`, emitting the upstream tag.
2. Add its row to the equivalence table in `internal/rules/ids.go`.
3. Add its row to the rule table in `docs/rules.md`.
4. Add a case to the corpus in the compatibility repository.
5. `make ci`.

## Fuzzing

astl runs in CI over repositories the operator does not control, so every byte
of a linted file and of a `.yamllint` is attacker-chosen (see `SECURITY.md`).
Three targets cover that surface:

| Target | Package | What it asserts beyond not crashing |
| ------ | ------- | ----------------------------------- |
| `FuzzLintFile` | `internal/rules` | Findings point at lines that exist, and rendering the same input twice produces the same bytes. |
| `FuzzLoadConfig` | `internal/yamllint` | A file reached through `extends` never has its own text quoted back into an error. |
| `FuzzNoqaIndex` | `internal/parse` | The `# noqa` index agrees with a plain scan of the map it indexes. |

Their seed corpora run as ordinary tests, so `make test` covers them; `make
fuzz` goes past the seeds. A crash is written to `testdata/fuzz/` under the
target's name. Commit that file: it is the regression test, and it reruns from
then on with the rest of the suite.

## GitHub Actions

Every `uses:` is pinned to a full commit SHA, with the version in a trailing
comment:

```yaml
- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
```

A tag is mutable, and `release.yml` signs and publishes binaries, so an action
whose tag moved would build a tampered artifact that still carries a valid
signature. Dependabot updates the SHA and the comment together; do not replace
a pin with a tag to make an edit easier.

Pin within the major version currently in use rather than jumping to the latest
release: a major bump is a separate change that deserves its own review.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `type(scope): subject`,
with type one of `feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert`.
Checked in CI on pull requests.

## Before opening a pull request

1. `make ci` passes.
2. `CHANGELOG.md` has an entry under `[Unreleased]` if the change is
   user-visible.
