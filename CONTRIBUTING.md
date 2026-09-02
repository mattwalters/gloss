# Contributing to Writ

Read [VISION.md](VISION.md) and [ARCHITECTURE.md](ARCHITECTURE.md) before
proposing or implementing anything. VISION.md is what this project is for
and what it deliberately is not; ARCHITECTURE.md is the record of settled
decisions and the reasoning behind them. If a decision you'd relitigate is
already in one of those two documents, the document wins — bring new
information or drop it, but don't reopen it from scratch in a PR. This file
is about how work turns into a merged change, not what we're building; for
the house rules and repo layout that apply to agents and humans alike, see
[AGENTS.md](AGENTS.md).

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

The house rules — the dependency policy, what counts as scope creep, how to
match surrounding style — live in AGENTS.md's "House rules" section, not
here. AGENTS.md set up CLAUDE.md and GEMINI.md as one-line imports of itself
specifically so there's a single copy to keep current; this file follows
the same principle by linking instead of restating.

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

If you're touching something else — a CLI flag, docs prose —
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
the right to submit it under the project's license (Apache-2.0).

Configure the repository's hook to sign off commits automatically:

```
git config core.hooksPath .githooks
```

This uses `.githooks/prepare-commit-msg` to append a `Signed-off-by` trailer
when not already present, sourced from your `user.name` and `user.email`.

Alternatively, sign off manually:

```
git commit -s
```

The trailer format is `Signed-off-by: Your Name <your.email@example.com>`.
The sign-off email must match the commit author's email (and for GitHub pull
requests, your GitHub account email so it survives squash merges). Use your
real name — no pseudonyms or anonymous contributions. If you forgot on your
last commit, `git commit --amend -s` fixes it. The DCO check on pull requests
enforces this on every commit.

## Build and test

One toolchain, one command, per ARCHITECTURE.md: `go build ./...` and
`go test ./...` from the repo root. CI runs the same tests plus
`golangci-lint` and `go test -race` on every PR; the conformance fixtures
run as part of the ordinary test suite as they land, so a fixture failure
in CI is the spec speaking.

## The public API baseline

`api/engine.txt` lists every exported symbol in `engine` and its public
subpackages. Downstream tools are meant to need nothing but that surface, so
it is kept as a file you can read rather than a fact you have to derive:

```
make api        # regenerate api/engine.txt
make api-check  # what CI's `api` job runs
```

**If you change the public API, run `make api` and commit the result in the
same PR.** `make api-check` regenerates the listing into a temp file and diffs
it against the committed one, so it fails on *any* drift — an added symbol as
readily as a removed one. A green `api` job therefore means the baseline is
current, not merely that nothing obviously broke.

Read the diff it produces: added lines are new surface, and a line that
changes or disappears is a breaking change. Breaking changes aren't forbidden
before 1.0, but they are never silent — call it out in the PR and add a
CHANGELOG.md entry.

The listing comes from `internal/cmd/apisurface`, which parses the source and
prints declarations. No type checking, no compiler export data: the bytes
depend only on the code, so two contributors on different machines and
different Go versions produce the same file. The baseline it replaced was
apidiff export data, which embedded the generating machine's absolute paths,
changed wholesale between toolchains, and — because it was only ever consulted
for *incompatible* changes — could not tell a stale baseline from a clean one.
