# 0009. Discovery exclusions follow upstream, so `.venv` is not special-cased

Status: accepted

## Context

[Issue #5](https://github.com/arhuman/ansible-static-lint/issues/5) reported
that astl lints a `.venv/` directory in kubespray and asked whether astl should
ignore it by default, on the belief that ansible-lint does.

It does not. Run directly on the reproducer's tree, ansible-lint 25.x flags
`.venv/test.yml`, and it still does with kubespray's own `.gitignore` and
`.ansible-lint` in place: neither file names `.venv`, and upstream's builtin
exclusion list does not contain it. The observed difference had a different
cause: kubespray pins its ansible-lint pre-commit hook to `stages: [manual]`,
so `pre-commit run --all-files` runs astl and never runs ansible-lint. The
comparison was against a linter that had not executed.

The report still pointed at a real gap. Upstream's `get_all_files`
(`src/ansiblelint/file_utils.py`) excludes, at every directory it expands:

- a builtin gitignore-style list: `.ansible`, `.git`, `.tox`, `.mypy_cache`,
  `__pycache__`, `.DS_Store`, `.coverage`, `.pytest_cache`, `.ruff_cache`,
  compiled into **one** pathspec together with the user's `exclude_paths`;
- the directory's own `.gitignore`, when present.

astl's walk hardcoded two directory names, `.git` and `__pycache__`, and read
no `.gitignore`. On kubespray that is a visible divergence without any `.venv`
involved: its `.gitignore` lists `venv/`, which upstream skips and astl linted.

## Decision

Do not special-case `.venv`. Ignoring it would diverge from upstream in the
direction the issue happened to want, and the parity invariant does not bend
for sympathy. Instead, reproduce upstream's discovery exclusions exactly,
including the behaviours that look like defects:

- The builtin list and `exclude_paths` compile into a single spec, so a user
  `!pattern` can re-include something the builtins excluded, as it can
  upstream.
- A directory's `.gitignore` applies only to that directory's **immediate
  children**. Upstream rebuilds its pathspecs from scratch on each recursion,
  so a root `.gitignore` pattern never reaches a grandchild. Reproduced, not
  repaired.
- Patterns are matched against the path as accumulated from the walk root, so
  an anchored pattern such as `/build` inside `sub/.gitignore` never matches
  `sub/build`. Unanchored patterns still match, because gitignore segment
  matching is unanchored. Also reproduced.
- Directory candidates are matched with a trailing slash appended (upstream's
  `pathspec.util.append_dir_sep`), which is what makes a dir-only pattern like
  `build/` prune the directory itself.
- Explicitly passed paths are exempt: a file named on the command line is
  linted with no exclusion check at all, and a directory named there is never
  matched against the specs itself, only its descendants are.

Include expansion is deliberately left alone. Upstream filters include
children through `exclude_paths` only (`Runner.is_excluded`); the builtins and
`.gitignore` live solely in directory discovery. astl's `ExpandIncludesWithin`
already mirrors that split.

Known residual divergence, carried knowingly: upstream follows directory
symlinks during its walk, `filepath.WalkDir` does not. Pre-existing, out of
scope here.

## Consequences

`.venv/test.yml` is still flagged, and that is the correct answer to the
issue: it is what ansible-lint prints too. A repository that wants it silenced
writes `.venv` into `exclude_paths` or its `.gitignore`, and both now work.

`internal/yamllint`'s pattern matcher gains a dir-aware entry point
(`MatchEntry`), since its existing `match` only ever saw file paths and a
dir-only pattern could not match the directory entry itself. The yamllint
`ignore:` path keeps the old semantics untouched.

The parity corpus cannot regress from this: its reference outputs were
produced by real ansible-lint runs, so matching upstream's discovery more
closely can only move astl toward the bytes already frozen.
