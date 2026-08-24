# 0007. SARIF sits outside the compatibility contract

Status: accepted

## Context

An editor-integration author asked whether astl exposes a stable
machine-readable result carrying a rule id, a source range, and an explicit
marker for the rules it deliberately does not evaluate. Answering meant
deciding what astl's SARIF output is *for*, and the repository had never said.

The port's invariant is written against one surface: under `--ids upstream`,
`-f pep8` reproduces ansible-lint's output byte for byte, and `make parity`
enforces it on a frozen corpus. Nothing states the equivalent for `-f sarif`,
and nothing compares the two tools' SARIF documents. That silence was being
read, by the author of this repository included, as "SARIF should match
upstream too". Diffing the two documents showed the assumption produces bad
decisions:

- ansible-lint declares `"columnKind": "utf16CodeUnits"`. It counts columns in
  Python string indices, which are code points, so the declaration is wrong for
  any character outside the BMP. Copying it would make astl assert something
  measurably untrue: on a line containing `ok_😀`, astl reports column 19 where
  a genuine UTF-16 count is 20.
- ansible-lint puts an absolute local path in `originalUriBaseIds`. In CI that
  writes the runner's directory layout into a published artifact, and relative
  URIs already resolve in both GitHub code scanning and the VS Code viewer.
- ansible-lint's rule descriptors carry its own prose in `shortDescription` and
  `help`. Reproducing upstream text verbatim is justified by ADR 0004 where a
  byte-for-byte contract requires it. No such contract covers these fields, so
  copying them would be reproduction with nothing asked of it.

Neither document is more correct than the other by default. They answer to
different readers: pep8 answers to a diff against upstream, SARIF answers to a
consumer that is not astl.

## Decision

**The pep8 output is the compatibility surface. SARIF is not.**

Verbatim upstream text is confined to what a contract requires:

- `result.ruleId` and `result.message.text` stay dual and follow `--ids`,
  defaulting to upstream, as ADR 0002 and ADR 0003 already specify. This is not
  copying for want of an alternative: an editor sharing a repository's
  `.ansible-lint` needs upstream ids to match its `skip_list` and its `# noqa`
  comments, and code scanning keys its deduplication on `ruleId`. Switching
  SARIF to native ids by default would break the interoperability that makes
  astl adoptable.
- Every other field is astl's own. Where a field needs prose, astl writes it.

Within that constraint, the SARIF document aims at being the best document for
a consumer, including where that means departing from upstream:

- `columnKind` is `unicodeCodePoints`, which is what astl actually counts.
- Rule descriptors are declared for every reportable tag, not only those a run
  referenced, and carry both taxonomies. A consumer learns the rule catalogue
  from any run, including a clean one.
- `shortDescription` carries astl's own one-line description of the defect, the
  same text `docs/rules.md` publishes, which a test holds to the table.
- The run declares `astl.scope`: the rules implemented and the rules
  deliberately not implemented, with what each would require. Upstream has no
  equivalent because it has no such boundary to declare.

Fields astl has no data for are left out rather than guessed.
`defaultConfiguration.level` is one: warning level is decided per finding, from
the experimental tag, `warn_list` and the ignore file, so there is no static
per-rule level to declare and `result.level` already carries the truth.
`properties.tags` is another: astl has no per-rule category table, upstream's
`idiom`/`formatting` tags playing no part in its selection logic.

## Consequences

- The absence of a SARIF parity gate is now deliberate rather than an
  oversight. `make parity` covers pep8 and says nothing about SARIF, correctly.
- A future divergence from upstream's SARIF is not a defect to file. It needs
  an argument about the consumer, which is the standard this ADR sets.
- `docs/sarif.md` is the contract a consumer reads, and it is astl's to keep
  stable: once published, a rule id and an `astl.scope` key do not change
  meaning.
- Native descriptions become a maintained surface. They live in the same table
  as the identifiers, and a test parses `docs/rules.md` to assert the two agree
  row for row, in the idiom the repository already uses for identifiers and
  messages.
- The claim that `--ids upstream` "keeps output byte for byte identical to
  ansible-lint's" has to be read as scoped to pep8 wherever it appears.
