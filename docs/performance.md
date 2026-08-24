# Performance

Why astl is fast, what that costs, and what guards the property.

## Measurements

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

## Where the time goes

The number that matters architecturally: the 36 rules that work off the parsed
document produce no measurable slowdown between them, because a rule is a
predicate over an already-parsed document and its marginal cost is
microseconds.

The one rule family that does cost something is `yaml[*]`, which runs a second,
token-level scan of every file for the yamllint checks. After streaming that
scan through a fixed four-token window and adopting a
lazy-GC-under-a-memory-ceiling posture suited to a lint-and-exit process
(`GOGC`/`GOMEMLIMIT` in the environment still win), the corpus sits at 37 ms
against 31 ms before the family existed.

The memory ceiling keeps the trade bounded: 42 MiB on the corpus, under 100 MiB
on a 4000-file monorepo. astl's cost remains startup, I/O and the two parses,
and stays nearly independent of how many static checks run on top.

## The guard

That property is guarded, not assumed: `make bench` fails the build if linting
the reference corpus exceeds 150 ms, roughly five times the current time. It
runs in CI on every pull request, and `make ci` includes it.

`make perfguard` is the second speed guard, asserting that noqa resolution stays
linear. It sits behind the `perfguard` build tag because it reads wall-clock
time, so it never gates the audit.
