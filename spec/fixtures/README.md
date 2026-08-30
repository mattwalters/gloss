# Fixture-repo storage & generation (WRIT-59)

The conformance corpus's fixtures are *git repositories* — multi-writer
DAGs, force-pushed histories, signed commits. A `.git` directory can't be
committed as a normal tree inside this monorepo, so this package is the
storage and generation strategy for that corpus.

## Decision

Each fixture repo is built from a declarative YAML description checked
into `testdata/descriptions/`, by the generator in this package. Nothing
about a fixture — refs, commits, file contents, signers, force-pushes —
is opaque: it's reviewable in a PR diff like any other text file. The
actual `.git` directory is never committed; it's built on demand into a
gitignored output directory (`spec/fixtures/out/`, or wherever a caller
points `Generate` — engine tests will generate into `t.TempDir()`).

**Alternatives considered and rejected:**

- **Git bundles** (`git bundle create`). A bundle is a single binary blob
  of packed objects — opaque in a diff, and reviewing a fixture change
  means trusting a regeneration script anyway, so committing the bundle
  buys nothing a description-plus-golden-manifest doesn't already give,
  while making every fixture edit an unreviewable binary diff.
- **Tarballs of a `.git` directory.** Same opacity problem as bundles,
  plus loose-object layout and packfile choices become incidental parts
  of the committed artifact — two developers regenerating the same
  history could produce a bit-different-but-semantically-identical `.git`
  directory (different pack boundaries, `.pack` vs loose objects) and now
  have a spurious diff to explain.

Generation scripts avoid both problems: the description is the reviewable
artifact, the generator is deterministic, and the golden manifest (a
tracked JSON summary — SHAs, not bytes) is what actually pins the
fixture down.

## Format

A description (`Description` in `description.go`) is a list of refs. Each
ref has one or more **generations** — contiguous, independently-rooted
commit chains. A ref with a single generation is ordinary history. A ref
with more than one generation models a **force-push**: every generation
but the last was, at some point, what the ref pointed at; only the last
generation is the ref's value in the generated repo.

A generation can set `keep_as: <ref name>` to also expose its tip under
an auxiliary ref. Without it, an overwritten generation's commits are
still written to the object store (and still covered by determinism and
golden-manifest checks) but nothing in the generated repo points at
them — exactly how a force-pushed-over branch tip behaves in real git
before GC. `keep_as` is for fixtures that need to resolve the
pre-rewrite state on purpose, e.g. the orphaned-anchors family
(ARCHITECTURE.md's anchoring section): a comment anchored to a blob that
a later force-push made unreachable from the branch, but that's still a
real, readable object.

See `testdata/descriptions/force-pushed-branch.yaml` for a worked
example, and `testdata/descriptions/multi-writer-refs.yaml` for two refs
signed by different identities in one repo.

Generations don't share ancestry with each other by design — real
force-pushed history need not share a base with what it replaced, and
independent chains keep both the format and the generator simple. A
fixture that specifically needs a shared-ancestry rewrite can repeat the
shared commits verbatim as a literal prefix in both generations;
content-addressing collapses them onto the same objects, same as real
git.

Every commit names an `author` from the fixed identity set in
`identity.go` (currently `alice`, `bob`) and carries an explicit UTC
`timestamp` — nothing here is ever read from the environment or
`time.Now()`, which is what makes generation reproducible.

## Signing and the test keys

Every commit is signed, via `ssh-keygen -Y sign` under the `git`
namespace (the same one `git commit -S` uses), with the ed25519 key
belonging to its author identity. **ed25519 signatures are deterministic
(RFC 8032)** — this is load-bearing, not incidental: since a signature
is embedded in the commit object before it's hashed, a non-deterministic
signature scheme (RSA-PSS, ECDSA) would make the resulting commit SHA
different on every regeneration, breaking the whole byte-determinism
story. Don't add a fixture identity backed by anything but ed25519.

`keys/` holds throwaway ed25519 keypairs — **committed on purpose, for
fixture determinism.** They are not, and must never become, a real
credential for anything. Do not reuse them outside this directory, and
do not "fix" the fact that private keys are checked into git — that's
the design, not an oversight.

## Regenerating the corpus

```
go run ./spec/fixtures/gen
```

Rebuilds every description in `testdata/descriptions/` into
`spec/fixtures/out/<name>/` (gitignored — a real bare repo per fixture,
inspectable with plain git) and checks each one's manifest against the
committed golden file in `testdata/golden/`. A mismatch exits non-zero:
either the generator's behavior changed, or a description changed
without updating its golden.

After an intentional change to a description (or the generator itself),
update the goldens:

```
go run ./spec/fixtures/gen -update-golden
```

`go test ./spec/fixtures/...` runs the same check (`TestCorpusMatchesGolden`)
plus an explicit determinism test (`TestGenerationIsDeterministic`, which
builds each description twice and diffs the manifests directly) — this is
what a CI job should run; wiring an actual pipeline is WRIT-55.

## Manifests

`Generate` returns a `Manifest`: every ref's final commit, plus every
generation's full commit chain (SHA, tree, parents, author, timestamp,
message, whether it's signed), whether or not that generation is
reachable from a ref. Golden files are this manifest, JSON-encoded — not
the repository's raw bytes. Comparing SHAs rather than the `.git`
directory sidesteps incidental packing/layout differences entirely: two
regenerations that produce the same objects under the same refs have the
same manifest by definition, since a commit's SHA already covers its
tree, parents, author/timestamp, message, and signature.
