# 0001. Static-only linting scope, no embedded ansible runtime

Status: accepted

## Context

ansible-lint gets its semantic depth from embedding ansible-core: module argspec validation, jinja templating, plugin resolution, and an `ansible-playbook --syntax-check` subprocess per playbook. That embedding is also the source of its cost: 0.52 s cold start and 2.1 s to lint a 6-line file. Reimplementing ansible-core semantics in Go would take months and chase a moving target.

## Decision

astl lints statically only: parsed YAML plus rule logic, no ansible runtime, no subprocesses. The MVP implements the 15 highest-frequency static rules measured on the upstream examples corpus (87% of findings). The syntax-check phase, `--fix`, jinja evaluation, argspec and schema validation, and custom rule plugins are out of scope.

## Consequences

- 144x faster cold start, 376x faster single-file lint, 19 MiB RSS (measured in the parent workbench).
- A known set of extra findings on the reference corpus versus upstream, all on files ansible's own loader rejects (28 at the time of this decision, growing with each rule added). The astl-compatibility-check repository documents them and pins the exact set.
- astl cannot claim full ansible-lint equivalence and should be positioned as a fast first-pass linter for editors, pre-commit, and CI hot loops, with ansible-lint as the deep pass.

## Alternatives considered

- Embedding a Python interpreter (cgo/embedded CPython): keeps the startup cost the port exists to remove.
- Reimplementing module/role resolution in Go: months of effort for the last 1.2% precision; revisit only if adoption demands it.
