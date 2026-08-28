# astl in CI

astl is not a drop-in replacement for ansible-lint, so the pipeline that gets
the most out of it runs both, at different frequencies:

| Tier | When | What runs | Why |
|---|---|---|---|
| Fast | every push and pull request | `astl` | seconds, and it covers the 38 rules that block most pipelines |
| Deep | merge to the default branch, or nightly | `ansible-lint` | the 13 runtime-dependent rules astl cannot decide ([scope.md](scope.md)) |

Adopting the fast tier costs no configuration. The default `--ids upstream`
keeps ansible-lint's rule identifiers, so an existing `.ansible-lint`
`skip_list` and existing `# noqa` comments keep working unchanged.

## GitHub Actions

```yaml
name: lint
on: [push, pull_request]

jobs:
  ansible:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: arhuman/ansible-static-lint@v0.2.0
```

The action downloads a release binary and checks it against the published
checksums, so the job needs neither Go nor Python. Its inputs are `paths`,
`version`, `format`, `ids`, `config`, `output`, `working-directory` and
`fail-on-findings`; it outputs `exit-code`. See [action.yml](../action.yml).
Pin it to a commit SHA if your policy requires it, as this repository does for
its own workflows.

Without the action, install the binary yourself:

```yaml
      - uses: actions/setup-go@v7
        with:
          go-version: stable
      - run: go install github.com/arhuman/ansible-static-lint/cmd/astl@latest
      - run: astl .
```

## Exit codes are the job status

astl exits 2 when it finds violations and 3 when it could not check a file, and
both fail the step. Exit 3 failing is the point: an unchecked file produces no
findings, so a run that let it pass would read as clean. Exit 1 is astl itself
failing, on a bad flag or a path that does not exist, and always fails the step
regardless of `fail-on-findings`. Full taxonomy in
[exit-codes.md](exit-codes.md).

## Uploading to GitHub code scanning

`-f sarif` emits SARIF 2.1.0, which is what code scanning consumes. The
document also declares which rules astl implements and which it deliberately
does not, so a dashboard can tell a rule that found nothing from a rule that
never ran; [sarif.md](sarif.md) is the contract. One thing
to get right: astl still exits 2 when it finds something, so in the obvious job
the lint step fails, the upload step never runs, and code scanning stays empty
while the log looks like an ordinary lint failure. Let the upload run, then
decide:

```yaml
      - uses: arhuman/ansible-static-lint@v0.2.0
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

## pre-commit

```yaml
repos:
  - repo: https://github.com/arhuman/ansible-static-lint
    rev: v0.4.0
    hooks:
      - id: astl
```

The hook builds astl from source at the pinned revision. With pre-commit 3.0
or newer nothing needs installing beforehand: `language: golang` bootstraps
the Go toolchain itself, compiles once, and reuses the cached binary.
[prek](https://github.com/j178/prek), the Rust pre-commit reimplementation,
runs the same hook declaration; verified against real repositories whose CI
uses prek. The hook lints the whole repository rather than the staged files,
because discovery decides a file's kind from its path and follows
`include_tasks` from the files it is given, so a narrowed list changes what
is reported. At tens of milliseconds there is nothing to gain by narrowing
it.

When the repository's CI runs ansible-lint through this same pre-commit
configuration, do not swap the hook: move the full ansible-lint hook to
`stages: [manual]`, add astl for the ordinary stages, and have CI invoke the
manual stage explicitly (`pre-commit run ansible-lint --hook-stage manual
--all-files`). Ordinary commits then pay only astl, CI runs both, and no
out-of-scope rule is lost.

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
