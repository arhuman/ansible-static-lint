# Changelog

<!-- Succinct and public-facing: what changed, not why or how it was decided. -->

All notable changes to this project are documented here. Format:
[Keep a Changelog](https://keepachangelog.com). Entries are grouped under
`[Unreleased]` by date, using Added / Changed / Fixed / Removed.

## [Unreleased]

### Fixed - 2026-10-18

- An include or import resolving outside the working directory is no longer
  followed. A linted playbook could name `../secret.yml` or `/etc/hosts` and
  have astl read and report on it; ansible-lint skips those files too.

### Fixed - 2026-09-24

- The action publishes its `exit-code` output again, and `fail-on-findings:
  "false"` is honoured. The runner invokes a composite step as `bash -e`, which
  the step's own `set` cannot undo, so astl exiting 2 killed the step before
  the code was read: the output was never written and every consumer gating on
  it saw a failed step instead.

## [0.7.0] - 2026-09-24

### Added - 2026-09-24

- `brew install --cask arhuman/tap/ansible-static-lint`. The cask is generated
  by GoReleaser from the release archives and pushed on every tag, so it
  cannot lag a release. The tap previously carried a hand-written formula
  whose four URLs and checksums were bumped by hand; it had gone stale at
  v0.6.0 with nothing to notice. Existing formula installs migrate on the
  next `brew update` via the tap's `tap_migrations.json`.

### Changed - 2026-09-24

- The published performance figures are remeasured with `make bench-compare`,
  ten runs per row on an idle machine: the corpus reads 45.6 ms against a
  documented 59 ms, cold start 2.9 ms against 3.3 ms, and the ansible-lint
  side moved too (33.5 s and 0.37 s, from 39.5 s and 0.49 s). astl got faster
  while gaining a rule, which is what the architecture note predicts: a rule
  is a predicate over an already-parsed document.
- The rule counts left behind by `no-free-form` are corrected in
  `docs/performance.md` and `docs/sarif.md` (39 to 40 implemented, 12 to 11
  unsupported, 36 to 38 working off the parsed document).

### Added - 2026-09-24

- A CI job runs `pipx install` on `windows-latest` and `ubuntu-latest`. It
  packs a wheel from the working tree with goreleaser's snapshot mode, installs
  it with pipx, and asserts the exit code and exact output over the `examples`
  fixture. The README advertises that install path and nothing ran it: the
  `action` job covers the download-a-release route, which is a different
  artifact built by a different script.

- The README states the Windows case plainly: ansible-lint ships MacOS and
  POSIX classifiers and needs ansible-core, so linting playbooks on Windows
  normally means WSL, a container or a Linux runner. astl needs none of them.
  The section carries a PowerShell download, the `pipx` path and the Actions
  snippet, and says what is not required. The binaries and wheels already
  shipped; the claim did not.

- `no-free-form` (`no-free-form[raw]`, `no-free-form[raw-non-string]`): the
  shorthand `module: key=value ...` syntax, which ansible re-parses with its
  argument splitter and which hides quoting bugs. `command`, `shell`,
  `win_command` and `win_shell` report only when the line carries one of the
  module's own options (`chdir=`, `creates=`, `executable=`, `removes=`,
  `stdin=`, `stdin_add_newline=`, `warn=`), since everything else there is
  part of the command. astl now covers 40 of ansible-lint's 51 built-in rule
  IDs, and the golden carries 2417 findings.

### Added - 2026-09-23

- The GitHub Action runs on Windows runners. It already shipped Windows
  binaries; the action rejected any runner that was not Linux or macOS, so
  `runs-on: windows-latest` needed a manual download. It now selects the `.zip`
  archive and `astl.exe`, and resolves the native path Windows needs on `PATH`.
- A CI job runs the action on `ubuntu-latest`, `macos-latest` and
  `windows-latest` against the `examples` fixture, asserting both the exit code
  and the exact output. Nothing tested the action before.

### Changed - 2026-09-23

- `docs/scope.md` lists all 12 unsupported rule IDs, including the four that
  report nothing on this corpus (`deprecated-module`, `no-same-owner`,
  `only-builtins`, `role-argument-spec`), so the twelve can be found in one
  place rather than inferred from a subtraction.
- "51 default rules" reads "51 built-in rule IDs" in the README and
  `docs/scope.md`, since three of the twelve are opt-in and never run by
  default.

### Fixed - 2026-09-23

- Documented performance figures now match what the current binary measures.
  The corpus row read 37 ms against a measured 59 ms, stale since the `latest`
  rule and the gitignore-semantics discovery landed; the speed guard never
  caught it because 59 ms is still inside its 150 ms budget. Cold start, the
  single-playbook row and both RSS figures were remeasured at the same time.
- The extra-findings count read 46 in the README and `docs/scope.md` where the
  pinned set in the compatibility harness holds 48.
- The README's Rules section said 38 static rules where the rest of the
  documentation says 39.

## [0.6.0] - 2026-09-21

### Added - 2026-09-21

- `latest` (`latest[git]`, `latest[hg]`): a version control checkout left on
  its moving default. A missing `version`/`revision` counts, as upstream reads
  the argument with the unpinned value as its default. astl now covers 39 of
  ansible-lint's 51 default rules, and the golden carries 2387 findings.

- PyPI distribution: `pipx install ansible-static-lint`. Wheels ship the
  released binary in `.data/scripts/`, so nothing is rebuilt and no Python
  interpreter starts at run time. Eight wheels cover linux, macOS and Windows
  on amd64 and arm64, with the static linux binary serving both manylinux and
  musllinux.
- `scripts/build_wheels.py` packs the wheels from the GoReleaser archives.
  Stdlib only, and byte-reproducible.
- Wheels carry the archive's SBOM at `dist-info/sboms/astl.spdx.json` and are
  uploaded via PyPI trusted publishing with PEP 740 attestations. The release
  job verifies the cosign-signed `checksums.txt` before packing, so a wheel's
  binary is the one that was signed.
- A manual `TestPyPI rehearsal` workflow repacks an existing release and
  publishes it to TestPyPI, so the first real upload is not the first attempt.

### Fixed - 2026-09-21

- A `# noqa` on one task inside a `block`/`rescue`/`always` no longer silences
  its siblings. The block container collected suppressions across its whole
  span, where ansible-lint attaches them to each task on its own. No shipped
  rule could reach the case, which is why no finding changed until now.

## [0.5.1] - 2026-09-12

### Fixed - 2026-09-12 (issue #5)

- Discovery now excludes what ansible-lint excludes: the builtin list
  (`.ansible`, `.git`, `.tox`, `.mypy_cache`, `__pycache__`, `.DS_Store`,
  `.coverage`, `.pytest_cache`, `.ruff_cache`) and each directory's own
  `.gitignore`, with upstream's exact scoping. A gitignored `venv/` is no
  longer linted. `.venv` is deliberately not ignored by default, because
  ansible-lint does not ignore it either; exclude it via `exclude_paths` or
  `.gitignore`. ADR 0009.
- The builtin list and `exclude_paths` form one pattern list, so an
  `exclude_paths` entry such as `!.tox` re-includes a builtin exclusion.
- A dir-only `exclude_paths` pattern (`build/`) now prunes the directory
  itself, as it does upstream.

## [0.5.0] - 2026-08-29

### Added - 2026-08-29

- The SARIF report records its invocation: `invocations[].workingDirectory`
  carries the absolute file URI result paths are relative to, so a saved or
  moved report still resolves them.
- `astl.scope` gains an `enabled` list: the rules this run turned on after
  applying the profile, `skip_list` and `enable_list`, distinct from
  `supported` (implemented) and `outOfScope` (cannot run). ADR 0008.

### Fixed - 2026-08-29 (found on freeipa)

- A clip-chomped `shell` scalar keeps its trailing newline when its arguments
  are joined, so `command-instead-of-shell` counts it as a shell feature the
  way ansible-lint's `join_args` does.

## [0.4.0] - 2026-08-28

### Fixed - 2026-08-28 (found on timothystewart6/k3s-ansible)

- `exclude_paths` entries are matched with gitignore semantics, as
  ansible-lint does through pathspec: `**` globs now work, and a plain entry
  matches a path name rather than any substring of it.

### Added - 2026-08-28

- `make release` now stamps the version pins in the adoption snippets
  (README, `.pre-commit-hooks.yaml`) as part of the release commit, and runs
  `scripts/release-preflight.sh` first, which refuses to release while the
  compatibility corpus the CI parity job clones is dirty or unpushed.

## [0.3.0] - 2026-08-28

### Fixed - 2026-08-28 (found on kubernetes-sigs/kubespray)

- A playbook whose first play is an `import_playbook` entry is now linted;
  it was previously skipped whole, later plays and their includes with it.
- A `roles/` subdirectory holding none of the five canonical role
  subdirectories is no longer reported by `role-name`, matching upstream's
  container-directory guard.
- pep8 rendering now reproduces upstream's console renderer as a stack
  machine instead of a subtag heuristic, so `[/]` artifacts land byte for
  byte, including the trailing one a bracketed message leaves.

### Added - 2026-08-24 (SARIF rule descriptors and scope)

- `-f sarif` now declares `tool.driver.rules`: every rule tag astl can report,
  with both identifier taxonomies, and a link to the rule's upstream page. A
  `result.ruleId` always resolves to one of them, so a consumer can render a
  rule name and a help link without shipping its own table.
- The run carries an `astl.scope` property naming the 38 rules astl implements
  and the 15 it does not, each with what reproducing it would require. This
  lets a consumer tell a rule that found nothing from a rule that never ran.
- Each descriptor carries `shortDescription`, astl's own one-line statement of
  the defect, the same sentence `docs/rules.md` publishes.
- The run declares `"columnKind": "unicodeCodePoints"`, which is what astl's
  column numbers count.
- [docs/sarif.md](docs/sarif.md) documents the output as an integration
  contract, including the one limitation it does not hide: a region is a point,
  and 93% of findings carry a line with no column.
- [ADR 0007](docs/adr/0007-sarif-outside-the-compatibility-contract.md) records
  that the SARIF output is not held to the pep8 compatibility contract and does
  not try to match ansible-lint's own SARIF.

### Added - 2026-08-24

- Documented installing a release binary directly, which needs neither Go nor
  Python. It was previously reachable only through the releases page.

### Changed - 2026-08-24

- README reordered around adoption: badges, a summary table and a "Try it on
  your repository" section (release binary, GitHub Action, pre-commit) now come
  first, and `make check` moved to a verification section near the end.
- Reference material moved out of the README, unchanged, into `docs/ci.md`,
  `docs/configuration.md`, `docs/exit-codes.md`, `docs/performance.md` and
  `docs/supply-chain.md`.

### Fixed - 2026-08-24

- The copy-paste snippets for the GitHub Action and the pre-commit hook pinned
  `v0.1.0` while the current release was `v0.2.0`.

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
