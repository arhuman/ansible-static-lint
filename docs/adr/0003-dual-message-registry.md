# 0003. Dual message registry selected by the existing --ids flag

Status: accepted

## Context

ADR 0002 gave astl its own rule identifiers while keeping ansible-lint's as permanent aliases, and noted the asymmetry it left open: identifiers are load-bearing configuration to be preserved, messages are expressive content to be rewritten. Until now only the identifiers were dual. Every finding carried exactly one message, ansible-lint's, reproduced verbatim.

Verbatim is what the port is for. Under the default taxonomy astl's `-f pep8` output must be byte for byte identical to ansible-lint's, which `make parity` proves against a frozen corpus in a sibling repository. The message text is part of that line, so it cannot be edited.

It is also, read on its own terms, not very good. `Avoid implicit behaviors` does not say which behavior. `Disallow prompting` names a policy rather than a defect. `File permissions unset or incorrect.` leaves the reader to guess which. None of them says what to do next, and a linter's message is read at the moment the reader wants exactly that.

## Decision

astl carries two message registries, selected by the flag that already selects the identifier taxonomy.

- `--ids upstream`, the default, prints ansible-lint's wording verbatim, as before. No byte of the default output moves.
- `--ids native` prints the native wording, in pep8 and in the SARIF `result.message.text` alike.

The two wordings are written side by side at the point the finding is constructed. `Finding` gains a `NativeMessage` field next to `Message`, and the five constructors (`at`, `onLine`, `whole`, `warnAt`, `warnOnLine`) take both strings as adjacent arguments. `Finding.MessageFor(style)` performs the selection so both output formats share one rule.

The native messages follow a fixed grammar: the observed defect as fact, then an imperative fix. Interpolated values are the ones upstream interpolates, where they survive the length budget below. No em or en dashes.

Three properties are enforced by tests that AST-scan the rules package, the same technique ADR 0002 used for identifier coverage:

- **Completeness.** Every finding construction carries both wordings, and every equivalence-table row bearing a subtag is either emitted with a message or named in a short `notEmitted` list (today: `name[prefix]`, which upstream raises only under a configured prefix policy).
- **Distinctness.** No native message equals its upstream counterpart, case-insensitively. A message that merely echoed upstream would give the reader nothing the default output does not already say.
- **Budget.** Every native message fits 100 runes once interpolation is rendered. GitHub code scanning annotations, editor problem panels and narrow CI terminals truncate past roughly that width, and truncation removes the tail of the string, which is exactly where the fix sentence lives.

## Consequences

- The default output is unchanged, so the parity contract and the compatibility repository need no edits. `make parity` is the proof, and it is the gate that matters for this change.
- `--ids native` becomes a coherent second presentation rather than a half-measure: identifiers and prose in one vocabulary.
- Cost: a new rule must word its defect twice. The AST tests turn a forgotten second wording into a test failure rather than a silently empty message.
- The budget forced several messages to drop values upstream interpolates: role paths, tag lists, sanity test names, the full key order. Those are unbounded user content, and the finding's own position already points at them.
- A finding with no `NativeMessage` falls back to the upstream text rather than printing an empty line. The completeness test is what keeps that fallback unreachable in practice.

## Alternatives considered

- **A separate `--messages` flag.** Two flags admit four combinations, two of which are incoherent (native identifiers with upstream prose, and worse, the reverse). The taxonomy a reader chooses is a single editorial choice; one flag expresses that. Rejected.
- **Making the native messages the default.** Breaks the byte-for-byte contract that is the reason the port exists. Rejected.
- **A lookup table keyed by rule id, populated at output time.** Several rules emit different messages under one tag (`meta-no-tags` has five, `meta-video-links` three), so the key would have to be a hand-invented message id: a second list maintained by hand, which is what ADR 0002 deliberately avoided, and which lets the pair drift apart in the source. Rejected in favour of keeping both wordings adjacent at the call site.
