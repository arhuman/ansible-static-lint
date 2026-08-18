# Security Policy

## Supported versions

ansible-static-lint has not cut a release yet. Until it does, only `main`
receives fixes.

| Version | Supported |
| ------- | --------- |
| `main` | yes |
| pre-release tags, once they exist | latest tag only |

## Reporting a vulnerability

Do not open a public issue for a security report.

Use GitHub's private vulnerability reporting on this repository
(Security -> Report a vulnerability), which opens a private advisory visible
only to the maintainers. Include a description and, where possible, a file that
reproduces the problem.

Expect an acknowledgement within a few days. Please allow time for a fix before
any public disclosure.

## Threat model

astl is a local command-line linter. It reads YAML files, writes to stdout and
stderr, and does not open sockets, execute subprocesses, or evaluate the content
it parses.

The input it reads is **not** assumed to be trusted: astl is intended to run in
CI over repositories the operator may not control. Reports about untrusted input
reaching an unintended effect are in scope, including terminal control sequences
surviving into output, path traversal or symlink following during discovery, and
resource exhaustion from a hostile file.

Every read of a path the linted repository chose goes through `internal/safeio`,
which refuses anything that is not a regular file and stops at a fixed ceiling:
64 MiB for a source file, 4 MiB for a configuration file. Both refusals bound
the read itself and not just its verdict, so a file that resolves to a character
device costs a stat rather than the ceiling. A path that fails either check is
named on stderr and the run exits non-zero; it is never passed over in silence.

One case is knowingly out of reach. Opening a FIFO blocks until a writer
appears, which happens before there is a descriptor to inspect. git cannot store
a FIFO, so reaching one requires a symlink to a FIFO that already exists on the
host at a path the attacker can predict.
