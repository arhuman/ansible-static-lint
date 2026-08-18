# 0002. Dual rule-identifier taxonomy with permanent upstream aliases

Status: accepted

## Context

astl inherited ansible-lint's rule identifiers verbatim. Those identifiers are functional interface (users key `skip_list` entries, `# noqa` comments, and CI greps on them), so they carry no copyright concern, but the inherited taxonomy is inconsistent: `no-changed-when` is kebab prose, `name[missing]` is rule-plus-subtag, `risky-octal` is adjective-noun. A rename-only fix would break every existing suppression in users' repositories; keeping only upstream names forecloses a coherent taxonomy of our own.

This is deliberately asymmetric with the message-text plan: messages are expressive content being rewritten to be genuinely better; identifiers are load-bearing configuration being preserved.

## Decision

astl carries two identifier taxonomies backed by one equivalence table (`internal/rules/ids.go`, the single source of truth; both lookup directions are built from one declaration):

- Canonical native IDs follow the grammar `domain.rule[tag]` with domains `name`, `task`, `deprecated`, `role`, `meta`, `galaxy`, `play`, `file`. The rule slug names the defect (`task.unguarded-change`, `meta.placeholder-values`, `galaxy.changelog-missing`). Upstream subtags carry over unchanged. Two identity exceptions where the upstream rule id already equals the native domain: `name` and `galaxy`. A domain names what the rule judges, so a rule that judges a play (`play.complexity`) or a whole file (`file.sanity-ignore`) does not sit under `task` merely because upstream implements it with a task hook.
- Upstream ansible-lint IDs are permanent aliases: accepted in `skip_list`, `# noqa`, and any future suppression surface, forever. The identifiers ansible-lint has itself retired, the numeric ids and the pre-rename slugs, resolve alongside them so that suppressions written against an older ansible-lint keep working; they resolve only, and astl never emits one.
- Output defaults to upstream IDs (byte-compatible pep8); `--ids native` switches display in pep8 and SARIF. Under `--ids native` the `[/]` rich-markup artifact is not reproduced: it is an upstream rendering bug imitated only for byte compatibility, so it stays confined to the upstream taxonomy.

## Consequences

- Existing ansible-lint suppressions keep working unchanged; astl gains a consistent, meaning-conveying taxonomy that can become the display default later without a migration.
- The table is guarded by tests: round-trip over every row, and an AST scan of the rules package proving every emittable tag has a row (the table cannot vouch for itself; `ids.go` is excluded from the scan).
- Cost: every new rule adds a table row; the coverage test turns a forgotten row into a test failure.

## Alternatives considered

- Rename outright to the new taxonomy: breaks every existing `skip_list` and `noqa` in user repos; rejected.
- Keep upstream IDs only: perpetuates an inconsistent taxonomy and ties the native vocabulary to upstream's forever; rejected.
- Alias table generated from upstream source at build time: reintroduces a GPL artifact into the build; the IDs are functional and few, a hand-maintained table with a coverage test is safer; rejected.
