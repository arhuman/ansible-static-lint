# The SARIF output

`astl -f sarif .` emits a SARIF 2.1.0 document, which GitHub code scanning
consumes and most editors can render. It validates against the published
`sarif-2.1.0` schema.

It is written for a consumer that is not astl: an editor plugin, a code
scanning dashboard, a policy gate. Such a consumer needs to know two things the
findings alone cannot tell it, so the document states both.

## Results

One result per finding.

```json
{
  "ruleId": "name[play]",
  "level": "error",
  "message": { "text": "All plays should be named." },
  "locations": [{ "physicalLocation": {
    "artifactLocation": { "uri": "examples/playbook.yml" },
    "region": { "startLine": 4, "startColumn": 3 }
  }}]
}
```

`level` is `error`, or `warning` for a rule upstream marks experimental, one
`warn_list` demoted, or one the ignore file kept without `skip`.

**A region is a point, not a range.** There is no `endLine` or `endColumn`, and
`startColumn` is absent entirely for the rules that report no column, which is
most of them: on the reference corpus, 93% of findings carry a line only. This
follows from the compatibility contract. Positions are ansible-lint's own,
reproduced byte for byte in the pep8 output, and upstream reports a point. An
editor anchoring on these will underline the whole line for most findings.

## Rule descriptors

`tool.driver.rules` declares every rule tag astl can report, whether or not the
run produced one. `result.ruleId` always resolves to one of them.

```json
{
  "id": "name[play]",
  "name": "name.play-missing",
  "shortDescription": { "text": "a play has no name" },
  "helpUri": "https://docs.ansible.com/projects/lint/rules/name/",
  "properties": { "upstreamId": "name[play]", "nativeId": "name.play-missing" }
}
```

`id` follows the run's `--ids` setting and `name` carries the other taxonomy,
while `properties` names both unconditionally. A consumer can therefore render
upstream ids while matching a repository's `.ansible-lint` `skip_list` written
in either, without shipping its own copy of the table. `helpUri` points at the
rule's upstream page; subtags have no page of their own and link their base
rule.

`shortDescription` is astl's own wording, not ansible-lint's, and is the same
sentence [rules.md](rules.md) publishes for that tag. A finding's
`message.text` is the other thing: it follows `--ids` and reproduces upstream's
text verbatim under the default. The distinction is [ADR
0007](adr/0007-sarif-outside-the-compatibility-contract.md): verbatim upstream
text appears only where a compatibility contract asks for it.

Two fields ansible-lint's own SARIF carries are deliberately absent.
`defaultConfiguration.level` would need a static per-rule level, and astl has
none: warning level is decided per finding, by the experimental tag, `warn_list`
and the ignore file, so `result.level` is where the answer lives.
`properties.tags` would need upstream's `idiom`/`formatting` categories, which
play no part in astl's selection and which it does not carry. Neither is
guessed at.

## Columns

The run declares `"columnKind": "unicodeCodePoints"`, which is what a column
number here counts: yaml.v3's scanner advances one column per code point.

ansible-lint declares `utf16CodeUnits` over Python string indices, which are
also code points. The two agree throughout the Basic Multilingual Plane and
disagree beyond it: on a line containing `ok_😀`, astl reports column 19 where a
genuine UTF-16 count is 20. astl states what it does rather than reproducing a
declaration it cannot honour.

## The scope declaration

The run carries `properties["astl.scope"]`:

```json
{
  "note": "astl reports only the ansible-lint rules decidable from YAML source alone. ...",
  "taxonomy": "upstream",
  "supported": ["name", "no-changed-when", "..."],
  "enabled": ["name", "no-changed-when", "..."],
  "outOfScope": [
    { "id": "fqcn", "requires": "resolving module names through Ansible's plugin loader" },
    { "id": "latest", "requires": "nothing: static, not implemented yet" }
  ]
}
```

This is the part that keeps a fast pass honest. Three lists answer three
different questions: `supported` names the 38 rules astl implements at all
(its capability boundary, including 5 opt-in rules the default profile does
not run), `enabled` names the subset this run actually turned on after
applying the profile, `skip_list` and `enable_list`, and `outOfScope` names
the 15 rules astl cannot run under any configuration. A report with no `fqcn`
finding does not mean `fqcn` passes: it means `fqcn` never ran. A consumer
that greys out the rules under `outOfScope`, or a `supported` rule missing
from `enabled`, or that declines to present the run as a full lint, is
reading the document correctly.

`enabled` is not "ran": a rule can be enabled and still produce no finding
because no file in the run triggers it. It says what was configured to run,
not what fired.

`supported`, `enabled` and `outOfScope` are always upstream ids, in the same
order and taxonomy, whatever `--ids` the run used, because a rule astl does
not implement has no native name and the lists have to stay directly
comparable. `requires` says what reproducing an out-of-scope rule would need,
and reads `nothing: static, not implemented yet` for the ones held back by
effort rather than by the static boundary. [scope.md](scope.md) quantifies
what each one costs.

## The invocation

The run carries `invocations`, one entry:

```json
{
  "executionSuccessful": true,
  "workingDirectory": { "uri": "file:///abs/path/to/repo/" }
}
```

Result `artifactLocation.uri` values are relative paths. `workingDirectory`
is the directory they are relative to, so a report that is saved or moved
away from the linted repository still resolves. The URI is an absolute
`file:` URI with a trailing slash, symlink-resolved, and is exactly the base
directory results were relativized against; nothing needs deriving from the
document's own location.

When the working directory cannot be determined, `invocations` is omitted
entirely rather than filled with a URI that resolves nowhere. Result paths
are then absolute already, so no base is needed. `executionSuccessful` is
always `true`: the document is only written once the run has completed.

## Stability

This document is not under the pep8 compatibility contract, and does not try to
match ansible-lint's own SARIF; [ADR
0007](adr/0007-sarif-outside-the-compatibility-contract.md) sets out why and
what governs it instead. What is promised is stability towards a consumer.
[ADR 0008](adr/0008-sarif-invocation-and-enabled-rules.md) applies that same
standard to `invocations` and to `astl.scope.enabled`.

The rule ids, both taxonomies, and the `astl.scope` keys, `enabled` included,
are part of the output contract: an id is never removed once published, and
`internal/rules/ids.go` is the single table both taxonomies and the
descriptions derive from. Adding a key to `astl.scope` or a field to an
`invocations` entry is compatible; a key's meaning does not change once
published. Tests assert that every reportable tag has a descriptor, that no
rule appears as both supported and out of scope, and that the descriptions
agree with `rules.md` row
for row.

The `outOfScope` list is maintained against a pinned ansible-lint version
rather than derived, since deriving it would need the runtime astl exists to
avoid. A test pins the total rule count so that a newer upstream adding a rule
fails the build rather than silently shrinking what the document declares.

Integration feedback is wanted, in particular on ranges and on anything an
editor needs that is not here yet:
[open an issue](https://github.com/arhuman/ansible-static-lint/issues).
