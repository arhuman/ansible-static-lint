# Exit codes and unchecked files

| Exit code | Meaning |
|---|---|
| 0 | clean, no violations |
| 1 | usage or runtime error, for example an input path that does not exist |
| 2 | violations were found |
| 3 | the run could not check every file it was given |

## Reading failures during discovery

An input path that cannot be read is fatal. A file or directory that becomes
unreadable during the walk is reported on stderr and skipped, leaving the rest
of the run intact.

## Why exit code 3 exists

Exit code 3 covers the files astl was given but could not examine: one that is
not readable, or one that is not valid YAML. Each is named on stderr, the files
around it are still linted, and their findings are still written to stdout.

It takes precedence over 2 because a violation is a result you can read, while
an unchecked file means there is no result for it, and an exit code is the only
place that can say so. Treat 3 as "fix the broken file, then trust the run".

In CI, exit 3 must fail the step. An unchecked file produces no findings, so a
run that let it pass would read as clean.

## What does not count as unchecked

Files that were never YAML in the first place, such as Jinja2 templates and
Python plugins, do not count: astl reads the ones it has rules for as text, so
failing to parse them as YAML is expected.

A multi-document YAML file does not count either, since the `yaml[*]` rules
still lint it.
