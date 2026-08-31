# Repository Registry Operations — repo, slug, remote (v1)

Status: **normative**. Schema: [`schemas/repo-ops.schema.json`](schemas/repo-ops.schema.json).
Vectors: [`testdata/repo/`](testdata/repo/).
Field rules: [`testdata/repo/field-rules.json`](testdata/repo/field-rules.json).

This document defines the operation vocabulary, payload schemas, and fold
semantics for the repository registry in Writ (`object_type: "repo"`). It covers
repository registration, slug changes, and remote additions (ARCHITECTURE.md §Object types,
`spec/identifiers.md`).

The key words MUST, MUST NOT, SHOULD, and MAY are to be interpreted as
described in RFC 2119.

## Scope & Object Model

The repository registry maps immutable repository designators (`repo-id`) to
mutable human-readable slugs and discovery remote URLs within a Writ workspace.

Repository registry objects are workspace-scoped collaborative objects that
reside in the designated workspace repository (ARCHITECTURE.md §Object types,
`spec/identifiers.md`).

### Decisions & Rationale

#### 1. `object_id` = `repo-id`
A repository registry object's `object_id` **is** the 32-hex `repo-id`, not a
separately minted object ID. Both are 128-bit random lowercase hex from the same
ID space (`^[0-9a-f]{32}$`).

The folded entry's `repo_id` is projected directly from `object_id` rather than
carried redundantly in the body. Uniqueness of registry entries falls out of the
object model: two writers registering the same repo converge on one object rather
than creating duplicate entries with competing object IDs that reference resolution
would have to arbitrate.

#### 2. `create` carries metadata only; sets are built by their own ops
The `create` body is `{"slug": string, "is_workspace"?: boolean}`. Remote URLs
arrive exclusively via `add-remote`. This mirrors `spec/issue-ops.md` (where
`create` carries title and description, while assignees and labels arrive via
`assign` and `label`) and `spec/review-ops.md`.

#### 3. `is_workspace` is `create-once`
A repository either is or is not the workspace repository; `spec/identifiers.md`
establishes that a repository belongs to at most one workspace. The merge strategy
for `is_workspace` is `create-once` (`spec/fold.md` §5.2): the first-written value
persists and cannot be toggled. There is no `set-workspace` operation in v1.

#### 4. Deliberate Omissions (v1)
Per VISION.md §Non-goals and `spec/identifiers.md` §Out of scope:
- **No `remove-remote`:** Remotes are additive discovery hints for fetching and
  cloning; stale remotes are harmless hints rather than authoritative routing
  tables.
- **No deletion or tombstones:** Repositories are registered permanently in
  workspace history.
- **No per-repo settings or permissions:** Workspace permission and access control
  semantics are governed by the underlying git transport and hosting infrastructure,
  not by repository op graphs.

## Envelope Binding

Every repository registry operation is carried in a git commit whose `op.json`
payload conforms to `spec/schemas/op-envelope.schema.json` and
`spec/schemas/repo-ops.schema.json`:

- `object_type` MUST be `"repo"`.
- `op_version` MUST be an integer ≥ 1. This document specifies version `1`.
- `object_id` MUST be the repository's 32-character lowercase hex designator
  (`^[0-9a-f]{32}$`). Conforming producers mint 32-hex `repo-id` strings per
  `spec/identifiers.md`.
- `op_type` MUST be one of the operation types defined below, or an unknown
  string tolerated under forward-compatibility rules.
- `body` MUST be a JSON object conforming to the schema for the declared
  `op_type` and `op_version`.

Commit author, timestamp, and signature are carried exclusively by the op
commit per `spec/op-envelope.md`. Producers MUST NOT mirror commit-carried
fields into the payload.

## Operation Vocabulary (`op_version: 1`)

The `repo` family defines three operation types:

| `op_type` | Body Schema | Description |
| --- | --- | --- |
| `create` | `{"slug": string, "is_workspace"?: boolean}` | Repository registration and workspace flag. |
| `set-slug` | `{"slug": string}` | Update the repository slug. |
| `add-remote` | `{"remote": string}` | Add a remote URL for discovery. |

### 1. `create`

Initializes a repository registry entry.

```jsonc
{
  "object_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
  "object_type": "repo",
  "op_type": "create",
  "op_version": 1,
  "body": {
    "slug": "writtendev/writ",
    "is_workspace": false
  }
}
```

- `slug` (string, required): Human-readable repository slug (e.g. `writtendev/writ`
  or `backend`). Non-empty string without whitespace (`minLength: 1`, pattern `^[^\s]+$`).
- `is_workspace` (boolean, optional): `true` if this repository is the workspace
  repository itself. Default is `false`.

### 2. `set-slug`

Updates the human-readable slug for a registered repository.

```jsonc
{
  "object_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
  "object_type": "repo",
  "op_type": "set-slug",
  "op_version": 1,
  "body": {
    "slug": "writtendev/writ-core"
  }
}
```

- `slug` (string, required): Updated repository slug. Non-empty string without
  whitespace (`minLength: 1`, pattern `^[^\s]+$`).

### 3. `add-remote`

Adds a git remote URL to the repository's set of known discovery remotes.

```jsonc
{
  "object_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
  "object_type": "repo",
  "op_type": "add-remote",
  "op_version": 1,
  "body": {
    "remote": "git@github.com:writtendev/writ.git"
  }
}
```

- `remote` (string, required): Non-empty remote URL string (`minLength: 1`).

## Field Rules & Merge Strategies

Every body property defined in this vocabulary is mapped to one merge strategy
from `spec/fold.md`'s closed catalogue. A machine-readable copy of these rules
is published in `spec/testdata/repo/field-rules.json`.

| `op_type` | Field | Merge Strategy | Details |
| --- | --- | --- | --- |
| `create` | `slug` | `lww` | Last writer wins in total order $L$ |
| `create` | `is_workspace` | `create-once` | First-written value persists |
| `set-slug` | `slug` | `lww` | Last writer wins in total order $L$ |
| `add-remote` | `remote` | `set-union` | Set union across all operations |

### Concurrency and Merge Semantics

- **Slug updates (`lww`):** Concurrent `create` and `set-slug` operations resolve
  via last-writer-wins in the deterministic total order $L$ (`spec/fold.md` §5.1).
- **Workspace flag (`create-once`):** `is_workspace` resolves via `create-once`
  (`spec/fold.md` §5.2); the value set by the earliest `create` op in total order $L$
  is preserved.
- **Remotes set (`set-union`):** `add-remote` operations accumulate into a set
  via `set-union` (`spec/fold.md` §5.3). Elements serialize in deterministic
  lexicographical byte order (`spec/fold.md` §8).

## Folded Registry Entry

Folding the operations for a `repo` object produces exactly the entry shape
consumed by reference resolution in `spec/identifiers.md` §Entry shape:

```jsonc
{
  "repo_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
  "slug": "writtendev/writ",
  "remotes": [
    "git@github.com:writtendev/writ.git",
    "https://github.com/writtendev/writ.git"
  ],
  "is_workspace": false
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `repo_id` | string | Projected directly from the op's `object_id` (32 lowercase hex characters). |
| `slug` | string | Folded repository slug (`lww`). |
| `remotes` | array of strings | Folded set of remote URLs (`set-union`), in deterministic sorted order. |
| `is_workspace` | boolean | Folded workspace repository flag (`create-once`), defaulting to `false`. |

## Forward Compatibility & Unknown Fields

- **Unknown body and envelope fields:** Conforming implementations MUST
  preserve and ignore any unknown fields in op payloads (`spec/forward-compatibility.md`).
- **Unknown `op_type` or future `op_version`:** Ops with unknown op types or
  unsupported versions for `object_type: "repo"` MUST remain in the DAG and
  contribute to total ordering ($t^*$) and ancestry, but contribute no field
  mutations to known repository state (WRIT-15).
