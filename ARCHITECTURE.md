# Writ — Architecture & Design Decisions

_Companion to VISION.md. This is the technical record: what we're building, how it's shaped, and — most importantly — why each decision went the way it did, so contributors (human or agent) don't have to relitigate settled questions without new information._

## Core model

Writ is an **event-sourcing engine that uses git as its storage and transport substrate**. Git supplies a content-addressed object store, a sync protocol, and credential/transport infrastructure; Writ supplies the semantics. Every SDLC artifact (review, issue, project, cycle) is a **collaborative object**: a DAG of small, signed, immutable **operations**, each stored as a git commit under a dedicated ref. Current state is never stored authoritatively; it is _derived_ by deterministically folding an object's operations. Concurrent writes don't conflict — they coexist as sibling ops in the DAG and are reconciled at fold time by spec-defined rules.

Design lineage, with gratitude: the op-log + per-writer refs + signing + fold-to-SQLite pattern is inspired by Radicle's collaborative objects (COBs); the refs-not-notes and meta-ref patterns are validated by Gerrit NoteDb; the line-oriented mergeability insight comes from git-appraise. Where Radicle pairs this data model with a peer-to-peer network in service of its sovereignty mission, Writ targets hub-and-spoke sync through whatever git remote a team already uses — a narrower goal that lets us omit the networking layer entirely.

### Ref layout

Per-writer namespaces are load-bearing: `refs/writ/<writer-id>/cobs/<type>/<object-id>`. A writer only ever pushes to their own namespace, so pushes cannot non-fast-forward against another writer — the entire class of push conflicts disappears, which is what keeps the sync layer simple. `writer-id` is (user, device) so multi-device self-races also dissolve into the DAG instead of ref conflicts. Reading an object means enumerating its ops across _all_ writer namespaces and folding.

We use plain refs rather than git-notes: notes don't fetch by default, and notes attached to commits are orphaned when commits are rewritten by rebase — limitations git-appraise's design had to work around. A one-time `writ init` writes fetch/push refspecs into `.git/config` so ordinary `git fetch` carries writ data; that config edit is the entire deployment story.

### The op envelope

Every op carries the same logical envelope — op id, parent op ids (DAG edges), object id, object type, op type + version, author, timestamp, signature, type-specific body — split across **two carriers with exactly one home per field** (amended with WRIT-6, which spec'd the envelope): the op commit itself carries op id (the commit's SHA), parent op ids (parent SHAs), author, timestamp, and signature; a canonical JSON blob at a fixed path in the commit tree carries object id, object type, op type + version, and the body. Nothing is mirrored between carriers — mirroring creates two sources of truth that can disagree (payload parents vs. commit parents), and the edges are incoherent anyway: a payload can't contain its own content-derived op id, nor a signature covering the payload the signature lives inside. The accepted cost is that the payload alone isn't self-describing; a conforming reader always needs the commit. Normative detail: `spec/op-envelope.md`. Two rules with teeth:

- **Canonicalization:** byte-stable encoding (canonical JSON, `spec/canonicalization.md`) because signatures and content-addressing demand it. This is spec-level, fixture-enforced.
- **Unknown-op tolerance:** implementations MUST preserve and ignore op types/fields they don't understand — never drop. Old clients must not destroy new clients' data. This is what lets the schema evolve without flag days.

Signing rides git's existing commit-signature machinery (SSH signing preferred — users already have the key). Every op is attributable and tamper-evident, which matters increasingly as agents become review actors.

### Object types (spec'd from day one, even where clients come later)

Repo-scoped: `review` (base/head, revisions, status), `comment` (threaded, anchored), `approval`/`ci-status`. Workspace-scoped (living in a designated **workspace repo**, per Gerrit's All-Projects precedent): `issue`, `project`, `cycle`, membership metadata. Object IDs and cross-references are workspace-global (`<repo-designator>#<object-id>` or content-addressed) so "issue in repo A fixed by review in repo B" is representable — the one-graph query is the point.

### Anchoring (the hard problem)

Line comments anchor to **content** (blob hash + hunk context), not line numbers, so they survive force-pushes and rebases as well as possible; when re-anchoring fails, comments degrade to "orphaned but preserved," never silently lost. The format (`spec/anchors.md`, WRIT-13) is dual-sided, following Radicle's `CodeLocation`: an anchor carries an `old` and/or `new` side — each a (commit, path, blob, line-range, captured-context) tuple — because deleted-line comments and GitHub's cross-side ranges are not representable as a single blob position. This gets its own spec section and its own fixture family; expect iteration.

## The five machines (engine internals)

1. **Op codec** — domain action ⇄ canonical signed payload in a commit. Owns canonicalization and signature verification.
2. **DAG store** — append op with parents; enumerate an object's ops across all writer refs; topological ordering. Pure object-database work.
3. **Fold** — the heart. Per-type deterministic reducers: ops in, materialized state out (`Review{status, comments, approvals}`, `Issue{title, state, assignee}`), concurrent-edit resolution exactly as fixtures dictate (last-writer-wins with op-id tiebreak as the default rule; per-field rules where the spec says so). Pure functions, no I/O, thoroughly testable. Must be boring and correct.
4. **Projection** — SQLite cache keyed by ref tips: on read, diff tips vs. last fold, fold only deltas, serve queries from indexed tables. Always droppable/rebuildable; never a source of truth. Also home of local-only state: drafts (kept out of shared refs, following Gerrit's draft-handling precedent — publish on intent, never per-keystroke commits), read/unread, sync cursors. An optional DuckDB/Parquet exporter hangs off the same op-log for analytics — derived only, never committed.
5. **Sync plane** — manages refspecs, invokes **system git** for fetch/push, reports per-remote status ("n ops unsynced"). Thin by design; per-writer refs made conflicts structurally impossible.

## Public API shape

Domain-shaped, never git-shaped — callers see no SHAs or refspecs unless they ask:

```
store := writ.Open(path)             // any git dir: clone, bare, worktree; fully offline
store.Reviews.Create(base, head) / .Comment(id, anchor, body) / .Approve(id)
store.Issues.Create(...) / .SetStatus(...)
store.Query(filters...)              // served from projection
store.Sync(remote)                   // explicit, separate concern
store.Watch() <-chan Event           // reactive clients
```

`Watch()` is what makes the TUI reactive instead of polling, and it is the seam any event-relay service plugs into. Everything above the engine — CLI, TUI, localhost web view, GitHub bridge, any hosted service — is a consumer of this one interface, distinguished only by rendering surface. Nothing gets private powers.

## Language: Go (decided; rationale preserved)

We seriously considered Rust; its three advantages dissolved under this project's constraints. (1) Bindings: the CLI in `--json` plumbing mode is the universal API — every language and agent can shell out to one static binary, which Go distributes exceptionally well; cgo-exported shared libraries remain a later option if in-process bindings are ever needed. (2) Git libraries: we take a **hybrid approach** — go-git (mature, pure Go) for local object I/O; **system git for all transport**, which is also git-appraise's approach and is quietly the right engineering call, because SSH agents, credential helpers, gitconfig, proxies, and enterprise auth setups all work for free. (3) Type-safety-for-correctness: the conformance fixture suite is the correctness story, not the compiler. Meanwhile the costs of splitting languages were real: Bubble Tea commits the TUI to Go; a Rust core underneath means a cgo seam, two toolchains, and a higher barrier for the contributor who starts in the TUI and ends up fixing an engine bug. One language, one `go build ./...`. A Rust (or Python, or TypeScript) implementation of the _spec_ by others would be genuinely welcome — an independent second implementation is the best proof a convention stands on its own.

## SQLite driver: pure Go (decided; rationale preserved)

The projection (five machines, #4) uses `modernc.org/sqlite`, not
`mattn/go-sqlite3`. `mattn` is faster on bulk insert — a spike benchmark on
a projection-shaped workload (5k reviews, 100k comments, one bulk-insert
transaction plus indexed reads by review) measured cgo at roughly 1.4–2.2x
there, with indexed reads too close to call (overlapping ranges across 10
runs) — but both are comfortably fast in absolute terms (sub-second full
refold, sub-tenth-millisecond point lookups), and cgo would compromise the
reason Go was chosen over Rust in the first place:
"the CLI in `--json` plumbing mode is the universal API ... which Go
distributes exceptionally well" (see Language section above). A cgo
dependency in the projection means the release matrix (WRIT-58: linux/macos/
windows × amd64/arm64) needs either a from-scratch build host per target or
a C cross-compiler toolchain wired into CI and kept working, plus musl for
genuinely static Linux binaries since static-linking cgo against glibc
breaks NSS lookups at runtime. `CGO_ENABLED=0` cross-compilation has none of
that: it's `GOOS`/`GOARCH` and nothing else, from any host. Full benchmark,
method, and reproduction steps: `docs/spikes/writ-60-sqlite-driver/`.

## Repo strategy: one monorepo

Everything open lives in a single Apache-2.0 monorepo because the spec, engine, and clients are one contract with several expressions: a fold-rule change should be one atomic PR touching spec text, fixtures, engine, and CLI together. Split repos invite version skew between fixtures and implementation — the exact failure that erodes trust in a convention. Layout:

```
/spec          — convention doc, JSON schemas, conformance fixtures (the real standard)
/engine        — codec, dag, fold, projection, sync (public Go API at the root package)
/cmd/writ      — CLI: porcelain for humans, --json plumbing for scripts/agents
/tui           — Bubble Tea client
/bridge/github — bidirectional PR/comment ⇄ ops sync (the migration path)
/docs
```

Any hosted service built on Writ — by us or anyone — should consume the engine's public API as an ordinary pinned Go module, with no reach into internals. Keeping the public API strong enough that _we_ never need private hooks is a deliberate design constraint: it keeps the convention honest. `spec/` can graduate to a neutral home once independent implementations exist and governance is worth formalizing.

That "ordinary pinned Go module" is one monorepo-wide `go.mod` at the repo root, module path `github.com/writtendev/writ`, covering `/engine`, `/cmd/writ`, `/tui`, and `/bridge/github` — not a separate module for the engine. A consumer that imports only `.../writ/engine` never compiles or links Bubble Tea or the GitHub client into its own build regardless, since only actually-imported packages get built, independent of which module they live in; `engine/internal/` enforces the API boundary the same way whether `engine` sits in its own `go.mod` or as a subtree of one. A second module and a `go.work` file are deferred until a real external consumer needs independent engine versioning (decision and full rationale: `docs/module-boundary-decision.md`, WRIT-61).

## Spec = fixtures

The standard is not the markdown; it's the conformance corpus: fixture repos exercising every tricky configuration (concurrent edits on every type, multi-device writers, orphaned anchors, unknown op types, future versions, malformed signatures) plus golden folded outputs any implementation must reproduce. Fixtures are the ground truth that lets an independent implementation claim compatibility. Fold determinism is fixture-enforced byte-for-byte.

## Known-hard list (tracked, not feared)

Anchoring across force-pushes; canonical encoding; compaction/GC for unbounded op history; workspace-repo permission semantics; identity mapping (signing key → directory identity), which is deliberately out of spec scope; host ref-namespace compatibility (the foundational spike — some namespaces like `refs/pull/*` are read-only on some hosts; verify `refs/writ/*` across GitHub, GitLab, Bitbucket, Gitea/Forgejo, Codeberg, and bare-SSH before anything else, with a branch-namespace-encoding fallback sketched in case any major host is restrictive).
