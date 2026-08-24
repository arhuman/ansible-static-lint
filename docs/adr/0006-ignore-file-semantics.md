# 0006. Reproduce `.ansible-lint-ignore` semantics, including the parts that read like defects

Status: accepted

## Context

A user reported that astl lints what their `.ansible-lint-ignore` silences. They
reached for that file because `# noqa` cannot reach every finding: one reported
against a file as a whole has no line to carry the comment. They used the `skip`
qualifier specifically so the finding would not be reported at all.

The file is also how a repository adopts a linter without fixing everything
first, which is why upstream gives it two strengths. That distinction is not
decorative, and the weaker of the two is the surprising one:

- `path rule` **keeps** the finding, marks it `ignored`, and demotes it to
  warning level. It still prints, and it prints **before** every other finding,
  because ansible-lint partitions its matches into an ignored block and a fatal
  block and renders them in that order. What changes is the exit code: warnings
  do not fail a run, so a repository whose only findings are ignored ones exits
  0.
- `path rule skip` **removes** the finding outright.

An implementation that read "ignore" as "drop" would therefore be wrong on the
common case: it would produce the right exit code by the wrong route and print
the wrong bytes, which is precisely what this port exists not to do.

The parser upstream applies to that file is idiosyncratic, and several of its
behaviours look like bugs. They are still what a repository's file was written
against, so a file that works with ansible-lint has to work identically here.

## Decision

Reproduce the semantics and the parser, and diverge only where upstream crashes
or is non-deterministic.

Reproduced deliberately, and not to be "fixed":

- Comments are cut at the **first `#` anywhere on the line**, not at a space
  followed by one, with no escape. A path containing `#` is unusable.
- Any run of whitespace separates the columns, so a path containing a space
  cannot be expressed at all.
- The qualifier is read only from a line of **exactly three** fields. A fourth
  field silently disarms the `skip`.
- No normalization is applied to the path column, while the finding's own path
  is normalized. `./play.yml` therefore never matches.
- Matching is exact on the full tag: `yaml` does not cover `yaml[indentation]`.
- An unknown rule id is accepted in silence and simply never fires. Upstream's
  own file carries one.
- Discovery reads `.ansible-lint-ignore`, then `.config/ansible-lint-ignore.txt`,
  **in the working directory only**. There is no walk up to a repository root,
  whatever upstream's documentation says, and the second name is an alternative
  rather than a supplement: the first found wins and they are never merged.

Diverged from deliberately:

- A line naming a path and no rule raises an `IndexError` upstream that its own
  handler does not catch, so ansible-lint dies with a traceback. astl reports
  the file and line and exits 1. Same refusal, said usefully.
- An unknown qualifier is a `RuntimeError` upstream; astl names the qualifier.
- `-i` on a missing file logs `Ignore file not found 'None'` upstream, which
  interpolates the wrong variable. astl names the path. Both continue the run:
  unlike a missing config, a missing ignore file cannot lint the repository
  under the wrong policy, it can only report findings already known.
- A path carrying the same rule twice, once bare and once `skip`, is stored in a
  set upstream and answered with whichever entry iteration reaches first, so its
  verdict is unstable. astl lets `skip` win.
- `ignore_file` is not a declared option upstream; it reaches its config object
  through a catch-all, and there the file overrides the flag. astl declares the
  key and lets `-i` win, which is the precedence every other option already has.
- The two stderr lines upstream logs around the blocks (`Listing N violation(s)
  marked as ignored...`) are not reproduced. astl does not reproduce upstream's
  logging, and the parity contract is written against `-f pep8` on stdout.

`--generate-ignore` is out of scope: it writes into the repository being linted,
and astl is built to run over repositories the operator does not control.

## Consequences

`Finding` gains an `Ignored` flag, separate from `Warning` because a rule
demoted by `warn_list` is also a warning yet belongs in the second block. One
flag could not carry both. `format.PEP8` makes two stable passes over the
already-sorted findings rather than one.

The corpus cannot cover this. The harness runs the linter from the repository
root, where an ignore file would apply to every other case at once, and upstream
resolves the file from the working directory only. The feature is instead pinned
by CLI tests whose expected bytes were taken from ansible-lint 26.8.0 itself,
run against the same fixture from the same directory: both forms, the block
ordering, the ` (warning)` suffix and exit 0 were confirmed identical.
