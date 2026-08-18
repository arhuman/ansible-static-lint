# 0005. Port yamllint's checks and vendor yaml.v3's scanner for yaml[*] and var-naming

Status: accepted

## Context

Measured across real failing CIs, the rule families that block pipelines are
dominated by two that astl excluded: `yaml[*]` (yamllint embedded whole by
ansible-lint) and `var-naming` (dependent on ansible-core's reserved-name
lists). docs/scope.md drew the boundary at both: `yaml` behind "a second
embedded tool", `var-naming` behind "tracking ansible-core internals as data".
Neither needs an Ansible runtime; the exclusions were about maintenance
surface, not about the no-runtime principle of ADR 0001.

Reproducing yamllint faithfully has two hard dependencies:

- yamllint's token rules consume pyyaml's scanner tokens, with rune-exact
  marks. Go has no library exposing an equivalent stream, but
  gopkg.in/yaml.v3 embeds a full port of libyaml's scanner, the same design
  pyyaml implements, without exporting it.
- yamllint is GPL-3.0-or-later, like ansible-lint, and its problem
  descriptions surface verbatim in ansible-lint's output, which the parity
  contract reproduces byte for byte.

## Decision

Implement both families. Concretely:

- Vendor the scanner portion of gopkg.in/yaml.v3 (scannerc.go, yamlh.go,
  readerc.go, apic.go, yamlprivateh.go) into `internal/yamlscan`, license
  headers and NOTICE preserved (MIT for the libyaml-derived files, Apache-2.0
  for the rest; both MIT-compatible with attribution). Two deliberate
  deviations restore pyyaml's mark placement where yaml.v3 had moved it for
  its comment-attribution feature: BLOCK-END tokens sit at the scanner's
  current mark, and stream end does not force a new line. Both carry `[astl]`
  comments in the vendored source. A committed golden test pins the token
  stream against pyyaml dumps, and the whole corpus was differentially
  verified token by token.
- Reimplement yamllint 1.38.0's checks in `internal/yamllint` from observed
  behavior and the reference implementation. No yamllint code or test data
  enters this repository; the bundled `default` and `relaxed` policies and
  ansible-lint's own overrides are re-expressed as Go tables rather than
  copied as YAML text.
- The verbatim yamllint problem descriptions (capitalized the way
  ansible-lint renders them) are embedded on the same basis ADR 0004
  established for ansible-lint's messages: short functional phrases required
  byte for byte by the compatibility contract, each paired with an original
  native wording under `--ids native`.
- `var-naming`'s data tables (python keywords, ansible reserved names,
  read-only and allowed special variables, role keywords) are vendored as Go
  tables pinned to the reference versions, with the extraction procedure
  recorded in the design document.

The full behavioral contract and the probe evidence are in
docs/design/static-yaml-and-var-naming.md.

## Consequences

- docs/scope.md's boundary moves: 38 of 51 rules, and the real-world
  coverage argument that motivated the change is recorded there.
- astl now carries a second parser pass per file. It first cost 31 ms to
  51 ms on the corpus; profiling attributed most of that to allocation
  pressure rather than scanning, and streaming the token pass (a four-token
  ring instead of materialized slices), pooling the scanner and linter, and a
  lazy-GC-with-memory-ceiling default brought it to 37 ms, a quarter of the
  150 ms bench budget. The scan's inherent single-threaded cost is ~10 ms.
- The vendored scanner is upstream-styled code: it is excluded from the
  repo's lint gates and from the coverage ratchet's denominator, and it is
  exercised end to end by the yamllint golden tests and the parity harness
  instead.
- The maintenance treadmill the old boundary avoided now exists and is
  pinned: yamllint 1.38.0 semantics, ansible-core 2.21.3 name tables. Moving
  the ansible-lint pin means re-extracting tables and re-running the
  differential validation, and the design document records how.
- A repository's own `.yamllint` is read and layered over the bundled policy,
  as ansible-lint does. This was deliberately deferred at first and reversed
  the same day: the check_repo rerun measured 8918 false positives on debops
  without it (issue 0003). `ignore`/`ignore-from-file` were deferred in the
  same way and reversed for the same reason once measured, at 4 false
  positives on idealista/kafka_role (issue 0006); they are applied with git's
  pattern semantics, scoped to the yaml family so that an ignored file is still
  linted by every other rule. Five yamllint rules stay unimplemented and are
  warned about on stderr rather than passed over silently.

## Alternatives considered

- Keep both families out (the prior boundary). Rejected on the check_repo
  evidence: roughly half of real-world CI-blocking findings sit in these two
  families, which caps astl's usefulness in exactly the environments it
  targets.
- Wrap an existing Go YAML lexer (goccy/go-yaml) instead of vendoring
  yaml.v3's scanner. Its token model is surface-level and does not match
  pyyaml's grammar-level tokens (block mapping starts, implicit keys, block
  ends), which yamllint's rules are written against. Rebuilding that mapping
  would be a scanner rewrite with extra steps. Rejected.
- Reimplement only the line-based yamllint rules and skip the token rules.
  Loses truthy, indentation, key-duplicates and octal-values, which the
  real-world evidence ranks at the top. Rejected.
- Apply the bundled policy unconditionally and treat a repository's own
  `.yamllint` as out of scope. This was the original decision, and the
  measurement that followed rejected it: 2 of the 4 repositories checked ship
  such a file, and on one of them the mismatch buried every true finding.
- Depend on pyyaml at runtime for tokenization. Contradicts the no-runtime
  principle outright. Rejected.
