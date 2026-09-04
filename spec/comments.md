# Comments — threaded, anchored discussion (v1)

Status: **normative**. Schema: [`schemas/comment.schema.json`](schemas/comment.schema.json).
Vectors: [`testdata/comments/`](testdata/comments/).

A **comment** records a unit of human or agent discussion attached to a
collaborative subject (a review, an issue, or any future object type). A
comment can be anchored to specific code context, organized into reply
threads, edited, or deleted.

The key words MUST, MUST NOT, SHOULD, and MAY are to be interpreted as
described in RFC 2119.

## Scope

This section defines the comment **op vocabulary** (`op_version: 1`):
the collaborative object model, the four operations (`create`, `edit`,
`delete`, `resolve`), body fields, threading, edit, deletion, and resolution
semantics, anchor delegation, and GitHub review-comment mappings. It deliberately does not
define:

- **The anchor format and invariants** — dual-sided line ranges and hunk
  context capture are defined in `spec/anchors.md` (WRIT-13). An anchor is
  embedded by value inside a comment `create` body via `$ref`.
- **Re-anchoring and orphan degradation** — resolving an anchor against
  a new git tree (`resolve(anchor, tree) → position | orphaned`) is
  specified in WRIT-14 and implemented by the engine's anchor resolver
  (WRIT-66).
- **Review workflow** — batch submission of review
  comments with an approval state belongs to the review op vocabulary (WRIT-8).
  Comments remain subject-agnostic discussion primitives.
- **Fold reduction, ordering, and concurrency tiebreaks** — how the
  engine reduces the op DAG into materialized comment state is specified
  in WRIT-12 and implemented by the comment reducer (WRIT-27).
- **Ref layout and writer identity** — how comment op commits are stored
  under `refs/writ/*` and attributed to writer keys (WRIT-7).
- **Workspace-global object IDs and cross-repo references** — WRIT-16.

Like all Writ operations, comment ops inherit the envelope rules defined
in `spec/op-envelope.md` and the canonical JSON encoding rules defined in
`spec/canonicalization.md`.

## The collaborative object model

A comment **is its own collaborative object**:

- `object_type`: `"comment"`
- `object_id`: the comment's own unique identifier.

Every op on a comment (`create`, `edit`, `delete`, `resolve`) carries the comment's
`object_id` in its envelope. The envelope already names the target object,
so `edit`, `delete`, and `resolve` require no separate target field.

Making comments first-class objects rather than operations on a review
ensures a single, uniform comment vocabulary serves reviews, issues, and
future collaborative types identically, with shared fold reduction and
projection caching.

### Commit-carrier rule (no mirroring)

Author, timestamp, and cryptographic signature are carried exclusively by
the op commit per `spec/op-envelope.md`. Comment op bodies MUST NOT contain
`author`, `created_at`, or `timestamp` fields. Readers MUST ignore any such
mirrored fields if present.

## Operations (`op_version: 1`)

The comment vocabulary defines four operations:

| `op_type` | Meaning | Body |
| --------- | ------- | ---- |
| `create`  | Brings the comment into existence | `subject`, `text`, optional `in_reply_to`, optional `anchor` |
| `edit`    | Replacement markdown text | `text` |
| `delete`  | Tombstone withdrawing the comment | `{}` |
| `resolve` | Sets thread resolution state (resolve/unresolve) | `resolved`, conditional `resolved_by` |

### 1. `create`

A `create` op initializes the comment, attaching it to a subject object,
optionally anchoring it to code, and optionally establishing a reply edge.

```jsonc
{
  "object_id": "c-9a4f",
  "object_type": "comment",
  "op_type": "create",
  "op_version": 1,
  "body": {
    "subject": {
      "object_type": "review",
      "object_id": "r-7f3a"
    },
    "text": "Consider adding context timeout handling here.",
    "in_reply_to": "c-1b2c",
    "anchor": { /* anchor object per spec/anchors.md */ }
  }
}
```

#### Body fields

- `subject` (object, required) — the collaborative object this comment is
  attached to.
  - `object_type` (string, required): lowercase string (`^[a-z][a-z0-9-]*$`,
    at most 64 characters), e.g. `"review"`, `"issue"`.
  - `object_id` (string, required): printable non-space ASCII
    (`^[\x21-\x7e]+$`, 1–256 characters).
  - Unknown fields on `subject` are permitted, preserved, and ignored
    (allowing future cross-repo qualifiers per WRIT-16).
  - A comment's `subject` is immutable: once created, a comment MUST NOT
    be moved to another subject.
- `text` (string, required) — the markdown content of the comment.
  - MUST be non-empty (`minLength: 1`).
  - Content format is GitHub-Flavored Markdown (GFM). No alternate
    content-type field is defined in v1.
  - No arbitrary length ceiling is imposed; canonicalization handles UTF-8
    validity and surrogate rejection.
- `in_reply_to` (string, optional) — the `object_id` of the parent comment
  being replied to (`^[\x21-\x7e]+$`, 1–256 characters).
  - Present: this comment is a reply in a thread.
  - Absent: this comment is a thread root.
  - Immutable: set at creation time, never mutated.
  - MUST NOT equal the comment's own `object_id` (a comment cannot reply to
    itself).
- `anchor` (object, optional) — an embedded anchor value object conforming
  to `spec/anchors.md` (`schemas/anchor.schema.json`).
  - Present: an inline or file-level comment on code.
  - Absent: a top-level subject discussion comment.
  - Immutable: set at creation time, never mutated.

### 2. `edit`

An `edit` op provides replacement content for the comment.

```jsonc
{
  "object_id": "c-9a4f",
  "object_type": "comment",
  "op_type": "edit",
  "op_version": 1,
  "body": {
    "text": "Consider adding context timeout handling here, defaulting to 5s."
  }
}
```

#### Body fields

- `text` (string, required) — full replacement GitHub-Flavored Markdown
  content (`minLength: 1`).

#### Invariants

- **Replacement, not patch**: `text` is the complete new text of the
  comment. Folding applies the latest edit directly without delta
  reconstruction.
- **Position and threading are immutable**: an `edit` body MUST NOT carry
  `anchor`, `subject`, or `in_reply_to`. Where a comment points in code is
  derived dynamically by the anchor resolver, not altered by edit ops.

### 3. `delete`

A `delete` op is a tombstone indicating that the comment is withdrawn.

```jsonc
{
  "object_id": "c-9a4f",
  "object_type": "comment",
  "op_type": "delete",
  "op_version": 1,
  "body": {}
}
```

#### Body fields

- `body` MUST be an empty object `{}` (`maxProperties: 0`).
- No `reason` or payload fields are defined in v1.

### 4. `resolve`

A `resolve` op records or updates the resolution state of a comment thread.

```jsonc
{
  "object_id": "c-9a4f",
  "object_type": "comment",
  "op_type": "resolve",
  "op_version": 1,
  "body": {
    "resolved": true,
    "resolved_by": "email:alice@example.com"
  }
}
```

#### Body fields

- `resolved` (boolean, required) — resolution status of the thread:
  - `true`: marks the thread as resolved.
  - `false`: marks the thread as unresolved (reopened).
- `resolved_by` (person identifier per [`spec/identifiers.md`](identifiers.md), conditional) —
  scheme-prefixed person identifier (`email:alice@example.com`, `user:alice`) on
  whose behalf the resolution is recorded. Normalized (scheme lowercased; value
  trimmed and case-folded) and bounded (a scheme of at most 32 characters, a
  value of at most 320 code points) per
  [`spec/identifiers.md`](identifiers.md). A bare, colonless identifier is not a
  person identifier.
  - When `resolved` is `true`: `resolved_by` is **required**. An unattributed
    resolution cannot be written by conforming producers or pass schema validation.
    Requiring `resolved_by` whenever `resolved` is `true` ensures the field is
    always sourced from the op that resolves the thread, eliminating interleaving
    misattribution across producers.
  - When `resolved` is `false` (unresolve): `resolved_by` **MUST NOT** be present.
    An unresolve operation reopens the thread; thread reopening attribution is
    supplied by commit metadata in the op DAG (author identity, writer ID,
    timestamp, and cryptographic signature) rather than a body field. Prohibiting
    `resolved_by` on unresolve prevents an unresolve author from being recorded as
    the resolver under independent LWW field reduction.

#### Target & Threading Invariants

- **Thread root attachment:** Resolution attaches to the comment object
  identified by `object_id`. In workflow and UI conventions, resolution applies
  to the **thread root** (the comment with no `in_reply_to`), resolving the
  entire discussion thread. If a `resolve` op targets a reply comment, the fold
  reduces resolution on that comment object, but standard thread resolution
  queries evaluate state at the thread root.
- **Reply-after-resolve behavior:** A new reply to a resolved thread does
  **not** automatically reopen the thread (matching GitHub convention). An
  explicit unresolve operation (`resolve` with `resolved: false`) is required
  to reopen the thread.
- **Reversibility:** Resolution is fully reversible. Multiple `resolve` and
  unresolve ops fold deterministically via Last-Writer-Wins (`lww`) in total
  order.

## Threading model

Threading in Writ is represented entirely without mutable state:

1. Every reply carries an immutable `in_reply_to` reference naming the
   `object_id` of its parent comment on its `create` op.
2. Thread roots omit `in_reply_to`.
3. The threading structure forms a directed forest of trees over comments
   belonging to the same subject.
4. Thread membership is defined as the transitive closure of `in_reply_to`
   edges leading to a root comment.

### General tree structure

Writ models threading as a general tree of arbitrary depth, rather than a
flat two-level structure. `in_reply_to` MAY point to any existing comment
under the same subject. Clients and UIs that present flat two-level threads
(such as GitHub) may flatten the tree for display, but the underlying data
model retains exact parentage.

## Fold Implications & Merge Strategies

Every body property defined in this vocabulary is mapped to one merge strategy
from WRIT-12's closed catalogue. A machine-readable copy of these rules is
published in `spec/testdata/comments/field-rules.json`.

Per WRIT-12, **any field without a declared strategy is not merged**; it is
treated as unknown data, preserved in the DAG, and ignored during fold.

| `op_type` | Field | Merge Strategy | Key / Details |
| --- | --- | --- | --- |
| `create` | `subject` | `create-once` | Immutable subject binding |
| `create` | `text` | `lww` | Initial text |
| `create` | `in_reply_to` | `create-once` | Immutable parent comment binding |
| `create` | `anchor` | `create-once` | Immutable anchor binding |
| `edit` | `text` | `lww` | Last writer wins in total order |
| `delete` | `deleted` | `tombstone` | Entity-level tombstone; deletion wins over concurrent edits |
| `resolve` | `resolved` | `lww` | Last writer wins in total order |
| `resolve` | `resolved_by` | `lww` | Last writer wins in total order |

### Edit, Deletion, and Resolution Semantics

#### Edit Semantics

- An `edit` op replaces the comment text in folded state via `lww`.
- If multiple edits exist concurrently across writer branches, fold
  resolution applies deterministic Last-Write-Wins (LWW) ordering per
  `spec/fold.md` (WRIT-12).

#### Deletion Semantics (Honest Tombstones)

- In materialized / folded state, a deleted comment is marked as deleted
  (`deleted: true`) and its text is redacted / withheld.
- **Append-only reality**: op commits in git history are immutable and
  permanent. A `delete` op does not erase the earlier `create` or `edit`
  commits from the git repository object database. Anyone with repository
  read access can inspect historical git commits. The specification states
  this honestly: deletion is a tombstone in the projection, not physical
  erasure from git history.
- **Delete is terminal**: once a comment object folds a `delete` op,
  subsequent `edit` ops on that comment object fold to nothing (the comment
  remains deleted).

#### Resolution Semantics

- A `resolve` op updates `resolved` (and `resolved_by`) via `lww`.
- Conforming producers MUST include `resolved_by` when `resolved: true` and
  MUST NOT include `resolved_by` when `resolved: false`.
- If concurrent `resolve` and unresolve ops are authored across branches,
  deterministic LWW total order tiebreaks decide the winning resolution state.
- In folded state, a comment object records `resolved: true/false` and
  `resolved_by: string`. If `resolved` is `false`, the comment thread is open.
- The op body field and the folded state field carry the same name,
  `resolved_by`: there is no rename across the fold layer.

### Representability vs. Authorization

**Writ has no authorization model at the specification layer.** Consistent with
review approvals and issue operations, anyone with git push access can push
an `edit`, `delete`, or `resolve` op. Operations are cryptographically
attributable (via commit signatures) and tamper-evident, not authoritative.
Policy questions — such as whether only the comment author, PR author, or
designated reviewers can resolve a thread — belong to the coordination service
or client application (VISION.md §The open core and the hosted layer). The spec
provides faithful event capture without imposing an enforcement mechanism it
cannot verify offline.

## Anchor delegation

Comment anchoring delegates entirely to `spec/anchors.md`:

- The `anchor` field in `create` is validated against
  `schemas/anchor.schema.json` and must satisfy all anchor cross-field
  invariants (OID format and length consistency, line range ordering
  `end >= start`, context capture rules).
- Comment position is fixed at creation time. Re-anchoring across target
  tree updates is computed by the anchor resolver (WRIT-14/WRIT-66). No
  `reanchor` op exists in v1.

## Forward compatibility

In accordance with `spec/op-envelope.md` and WRIT-15:

- Unknown fields in op bodies (and inside `subject`) MUST be preserved
  and ignored by conforming readers.
- Unknown `op_type` values under `object_type: "comment"` MUST be preserved
  in the DAG and ignored during fold reduction by v1 readers.
- Ops carrying a future `op_version` under `object_type: "comment"` MUST
  be preserved the same way: they remain in the DAG and contribute to
  total ordering and ancestry, but contribute no field mutations to known
  comment state (WRIT-15).

[`schemas/comment.schema.json`](schemas/comment.schema.json) gates its v1
body rules on `op_version: 1`, so an op carrying an unknown `op_type` or a
future `op_version` is a **valid instance** of it. That is deliberate: the
published schema is the artifact a third-party reader validates against,
and it must not reject what this section requires readers to tolerate. The
corpus pins it with
[`testdata/comments/valid/unknown-op-type.json`](testdata/comments/valid/unknown-op-type.json)
and
[`testdata/comments/valid/unknown-future-version.json`](testdata/comments/valid/unknown-future-version.json),
matching the equivalent vectors the other five vocabularies ship.

Producers are bound the other way round: `spec/op-envelope.md` §Producer
validation rule 4 forbids authoring an `op_type` or `op_version` the
producer does not itself define for `comment`. Reader tolerance is not a
licence to write an op no reader can interpret.

## Out of scope, with forward references

- **Ref layout and writer identification**: `refs/writ/comments/*` vs
  per-writer refs (WRIT-7).
- **Review vocabulary**: approval status, review submission batching (WRIT-8).
- **Fold reduction and concurrency rules**: LWW tiebreaks, tombstone
  fold reduction (WRIT-12).
- **Anchor resolution and orphaning**: pure resolver algorithm (WRIT-14 /
  WRIT-66).
- **Forward-compatibility rules**: handling unknown op types in fold
  (WRIT-15).
- **Workspace-global object IDs**: cross-repo subject identifiers (WRIT-16).
- **Comment fold reducer**: engine implementation (WRIT-27).
- **Fixture repositories**: full signed git commit fixture repos for
  comments (WRIT-18).

## Appendix A: GitHub review-comment shapes (informative)

This appendix illustrates how GitHub comments map to Writ comment
operations:

### 1. Top-level discussion comment

A standard GitHub issue comment or top-level PR conversation comment maps to
a `create` op attached to the review subject with no anchor:

- `op_type`: `"create"`
- `body.subject`: `{ "object_type": "review", "object_id": "<review-id>" }`
- `body.text`: comment body markdown
- `body.anchor`: absent
- `body.in_reply_to`: absent

### 2. Inline review comment

A GitHub diff review comment on a specific file or line maps to an anchored
`create` op:

- `op_type`: `"create"`
- `body.subject`: `{ "object_type": "review", "object_id": "<review-id>" }`
- `body.text`: comment body markdown
- `body.anchor`: anchor constructed from GitHub diff position per
  `spec/anchors.md` Appendix A
- `body.in_reply_to`: absent

### 3. Review comment reply

A reply in a GitHub review comment thread maps to a `create` op referencing
the parent comment:

- `op_type`: `"create"`
- `body.subject`: `{ "object_type": "review", "object_id": "<review-id>" }`
- `body.text`: reply body markdown
- `body.in_reply_to`: `<parent-comment-object-id>`
- `body.anchor`: omitted (or replicated from parent)

### 4. Review submission summary

The summary body submitted as part of an overall GitHub Pull Request Review
(e.g., approval comment) maps to the review vocabulary (WRIT-8), not an
independent comment object.
