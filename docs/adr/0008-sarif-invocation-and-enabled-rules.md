# 0008. SARIF invocation context and enabled-rule declaration

Status: accepted

## Context

An external integrator prototyping a JetBrains adapter against
`docs/sarif.md` reported two gaps while resolving results back to files on
disk and rendering the scope block:

- A saved or moved SARIF report has no way to tell what a result's relative
  `artifactLocation.uri` is relative to. The directory is known at run time
  (`discover.WorkingDir()`, the base every result path is already
  relativized against), but the document never states it.
- `astl.scope.supported` lists every rule astl can implement, including five
  opt-in rules the default profile does not run. A consumer reading only
  `supported` cannot tell which rules actually ran for a given invocation:
  `skip_list`, `enable_list` and the profile all change that set, and the
  report gave no way to recover it without re-deriving the run's
  configuration.

ADR 0007 already sets the standard for this document: prefer a standard SARIF
field over inventing an `astl.*` property, and omit a field astl has no data
for rather than guess. Both gaps are answered by applying that standard
rather than by a new rule.

## Decision

**Invocation context.** `SARIF` now emits the standard `runs[].invocations`
array with one entry, `{"executionSuccessful": true, "workingDirectory":
{"uri": ...}}`. The URI is `discover.WorkingDir()` rendered as an absolute
`file:` URI with a trailing slash, symlink-resolved, and is exactly the
directory result paths were made relative to. When the working directory
cannot be read (`os.Getwd` failing), `invocations` is omitted entirely rather
than filled with a URI that resolves nowhere; result paths are then absolute,
so nothing needs a base. `executionSuccessful` is always `true`: the document
is only written once the run has completed, and a run that fails outright
exits before any format is rendered.

**Enabled-rule declaration.** `astl.scope` gains an `enabled` array: the
rules this run could actually report, computed by `rules.EnabledRules` after
applying the profile, `skip_list` and `enable_list`. A rule is enabled when
`skip_list` does not name it, and either `enable_list` names it or the
profile keeps it and it is not one of the opt-in rules; `skip_list` beats
`enable_list`, and `warn_list` is not consulted because it sets a finding's
level, not whether the rule runs. Ids are bare upstream spellings, in the
same order and taxonomy as `supported`, regardless of the run's `--ids`
setting, so the two lists stay directly comparable.

The name is `enabled`, not `ran` or `selected`. An enabled rule can still
produce no finding because no file in the run triggers it; the field states
configuration, not execution outcome. `supported` keeps its existing meaning,
astl's implementation boundary, and does not change: `enabled` is always a
subset of it.

## Consequences

- A consumer resolving `artifactLocation.uri` against a report read from disk
  no longer needs the invocation's original current directory out of band.
- Greying out a rule under `outOfScope` was already correct; a consumer can
  now also grey out a `supported` rule this run turned off, by checking it
  against `enabled` instead of assuming `supported` implies it ran.
- `docs/sarif.md` documents both keys as part of the stability contract set
  by ADR 0007: `invocations.workingDirectory` and `astl.scope.enabled` do not
  change meaning once published.
- No parity or compatibility surface is affected: both changes are additive
  fields inside the SARIF document, which ADR 0007 already excludes from the
  pep8 byte-for-byte contract.

## Alternatives considered

- An `astl.workingDirectory` property, mirroring `astl.scope`. Rejected:
  SARIF already has a standard field for exactly this, and ADR 0007's
  standard is to use it instead of inventing one.
- Naming the new array `ran`. Rejected as misleading: it would read as an
  execution report, and astl has no per-rule "did this actually match
  anything" signal to back that claim.
- Making `enabled` follow `--ids` like `result.ruleId` does. Rejected for
  consistency with `supported`, which is deliberately taxonomy-fixed so the
  two lists can be diffed directly without a consumer normalizing ids first.
