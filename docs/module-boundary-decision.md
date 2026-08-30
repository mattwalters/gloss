# Decision: Go module boundary — single module vs. engine module

Spike for GLS-61. ARCHITECTURE.md commits to two things that pull gently
against each other: "one language, one `go build ./...`" and the engine
"consumed as an ordinary pinned Go module" by anything built on top,
including a future hosted layer. This resolves that into one deliberate
choice rather than leaving it to drift once code lands (GLS-53).

## Decision

**Single module for the whole monorepo.** One `go.mod` at the repo root,
module path `github.com/mattwalters/gloss`. The engine's public API lives
at the `engine` subpackage — import path `github.com/mattwalters/gloss/engine`
— matching the layout ARCHITECTURE.md already lays out under Repo strategy.
No `go.work` is needed yet; that tool solves multi-module local development,
and there is only one module.

## Why

**Module graph pruning already buys most of what a split module would.**
Since Go 1.17, a consumer's build list is computed from the packages it
actually imports, not from everything the providing module's `go.mod`
requires. A service that imports only `.../gloss/engine` and never reaches
into `tui` or `bridge/github` does not pull Bubble Tea or the GitHub client
into its build just because they live in the same module — as long as
`engine` itself doesn't import them, which the layered design (engine has
no dependency on its own consumers) already guarantees. Splitting the
module would harden a property pruning already gives for free.

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
`engine/go.mod` and adding a root `go.work` is mechanical and doesn't touch
import paths for in-repo callers. Committing to two modules today, before
any code exists, is exactly the "speculative abstraction" the house rules
flag as scope growth to avoid without a concrete reason.

## Consequence: module path

`github.com/mattwalters/gloss` is the current repo location and is what
GLS-53 should use verbatim for the root `go.mod`. The naming-collision spike
(GLS-5) kept "Gloss" as the project name but found the bare `gloss` GitHub
org handle already taken by an unrelated party — so if this project ever
moves to a dedicated org, the module path changes with it, same as any Go
project that relocates. That's a separate, later decision (out of scope
here) and not a reason to delay picking a path now: GLS-53 needs a concrete
string to put in `go.mod`, and import-path renames are a mechanical
`gofmt -r`-and-`go mod edit` exercise, not a design question.

## What this means for GLS-53

Root `go.mod` at module path `github.com/mattwalters/gloss`, covering
`/engine`, `/cmd/gloss`, `/tui`, `/bridge/github` per the layout ARCHITECTURE.md
already specifies. No second `go.mod`, no `go.work`, until a real external
consumer motivates the split described above.
