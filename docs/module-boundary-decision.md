# Decision: Go module boundary — single module vs. engine module

Spike for WRIT-61. ARCHITECTURE.md commits to two things that pull gently
against each other: "one language, one `go build ./...`" and the engine
"consumed as an ordinary pinned Go module" by anything built on top,
including a future hosted layer. This resolves that into one deliberate
choice rather than leaving it to drift once code lands (WRIT-53).

## Decision

**Single module for the whole monorepo.** One `go.mod` at the repo root,
module path `github.com/writtendev/writ`. The engine's public API lives
at the `engine` subpackage — import path `github.com/writtendev/writ/engine`
— matching the layout ARCHITECTURE.md already lays out under Repo strategy.
No `go.work` is needed yet; that tool solves multi-module local development,
and there is only one module.

## Why

**A single module doesn't leak Bubble Tea or the GitHub client into an
engine-only consumer's build.** That was never module-boundary-dependent —
Go has always resolved a build from the actual per-package import graph, so
a service importing only `.../writ/engine` and never reaching into `tui` or
`bridge/github` doesn't compile or link those packages in, single module or
not (verified: `go mod why` reports an unused sibling package's dependency
as not needed, and it never gets a `go.sum` entry, even when the providing
module is a real, checksummed dependency rather than a local `replace`).
What Go 1.17+ module graph pruning adds on top is narrower but still
relevant to "dependency-surface hygiene": it avoids needing to load the
`go.mod` files of a module's *unused* transitive dependencies, which keeps
`go.sum` for that consumer proportional to what it actually imports rather
than to everything the monorepo's `go.mod` requires. One caveat: `go list -m
all` still lists Bubble Tea and the GitHub client themselves as resolved
versions in the module graph, since MVS operates module-by-module rather
than package-by-package — that bookkeeping entry is harmless (nothing
downloads, verifies, or links them) but it's why the claim above is scoped
to `go.sum`, not the whole module graph. Splitting the module wouldn't
improve on the `go.sum` property, which already holds for a single
well-layered module — it would only trim that harmless `go list -m all`
bookkeeping entry.

**`internal/` enforces the API boundary regardless of module shape.** The
house rule that "no SHAs or refspecs leak to callers unless they ask" is a
package-visibility problem, not a module-boundary problem — `engine/internal/`
keeps codec/dag/fold plumbing unreachable from outside whether `engine` sits
in its own `go.mod` or as a subtree of one. A second module buys no
additional enforcement here.

**A split module is a real, ongoing cost with no consumer yet to justify
it.** Two `go.mod`/`go.sum` pairs to keep in sync, a `go.work` file to
maintain, and — the part that actually matters once it happens — two
separate version-tag lineages to reason about (is `v0.4.0` an engine change,
a TUI change, or both?). That version-signal purity is the one real thing a
split buys: a consumer pinning the engine shouldn't see a required-version
bump because the TUI's Bubble Tea dependency changed. It's a legitimate
concern, but it only bites once something outside this repo is pinning the
engine independently — the hosted layer or a third party, both later per
VISION.md's sequencing. Splitting now optimizes for a consumer that doesn't
exist yet, against the one-monorepo rationale (a fold-rule change lands as
one atomic PR across spec, fixtures, engine, and CLI) that does apply today.

**The move is cheap to make later and expensive to unmake now.** If a real
external consumer shows up wanting independent engine versioning, extracting
`engine/go.mod` and adding a root `go.work` is a mechanical file split that
doesn't touch import paths for in-repo callers — though standing up
independent tag/versioning discipline for the engine subtree afterward is a
real, separate cost, not something the file split buys on its own.
Committing to two modules today, before any code exists, is exactly the
"speculative abstraction" the house rules
flag as scope growth to avoid without a concrete reason.

## Consequence: module path

`github.com/writtendev/writ` is the current repo location and is what
WRIT-53 should use verbatim for the root `go.mod`. (This decision was
originally written against the pre-transfer location
`github.com/mattwalters/gloss` and anticipated that a move to a dedicated
org would change the module path with it; that move has since happened —
the repo now lives at `writtendev/writ`, and the WRIT-65 rebrand carried
the module path along, exactly the mechanical rename predicted here.)
WRIT-53 needs a concrete string to put in `go.mod`, and import-path
renames are a mechanical `gofmt -r`-and-`go mod edit` exercise, not a
design question.

## What this means for WRIT-53

Root `go.mod` at module path `github.com/writtendev/writ`, covering
`/engine`, `/cmd/writ`, `/tui`, `/bridge/github` per the layout ARCHITECTURE.md
already specifies. No second `go.mod`, no `go.work`, until a real external
consumer motivates the split described above.

## Module path decision: keep `github.com/writtendev/writ` (WRIT-73)

The root module path remains `github.com/writtendev/writ`.

A vanity import path (such as `writ.dev/writ`) buys cosmetics at the ongoing
operational cost of hosting and serving a `go-import` meta tag in perpetuity.
If that domain or its redirect infrastructure ever lapses, `go get` breaks
permanently for every version ever published under that path.

Revisit rule and deadline: The module path decision is closed and should only
be revisited before the first release tag (WRIT-58). Changing a module path
after public release tags exist is a v2-shaped break across the ecosystem.

