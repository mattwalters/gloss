# Issue Operations — issue, state, assign, label, link (v1)

Status: **normative**. Schema: [`schemas/issue-ops.schema.json`](schemas/issue-ops.schema.json).
Vectors: [`testdata/issue-ops/`](testdata/issue-ops/).
Field rules: [`testdata/issue-ops/field-rules.json`](testdata/issue-ops/field-rules.json).
Fixtures: [`spec/fixtures/testdata/descriptions/issue-*.yaml`](fixtures/testdata/descriptions/) and [`spec/fixtures/testdata/golden/issue/`](fixtures/testdata/golden/issue/).

This document defines the operation vocabulary, payload schemas, and fold
semantics for issues in Writ (`object_type: "issue"`). It covers issue
creation, metadata updates, state transitions, assignments, labels, and
cross-references (ARCHITECTURE.md §Object types).

The key words MUST, MUST NOT, SHOULD, and MAY are to be interpreted as
described in RFC 2119.

## Scope & Object Model

An issue is represented in Writ as a workspace-scoped collaborative object with
`object_type: "issue"`. Issues live in the designated workspace repository
(ARCHITECTURE.md §Object types, `spec/identifiers.md`) and can reference or be
referenced by reviews and other collaborative objects across repositories in the
workspace.

### Decisions behind the vocabulary

- **`update` covers both retitle and description edits:** Rather than separate
  ops for editing title vs. editing description, a single `update` op with two
  optional fields (where an empty body `{}` is rejected by schema) handles all
  metadata edits. This mirrors the `review` family's `update` op and avoids
  multiplying op types for identical LWW merge semantics.
- **`create` carries only title and description:** Initial issue creation
  captures the core problem statement. State, assignees, labels, and links
  arrive as their own operations, ensuring that "opened and immediately
  triaged" and "triaged later" share one uniform append pipeline and fold path.
  Absent any `set-state` op, the folded issue state defaults to `"open"`.
- **`set-state` is `lww`, not `lattice`:** Issues reopen; state transitions
  (`open` $\leftrightarrow$ `closed`) are not monotone, so a join-semilattice
  would be incorrect. Last-writer-wins in the fold's total order resolves
  concurrent state transitions deterministically. The optional `reason` field
  is a free string carrying external reasons (such as GitHub's `state_reason`:
  `"completed"`, `"not_planned"`) or human-provided explanations without
  Writ minting an un-enforceable closed enum.
- **`assign` and `label` are add-wins OR-sets (`set-observed-remove`):**
  Concurrent assignment or labeling on one device and removal on another is
  reconciled via `set-observed-remove` (WRIT-12, `spec/fold.md`). Additions
  win over concurrent removals. Assignee and label values are opaque non-empty
  strings.
- **`link` mirrors `approval`:** A link records an association between the
  issue and another collaborative object (such as a code review or companion
  issue). It uses `keyed-lww` keyed by `target`. Emitting a `link` op with
  `relation: "none"` retracts the link for that target, following the same
  retraction idiom used by approval votes (`verdict: "none"`). The `target`
  field is a reference string per `spec/identifiers.md` (bare `<object-id>`
  within the same repo, or `<repo-id>#<object-id>` cross-repo).
- **No tombstone in v1:** Issues close; they are not deleted. There are no
  tombstones defined for `issue` objects in v1.

### Scope boundaries

- **Comments on issues:** Discussion threads attached to issues are separate
  collaborative objects (`object_type: "comment"`) whose `subject.object_type`
  is `"issue"` (WRIT-9, `spec/comments.md`).
- **Project and cycle membership:** Grouping issues into projects and cycles
  belongs to the project and cycle op vocabulary (WRIT-11).
- **Cross-repo references:** Identifiers, repo designators, and reference
  resolution algorithms are defined in `spec/identifiers.md` (WRIT-16).
- **Fold driver and total ordering:** The deterministic total order $L$ and
  concurrency resolution are specified in `spec/fold.md` (WRIT-12).

## Envelope Binding

Every issue operation is carried in a git commit whose `op.json` payload
conforms to `spec/schemas/op-envelope.schema.json` and
`spec/schemas/issue-ops.schema.json`:

- `object_type` MUST be `"issue"`.
- `op_version` MUST be an integer ≥ 1. This document specifies version `1`.
- `object_id` MUST be a non-empty string identifier (1–256 characters,
  printable non-space ASCII `^[\x21-\x7e]+$`). Canonical Writ-minted object IDs
  are 32 lowercase hexadecimal characters (`^[0-9a-f]{32}$`).
- `op_type` MUST be one of the operation types defined below, or an unknown
  string tolerated under forward-compatibility rules.
- `body` MUST be a JSON object conforming to the schema for the declared
  `op_type` and `op_version`.

Commit author, timestamp, and signature are carried exclusively by the op
commit per `spec/op-envelope.md`. Producers MUST NOT mirror commit-carried
fields into the payload.

## Operation Vocabulary

The issue family defines six operation types for `op_version: 1`:

| `op_type` | Body Schema | Description |
| --- | --- | --- |
| `create` | `{"title": string, "description"?: string}` | Issue creation and initial description. |
| `update` | `{"title"?: string, "description"?: string}` | Metadata edits (title, description). |
| `set-state` | `{"state": "open"\|"closed", "reason"?: string}` | State transitions and optional reason. |
| `assign` | `{"add"?: [string], "remove"?: [string]}` | Add or remove assignees. |
| `label` | `{"add"?: [string], "remove"?: [string]}` | Add or remove labels. |
| `link` | `{"target": reference, "target_type"?: string, "relation": "fixes"\|"relates"\|"none"}` | Associate or retract cross-references. |

### 1. `create`

Initializes an issue object.

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "create",
  "op_version": 1,
  "body": {
    "title": "Fix parser crash on empty input",
    "description": "Parser panics with index out of range when given empty input."
  }
}
```

- `title` (string, required): Summary title of the issue, minLength 1.
- `description` (string, optional): Full Markdown body describing the issue.

Absent any subsequent `set-state` op, the folded state of an issue is `"open"`.

### 2. `update`

Modifies issue metadata (title, description).

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "update",
  "op_version": 1,
  "body": {
    "title": "Fix parser crash on empty and nil input"
  }
}
```

- `title` (string, optional): Updated issue title, minLength 1.
- `description` (string, optional): Updated issue description.

At least one of `title` or `description` MUST be present in an `update` body.
An empty `{}` update body is invalid.

### 3. `set-state`

Transitions the issue state between open and closed.

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "set-state",
  "op_version": 1,
  "body": {
    "state": "closed",
    "reason": "not_planned"
  }
}
```

- `state` (string, required): One of `"open"`, `"closed"`.
- `reason` (string, optional): Explanation for the state change (e.g. `"completed"`,
  `"not_planned"`, or a free-form human string).

### 4. `assign`

Adds or removes assignees for the issue.

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "assign",
  "op_version": 1,
  "body": {
    "add": ["alice", "bob"],
    "remove": ["charlie"]
  }
}
```

- `add` (array of non-empty strings, optional): Assignee identifiers to add.
- `remove` (array of non-empty strings, optional): Assignee identifiers to remove.

At least one of `add` or `remove` MUST be present and contain at least one item.
An empty `{}` body or empty arrays (`"add": []`) are invalid.

### 5. `label`

Adds or removes labels on the issue.

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "label",
  "op_version": 1,
  "body": {
    "add": ["bug", "priority/high"],
    "remove": ["needs-triage"]
  }
}
```

- `add` (array of non-empty strings, optional): Label names to add.
- `remove` (array of non-empty strings, optional): Label names to remove.

At least one of `add` or `remove` MUST be present and contain at least one item.
An empty `{}` body or empty arrays (`"remove": []`) are invalid.

### 6. `link`

Creates, updates, or retracts a cross-reference link to another collaborative
object (such as a code review or another issue).

```jsonc
{
  "object_id": "0123456789abcdef0123456789abcdef",
  "object_type": "issue",
  "op_type": "link",
  "op_version": 1,
  "body": {
    "target": "11112222333344445555666677778888#fedcba9876543210fedcba9876543210",
    "target_type": "review",
    "relation": "fixes"
  }
}
```

- `target` (reference string, required): Reference to the target object per
  `spec/identifiers.md`. Either a bare `<object-id>` (for objects in the same
  repository) or a fully-qualified `<repo-id>#<object-id>` (for cross-repo
  references, such as an issue in a workspace repo linking to a review in a
  code repo).
- `target_type` (string, optional): Type of the target collaborative object
  (e.g. `"review"`, `"issue"`), minLength 1.
- `relation` (string, required): One of:
  - `"fixes"`: The target fixes or resolves this issue.
  - `"relates"`: The target is related to this issue.
  - `"none"`: Retracts any existing link to `target`.

## Fold Implications & Merge Strategies

Every body property defined in this vocabulary is mapped to one merge strategy
from `spec/fold.md`'s closed catalogue. A machine-readable copy of these rules
is published in `spec/testdata/issue-ops/field-rules.json`.

Folded issue state is `Issue{title, description, state, reason, assignees, labels, links}`.

| `op_type` | Field | Merge Strategy | Key / Details |
| --- | --- | --- | --- |
| `create` | `title` | `lww` | Last writer wins in total order $L$ |
| `create` | `description` | `lww` | Last writer wins |
| `update` | `title` | `lww` | Last writer wins |
| `update` | `description` | `lww` | Last writer wins |
| `set-state` | `state` | `lww` | Last writer wins; default is `"open"` |
| `set-state` | `reason` | `lww` | Last writer wins |
| `assign` | `add` | `set-observed-remove` | Add-wins OR-set over assignee strings |
| `assign` | `remove` | `set-observed-remove` | Add-wins OR-set over assignee strings |
| `label` | `add` | `set-observed-remove` | Add-wins OR-set over label strings |
| `label` | `remove` | `set-observed-remove` | Add-wins OR-set over label strings |
| `link` | `target` | `keyed-lww` | Scoped by key `["target"]` |
| `link` | `target_type` | `keyed-lww` | Scoped by key `["target"]` |
| `link` | `relation` | `keyed-lww` | Scoped by key `["target"]`; `"none"` retracts |

### Concurrency and Retraction Semantics

- **Total order and LWW:** Concurrent edits to `title`, `description`, `state`,
  or `reason` resolve via last-writer-wins in the deterministic total order $L$
  (defined in `spec/fold.md` §Causality-monotone total order $L$).
- **Add-Wins OR-Sets:** `assign` and `label` use `set-observed-remove`
  (`spec/fold.md` §5). If an addition of an assignee or label is concurrent
  with a removal ($a \parallel r$), the addition wins and the element remains
  present in the folded set.
- **Keyed link map and retraction:** Links are stored as a map from `target`
  reference string to link attributes. Concurrent links to different targets do
  not conflict. A link is retracted by emitting `relation: "none"`, causing the
  link for that `target` to present as absent in materialized state while the
  operation remains preserved in the DAG.
- **Deletion:** Issues are never deleted in v1. Status transitions such as
  `"closed"` represent lifecycle state, not object deletion.

## Forward Compatibility & Unknown Fields

- **Unknown body and envelope fields:** Conforming implementations MUST
  preserve and ignore any unknown fields in op payloads (`spec/forward-compatibility.md`).
- **Unknown `op_type` or future `op_version`:** Ops with unknown op types or
  unsupported versions for `object_type: "issue"` MUST remain in the DAG and
  contribute to total ordering ($t^*$) and ancestry, but contribute no field
  mutations to known issue state (WRIT-15).

---

## Appendix A — GitHub Issue Representability (Informative)

To ensure fidelity on the GitHub bridge read path (`/bridge/github`), every
standard GitHub issue mutation maps to the Writ issue op vocabulary.

The conversion vectors under [`testdata/issue-ops/github/`](testdata/issue-ops/github/)
illustrate these mappings.

| GitHub Issue Field / Event | Disposition in Writ |
| --- | --- |
| `title` | `create.title` / `update.title` (`lww`) |
| `body` | `create.description` / `update.description` (`lww`) |
| `state` (`"open"`, `"closed"`) | `set-state.state` (`"open"`, `"closed"`) (`lww`) |
| `state_reason` (`"completed"`, `"not_planned"`) | `set-state.reason` (`lww`) |
| `assignees` (added / removed) | `assign.add` / `assign.remove` (`set-observed-remove`) |
| `labels` (added / removed) | `label.add` / `label.remove` (`set-observed-remove`) |
| Closing PR / Linked PR | `link` op (`target: <repo-id>#<review-id>`, `relation: "fixes"`, `target_type: "review"`) |
| `user` | Op commit author |
| `created_at`, `updated_at`, `closed_at` | Op commit timestamps on respective ops |
| `comments` | Separate `comment` objects (WRIT-9) referencing `object_id` |
| `milestone` | Workspace metadata / cycle or project link (WRIT-11) |
