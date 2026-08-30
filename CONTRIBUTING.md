# Contributing to Writ

Read [VISION.md](VISION.md) and [ARCHITECTURE.md](ARCHITECTURE.md) before
proposing or implementing anything. VISION.md is what this project is for
and what it deliberately is not; ARCHITECTURE.md is the record of settled
decisions and the reasoning behind them. If a decision you'd relitigate is
already in one of those two documents, the document wins — bring new
information or drop it, but don't reopen it from scratch in a PR. This file
is about how work turns into a merged change, not what we're building; for
day-to-day agent-facing conventions see [AGENTS.md](AGENTS.md).

## License

Writ is Apache-2.0 (see `LICENSE`). By contributing, you agree your
contribution is provided under that license.

No per-file license headers: `LICENSE` at the repository root governs the
whole tree, and Apache-2.0 doesn't require a header on every file to apply.
Stated once, here, so it doesn't get re-litigated file by file.

No `NOTICE` file for now: NOTICE exists to carry forward attribution notices
from bundled third-party Apache-licensed code, and this repo doesn't bundle
any yet. Add one the day a dependency's own NOTICE requires it — not before.

## House style

Boring, small, direct. Prefer the standard library; go-git for local object
I/O, system git for all transport, Bubble Tea for the TUI. New dependencies
need a reason — "it's convenient" is not one. Scope growth, speculative
abstraction, and framework-building are treated as bugs, not ambition. Match
the style of the surrounding code rather than introducing a new one.

## The fixture-first workflow

The spec is the conformance fixtures, not the prose in `/spec`. Markdown can
describe a rule persuasively and still be wrong about what the code does;
a fixture — input ops in, a golden folded state out — can't drift from
reality the same way, because implementations are judged against it
directly. That makes tests-as-spec the load-bearing convention here, and it
carries one hard rule:

**A change to fold behaviour, the op envelope, or canonical encoding lands
as one atomic PR touching spec text, fixtures, and implementation
together.** Not spec-then-code in sequence, not a follow-up PR to "add the
fixture later." If those three pieces disagree, there's no way to tell
which one is the bug, and that ambiguity is exactly what erodes trust in a
convention other implementations are meant to build against.

A worked example, once `/spec` and `/engine` exist per the layout in
ARCHITECTURE.md: suppose the fold rule for concurrent edits to an `Issue`'s
title needs to change — say, from last-writer-wins to a per-field rule. The
PR that makes that change includes:

1. A new or updated fixture under `spec/fixtures/` — the op sequence
   (including the concurrent title-edit ops) and the golden folded output
   the new rule produces.
2. The spec prose describing the rule, updated to match.
3. The fold reducer in `engine/fold` implementing it.
4. Nothing else. Fold is pure and deterministic — ops in, state out, no
   I/O — so the fixture is a direct, mechanical check on the reducer; keep
   it that way rather than smuggling in unrelated cleanup.

If you're touching something else — a CLI flag, TUI layout, docs prose —
the fixture-first rule doesn't apply, but the general one still does: keep
the PR to what it says it does.

Two related rules, already settled in ARCHITECTURE.md, that fixtures exist
to enforce:

- **Unknown-op tolerance.** Implementations must preserve and ignore op
  types and fields they don't understand, never drop them. A fixture that
  exercises an unknown-future-version op belongs alongside any change that
  touches the envelope, so this doesn't regress silently.
- **Canonicalization.** Signing and content-addressing require byte-stable
  encoding. Changes here are exactly the kind that need a fixture proving
  the encoding is still stable, not just a description of the intent.

## Developer Certificate of Origin (DCO)

Every commit must be signed off, certifying you wrote it or otherwise have
the right to submit it under the project's license (Apache-2.0):

```
git commit -s
```

That appends a `Signed-off-by: Your Name <your.email@example.com>` trailer
using the name and email from your git config. Use your real name — no
pseudonyms or anonymous contributions. If you forgot on your last commit,
`git commit --amend -s` fixes it. The DCO check on pull requests enforces
this on every commit.

## Build and test

One toolchain, one command, per ARCHITECTURE.md: `go build ./...` and
`go test ./...` from the repo root. CI runs the same tests plus
`golangci-lint` and `go test -race` on every PR; the conformance fixtures
run as part of the ordinary test suite as they land, so a fixture failure
in CI is the spec speaking.
