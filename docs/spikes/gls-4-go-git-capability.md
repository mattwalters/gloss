# GLS-4: go-git capability check on a large real repo

Spike for the hybrid Go/git approach settled in `ARCHITECTURE.md` (go-git
for local object I/O, system git for all transport). This does not
relitigate that decision — it checks whether go-git's local half is
adequate for Gloss's actual write pattern: one op = one signed commit, one
ref per collaborative object under a per-writer namespace
(`refs/gloss/<writer-id>/cobs/<type>/<object-id>`), at a scale a real
project's op history will eventually reach.

Tool: `spike/gogit/` (throwaway spike code, not engine code — see its
README for usage). Guinea pig: a bare clone of
[FFmpeg/FFmpeg](https://github.com/FFmpeg/FFmpeg), chosen for commit count
without linux-kernel-scale clone time/size.

## Repo used

| | |
|---|---|
| Commits (`master`) | 126,336 |
| Objects (packed)   | 812,906 |
| Pack size           | ~516 MB |
| Clone time (bare)  | ~49s over the network available in this environment |

Satisfies the ≥100k-commit measurement requirement.

## 1. DAG-walk performance

| Operation | Result |
|---|---|
| Full history walk (all 126,336 commits, `repo.Log` from `HEAD`) | 5.3–5.9s, ~21,000–24,000 commits/sec |
| Bounded walk, first 1,000 commits | ~22–26ms |
| Single commit lookup by hash (`CommitObject`) | ~20–24µs |

**Verdict: adequate, no cliff found.** A cold rebuild of a projection by
walking a 126k-commit history end to end costs single-digit seconds, not
minutes. Single-object lookup — the shape a fold takes when resolving one
op's parent rather than rebuilding from scratch — is tens of microseconds.
Nothing here threatens the "walk only new ops since last fold" incremental
design in `ARCHITECTURE.md` §Projection.

## 2. Op-write throughput

Each op was written as go-git actually would: an empty-tree commit object
(payload in the commit message, standing in for a real op's small JSON
body) plus a `SetReference` for its own ref, chained as one writer's
history — 5,000 ops written this way, against the existing 812k-object
FFmpeg store.

| | |
|---|---|
| Throughput | ~2,000 ops/sec sequential, single-threaded |
| Per-op latency | p50 292µs · p90 708µs · p99 2.4ms |
| Max observed | 154ms (outlier) |

**Verdict: adequate.** Steady-state per-op cost is well inside the
single-digit-millisecond write VISION.md promises. The p99/max tail traces
to a one-time cost, not a recurring one: a fresh bare clone has zero
`objects/xx/` prefix directories on disk (verified: `ls objects/` on the
pristine clone shows none of the 256 possible hex prefixes), so the first
loose object written under each new prefix pays a `mkdir` it will never pay
again. That's bounded by 256 occurrences per repo, not something that
scales with op count — not a cliff, just a note for anyone reading raw
percentile output and wondering about the tail.

## 3. Many-refs behavior at `refs/gloss/*` scale

This is where go-git's behavior stops being uniformly good, and the
answer depends on whether refs are loose or packed — which is not
something Gloss controls; git's own `gc.packRefs`/`gc.auto` decide that on
a schedule Gloss doesn't own.

Measured at 5,000 and 20,000 refs under a single writer's namespace.

| Operation | Loose | Packed |
|---|---|---|
| Enumerate all `refs/gloss/*` | 309ms (5k) / 1.87s (20k) | 5ms (5k) / 18ms (20k) |
| Single-name lookup, avg | ~37–42µs | ~2.7–2.95ms |
| Update existing ref (fast-forward) | p50 69–77µs, p99 94–395µs | (not separately measured; see removal below) |
| `git pack-refs --all` | — | 2.4s (5k) / 8.1s (20k) |

Two distinct findings here, both traced to source
(`storage/filesystem/dotgit/dotgit.go` in go-git v5.19.2):

**3a. Packed single-ref lookup is ~70–80x slower than loose, and roughly
flat across the 5k→20k range rather than scaling down per-ref.**
`DotGit.packedRef` does an unindexed linear scan of the packed-refs file
per call (`findPackedRefs` — `bufio.Scanner` over the whole file, matching
line by line, stopping early only once the target is found), reopening
the file each time. There's no in-memory index and no cache between calls.
Enumeration (`IterReferences`, one scan, iterate in memory) is fast for
exactly the reason repeated single lookups are slow: it pays the scan cost
once. **Mitigation:** never call `Storer.Reference(name)` in a loop over
many refs — enumerate once into a map instead. Gloss's actual access
pattern (enumerate an object's ops across all writer namespaces) is
already shaped this way; the risk is a future code path that does
one-off lookups against a large shared ref space and inherits this
silently.

**3b. Bulk ref removal after packing is quadratic, not linear.** Every
`RemoveReference` call against a ref that's already in `packed-refs` (as
opposed to a loose file) calls `rewritePackedRefsWithoutRef`, which opens
the *entire* packed-refs file, scans it, writes a filtered copy to a temp
file, and renames it into place — for that one ref. Removing k of n
packed refs is O(k·n) file I/O. Measured directly: cleaning up 5,000 already-packed refs (removing them
one at a time, as `RemoveReference` requires) took ~42s (~8.4ms/removal
average); cleaning up 20,000 took ~7m16s / 436s (~21.6ms/removal average)
— a **~10x wall-clock cost for a 4x increase in ref count**, and the
per-removal average itself grew 2.6x along with it. Both are consistent
with the O(k·n) mechanism found in source, not linear scaling (which would
predict ~4x, not ~10x) and nowhere near constant per-removal cost.
**This is the finding with the most direct bearing on VISION.md's open
"repo growth / GC story" risk**: any future compaction or per-writer-ref
pruning built by calling go-git's `RemoveReference` in a loop will fall
off a cliff exactly when it matters most (a repo old enough to need
compaction is a repo with many refs). **Mitigation options:** (a) shell
out to system git for bulk ref deletion — consistent with the project's
existing "system git for anything git already does well" pattern — since
`git pack-refs` itself already does a single-pass rewrite; (b) write a
batch removal path directly against the packed-refs file (read once,
filter out the whole set, write once) rather than using go-git's
per-ref `RemoveReference` in a loop.

## 4. Correctness gap: SSH commit-signature verification

`ARCHITECTURE.md` states SSH signing is preferred for ops ("users already
have the key"). Checked go-git v5.19.2's signature handling
(`plumbing/object/commit.go`):

- **Storage round-trips correctly.** Git stores any signature — PGP or
  SSH — under the generic `gpgsig` commit header. go-git reads and writes
  that header byte-for-byte into `Commit.PGPSignature` regardless of
  format, so an SSH-signed op commit is preserved and re-encodes
  identically. This satisfies the "unknown data preserved, never dropped"
  rule for the signature bytes themselves.
- **Verification does not.** `Commit.Verify(armoredKeyRing)` only
  implements OpenPGP verification (via `ProtonMail/go-crypto/openpgp`).
  There is no SSH-signature verification path anywhere in go-git.

**Mitigation options:** (a) shell out to `ssh-keygen -Y verify` (or `git
verify-commit`, which already knows how to dispatch on `gpg.format`) —
same hybrid pattern as transport; (b) implement SSH-signature verification
directly against `golang.org/x/crypto/ssh`, which is a dependency go-git
already pulls in transitively. Either is a small, contained piece of work,
not a redesign — flagging it now so fold/verification code doesn't
discover it by trying `Commit.Verify()` on an SSH-signed op and getting a
wrong answer instead of a "not implemented."

## Verdict

**Adequate with caveats.** DAG-walk and steady-state op-write throughput
comfortably meet what VISION.md promises. Two concrete items need to be
designed around, not around go-git's defaults:

1. Never do per-ref `Reference()` lookups over a large ref space — always
   enumerate-once-into-a-map. (Cheap to avoid; just needs to be a known
   rule, not discovered by an on-call at scale.)
2. Any future ref-compaction/pruning work (the open GC-story risk in
   VISION.md) must not be built on a loop of go-git's `RemoveReference`
   against packed refs — it needs a batch path, most likely via system
   git, from day one.

Plus one correctness gap to close before SSH-signed ops can be verified
in-process: go-git verifies PGP signatures only, so SSH signature
verification needs to be added explicitly (shell-out or direct
implementation) rather than assumed to come free with go-git.

None of this reopens the go-git/system-git split in `ARCHITECTURE.md` —
if anything it reinforces it: the same "use system git for what it
already does well" reasoning that put transport on system git extends
cleanly to bulk ref maintenance and signature verification.
