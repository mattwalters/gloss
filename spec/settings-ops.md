# Settings Operations — configuration, scales, and cadence (v1)

Status: **normative**. Schema: [`schemas/settings-ops.schema.json`](schemas/settings-ops.schema.json).
Field rules: [`testdata/settings/field-rules.json`](testdata/settings/field-rules.json).

This document defines the operation vocabulary, payload schemas, and fold
semantics for settings in Writ (`object_type: "settings"`). Settings store
shared, mergeable configuration for the repository they live in.

The key words MUST, MUST NOT, SHOULD, and MAY are to be interpreted as
described in RFC 2119.

---

## 1. Scope & Object Model

Settings define configuration that must be shared across all clients, devices,
and users of a repository.

Unlike local git configuration (which is per-clone and per-user) or flat repo
files like `.writ/settings.toml` (which reintroduce three-way git merge conflicts),
Writ settings are modeled as a collaborative object stored on append chains
(`refs/writ/<writer-id>/settings`) and folded deterministically via per-field
Last-Writer-Wins (`lww`).

### 1.1. Scoping
Settings are repository-scoped: they belong to the repository whose `refs/writ/*`
carry their ops, exactly like every other collaborative object. There is no
routing to a designated configuration repository, and there is no team partition
in v1. A team wanting one shared configuration across several repositories
composes that above writ, not inside it.

### 1.2. Singleton Object Identifier
To eliminate distributed ID coordination across writers, the settings object
uses a well-known canonical 32-character hexadecimal object ID:
`00000000000000000000000073657474` (representing `"sett"` in ASCII hex, padded
with 24 leading zeroes). Conforming producers MUST write to this object ID for the
canonical settings object. Conforming readers MUST accept any valid 32-hex
`object_id` for forward-compatible settings objects if multiple settings objects
are ever supported in the future.

### 1.3. Estimate Scale Vocabulary & T-Shirt Mapping
Writ supports five estimate scales:

| Scale | Description |
| --- | --- |
| `none` | Estimation is disabled; issues display no estimate field. |
| `fibonacci` | Fibonacci numbers (`0`, `1`, `2`, `3`, `5`, `8`, `13`, `21`). Default. |
| `exponential` | Powers of two (`0`, `1`, `2`, `4`, `8`, `16`). |
| `linear` | Consecutive integers (`0`, `1`, `2`, `3`, `4`, `5`). |
| `t-shirt` | Display mapping over underlying numeric values. |

**The T-shirt scale is a display mapping over numeric estimates.**
An issue's `estimate` field always stores a scalar number (e.g. `1`, `2`, `3`, `5`, `8`).
When `estimate_scale: "t-shirt"` is configured in settings, clients
MUST render these numeric estimates according to the normative display mapping:
- `1` $\rightarrow$ `XS`
- `2` $\rightarrow$ `S`
- `3` $\rightarrow$ `M`
- `5` $\rightarrow$ `L`
- `8` $\rightarrow$ `XL`

If an issue carries an estimate value not defined in the display mapping (or if
`estimate_scale` changes between numeric and t-shirt), clients SHOULD render the
raw numeric value.

The setting `allow_zero_estimates` (boolean, default `false`) governs whether `0`
is accepted and rendered as an estimate.

### 1.4. Cycle Cadence & Boundaries
Cycle cadence fields provide the parameters needed to calculate exact UTC intervals
`[starts_at, ends_at)` for auto-generating future cycles:
- `cycles_enabled` (boolean, default `false`): Governs whether cycle views and cadence
  are active.
- `cycle_duration_weeks` (integer $\ge 1$, default `2`): Length of each cycle in weeks.
- `cycle_start_day` (integer $1 \dots 7$, default `1`): Day of the week on which cycles
  begin, where $1$ is Monday and $7$ is Sunday (ISO 8601 day of week).
- `cycle_cooldown_weeks` (integer $\ge 0$, default `0`): Duration of cooldown period
  between consecutive cycles in weeks.
- `timezone` (string, default `"UTC"`): IANA Time Zone identifier (e.g. `"UTC"`,
  `"America/New_York"`). Governs local calendar midnight boundaries when calculating
  start and end timestamps.

### 1.5. Unknown Settings Key Preservation
Newer clients may write settings properties that older clients do not recognise.
Any unknown property present in the `body` of a `set` operation is treated as an
independent scalar register and merged into the folded state under `unknown_keys`
using Last-Writer-Wins (`lww`) in total order $L$.

Producers updating settings MUST only emit the specific keys being changed in the
`set` operation body. Older clients updating known fields will therefore never
drop, overwrite, or clobber unknown settings keys written by newer clients.

### 1.6. Fresh Repository Defaults
If a repository contains zero `settings` operations, the folded settings state is
defined by the following defaults:
- `name`: `""`
- `identifier`: `""`
- `timezone`: `"UTC"`
- `estimate_scale`: `"fibonacci"`
- `allow_zero_estimates`: `false`
- `cycles_enabled`: `false`
- `cycle_duration_weeks`: `2`
- `cycle_start_day`: `1` (Monday)
- `cycle_cooldown_weeks`: `0`
- `triage_enabled`: `false`
- `unknown_keys`: `{}`

---

## 2. Envelope Binding

Every settings operation is carried in a git commit whose `op.json` payload
conforms to `spec/schemas/op-envelope.schema.json` and
`spec/schemas/settings-ops.schema.json`:

- `object_type` MUST be `"settings"`.
- `op_version` MUST be an integer $\ge 1$. This document specifies version `1`.
- `object_id` MUST be 32 lowercase hex characters (`^[0-9a-f]{32}$`).
  Conforming producers write `00000000000000000000000073657474`.
- `op_type` MUST be `"set"`, or an unknown string tolerated under forward-compatibility
  rules.
- `body` MUST be a JSON object conforming to the schema for `op_type: "set"` and
  `op_version: 1`.

---

## 3. Operation Vocabulary (`op_version: 1`)

The `settings` family defines a single operation type:

| `op_type` | Body Schema | Description |
| --- | --- | --- |
| `set` | `{"name"?: string, "identifier"?: string, "timezone"?: string, "estimate_scale"?: string, "allow_zero_estimates"?: bool, "cycles_enabled"?: bool, "cycle_duration_weeks"?: int, "cycle_start_day"?: int, "cycle_cooldown_weeks"?: int, "triage_enabled"?: bool, ...}` | Updates one or more configuration fields. |

### 3.1. `set`
Updates one or more settings fields. At least one property MUST be present in `body`.
Unknown properties are permitted and preserved.

Example payload:
```json
{
  "object_id": "00000000000000000000000073657474",
  "object_type": "settings",
  "op_type": "set",
  "op_version": 1,
  "body": {
    "name": "Writ Project",
    "identifier": "WRIT",
    "timezone": "America/New_York",
    "estimate_scale": "fibonacci",
    "allow_zero_estimates": false,
    "cycles_enabled": true,
    "cycle_duration_weeks": 2,
    "cycle_start_day": 1,
    "cycle_cooldown_weeks": 0,
    "triage_enabled": false
  }
}
```

---

## 4. Fold Semantics

Folding `settings` operations is a pure, deterministic fold over the operations
sorted into total order $L$ (`spec/ordering.md`).

1. **Initialization:** The fold state begins with the default settings defined in §1.6.
2. **Reduction:** For each operation in total order $L$:
   - If `op_type` is `"set"` and `op_version` is `1`:
     - For each known field present in `body`, overwrite the current state field with
       the value from `body` (Last-Writer-Wins).
     - For each unknown field present in `body`, record or overwrite the entry in
       `unknown_keys` (Last-Writer-Wins).
   - If `op_type` is unknown or `op_version > 1`, preserve the operation in
     `unknown_ops` per `spec/forward-compatibility.md`.
3. **Commutativity:** Because all properties are scalar values resolved via Last-Writer-Wins
   in total order $L$, folding is strictly deterministic and idempotent.
