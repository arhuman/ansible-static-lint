# 0010. One project root confines the walk and the includes, as upstream's project_dir

Status: accepted

## Context

ansible-lint confines a run to a project directory. `Runner.is_excluded`
(`src/ansiblelint/runner.py`) skips any lintable that was not named on the
command line and whose resolved path does not start with `project_dir`. It
applies that test both to the files directory discovery finds and to the
children an include or import names. Lintable paths are resolved in
`Lintable.__init__`, so a symlink is judged by its target.

astl had no such notion. Its walk followed a symlink in the linted tree to a
file anywhere on the host, read it, and printed its path and content-derived
names. Its include expansion was confined to the working directory instead,
which is narrower than upstream: run from a subdirectory of a repository, it
dropped an in-repository `../common.yml` that ansible-lint lints. The two
boundaries disagreed with upstream in opposite directions because there was no
single one.

Upstream computes `project_dir`, when neither `--project-dir` nor the
`project_dir` key sets it, with `find_project_root` (`src/ansiblelint/file_utils.py`), called
from `src/ansiblelint/cli.py` **after** the configuration is loaded:

1. Resolve every command-line path, and take their deepest common ancestor. A
   directory counts as its own ancestor, a file does not.
2. Try that base and each of its ancestors in turn. The first holding a `.git`
   (any kind of entry), a `.hg` directory, or a configuration file (a regular
   file) is the root.
3. The configuration files looked for are `CONFIG_FILENAMES` when no
   configuration was loaded. When one was, `load_config` has already replaced
   `options.config_file` with the absolute path of the file it loaded, whether
   `-c` named it or the search found it, so that file is the only one looked
   for. Being absolute, it matches at the first candidate where no `.git` or
   `.hg` comes first, and the root is then its directory, or that directory's
   parent when it is named `.config`.
4. If nothing matches, the root is the filesystem root.

Point 3 was measured against ansible-lint 26.8.0, not inferred: with
`proj/.ansible-lint` and `proj/sub/.ansible-lint`, running `ansible-lint sub`
from `proj` reports `proj` as the root, because the loaded file is
`proj/.ansible-lint`. So is its consequence: with `-c` naming a file outside a
tree that has no `.git`, the root is that file's directory, and a walk of `.`
lints nothing while a file named on the command line is still linted.

## Decision

astl computes the root once per run, with upstream's search, and confines both
the walk and include expansion to it.

- `discover.ProjectRoot(srcs, cfgFiles)` implements steps 1 to 3. `cmd/astl`
  calls it with the command-line paths and either the absolute path of the
  configuration in use (the `-c` file, or the one `config.Find` locates) or,
  when there is none, `config.Filenames()`, the same list the configuration
  search uses.
- The same value is passed to `discover.WalkWithin` and
  `discover.ExpandIncludesWithin`. A walked file or include child whose
  resolved path lies outside it is skipped silently, as upstream skips it.
- A path named on the command line is always linted, wherever it resolves, as
  upstream never excludes an explicit lintable.

Two divergences are deliberate. Both only ever make astl read less than
upstream, and both exist because astl runs over repositories the operator does
not control (SECURITY.md).

- **No filesystem-root fallback.** When nothing marks a project, upstream's root
  is `/`, which confines nothing: a symlink in an unmarked tree reaches the whole
  host. astl falls back to the common base of the command-line paths instead.
  A tree with no `.git`, no `.hg` and no configuration is rare in CI, where
  checkouts carry `.git`.
- **Containment by path component.** Upstream tests `abs_path.startswith(project_dir)`,
  so `proj-evil/x.yml` is inside `proj`. astl compares resolved paths component
  by component, so a sibling sharing the root's prefix is outside.

Out of scope:

- **Configuration lookup is unchanged.** Upstream loads its configuration before
  it computes the root and independently of it, and so does astl. The root only
  consumes which file was loaded.
- **`--project-dir` and the `project_dir` key.** astl supports neither today, so
  the root is always the computed one. Adding them is a separate decision.

## Consequences

- Output on real repositories is unchanged, which the parity corpus confirms: a
  checkout carries `.git` at its top, and everything a walk or an include
  reaches there was already inside it.
- A symlink to a file outside the project, and an include climbing out of it,
  are no longer read. An include climbing out of the working directory but
  staying in the repository is followed again, under its absolute path, as
  upstream prints it.
- `-c` with a file outside a tree that has no `.git` confines a directory walk
  to that file's directory, which lints nothing from the tree. This is
  upstream's behaviour and is reproduced, not repaired.
- `Walk` and `ExpandIncludes` keep their signatures for callers with no
  configuration: `Walk` confines to `ProjectRoot(roots, nil)` and
  `ExpandIncludes` to the working directory. The run uses the `Within` variants.
