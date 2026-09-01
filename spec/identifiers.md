# Identifiers, cross-repo references & workspace repo

Status: normative. Schema: [`schemas/identifiers.schema.json`](schemas/identifiers.schema.json).
Vectors: [`testdata/references/`](testdata/references/).

Writ connects code reviews, issues, projects, and cycles across multiple
repositories into a unified software development lifecycle graph. Cross-repo
linking — such as an issue in repository A resolved by a review in repository B —
requires that object identities and cross-references be workspace-global from
day one (VISION.md §Scope architecture; ARCHITECTURE.md §Object types).

The key words MUST, MUST NOT, SHOULD, and MAY are to be interpreted as
described in RFC 2119.

## Scope

This section defines:

- **Object identifiers** — the canonical form minted by Writ producers.
- **Person identifiers** — format, normalization, and byte-comparison rules for
  collaborative actor identities (assignees, approval subjects).
- **Repository designators** — the immutable identifier of a git repository
  within a Writ workspace.
- **Reference grammar** — the syntax for bare local and fully-qualified
  cross-repository references.
- **The workspace repository convention** — the repository holding
  workspace-scoped objects and the repository registry.
- **The repository registry entry shape** — the folded metadata structure
  mapping repository designators to human-readable slugs and remote URLs.
- **The reference resolution algorithm** — how references are resolved
  against a workspace checkout.

This section deliberately does not define:

- **The op envelope's `object_id` validation** — op envelopes accept any
  printable non-space ASCII string (1–256 characters) for forward
  compatibility ([`spec/op-envelope.md`](op-envelope.md)). This section
  constrains what conforming Writ producers mint.
- **The operation vocabulary for modifying the repository registry** — the
  `repo` create, set-slug, and add-remote op payloads are specified in the
  repository registry op vocabulary ([`spec/repo-ops.md`](repo-ops.md)). This
  document defines the folded registry entry shape that resolution consumes.
- **Workspace permission semantics** — see [Out of scope](#out-of-scope).

## Object identifiers

A collaborative object in Writ (a review, comment, issue, project, cycle)
possesses an identifier that is unique across all repositories, writers, and
devices in a workspace.

```jsonc
"0123456789abcdef0123456789abcdef"
```

The canonical format minted by Writ producers is **128 bits of cryptographically
secure pseudorandomness, formatted as 32 lowercase hexadecimal ASCII characters**
(`^[0-9a-f]{32}$`).

Object IDs are minted client-side at object creation with no coordination,
central registry, or network communication.

### Collision safety

With 128 bits of entropy generated from a cryptographically secure random
number generator (CSPRNG), the probability $P$ of an ID collision among $N$
minted objects across all repositories and writers is bounded by the birthday
paradox approximation:

$$P \approx \frac{N^2}{2 \times 2^{128}} = \frac{N^2}{2^{129}}$$

For a large workspace containing one trillion ($10^{12}$) objects, the
probability of a collision is less than $1.47 \times 10^{-15}$. This ensures
collision safety across independent writers without requiring centralized
locking.

### Rationale and closed alternatives

Two alternative identification schemes were considered and rejected:

1. **The create op's commit ID (SHA):** In Writ's envelope model, the op
   commit ID is computed over a tree containing `op.json`. The `op.json`
   payload explicitly carries `object_id` (so all ops belonging to an object
   share the same object ID). Using the first op's commit ID as the object ID
   creates an impossible circular dependency: the commit ID cannot be known
   until `op.json` is constructed and hashed, but `op.json` would need to
   contain the commit ID.
2. **Sequential integers (e.g. #1, #2):** In Writ's distributed architecture,
   writers append ops to per-writer append chains (`refs/writ/<writer-id>/<type>`)
   and push to their own namespaces without locking. Sequential numbering
   requires a central coordinator to allocate monotonic sequence numbers,
   which is incompatible with offline creation and conflict-free concurrent
   pushes.

### Producer and reader conformance

- **Producers MUST** mint object IDs using 128 bits of cryptographically
  secure randomness formatted as 32 lowercase hexadecimal characters.
- **Readers MUST NOT** reject object IDs that fail to match the 32-hex
  lowercase format if they satisfy the envelope's printable non-space ASCII
  constraint (`^[\x21-\x7e]+$`, 1–256 characters). Readers MUST treat
  non-conforming or foreign IDs as opaque identifiers. This forward-compatibility
  rule ensures older readers do not discard objects created by newer or
  third-party producers.

## Person identifiers (`person-id`)

A collaborative actor in Writ (a human author, reviewer, assignee, or bot) is
identified in operation payloads using a **person identifier**.

```jsonc
"alice@example.com"
```

Person identifiers appear in op payloads across the SDLC vocabulary:
- **Assignees** on reviews ([`spec/review-ops.md`](review-ops.md) §5 `assign`)
- **Assignees** on issues ([`spec/issue-ops.md`](issue-ops.md) §4 `assign`)
- **Approval and dismissal subjects** on reviews ([`spec/review-ops.md`](review-ops.md) §6 `approval`)
- **Resolution actors** on comments ([`spec/comments.md`](comments.md) §5 `resolve`)

### Format

The canonical person identifier format is an **email address** (e.g.
`alice@example.com`, `bot@ci.writ.dev`).

Email addresses match git author identities (`user.email`), git commit signatures,
and forge export formats (GitHub, GitLab, Gerrit).

### Normalization rules

To guarantee deterministic comparison, portable queries, and interoperability
across independent implementations, person identifiers MUST be normalized:

1. **Whitespace trimming:** All leading and trailing whitespace characters
   (ASCII space `\x20`, tab `\t`, newline `\n`, carriage return `\r`, and
   Unicode whitespace) MUST be removed.
2. **Case folding:** All characters MUST be converted to lowercase ASCII
   (`a-z`).
3. **Non-empty:** After trimming, the normalized string MUST contain at least
   one character.

$$\text{norm}(s) = \text{lowercase}(\text{trim\_whitespace}(s))$$

### Comparison and equality

Two person identifiers $A$ and $B$ denote the same person if and only if their
normalized byte representations are equal:

$$\text{equal}(A, B) \iff \text{norm}(A) == \text{norm}(B)$$

For example:
- `"  Alice@Example.COM  "` normalizes to `"alice@example.com"`.
- `"alice@example.com"` and `"  ALICE@EXAMPLE.COM  "` compare as equal.
- Deduplication, set membership tests in add-wins OR-sets (`set-observed-remove`),
  and keyed LWW lookups (`keyed-lww`) operate on the normalized string.

### Producer and reader conformance

- **Producers MUST** emit normalized person identifiers (trimmed, lowercase)
  when writing operation payloads.
- **Readers and Reducers MUST** normalize person identifiers upon reading op
  payloads prior to evaluating set membership, keyed lookups, deduplication,
  or projection indices.
- **Reducers MUST** carry the normalized form into the OR-set members and
  `keyed-lww` entries they fold, not merely into the comparison that selects
  them. Where a `keyed-lww` key component is derived from a person identifier,
  the value stored under that key reads back normalized as well: normalizing an
  identifier for keying and then storing the payload verbatim is non-conforming.

### Relationship to `writer-id`

Writ clearly separates device-scoped physical namespaces from collaborative actor
identities:

| Concept | Format | Scope | Purpose |
| --- | --- | --- | --- |
| **`writer-id`** | 16 lowercase hex characters (`^[0-9a-f]{16}$`) | Device-scoped `(user, device)` | Git ref namespace (`refs/writ/<writer-id>/`) for append-only concurrent writes without locking. |
| **`person-id`** | Email address string (normalized lowercase, trimmed) | Workspace-global collaborative actor | Collaborative actor identity (assignee, reviewer, voter) across multiple devices and repositories. |

A single person (e.g. `alice@example.com`) may author ops from multiple machines
and devices, each with its own distinct `writer-id` (e.g. laptop `4d8a23b35dd50102`
and desktop `0123456789abcdef`). The `writer-id` partitions the git refspace; the
`person-id` identifies the collaborative actor.

### Identity mapping out of scope

Mapping cryptographic signing keys (SSH/GPG) or person email addresses to
central directory identities (such as LDAP, SSO, or corporate IAM) is
**deliberately out of scope** for the open format and specification
(ARCHITECTURE.md §Known-hard list, "identity mapping").

Writ's open format records what was declared in op payloads and verifies
tamper-evident commit signatures; organizational authority and identity verification
policies belong to the hosting forge or coordination service.

## Repository designators (`repo-id`)

A repository designator uniquely identifies a git repository within a
workspace.

```jsonc
"a1b2c3d4e5f60718293a4b5c6d7e8f90"
```

A repository designator MUST be **32 lowercase hexadecimal characters**
(`^[0-9a-f]{32}$`), minted once using 128 bits of CSPRNG entropy when
`writ init` initializes the repository.

The repository designator is **immutable**: it remains unchanged when a
repository is renamed, transferred between organizations, cloned, or mirrored
across different remotes.

### Storage and discovery

To allow a local clone to determine its own repository identity:

1. **Ref carrier (`refs/writ/meta/repo-id`):** The repository designator is
   recorded in git under `refs/writ/meta/repo-id` as a lightweight ref (or
   single commit holding the repo ID). Because it lives under `refs/writ/*`,
   it is fetched by standard `git fetch` operations, ensuring a fresh clone
   immediately knows its repository ID.
2. **Config cache (`writ.repo-id`):** `writ init` records the repository
   designator in `.git/config` under the key `writ.repo-id`. When present,
   engines and tools MAY read `writ.repo-id` from local config as a fast cache
   to avoid reading git ref objects on every invocation. If `.git/config` lacks
   the key, the engine reads `refs/writ/meta/repo-id` and populates the config
   cache.

## Reference grammar

A reference points to a collaborative object. References appear in op bodies
(for example, an issue linking to a fixing review, or a comment replying to a
review).

### Syntax

A reference string MUST take one of two forms:

```text
reference = [ repo-id "#" ] object-id
```

| Form | Syntax | Meaning | Example |
| --- | --- | --- | --- |
| **Bare reference** | `<object-id>` | Points to an object in the **same repository** carrying the referencing operation. | `0123456789abcdef0123456789abcdef` |
| **Fully-qualified reference** | `<repo-id>#<object-id>` | Points to an object in the repository identified by `<repo-id>`. | `a1b2c3d4e5f60718293a4b5c6d7e8f90#0123456789abcdef0123456789abcdef` |

### Rules

1. **Same-repo scoping:** When an operation references an object in the same
   repository (for example, a comment or approval referencing a review in the
   same repo), producers SHOULD emit the **bare reference** form (`<object-id>`).
   This keeps local references compact and independent of repository
   designators.
2. **Cross-repo scoping:** When an operation references an object in a different
   repository (or when a workspace-scoped issue references a review in a code
   repository), producers MUST emit the **fully-qualified reference** form
   (`<repo-id>#<object-id>`).
3. **Normalization:** References MUST be lowercase-normalized. All hexadecimal
   digits in `<repo-id>` and canonical `<object-id>` MUST be lowercase ASCII
   characters (`0-9`, `a-f`). This guarantees that references are
   **byte-comparable**, which is required because deterministic fold reducers
   use string comparison of IDs and references as deterministic tiebreakers.
4. **No interior whitespace:** References MUST NOT contain whitespace, control
   characters, or multiple `#` characters.

### Short forms and presentation

In human-facing user interfaces (CLI, TUI, web), clients MAY display shortened
prefixes of object IDs (e.g. `a1b2c3d`) or replace `<repo-id>` with the
human-readable repository slug (e.g. `writ#a1b2c3d` or `writtendev/writ#a1b2c3d`).

Short forms are strictly a client display concern. Canonical references stored
in op payloads MUST always use full, unabbreviated identifiers.

## Workspace repository

A **workspace repo** is an ordinary git repository holding workspace-level
collaborative objects and workspace metadata.

> **Definition (normative):** The workspace repo is **an ordinary git repo,
> convention only, no special server behavior**.

Precedent: Gerrit's `All-Projects` and `All-Users` repositories, which store
site-wide configuration and metadata as ordinary git commits.

Being the workspace repository is a **role**, not a kind of repository. Any
member repository MAY play it, and the common cases involve no additional
repository at all (WRIT-113).

### Roles of the workspace repository

1. **Host for workspace-scoped objects:** Objects that are not bound to a
   single code repository — `issue`, `project`, `cycle`, and team/membership
   metadata — live in the workspace repository under standard `refs/writ/*`
   namespaces.
2. **Host for the repository registry:** The workspace repository maintains
   the registry of all member repositories in the workspace.
3. **Ordinary code repository:** A workspace repository is itself an ordinary
   git repository and MAY also contain code, branches, and code reviews.

### The self-workspace default

A repository MAY be its own workspace repository. This is the expected
configuration for a single-repository project or a monorepo: the repository
hosts its own registry (with `is_workspace: true` on its own entry, per
[`spec/repo-ops.md`](repo-ops.md)) alongside its code and reviews, and the
workspace involves no second repository.

A team working across several repositories designates one of them as the
workspace repository, or MAY create a dedicated repository for the role if it
prefers the separation. All three configurations — self, designated member,
dedicated — are equivalent under this specification; nothing in the format
distinguishes them.

### Workspace re-homing

The workspace repository is a *location*, not part of any object's identity.
Object IDs are random (`§Object identifiers`), operations are self-contained
signed commits, and no op payload or envelope field records which repository
hosts it. A workspace therefore moves to a new home by pushing its writ refs
(`refs/writ/*`) to the new repository and updating each member repository's
`writ.workspace` configuration. References — bare or fully-qualified — remain
valid unchanged, because they name `repo-id`s and `object-id`s, never
locations.

### Team scope

One workspace corresponds to one team (decided, WRIT-113). Workspace-scoped
configuration objects — workflow states, labels, settings — are
workspace-global; this specification defines no `team` object type and no team
scoping field in v1. Organizations with multiple teams run multiple
workspaces.

If team scoping is introduced later, it MUST arrive additively: a `team`
object type plus an optional scoping field on affected objects, such that
existing scopeless objects fold as belonging to a default team and older
clients degrade to ignoring the unknown field per
[`spec/forward-compatibility.md`](forward-compatibility.md) — never a breaking
change to existing payloads.

### Repository association

A repository belongs to at most one workspace.

A code repository associates with its workspace repository via local git
configuration (`writ.workspace`, set to a remote URL or path) and by having its
`repo-id` recorded in the workspace repository's registry.

### Read-side aggregation

Workspace homing governs where operations are *written*. On the read side,
clients SHOULD treat the workspace's member repositories additively: fetch
`refs/writ/*` from each registered repository and fold each repository's
objects into one workspace-wide projection, so cross-repository views ("all
reviews across the team's repos") require no additional convention. Aggregation
is a projection/client concern; the fold itself remains per-repository, pure,
and unchanged.

## Repository registry

The workspace repository maintains a folded repository registry that maps
immutable `repo-id` designators to mutable human-readable slugs and remote
URLs.

### Entry shape

A repository registry entry is a JSON object with the following fields:

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

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `repo_id` | string | yes | The repository's 32-hex designator (`^[0-9a-f]{32}$`). |
| `slug` | string | yes | Human-readable repository slug (e.g. `writtendev/writ` or `backend`). Non-empty string without whitespace. |
| `remotes` | array of strings | no | Known git remote URLs for cloning and fetching the repository. |
| `is_workspace` | boolean | no | `true` if this repository is the workspace repository itself. Default is `false`. |

Unknown fields MUST be preserved and ignored, per standard envelope evolution
rules.

### Operations and fold

The concrete op vocabulary for creating and modifying repository registry
entries (e.g. `repo/create`, `repo/set-slug`, `repo/add-remote`) is specified in
[`spec/repo-ops.md`](repo-ops.md). This document defines the folded output shape
that resolution consumes.

## Reference resolution

Reference resolution is the process of mapping a reference string to a target
repository and object ID:

$$\text{resolve}(\text{reference}, \text{local\_repo\_id}, \text{registry}) \rightarrow \text{ResolvedReference} \mid \text{UnresolvedReference}$$

### Resolution algorithm

Given:
- `reference`: A reference string.
- `local_repo_id`: The 32-hex designator of the local repository where the op
  resides.
- `registry`: The folded repository registry from the workspace repository
  (a list or map of registry entries).

The resolution procedure executes as follows:

1. **Parse reference:**
   - If `reference` contains no `#`:
     - `designator = ""`
     - `target_object_id = reference`
   - If `reference` contains `#`:
     - Split at the first `#`: `designator = reference[0:index]`, `target_object_id = reference[index+1:]`.
2. **Same-repo short-circuit:**
   - If `designator == ""` OR `designator == local_repo_id`:
     - Return **ResolvedReference**:
       - `scope = "local"`
       - `repo_id = local_repo_id`
       - `object_id = target_object_id`
3. **Cross-repo registry lookup:**
   - If `designator != ""` and `designator != local_repo_id`:
     - Search `registry` for an entry where `entry.repo_id == designator`.
     - If a matching entry $E$ is found:
       - Return **ResolvedReference**:
         - `scope = "cross-repo"`
         - `repo_id = E.repo_id`
         - `slug = E.slug`
         - `remotes = E.remotes`
         - `object_id = target_object_id`
     - If no matching entry is found (or if the registry is unavailable):
       - Return **UnresolvedReference**:
         - `scope = "unresolved"`
         - `reference = reference`
         - `designator = designator`
         - `object_id = target_object_id`
         - `reason = "unknown_repo"`

### Unresolved reference preservation rule

If a reference cannot be resolved (for example, because the target repository
has not yet been registered in the workspace, or the workspace repository is not
currently accessible), the reference is **preserved and surfaced as
unresolved, never dropped or rewritten**.

This mirrors the orphaned-anchor rule in comment anchoring (`spec/anchors.md`):
unresolvable data remains intact in the op payload and projection, preserving
historical intent and automatically resolving if the target repository is later
registered or fetched.

### Separation from fold

Reference resolution is **explicitly not the fold's job**.

The fold reducer is a pure function that processes operations into domain state
carrying reference strings verbatim as data. Resolution requires access to
external context (the workspace repository registry and local repository
checkouts), which may vary depending on what repositories are cloned locally.
Separating resolution from fold ensures that `fold(ops) → state` remains 100%
deterministic and produces byte-identical results across all machines regardless
of local clone topology.

Resolution is performed by the projection layer (`engine/projection`) and
queried by client porcelain.

## Out of scope

The following concerns are explicitly out of scope for this specification:

- **Workspace permission semantics:** Access control, branch protection, and
  write permissions are governed by the underlying git transport and hosting
  provider (e.g. SSH keys, forge repository permissions). Writ defines no
  custom server-side permission or ACL system. Tracked in ARCHITECTURE.md
  §Known-hard list ("workspace-repo permission semantics").
- **Identity mapping:** Mapping cryptographic signing keys (SSH/GPG) to
  directory identities (LDAP, SSO, email) is tracked in ARCHITECTURE.md
  §Known-hard list.
- **Backlink indexing:** References are strictly one-directional in op payloads
  (e.g. issue → review). Bidirectional links ("find all reviews addressing this
  issue") are computed dynamically by the local SQLite projection index, not
  recorded in op graphs.
- **Cross-repo transactional atomicity:** Git operations are per-repository.
  Cross-repository references are eventually consistent across repository syncs;
  no distributed 2-phase commit or transactional locking across distinct git
  repositories is attempted.
