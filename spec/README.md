# Spec & Conformance Fixtures

Per ARCHITECTURE.md ("Spec = fixtures"), the conformance fixtures are the
ground truth for the Writ specification. prose describes intent, but
the conformance corpus defines observable correctness byte-for-byte.

## Repository Layout

```
spec/
├── README.md               — this document: conformance model & independent implementation guide
└── fixtures/
    ├── README.md           — fixture storage, repo generation, and test harness details
    ├── corpus.go           — loads descriptions from testdata/descriptions/
    ├── description.go      — YAML description parser and schema
    ├── diff.go             — standard unified diff engine for readable failure/update diffs
    ├── generate.go         — deterministic bare git repository builder
    ├── harness.go          — golden-file test harness and fixture family runner
    ├── identity.go         — fixture identities (alice, bob) and signing config
    ├── manifest.go         — manifest data model (SHAs, trees, parents, refs)
    ├── sign.go             — ed25519 SSH commit signing
    ├── tree.go             — git tree object generation
    ├── gen/                — CLI command to regenerate fixture repos and check/update goldens
    ├── keys/               — throwaway ed25519 keypairs for deterministic signatures
    └── testdata/
        ├── descriptions/   — declarative YAML descriptions of fixture git histories
        └── golden/         — golden JSON outputs pinned byte-for-byte
```

## Conformance Model

Any Writ implementation (the reference Go engine or an independent
implementation in Rust, Python, TypeScript, etc.) is verified by running
the conformance fixture corpus through its pipeline and comparing the output
byte-for-byte against the golden files in `spec/fixtures/testdata/golden/`.

### The Fixture Lifecycle

1. **Declarative Description:** Checked in as human-readable YAML under
   `spec/fixtures/testdata/descriptions/`. Specifies refs, commit chains,
   authors, timestamps, and file contents.
2. **Deterministic Repository Generation:** Built into a bare git repository
   with deterministic ed25519 SSH commit signatures.
3. **Execution Under Test:** The implementation under test reads the
   repository's `refs/writ/*` namespaces, enumerates operations, and folds
   them into materialized state.
4. **Golden Comparison:** The output is compared byte-for-byte against
   `testdata/golden/<name>.json`.

## Reusing the Corpus in an Independent Implementation

An independent implementation can reuse the conformance corpus in one of
two ways:

### Option A: Pre-generate Repositories via the Tooling

Generate real git repositories on disk using the reference generator:

```bash
go run ./spec/fixtures/gen -out /tmp/writ-fixtures
```

This creates standard bare git repositories in `/tmp/writ-fixtures/<name>`
that any git library (e.g. `git2-rs`, `pygit2`, `nodegit`, or system git)
can inspect, open, and read.

Your test suite then:
1. Iterates over each repository in `/tmp/writ-fixtures/`.
2. Reads the refs under `refs/writ/` (or fixture refs).
3. Executes your fold reducer / projection pipeline.
4. Serializes the folded state to canonical JSON.
5. Asserts that the output matches `spec/fixtures/testdata/golden/<name>.json`
   byte-for-byte.

### Option B: Self-Contained Repository Generation

If your implementation prefers not to depend on the Go generator, you can
reproduce repository generation directly:
1. Parse the YAML descriptions in `spec/fixtures/testdata/descriptions/`.
2. Read the author private keys from `spec/fixtures/keys/`.
3. Construct standard git commit objects with RFC 3339 UTC timestamps.
4. Sign commits using standard OpenSSH ed25519 signatures (`ssh-keygen -Y sign -n git`).
   Because RFC 8032 ed25519 signatures are purely deterministic, your generated
   commits will have the exact same SHAs as the reference manifests.
