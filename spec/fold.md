# Fold semantics & concurrency rules

Status: **normative**. The key words MUST, MUST NOT, SHOULD, and MAY are to be
interpreted as described in RFC 2119.

Writ is an event-sourcing engine: current state is never stored
authoritatively; it is derived by deterministically **folding** an object's
operations. Concurrent writes do not conflict at push time — they coexist as
sibling operations in the git commit DAG and are reconciled at fold time by
spec-defined rules (ARCHITECTURE.md §Core model).

This document defines fold semantics completely: the input model, the
causality-monotone effective timestamp $t^*$, the deterministic total order,
the definition of concurrency, the generic tombstone and deletion/edit
interleaving model, and the **closed catalogue of per-field merge strategies**.
A conforming implementation written from this specification alone MUST produce
the exact same total order and folded state byte-for-byte on all inputs. The
normative test vectors in `spec/testdata/fold/` enforce this requirement.

## 1. Input model

Fold is a pure, deterministic function: `fold(ops) → state`. It performs no
I/O and accesses no git storage directly.

### The input set

The input to fold is a finite set of operations $S$ sharing a single
`object_id`. How operations are discovered and enumerated across writer chain
refs (`refs/writ/<writer-id>/<type>`) is specified in WRIT-7. Fold receives the
set of operations that are present in the local repository and belong to the
target object.

Each operation $u \in S$ carries:
- `id`: the git commit SHA (lowercase hex string).
- `parents`: the list of parent commit SHAs from the git commit carrier.
- `time`: the commit author timestamp as an integer (seconds since Unix epoch UTC, `1970-01-01T00:00:00Z`).
- `object_id`: the target object identifier from the payload carrier.
- `op_type`: the operation type string from the payload carrier.
- `body`: the JSON object containing type-specific fields.

### Ancestry restriction

Because a writer's append chain stores ops for multiple objects sequentially
(ARCHITECTURE.md §Ref layout), an op's git commit parents often include
commits belonging to other objects or non-op commits.

Fold operates strictly on the **ancestry-restricted DAG**:
For an op $u \in S$, its parents in the restricted DAG, denoted
$\text{Parents}_S(u)$, are the subset of its commit parents that belong to the
same input set $S$:

$$\text{Parents}_S(u) = [\, p \in u.\text{parents} \mid p \in S \,]$$

Edges pointing to commits outside $S$ are excluded from $\text{Parents}_S(u)$.
The order of parents in $\text{Parents}_S(u)$ preserves their relative order in
$u.\text{parents}$.

### Partial ancestry and truncated graphs

In distributed git workflows, a repository may contain a shallow clone, a
partial fetch, or an op referencing a parent commit that has not yet been
fetched.

A missing parent commit **truncates** ancestry: if a parent commit SHA is not
present in $S$, that edge is omitted from $\text{Parents}_S(u)$. Fold MUST NOT
fail, error, or reject an input set due to missing parent commits. Fold is a
pure function of the operations present at the time of evaluation; new state
becoming available when additional operations arrive is ordinary fetch
progression, not non-determinism.

Malformed input that contains a directed cycle within $S$ is invalid; a
conforming reader MUST reject an input set containing cycles.

## 2. Causality and concurrency

### Causality (happens-before)

Within the restricted DAG of $S$:
- An op $u \in S$ is an **immediate parent** of $v \in S$ if $u \in \text{Parents}_S(v)$.
- An op $u \in S$ **causally precedes** $v \in S$ (denoted $u \prec v$, or "$u$ happens-before $v$") if there is a non-empty directed path of parent-to-child edges from $u$ to $v$ in the restricted DAG.

### Concurrency

Two distinct operations $u, v \in S$ ($u \ne v$) are **concurrent** (denoted
$u \parallel v$) if and only if neither causally precedes the other:

$$u \parallel v \iff (u \not\prec v \land v \not\prec u)$$

Concurrency is a structural property of the restricted DAG. Implementations
MUST NOT approximate concurrency using timestamps.

## 3. Effective time $t^*$

Raw commit author timestamps cannot be used directly to order operations: clock
skew or a misconfigured clock could assign a child op an author timestamp
earlier than its parent ($u \prec v$ but $v.\text{time} < u.\text{time}$), which
would contradict causality.

Fold derives a **causality-monotone effective timestamp** $t^*(u)$ for every
op $u \in S$:

$$t^*(u) = \max\left(u.\text{time}, \max_{p \in \text{Parents}_S(u)} t^*(p)\right)$$

If $\text{Parents}_S(u)$ is empty (a root op in the restricted DAG),
$t^*(u) = u.\text{time}$.

### Properties of $t^*$

1. **Causal monotonicity:** For all $u, v \in S$, if $u \prec v$, then $t^*(u) \le t^*(v)$. A descendant can never have an effective timestamp earlier than its ancestor.
2. **Derived at fold time:** $t^*$ is computed entirely from data already carried in the op envelope (commit author time and commit parents). It requires no new envelope fields.
3. **Intuitive wall-clock ordering:** For concurrent operations ($u \parallel v$), $t^*$ reflects the author timestamp, ensuring concurrent edits order naturally by real-world time under normal clock conditions.

### Adversarial and clock-skew exposure

Because fold is pure and sees only signed operations, it cannot detect whether
a fast clock was accidental skew or intentional grinding. A writer with a clock
set far in the future will win last-writer-wins races against contemporary
writers until other writers advance past that time. Similarly, git commit SHAs
can be ground to produce a lexicographically smaller op ID to win tiebreaks.
This is an accepted trade-off of pure, offline, decentralized CRDT/event-fold
systems; trust and key authorization are handled at the signing and
verification layers (WRIT-22), not inside fold.

## 4. The deterministic total order

Fold consumes operations in a single, deterministic **total order** $L$, which
is a sequence containing every operation in $S$ exactly once.

The total order is constructed using a deterministic topological sort
(Kahn's algorithm with a priority queue) ordered by $(t^*, \text{id})$:

### Total order algorithm

1. Let $E = \emptyset$ be the set of already emitted operations.
2. Let $R \subseteq S$ be the set of **ready** operations whose restricted parents have all been emitted:
   $$R = \{\, u \in S \setminus E \mid \text{Parents}_S(u) \subseteq E \,\}$$
3. Let $L = [\,]$ be the ordered list of emitted operations.
4. While $R$ is not empty:
   a. Select the candidate $u \in R$ that minimizes the tuple $(t^*(u), u.\text{id})$:
      - Compare $t^*(u)$ and $t^*(v)$ as signed 64-bit integers (smaller timestamp comes first).
      - If $t^*(u) == t^*(v)$, compare $u.\text{id}$ and $v.\text{id}$ lexicographically by byte values as lowercase ASCII hexadecimal strings (smaller byte value comes first).
   b. Remove $u$ from $R$.
   c. Append $u$ to $L$.
   d. Add $u$ to $E$.
   e. For every child $v \in S \setminus E$ where $u \in \text{Parents}_S(v)$:
      if $\text{Parents}_S(v) \subseteq E$, add $v$ to $R$.
5. If $|L| \ne |S|$ upon termination, the graph contains a cycle and the input MUST be rejected. Otherwise, $L$ is the deterministic total order.

### Total order as the single primitive

The total order $L = [o_1, o_2, \dots, o_n]$ is the single primitive that
drives all fold execution. Reducers process operations strictly in the sequence
given by $L$.

**The ancestor/descendant equal-$t^*$ rule:**
If an ancestor $A$ and a descendant $D$ ($A \prec D$) have the same effective
timestamp $t^*(A) = t^*(D)$, but $A.\text{id} > D.\text{id}$ in byte order, a
naive key comparison would rank $D$ before $A$. However, because $D$ cannot
enter the ready set $R$ until $A \in E$, topological validity is strictly
preserved: $A$ is emitted before $D$ in $L$. Last-writer-wins evaluates
operations in $L$ order, so $D$ (the descendant) is processed after $A$ and
overwrites $A$.

## 5. Per-field merge strategies catalogue

To prevent implicit or undefined merge behavior, Writ establishes a
**closed catalogue** of 7 per-field merge strategies.

### The "no implicit behavior" requirement

Every op-vocabulary specification (WRIT-8 through WRIT-11) MUST publish a
complete field-rule table declaring exactly one catalogue strategy for every
field of every body it defines.

A field present in an operation payload that has no declared strategy in the
corresponding vocabulary spec is **not merged**: it is treated as unknown data,
preserved and ignored per the forward-compatibility rule. Implementations MUST
NOT invent default merge behaviors for undeclared fields.

### Catalogue strategies

| Strategy | Target Data Type | Description |
| --- | --- | --- |
| `lww` | Scalar register | Last-Writer-Wins: the last operation in the total order $L$ that writes the field sets its final value. |
| `create-once` | Immutable register | The first operation in the total order $L$ that writes a non-null value sets the field; all subsequent writes to the field are ignored. |
| `set-union` | Grow-only set | Set of distinct elements; the folded state is the union of all elements added by any operation in $S$. |
| `set-observed-remove` | Add-Wins set | Set supporting addition and removal; additions win over concurrent removals. |
| `append` | Ordered list | Sequence of items (e.g. comments, revisions) ordered strictly by the total order $L$ of the operations that created them. |
| `tombstone` | Deletable entity | Entity deletion state; deletion wins over concurrent edits while edits still fold into state. |
| `lattice` | Join-semilattice | Monotone status transitions governed by a declared join operator ($\sqcup$). |
| `keyed-lww` | Keyed register map | A map from a declared composite key to a register; `lww` applied independently within each key. |

Counters are deliberately omitted from this catalogue; adding a counter
strategy requires a spec amendment.

---

### Detailed strategy specifications

#### 1. `lww` (Last-Writer-Wins)
- **Initial state:** `null` / unset (or schema default).
- **Reduction:** As operations are consumed in total order $L$, if an operation's `body` specifies a value for the field, that value replaces the current state.
- **Result:** The value written by the latest operation in $L$ that specified the field.

#### 2. `create-once`
- **Initial state:** `null` / unset.
- **Reduction:** As operations are consumed in total order $L$, if the field is currently unset and an operation's `body` specifies a non-null value, the field is set to that value. Subsequent operations that specify a value for this field have no effect.
- **Result:** The value set by the earliest operation in $L$ that specified the field.

#### 3. `set-union`
- **Initial state:** Empty set $\emptyset$.
- **Reduction:** Any operation specifying one or more elements adds them to the set.
- **Empty elements are dropped:** An element whose value, after any normalization the field declares (`spec/identifiers.md` §Person identifiers), is the empty string MUST NOT enter the set. This rule applies to **every** item-valued field regardless of its op type, not only to person-valued fields: an empty label, an empty remote URL, and an empty assignee are equally meaningless, and reducers MUST agree on discarding them. Producers are already forbidden from emitting such elements by the vocabulary schemas; the rule exists so that a non-conforming or future writer cannot make two conforming readers disagree. Dropping governs materialized state only and does not weaken the preserve-and-ignore rule (`spec/forward-compatibility.md`): the operation carrying the element remains in the DAG, reachable, replicated, and byte-for-byte intact.
- **Result:** The mathematical set union of all added elements. In serialized state, elements are emitted in canonical sorted order (UTF-16 code unit order for strings, ascending numerical order for numbers). An operation whose elements are all dropped still counts as a write of the field: the field is present in folded state with the empty set as its value.

#### 4. `set-observed-remove` (Add-Wins OR-Set)
- **Initial state:** Empty set $\emptyset$.
- **Mechanism:**
  - An add operation $a \in S$ adds an element $x$.
  - A remove operation $r \in S$ removes an element $x$ and targets all add operations of $x$ in its causal past ($a \prec r$).
  - An element $x$ is present in the folded set if and only if there exists at least one add operation $a \in S$ for $x$ such that no remove operation $r \in S$ for $x$ causally follows $a$:
    $$\text{present}(x) \iff \exists a \in S \text{ s.t. } \text{adds}(a, x) \land (\forall r \in S \text{ s.t. } \text{removes}(r, x), a \not\prec r)$$
- **Concurrency behavior:** If an addition $a$ and removal $r$ of the same element $x$ are concurrent ($a \parallel r$), the addition wins and $x$ is present in the folded set.
- **Empty elements are dropped:** As in `set-union` (§5.3), an element whose value, after any normalization the field declares, is the empty string MUST NOT enter either the add side or the remove side of the OR-set. This rule applies to **every** item-valued field regardless of its op type — `label` items are dropped on the same terms as `assign` items, even though only the latter are normalized before the test.
- **Result:** Present elements emitted in canonical sorted order. An operation whose elements are all dropped still counts as a write of the field: the field is present in folded state with the empty set as its value.

#### 5. `append`
- **Initial state:** Empty list `[]`.
- **Reduction:** When an operation in total order $L$ appends an entry (or entries), the entry is added to the tail of the list.
- **Result:** Entries ordered strictly by the position in $L$ of their producing operations. If a single operation produces multiple entries, their relative order within that operation is preserved.

#### 6. `tombstone` (Deletion and edit interleavings)
- **Initial state:** `deleted = false`.
- **Semantics:**
  - A delete operation marks the entity as `deleted = true`.
  - An undelete operation marks the entity as `deleted = false`.
  - **Causal undelete requirement:** An undelete operation $u$ only clears deletions in its causal past:
    $$\text{deleted} \iff \exists d \in S \text{ s.t. } \text{is\_delete}(d) \land (\forall u \in S \text{ s.t. } \text{is\_undelete}(u), d \not\prec u)$$
  - **Concurrent delete and undelete:** If a delete $d$ and undelete $u$ are concurrent ($d \parallel u$), deletion wins (`deleted = true`).
  - **Concurrent delete and edit:** If a delete $d$ and edit $e$ are concurrent ($d \parallel e$), deletion wins (`deleted = true`). However, the field edits in $e$ MUST still be applied to the entity's underlying folded state according to their respective field strategies. Content remains completely reconstructible and is never discarded.
  - **Causal edit after delete:** An edit $e$ that causally succeeds a delete ($d \prec e$) without an intervening undelete applies its field writes to state, but the entity remains `deleted = true` unless an explicit undelete operation is present.

#### 7. `lattice` (Monotone status transitions)
- **Initial state:** The bottom element $\bot$ of the declared semilattice.
- **Semantics:** The field's allowed values form a bounded join-semilattice $(V, \sqcup, \le)$ with partial order $\le$ and join operation $\sqcup$.
- **Reduction:** When an operation writes value $v \in V$, the new state is $\text{state} \sqcup v$.
- **Result:** Because $\sqcup$ is associative, commutative, and idempotent, concurrent transitions $u \parallel v$ reconcile deterministically to $v_u \sqcup v_v$ regardless of arrival or topological order.

#### 8. `keyed-lww` (Keyed Last-Writer-Wins registers)
- **Initial state:** Empty map `{}`.
- **Key:** A field declaring this strategy MUST also declare a non-empty, ordered list of body fields forming its key $k$. Two operations address the same register if and only if their key tuples are equal, compared component-wise over canonical values.
- **Reduction:** As operations are consumed in total order $L$, an operation writing the field replaces the value stored at its own key $k$ and leaves every other key untouched — that is, `lww` applied independently within each key.
- **Result:** For each key, the value written by the latest operation in $L$ bearing that key. Entries are serialized as a list of `{key, value}` records ordered by their key tuples, compared component-wise.
- **Normalization:** Where a body field carries a person identifier, the normalization of `spec/identifiers.md` applies to the **stored value** exactly as it applies to the key component derived from it. A reducer MUST NOT normalize a person identifier for keying and then store it verbatim: where `approval.subject` is a string, the folded entry's key component and its value are the same normalized string. A non-string `subject` is schema-invalid against `person-id` (`spec/schemas/identifiers.schema.json`) and folds verbatim.
- **Why this is not `lww` or a set:** Registers scoped to a key — one vote per (voter, revision), one status per (revision, check name) — need a later write under one key to leave the others alone. Plain `lww` would collapse them to a single register; a set has no notion of a value being replaced.

## 6. Anchors and external references

Comment anchors (`spec/anchors.md`) and external references are **opaque to fold**:
- Fold treats anchor objects as raw data values and preserves them verbatim.
- Fold MUST NOT perform anchor resolution or attempt to open git trees/blobs. Anchor resolution is the responsibility of the Anchor Resolver (ARCHITECTURE.md §The six machines #4) and runs during projection materialization.

## 7. Unknown operations and forward compatibility

In accordance with the op envelope specification (`spec/op-envelope.md`) and
forward compatibility rules (WRIT-15):
- An operation carrying an unknown `op_type` or unrecognized fields MUST NOT cause fold to error or abort.
- Unknown operations remain full members of the restricted DAG: they participate in $t^*$ calculation and the total order $L$, maintaining causal relationships for any descendant operations.
- An unknown operation contributes no field writes to recognized fields.

## 8. State serialization order

To guarantee that folded state is byte-identical across independent
implementations:
- JSON object fields are serialized canonically per `spec/canonicalization.md`.
- Collections derived from `append` strategies are ordered by the total order $L$.
- Collections derived from `set-union` and `set-observed-remove` are serialized as JSON arrays sorted in canonical code unit order.

## 9. Conformance data

The normative test vectors and fixture repositories verify compliance:
- `spec/testdata/fold/order/`: Abstract op graphs testing total order derivation across linear chains, multi-writer forks, equal-$t^*$ ties, skewed clocks, ancestry truncation, and multi-object interleaving.
- `spec/testdata/fold/merge/`: Op graphs testing each catalogue strategy, including delete/edit interleavings and concurrent mutations.
- `spec/fixtures/testdata/descriptions/fold-*.yaml` and `spec/fixtures/testdata/golden/fold/`: Signed fixture repositories exercising concurrent field edits, multi-device writer races, LWW and tiebreaks, per-field merge strategies, and ancestry truncation.
- `spec/fixtures/testdata/descriptions/issue-*.yaml` and `spec/fixtures/testdata/golden/issue/`: Signed fixture repositories exercising issue lifecycle, state transitions, concurrent assign and label OR-sets, and cross-repo links.
- `spec/fixtures/testdata/descriptions/project-*.yaml` and `spec/fixtures/testdata/golden/project/`: Signed fixture repositories exercising project lifecycle, status transitions, and issue membership races.
- `spec/fixtures/testdata/descriptions/cycle-*.yaml` and `spec/fixtures/testdata/golden/cycle/`: Signed fixture repositories exercising cycle lifecycle, concurrent date updates, and issue membership.
