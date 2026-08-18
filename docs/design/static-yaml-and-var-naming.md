# Design: static `yaml[*]` and `var-naming[*]`

Status: approved for implementation.
Reference implementation: ansible-lint 26.8.0, yamllint 1.38.0, ansible-core 2.21.3
(the pinned versions in `../bench/venv`, outside this repository). Upstream
source files named below (`linter.py`, `parser.py`, `var_naming.py`, rule
modules) refer to that venv's yamllint package and to the ansible-lint clone
in `../original`, never to files in astl.

## 1. Why

Measured across real failing CIs (five check_repo runs plus a multi-repo
scan), the rule families that actually block pipelines are dominated
by two families astl excludes:

- `yaml[*]` is the top blocker overall (line-length, trailing-spaces,
  empty-lines, truthy, indentation). It is fully static; upstream excludes it
  from its own rule set only because it delegates to yamllint.
- `var-naming[*]` is next (98% of debops's failures, 19/86 on 12urenloop).
  Reading its implementation shows every subtag it can emit today is decidable
  from YAML source plus fixed data tables. No Ansible runtime is involved.

Implementing both moves astl's raw real-world match from roughly 45% toward
80% or more without touching the no-runtime principle (ADR 0001). This
supersedes the softer boundary drawn in `docs/scope.md`, which classified
`var-naming` as "tracking ansible-core internals as data" and left it out; the
real-world evidence says the treadmill is worth it. `docs/scope.md` is updated
as part of this work.

## 2. Scope

### In scope

`yaml[*]`, reproducing yamllint 1.38.0 exactly as ansible-lint embeds it, with
ansible-lint's own effective configuration (its bundled `.yamllint`, which
extends yamllint's defaults). The enabled rules and their effective settings:

| yamllint rule | Effective settings (differences from yamllint defaults marked) |
|---|---|
| anchors | forbid-undeclared-aliases only |
| braces | min-spaces-inside 0, max-spaces-inside 1 (upstream override) |
| brackets | min 0, max 0 |
| colons | max-before 0, max-after 1 |
| commas | max-before 0, min-after 1, max-after 1 |
| comments | require-starting-space, ignore-shebangs, min-spaces-from-content 1 (override) |
| empty-lines | max 2, max-start 0, max-end 0 |
| hyphens | max-spaces-after 1 |
| indentation | spaces consistent, indent-sequences true, no multi-line-string checks |
| key-duplicates | merge keys allowed to repeat |
| line-length | max 160 (override), allow-non-breakable-words, no inline-mapping exemption |
| new-line-at-end-of-file | on |
| octal-values | forbid implicit and explicit (override; disabled entirely in yamllint defaults) |
| trailing-spaces | on |
| truthy | allowed-values [true, false], check-keys |

Disabled by ansible-lint's config. `comments-indentation` and `document-start`
are nonetheless implemented, because a repository config that says
`extends: default` puts them back (section 2, out-of-scope note). The rest are
off in every bundled policy and stay unimplemented: document-end,
empty-values, float-values, key-ordering, quoted-strings.

`new-lines` is enabled upstream but can never fire: ansible-lint reads file
content with Python universal newlines, so yamllint never sees a `\r`. It is
not implemented; the buffer normalization in section 4.1 preserves the
equivalence.

`var-naming[*]`, all subtags the upstream code can actually emit:

| Subtag | Trigger |
|---|---|
| pattern | name does not match `^[a-z_][a-z0-9_]*$` |
| no-keyword | name is a Python keyword |
| non-ascii | name does not encode to ASCII |
| no-reserved | name is in Ansible's reserved-names table |
| read-only | name is in the read-only special-variables table |
| non-string | mapping key is not a string |
| no-role-prefix | var defined in role context lacks `<role>_` prefix |

`var-naming[no-jinja]` is dead code in 26.8.0 (`"{{" in ident` returns early,
before any check could fire); astl does not implement it and the equivalence
table reserves no row for it.

### Out of scope, unchanged

`syntax-check`, `fqcn`, `jinja`, `schema`, `latest` and everything else ADR
0001 excludes. Also out of scope for this iteration:

- ~~Honoring a repository's own `.yamllint` override file.~~ **Reversed**: the
  check_repo rerun measured 8918 false positives
  on debops without it, so it shipped as issue 0003's fix. `internal/yamllint`
  takes a `Config`, and a loader reproduces ansible-lint's resolution
  (search order, `extends: default`/`relaxed`, the `enable`/`disable`/bare
  `false` forms, and the default back-filling that makes a partial rule
  override reset its siblings). `document-start` and `comments-indentation`
  had to be implemented, since any `extends: default` re-enables them over
  ansible-lint's disable, and line-length's inline-mapping exemption had to be
  completed for `extends: relaxed`. Still unread: `ignore`/`ignore-from-file`
  patterns, and five rules that are off in every bundled policy
  (`quoted-strings`, `key-ordering`, `empty-values`, `float-values`,
  `document-end`); both warn on stderr instead of failing silently.
- yamllint syntax-error problems (`rule=None`). Empirically upstream reports
  unparsable files through `load-failure` and emits no `yaml[*]` findings for
  them, which is exactly astl's existing "parse failure means silence"
  behavior. Nothing to add.

## 3. Upstream behavior contract

Everything below was verified empirically against the pinned reference
versions, not inferred from documentation. These are the parity-critical
facts the implementation must reproduce. The configuration-merge facts added
by issue 0003 are in section 3.4.

### 3.1 yaml[*]

- Tag is `yaml[<yamllint rule id>]`. pep8 rendering adds the stray `[/]`
  exactly where astl already does (single-word `[a-z]+` subtags only):
  `yaml[truthy][/]` but `yaml[key-duplicates]`.
- Message is yamllint's `problem.desc` passed through Python
  `str.capitalize()`: first character title-cased, every other character
  lowercased. A duplicated key `FOO` is therefore reported as
  `Duplication of key "foo" in mapping`. Implement `pyCapitalize` with those
  exact semantics (Unicode-aware, like Python).
- Findings carry the yamllint line but no column (upstream discards
  `problem.column`).
- All `yaml[*]` findings are errors: the yamllint "warning" level (comments,
  truthy) only feeds upstream severity metadata, which pep8 output ignores.
- truthy findings are suppressed entirely for files whose parent directories
  end in `.github/workflows`.
- yamllint's own comment directives work: `# yamllint disable-file`,
  `# yamllint disable[ rule:x]`, `# yamllint enable[ rule:x]`,
  `# yamllint disable-line[ rule:x]` (current line when inline, next line when
  standalone).
- `# noqa: yaml[truthy]` style skips also work, through the existing astl
  noqa machinery.
- Counting is character-based, not byte-based: `line too long (167 > 160
  characters)` counts runes, and all token columns are rune columns.

### 3.2 var-naming

- Rule id `var-naming`, severity error, no `(warning)` suffix.
- The name checks run in this order, first hit wins: non-string, annotation
  and allowed-special allowlists, non-ascii, no-keyword, no-reserved,
  read-only, jinja early-exit (`{{` anywhere in the name means silence),
  pattern, no-role-prefix.
- Messages, verbatim (values interpolated):
  - `Variables names must be strings. (vars: 123)`
  - `Variables names must be ASCII. (résumé) (vars: résumé)`
  - `Variables names must not be Python keywords. (import) (vars: import)`
  - `Variables names must not be Ansible reserved names. (lipsum) (vars: lipsum)`
  - `This special variable is read-only. (playbook_dir) (vars: playbook_dir)`
  - `Variables names should match ^[a-z_][a-z0-9_]*$ regex. (FOO_Bar)`
  - `Variables names from within roles should use myrole_ as a prefix. (vars: unprefixed_var)`
- Suffix rules for the ` (vars: k)` / ` (set_fact: k)` / ` (register: k)`
  addition: play-level `vars:` in a playbook get no suffix; everything else
  gets one (vars files, role entries in a play's `roles:` section, task vars,
  set_fact, register).
- Positions:
  - vars files, play vars, role-entry keys, task vars: line and column of the
    offending key itself (1-based, both).
  - register: line of the task's first key, column of the register value
    scalar.
  - set_fact: line of the task's first key, column of the offending set_fact
    key.
- Task `vars:` in a tasks or handlers file are reported twice by upstream,
  once through its play path (no suffix) and once through its task path
  (with ` (vars: k)` suffix), same line and column. In a playbook, task vars
  are reported once (task path only). Reproduce both behaviors.
- Role context (`no-role-prefix`):
  - In a role's files, the prefix is the role directory name
    (`parse.File.Role` already computes it).
  - In a play's `roles:` section, each list-entry mapping is checked: keys
    outside PLAYBOOK_ROLE_KEYWORDS plus all keys under its `vars:` mapping.
    Prefix comes from the entry's `role` (or `name`) value: FQCN (two or more
    dots) means "any prefix accepted", otherwise the last `/` segment.
  - For `include_role` / `import_role` tasks, the task's `vars:` keys are
    checked against the `name:` argument, same FQCN handling.
  - Names starting with `ansible_` (after stripping leading underscores) are
    exempt, as are prefixes containing jinja or not matching
    `^\w+(\.\w+){2,100}$|^\w+$`.
- set_fact key iteration skips `cacheable` and `__`-prefixed keys; register
  skips `__`-prefixed names.
- The allowlists to vendor (from the pinned versions, kept as sorted Go
  tables with a comment naming their origin):
  - Python keywords (35 entries, `keyword.kwlist`; soft keywords like `match`
    are not included).
  - Ansible reserved names (73 entries, `ansible.vars.reserved
    .get_reserved_names()` at ansible-core 2.21.3).
  - read-only names (44 entries) and allowed special names (7 entries), both
    literal sets in upstream `var_naming.py`.
  - ANNOTATION_KEYS (5 entries) from `ansiblelint.constants`.
  - PLAYBOOK_ROLE_KEYWORDS (28 entries) from `ansiblelint.constants`.
- The `var_naming_pattern` config option overrides the pattern and is
  interpolated into the pattern message. astl adds `VarNamingPattern` to its
  config surface with identical semantics.

### 3.3 Ordering and dedupe (engine-level)

Upstream sorts and deduplicates matches with one key:
`(filename, lineno, rule id, message, details, column or -1 when unset)`.
Two consequences astl must adopt:

- `rules.Sort` changes to that key: path, line, rule id (not full tag),
  upstream message, then column with unset ordering first. This is what puts
  `Too few spaces after comma` before `Too many spaces inside brackets` on the
  same line, and yaml findings (no column) before columned findings on the
  same line. The upstream message is the sort key under both taxonomies, so
  `--ids native` output keeps the same order. Note (as built): because the
  yamllint column is discarded before the key is computed, same-line yaml
  findings order purely by message; no hidden sort-column field is needed.
- Findings identical under that key are deduplicated. This is observable:
  `{  a: 1  }` produces two identical braces problems in yamllint and exactly
  one upstream finding. astl gains a dedupe pass after Sort. astl carries no
  `details` field; details only differ between identical-message findings from
  two different tasks, which cannot share a line, so omitting it is safe.

Changing the sort key touches all existing rules; `make parity` on the frozen
corpus is the gate proving the change is byte-neutral there.

### 3.4 Configuration resolution (issue 0003)

Verified against ansible-lint's own `load_yamllint_config` on debops, okfde, a
no-config baseline and four synthetic fixtures; the effective rule sets match
key for key.

- Search order, first hit wins: `.yamllint`, `.yamllint.yaml`, `.yamllint.yml`
  in the working directory, then `$YAMLLINT_CONFIG_FILE`, then
  `${XDG_CONFIG_HOME:-~/.config}/yamllint/config`.
- The repository config is layered **over** ansible-lint's bundled one with
  yamllint's own `extend`: a rule configured as a mapping on both sides merges
  option by option with the repository winning; anything else (a disable, or a
  rule the base has disabled) replaces the base entry outright.
- A parsed config is completed from yamllint's stock defaults **before** it is
  layered, which has a counter-intuitive consequence worth stating plainly:
  touching one option of a rule resets that rule's other options to yamllint's
  values, discarding ansible-lint's overrides. A file that merely says
  `comments: {require-starting-space: false}` therefore also moves
  `min-spaces-from-content` from ansible-lint's 1 back to yamllint's 2.
- `extends: default` and `extends: relaxed` resolve to yamllint's bundled
  policies, re-expressed as Go tables. Because the chain completes the rule
  set, `extends: default` silently re-enables `document-start` and
  `comments-indentation` over ansible-lint's disable.
- A rule value may be `enable`, `disable`, a mapping, or a bare `false`:
  yamllint rewrites only the two keywords, and its validation then reads any
  remaining `false` as disabled. A bare `true` is an error there and here.
- `level` is validated and ignored: it sets a severity ansible-lint's pep8
  output never prints (a `level: warning` truthy finding prints with no
  `(warning)` suffix).
- Unknown rule ids and unknown option names are errors, as upstream.

## 4. Architecture

Three new pieces, one engine adjustment.

### 4.1 `internal/yamlscan`: a pyyaml-equivalent token stream

yamllint's token rules consume pyyaml scanner tokens with start and end marks
(line, column, character pointer) and buffer access. gopkg.in/yaml.v3 contains
a full port of libyaml's scanner (the same design pyyaml implements) but does
not export it.

Extract the scanner from yaml.v3 into `internal/yamlscan`: `scannerc.go`,
`yamlh.go`, `readerc.go`, `apic.go` (the needed subset), `yamlprivateh.go`,
keeping their copyright headers, plus a `NOTICE` recording the provenance
(yaml.v3, Apache-2.0 and MIT for the libyaml-derived files; both are
MIT-compatible with attribution preserved). On top of it, a small exported
API mirroring what the rules need:

```go
type Mark struct{ Line, Column, Pointer int } // 0-based, rune-based
type Token struct {
    Kind        Kind   // StreamStart, BlockMappingStart, Key, Value, Scalar, ...
    Start, End  Mark
    Value       string // scalar, anchor, alias value
    Style       Style  // plain, single, double, literal, folded
}
func Tokens(buffer []rune) []Token // stops silently at the first scanner error
```

The buffer is `[]rune` because every yamllint computation (pointers, columns,
line lengths, spaces between tokens) is character-based in Python. Converting
once per file and indexing runes directly is simpler and faster than
recomputing byte/rune offsets per access.

Input normalization, applied before scanning and before the line rules:
`\r\n` and lone `\r` become `\n` (Python universal newlines). This is what
makes `new-lines` and CR-related trailing-spaces findings impossible, exactly
as upstream.

Known risk: pyyaml and libyaml scanners have minor mark-placement differences
on edge cases (plain scalar end marks around trailing whitespace are the
classic one; yamllint itself compensates in `get_real_end_line`). The
compatibility corpus plus differential runs over the check_repo repositories
are the detection net; any residual divergence gets pinned in PARITY.md.

### 4.2 `internal/yamllint`: the rule port

A self-contained package: no imports from `internal/rules` or
`internal/parse`. Public surface:

```go
type Problem struct {
    Line, Column int    // 1-based, column used only for ordering upstream discards
    Rule         string // yamllint rule id: "trailing-spaces", ...
    Desc         string // yamllint's lowercase description, verbatim
}
func Lint(text string) []Problem
```

Internally it reproduces `yamllint/linter.py`'s
`token_or_comment_or_line_generator` loop: line objects, comment extraction
between consecutive tokens (`parser.py` logic), per-line problem cache flushed
at each line end, and the three disable-directive processors plus
`# yamllint disable-file`. Each rule is one file, a direct transcription of
its yamllint counterpart operating on `(token, prev, next, nextnext, context)`
or `(line)` or `(comment)`. Config is a struct literal holding the effective
settings from section 2; every rule reads its knobs from it so a future
`.yamllint` loader only has to build a different struct.

The 15 rules split by cost: 5 line rules and the comments rule are trivial;
truthy, octal-values, key-duplicates, anchors, hyphens, colons, commas,
braces, brackets are each under ~40 lines given the token stream; indentation
is the one real port (~250 lines of state machine, transcribed 1:1 from
yamllint including its `cannot infer indentation` assertion fallback).

### 4.3 `internal/rules` additions

`yaml.go`: the adapter. Runs `yamllint.Lint` on the normalized text, maps each
problem to a Finding: tag `yaml[<rule>]`, upstream message capitalized the way
Python's `str.capitalize()` renders it (checked against a `pyCapitalize`
reference in a test, since the AST message tests require the capitalized
literal at each construction site), native message per the dual-registry
convention, line from the problem, no column. Drops truthy findings under
`.github/workflows`. Gate: runs
for every file whose kind has YAML base kind (everything astl loads except
`jinja2`, `text`, `python`, `sanity-ignore-file`), before the
`hasUnparsableTask` gate (upstream's yamllint pass does not care about task
shape) but after the `f.Err` gate, with one exception: `errMultiDocument`
does not block it. Upstream lints multi-document plain-yaml files with
yamllint; for multi-document playbooks upstream aborts on load-failure where
astl will now emit yaml findings, which joins the existing, harness-pinned
"astl keeps linting where the runtime gives up" extras class.

`varnaming.go` plus `varnaming_data.go` (the vendored tables): implements
section 3.2 over `parse.File`'s node tree. Positions come from the key nodes'
`Line`/`Column`, which the probes showed match upstream's annotated-position
output 1:1. Role prefix for role files comes from `parse.File.Role`.

`ids.go` gains one row per emitted tag. Native taxonomy:

| Upstream | Native |
|---|---|
| yaml | yaml |
| yaml[anchors] | yaml.undeclared-alias |
| yaml[braces] | yaml.brace-spacing |
| yaml[brackets] | yaml.bracket-spacing |
| yaml[colons] | yaml.colon-spacing |
| yaml[commas] | yaml.comma-spacing |
| yaml[comments] | yaml.comment-spacing |
| yaml[empty-lines] | yaml.blank-lines |
| yaml[hyphens] | yaml.hyphen-spacing |
| yaml[indentation] | yaml.indentation |
| yaml[key-duplicates] | yaml.duplicate-key |
| yaml[line-length] | yaml.long-line |
| yaml[new-line-at-end-of-file] | yaml.missing-final-newline |
| yaml[octal-values] | yaml.octal-literal |
| yaml[trailing-spaces] | yaml.trailing-whitespace |
| yaml[truthy] | yaml.ambiguous-truthy |
| var-naming | var.naming |
| var-naming[pattern] | var.naming[pattern] |
| var-naming[no-keyword] | var.naming[keyword] |
| var-naming[non-ascii] | var.naming[ascii] |
| var-naming[no-reserved] | var.naming[reserved] |
| var-naming[read-only] | var.naming[read-only] |
| var-naming[non-string] | var.naming[string] |
| var-naming[no-role-prefix] | var.naming[role-prefix] |

Native messages follow ADR 0003: side by side at the construction site,
distinct wording, same interpolated values, 100-rune budget. The existing AST
tests extend to the new files for free.

### 4.4 Engine adjustments

- `rules.Sort` and a new dedupe pass per section 3.3.
- `rules.IDs` gains "var-naming" and "yaml".
- `config` gains `VarNamingPattern`.
- `Finding` gains the unexported sort-column field (yaml adapter only).

## 5. Licensing

yamllint is GPL-3.0-or-later, like ansible-lint. This design adds no yamllint
code or test data to astl: the rules are re-implemented in Go from observed
behavior, and the messages are reproduced verbatim under the same basis ADR
0004 established for ansible-lint's messages (short functional phrases,
required byte-for-byte by the compatibility contract). A new ADR 0005 records
the extension of that basis to yamllint and the yaml.v3 scanner vendoring.
GPL-licensed test fixtures (yamllint examples, ansible-lint examples) stay in
the astl-compatibility-check repository, never in astl.
