# Forward compatibility & unknown-op rules

Status: **normative**. The key words MUST, MUST NOT, SHOULD, and MAY are
to be interpreted as described in RFC 2119.

Writ operations are append-only and distributed across independent writers.
Because writers update at different cadences, older and newer clients routinely
collaborate on the same objects. This document defines the forward-compatibility
model: how implementations handle unknown op types, unrecognized fields, and
future schema versions, ensuring that schema evolution never requires flag days
or lockstep upgrades (ARCHITECTURE.md §The op envelope).

## The core principle: preserve and ignore

Implementations MUST **preserve and ignore** op types and fields they do not
understand — never drop. Old clients must not destroy new clients' data. An
older client encountering operations or fields emitted by a newer client MUST
retain them in the DAG, replicate them during sync, and maintain them through
all storage and codec operations unmodified.

## The three-way disposition split

When a conforming reader encounters an op in an object's history, it classifies
the op into one of three mutually exclusive categories:

1. **Malformed (Rejected):** The op fails reader validation as defined in
   `spec/op-envelope.md` §Reader validation:
   - The commit tree does not contain exactly one entry (`op.json`, mode `100644`).
   - The payload bytes fail the byte-equality rule (invalid canonical JSON,
     duplicate keys, unescaped control characters, lone surrogates, or non-canonical form).
   - The payload fails schema validation against `spec/schemas/op-envelope.schema.json`
     (missing required envelope fields, invalid field types, or malformed identifiers).
   - The committer identity and timestamp are not byte-identical to the author's.

   Malformed ops are invalid data. A conforming reader MUST reject them at parse
   time with an explicit error. Malformed ops MUST NOT enter the DAG and MUST NOT
   be folded into domain state.

2. **Interpretable:** The op is a valid envelope whose `(object_type, op_type, op_version)`
   triple is implemented by the reader. The reader decodes the op's `body` and
   folds its effects into domain state according to the op type's normative specification.

3. **Valid-but-uninterpretable (Opaque):** The op is a valid envelope whose
   `object_type` is unknown to the reader, whose `op_type` is unknown under a known
   `object_type`, or whose `op_version` is greater than the highest version the
   reader implements.

   **Unknown is not invalid.** An uninterpretable op is a valid, well-formed
   operation. It MUST remain in the DAG, stay reachable, be synchronized to peers,
   and be preserved byte-for-byte. A reader MUST NOT reject or discard an op simply
   because its type or version is uninterpretable.

## Unknown fields (per-field tolerance)

Forward compatibility applies to individual fields as well as whole operations:

- **Envelope-level fields:** The op envelope schema explicitly allows additional
  properties (`additionalProperties: true`). Any unrecognized top-level property
  in `op.json` MUST be preserved and ignored.
- **Body-level fields:** An op whose type and version are known MAY contain
  unrecognized fields inside `body` (added by subsequent minor revisions or client
  extensions). Readers MUST preserve and ignore unrecognized fields within `body`.
- **Nested structures:** Unrecognized fields in nested objects and arrays within
  `body` MUST likewise be preserved and ignored.

An unknown field MUST NOT cause schema rejection, MUST NOT prevent an otherwise
interpretable op from folding, and MUST NOT alter the folded values of known fields.

## Fold behavior and materialized state

Fold is a pure, deterministic function (`ops in, state out`) that computes
materialized state from an object's op DAG. The presence of uninterpretable ops
governs fold as follows:

### Field isolation

An uninterpretable op **MUST NOT change or perturb the folded value of any field
a reader understands**.

Two readers with different capability levels evaluating the same DAG MUST arrive
at identical values for all mutually understood fields. Ordering rules, sequence
numbers, tiebreak counters, and state transitions MUST NOT be affected by the
interleaving of uninterpretable ops.

### Opaque record visibility

Folded state MUST surface uninterpretable ops as an explicit, opaque list rather
than skipping them silently. For each uninterpretable op encountered in an object's
DAG, the folded object MUST expose a record containing:

- `op_id`: the commit SHA of the op
- `object_type`: the object type string from the payload
- `op_type`: the op type string from the payload
- `op_version`: the integer version from the payload

No fields from `body` are exposed in the opaque record. Surfacing opaque ops
makes the presence of newer-client modifications observable in user interfaces
(e.g., displaying "3 operations from a newer version of Writ") and verifiable in
conformance goldens, without guessing at payload semantics.

### Stale vs. corrupt state

Ignoring an uninterpretable op produces a **stale view, never a corrupt one**.
For example, if a newer client closes a review using a new op type that an older
client does not recognize, the older client continues to display the review as
open (accompanied by the opaque op record). This trade-off is deliberate: stale
views are safe and honest, whereas guessing semantics causes data corruption.

### Fault isolation

An uninterpretable op MUST NOT invalidate other ops in the DAG or cause the
containing object to fail. Fold MUST process all interpretable ops in the DAG
normally, regardless of how many uninterpretable ops are present.

## Version-bump semantics

Every op payload specifies an integer `op_version` (≥ 1). The evolution of op
schemas follows these rules:

1. **Additive changes do not bump version:** Adding new optional fields to an
   existing op type MUST NOT bump `op_version`. Unknown-field tolerance carries
   additive extensions transparently.
2. **Breaking changes bump version:** Any change that alters the meaning of an
   existing field, adds a required field, changes a default value, or modifies
   fold semantics MUST bump `op_version` to the next integer.
3. **Monotonic, per-type version space:** `op_version` is scoped to the
   `(object_type, op_type)` pair. Version numbers are strictly monotonic positive
   integers and MUST NEVER be reused. An `op_type` name MUST NEVER be repurposed
   for different semantics.
4. **Producer constraint:** A producer MUST NOT emit an `op_version` whose
   semantics it does not implement.
5. **No best-effort downgrade:** A reader that implements up to version *N−1*
   encountering a version *N* op MUST treat the op as uninterpretable (opaque).
   Readers MUST NOT attempt best-effort downgrade or interpret version *N* under
   version *N−1* rules. Best-effort downgrading is prohibited because it allows
   older clients to write or derive states that newer clients never authored.

## Round-trip preservation per leg

Preservation requires specific guarantees across all four engine subsystems:

- **Codec leg:** The unit of preservation is the raw payload blob bytes (`op.json`).
  Any decoding operation that will be followed by a re-emit (such as rebasing or
  re-signing commits) MUST retain the original payload bytes. Implementations
  MUST NOT re-serialize payloads from decoded structs with unknown fields dropped,
  as lossy re-serialization changes canonical bytes and invalidates commit signatures.
- **Fold leg:** Fold never writes to disk. Fold preservation requires that the
  reducer function neither errors nor terminates when encountering uninterpretable
  ops, and includes their opaque descriptors in the folded output.
- **Projection leg:** The SQLite projection is a droppable, rebuildable cache,
  never an authoritative store. Cache invalidation and rebuilds MUST reproduce
  the exact same folded state. Implementations MUST NOT prune uninterpretable ops
  from the projection and treat the pruned state as authoritative.
- **Sync leg:** Sync operates at the git transport layer over commit chains and
  refs. Sync transport MUST be type-blind: implementations MUST NOT filter,
  exclude, or rewrite ops during fetch or push based on op type, version, or
  interpretability.

## Explicit prohibitions ("Never drop")

To prevent data destruction across client generations, conforming implementations
MUST NOT:

1. Compact, garbage-collect, or prune ops from the DAG that they cannot interpret.
2. Strip or discard unknown fields when decoding and re-emitting operations.
3. Filter out uninterpretable ops or unknown object types during sync (push/fetch).
4. Abort fold or mark an entire collaborative object invalid because one or more
   ops are uninterpretable.
5. Guess semantics or apply fallback interpretation to unknown op types or future
   versions.

## Normative rules summary

| Rule ID | Statement | Subsystem |
| --- | --- | --- |
| `FC-1` | An op envelope satisfying reader validation with an unknown `object_type`, unknown `op_type`, or unimplemented `op_version` MUST be treated as valid-but-uninterpretable and retained in the DAG. | Reader / DAG |
| `FC-2` | Unknown top-level fields in `op.json` MUST be preserved byte-for-byte and ignored by readers. | Codec / Envelope |
| `FC-3` | Unknown fields inside `body` (including arbitrarily nested objects and arrays) MUST be preserved byte-for-byte and ignored by readers. | Codec / Payload |
| `FC-4` | Uninterpretable ops MUST NOT alter or perturb the folded value of any field understood by the reader. | Fold |
| `FC-5` | Folded state MUST surface each uninterpretable op as an opaque record containing `op_id`, `object_type`, `op_type`, and `op_version`. | Fold / Projection |
| `FC-6` | Additive optional fields MUST NOT bump `op_version`. | Schema / Evolution |
| `FC-7` | Breaking schema changes, altered field semantics, changed defaults, or modified fold behavior MUST bump `op_version` to the next monotonic integer. | Schema / Evolution |
| `FC-8` | `op_version` numbers MUST be strictly monotonic per `(object_type, op_type)` and never reused; op type names MUST NOT be repurposed for different semantics. | Schema / Evolution |
| `FC-9` | Producers MUST NOT emit an `op_version` whose semantics they do not implement. | Producer |
| `FC-10` | Readers MUST NOT downgrade or guess semantics for an `op_version` higher than they implement; higher versions MUST be treated as uninterpretable. | Reader / Fold |
| `FC-11` | Decoders that re-emit ops MUST retain and write back the exact original payload bytes rather than re-serializing lossy parsed structs. | Codec |
| `FC-12` | Fold MUST NOT error, crash, or abort when encountering uninterpretable ops. | Fold |
| `FC-13` | Projection caches MUST be fully rebuildable from the raw DAG without losing or mutating uninterpretable ops. | Projection |
| `FC-14` | Sync transport MUST NOT filter, prune, or inspect ops based on op type, version, or interpretability. | Sync |
| `FC-15` | An uninterpretable op MUST NOT invalidate or fail the containing collaborative object or other valid ops in the DAG. | Engine / DAG |
