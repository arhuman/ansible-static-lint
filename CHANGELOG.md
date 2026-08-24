# Changelog

<!-- Succinct and public-facing: what changed, not why or how it was decided. -->

All notable changes to this project are documented here. Format:
[Keep a Changelog](https://keepachangelog.com). Entries are grouped under
`[Unreleased]` by date, using Added / Changed / Fixed / Removed.

## [Unreleased]

## [0.2.0] - 2026-08-24

## [0.1.2] - 2026-08-23

## [0.1.1] - 2026-08-23

## [0.1.0] - 2026-08-21

Initial public release. This section lists what astl ships, not the history of
how it got there.

### Added

- Static linting of Ansible content covering the 38 ansible-lint rules that can
  be decided from the YAML source alone, with `-f pep8` output matching
  ansible-lint byte for byte, line and column included, within that scope.
- The `yaml[*]` family, ansible-lint's embedded yamllint pass. It reads a
  repository's own `.yamllint` (including `extends`, `ignore`,
  `ignore-from-file` with git's pattern semantics, per-rule `ignore:` blocks and
  `# yamllint disable` directives) and falls back to ansible-lint's bundled
  policy otherwise. A yamllint rule astl does not implement is reported on
  stderr rather than passed over.
- `var-naming[*]`, with `var_naming_pattern` from `.ansible-lint` replacing the
  default pattern.
- The ansible-lint config file is read with ansible-lint's own key names, so one
  file drives both linters: `profile`, `enable_list`, `skip_list`, `warn_list`,
  `exclude_paths`, `loop_var_prefix`, `max_tasks`, `max_block_depth` and
  `var_naming_pattern`, resolved in ansible-lint's order. `warn_list` rules
  print with the trailing ` (warning)` and do not fail the run. All five of
  upstream's file names are searched in upstream's order (`.ansible-lint`,
  `.ansible-lint.yml`, `.ansible-lint.yaml`, `.config/ansible-lint.yml`,
  `.config/ansible-lint.yaml`), the first hit being the whole policy rather
  than one layer of it. `-c` / `--config` names a file directly, and reports it
  as an error when it does not exist.
- `include_tasks`, `import_tasks`, `include` and `import_playbook` are followed
  to a fixpoint, so a task list is linted as tasks even when it does not live
  under a `tasks/` directory. A file reached both by the directory walk and
  through an include is linted under both kinds and its duplicate findings are
  removed, as upstream does. Templated targets are left alone, since resolving
  them needs the Ansible runtime astl does not have.
- Suppressions: inline `# noqa` comments and `tags: [skip_ansible_lint]` at play
  and task level, plus `skip_list` and `exclude_paths`. Both accept either
  identifier taxonomy and ansible-lint's retired identifiers, mixed freely.
- `--ids {upstream|native}` selects the output vocabulary: rule identifiers and
  message wording together. Under `native`, findings are described in astl's own
  words, stating the defect and the fix. The default, `upstream`, keeps output
  byte identical to ansible-lint.
- `-f sarif` emits a minimal SARIF 2.1.0 document.
- Exit codes: 0 clean, 1 usage or runtime error, 2 violations found, 3 the run
  could not check every file it was given. A file that is unreadable or is not
  valid YAML is named on stderr, the files around it are still linted, and 3
  takes precedence over 2 so an unchecked file never reads as success.
- `--version` reports the version, commit and build date.
- Built to run in CI over repositories the operator does not control: reads of a
  repository-chosen path are bounded, `extends` chains are cycle-checked and
  capped, symlinks are resolved before anything reads them, and a path printed
  in pep8 output is stripped of control characters so one finding is always one
  line. See SECURITY.md.
- `make check` builds `bin/astl`, lints the bundled `examples/playbook.yml` and
  asserts both the findings and the exit code against `examples/expected.txt`.
  It needs no Python, no ansible-lint and no compatibility corpus, so cloning to
  a verified binary is three commands.
- Install with
  `go install github.com/arhuman/ansible-static-lint/cmd/astl@latest`. Go 1.26
  or newer, nothing else.
- A GitHub Action (`action.yml`) and a pre-commit hook (`.pre-commit-hooks.yaml`).
  The action downloads a release binary and checks it against the published
  checksums, so a job needs neither Go nor Python. Its `fail-on-findings` input
  exists for the SARIF case: astl exits 2 on findings, which would otherwise
  fail the step before an upload could run, leaving code scanning empty while
  the log reads as an ordinary lint failure. The README documents the two-tier
  pipeline both are meant for.
