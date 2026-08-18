# 0004. License basis for embedding ansible-lint's verbatim message text

Status: accepted

## Context

Under `--ids upstream` (the default, ADR 0003), every finding's message is
ansible-lint's exact diagnostic string, embedded as a Go string literal at the
point the finding is constructed: for example
`"Commands should not change things if nothing needs doing."` in
`internal/rules/task.go`. That text is copied character for character from
ansible-lint's Python source (`rules/*.py`), which is GPL-3.0-or-later.

Project documentation (NOTICE.md and README.md in this repository and in
astl-compatibility-check) stated that astl "contains none of ansible-lint's
source". That claim does not survive a literal reading against the fact above
and has been corrected. This ADR records the actual reasoning for keeping the
verbatim strings, which was never written down: ADR 0001 addressed reuse of
ansible-lint's semantics, ADR 0002 addressed identifiers, ADR 0003 addressed
the message architecture, and none of the three addressed the license status
of the message text itself.

## Decision

Keep the verbatim upstream strings under the default output mode. The basis:

- Each message is short (typically under fifteen words), states one fact about
  the input in close to the only natural way to state it, and is functional
  rather than expressive. Copyright protects expression, not the small set of
  ways a fact this constrained can be phrased; that convergence is what merger
  doctrine and scenes-a-faire describe.
- They exist for output compatibility, which is the reason the port exists
  (ADR 0001): a caller matching on message text, not just rule id, gets the
  same string from either tool.
- `--ids native` is a complete, original restatement of every one of them
  (ADR 0003). Its existence is evidence that astl's output does not depend on
  copying ansible-lint's expression: an independent expression of the same
  fact was available and was written.

## Consequences

- Project documentation states this precisely rather than denying it: no
  corpus, no golden data, no functional code taken from ansible-lint, but yes,
  under the default mode, ansible-lint's exact diagnostic strings.
- Residual risk, named rather than hidden: the individual-message argument
  above does not obviously extend to the full set of roughly sixty messages
  taken together. A compilation of otherwise-thin expressions can attract its
  own protection as a selection and arrangement. No case law directly on this
  fact pattern was identified. The mitigant is practical, not doctrinal:
  `--ids native` is a ready, complete substitute if this posture is ever
  challenged, not a preemptive defense.
- If the posture changes, the fix is mechanical and already designed for: flip
  the default to `--ids native` (ADR 0003 named and rejected this for the
  parity contract's sake, not for license reasons; that tradeoff would need to
  be revisited together with this one), or move the verbatim strings out of
  this repository entirely, into astl-compatibility-check, loaded at
  build or run time. Both remain available; neither is done today.

## Alternatives considered

- State no overlap exists (the prior documentation). Factually wrong once the
  verbatim strings are accounted for. Rejected.
- Move the verbatim strings into astl-compatibility-check and load them
  externally for the default mode. Removes the question entirely but makes
  astl's default behavior depend at runtime on data from a GPL repository,
  which contradicts it shipping as a standalone MIT binary. Rejected for now;
  available if the residual risk above is judged unacceptable.
- Relicense astl GPL-3.0-or-later. Removes the question entirely at the cost
  of the reason a permissive port exists. Already rejected in the project's
  original license strategy; rejected again here for the same reason.
