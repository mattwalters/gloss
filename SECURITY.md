# Security Policy

Writ signs and stores SDLC data inside your git repository. We take
reports about signature verification, op-envelope handling, and
anything that could let unsigned or forged data pass as authentic
especially seriously.

## Reporting a vulnerability

Please report suspected vulnerabilities privately, using GitHub's
[private vulnerability reporting](https://github.com/writtendev/writ/security/advisories/new)
for this repository, rather than opening a public issue. This lets us
confirm the report, work on a fix, and coordinate disclosure before
details are public.

Include what you'd include in any good bug report: the affected
version or commit, reproduction steps or a fixture, and the impact as
you understand it.

We'll acknowledge new reports within a few business days.

## Scope

In scope: the spec, the engine (codec, dag, fold, projection, sync),
the CLI, the TUI, and the GitHub bridge — anything in this monorepo.
