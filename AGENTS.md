# Agent brief

Writ is an open SDLC layer that stores code review — and eventually
issues, projects, and cycles — inside the git repository itself, as
signed, append-only operations under `refs/writ/*`. Written in Go,
Apache-2.0, one monorepo.

Before proposing or implementing anything, read `VISION.md` (what this
is for, what it is deliberately not, and the order the work goes in)
and `ARCHITECTURE.md` (the technical record: settled decisions and the
reasoning behind them). Those two are the fence around this project.
When a proposal conflicts with them, the proposal loses or the document
is amended deliberately — never by drift.

## This file

AGENTS.md is the only agent brief here. CLAUDE.md and GEMINI.md are
one-line `@AGENTS.md` imports, so every toolchain reads the same text
and there is nothing to keep in sync. Edit AGENTS.md; leave the two
stubs alone. Same pattern as the rest of the studio.

## House rules

- Boring, small, direct. Prefer the standard library; go-git for local
  object I/O, system git for all transport. New dependencies need a
  reason.
- Treat scope growth, speculative abstraction, and framework-building
  as bugs.
- Match the style of surrounding code.
- The spec is the conformance fixtures, not the prose. A change to fold
  behaviour, the op envelope, or canonical encoding lands as one atomic
  change touching spec text, fixtures, and implementation together.
- Fold is pure and deterministic: ops in, state out, no I/O. Keep it
  that way — it is the part that has to be boring and correct.
- The SQLite projection is a droppable cache, never a source of truth.
- Unknown op types and fields are preserved and ignored, never dropped.
  Old clients must not destroy new clients' data.
- The public Go API is domain-shaped: no SHAs or refspecs leak to
  callers unless they ask. Anything built on top — including anything
  we host — consumes that public API with no reach into internals.
- When you file a Linear ticket, set a priority and an estimate — your
  best judgment, stated once, not discussed.

## Layout

Planned monorepo layout (see `ARCHITECTURE.md` for the rationale):

```
/spec          — convention doc, JSON schemas, conformance fixtures
/engine        — codec, dag, fold, resolve, projection, sync (public Go API)
/cmd/writ      — CLI: porcelain for humans, --json for scripts/agents
/docs
```

## Workflow

The pipeline is three composable skills, each in `.agents/skills/`:
`implement-ticket` takes one Linear WRIT ticket to a CI-green draft PR
in a detached git worktree; `adversarial-review` runs reviewer/fixer
rounds on an open PR to a mergeable or capped verdict; `dispatch`
orchestrates a batch of tickets through both of those plus a
human-approved merge queue. The first two stand alone for a single
ticket a human is already driving; `dispatch` is for running the
queue. Read a skill's `SKILL.md` before changing what its stage
produces; read `dispatch`'s before changing how runs are queued.

Build and test commands will be documented here once code exists.
