# GLS-60 spike: SQLite driver — CGO vs pure Go

Benchmark and findings for the projection layer's SQLite driver:
`mattn/go-sqlite3` (cgo, canonical) vs `modernc.org/sqlite` (pure Go). Run the
benchmark with `go test ./... -run xxx -bench . -benchtime 50x` from this
directory; `BenchmarkIndexedRead*` alone is noisy at low iteration counts, so
prefer `-bench BenchmarkIndexedRead -benchtime 2000x -count 3` for that one.

Answer: pure Go, `modernc.org/sqlite`. Recorded in ARCHITECTURE.md under
"SQLite driver: pure Go".

## The benchmark

Schema shaped like the projection (`schema.go`): a `reviews` table and a
`comments` table indexed on `review_id`, matching the two operations the
projection actually does — a from-scratch fold writing everything in one
transaction, and a review view reading one review's comments by index.
Sized at 5,000 reviews × 20 comments/review (100,000 comments) to sit in the
neighborhood of an imported PR history for an active repo, per the ticket's
"imported PR history scale" (`schema.go`'s `numReviews`/`commentsPerReview`).

On an Apple M2 Max (three runs each, `go test -bench`):

| | bulk insert (100k comments, 1 tx) | indexed read (20 rows by index, 2000x/run) |
|---|---|---|
| `mattn/go-sqlite3` (cgo) | 355–396 ms | 35–42 µs |
| `modernc.org/sqlite` (pure Go) | 646–691 ms | 48–83 µs |

cgo is consistently ~1.7–1.9x faster on bulk insert; ~1.3–2x faster on
indexed reads, with pure Go showing more run-to-run variance there. Both are
comfortably fast in absolute terms: a from-scratch refold of 100k ops is
sub-second either way, and both drivers serve the point-lookup a review view
needs in well under a millisecond. This is the only axis where cgo wins.

## Findings

**1. Cross-compilation is where this decision actually gets made.**
Cross-compiling this spike's test binary from this darwin host to
`linux/arm64` with `CGO_ENABLED=1` fails outright — no linux C toolchain on
a mac, so `runtime/cgo`'s C sources don't build (`setresgid`/`setresuid`
undeclared under the macOS SDK headers). Producing GLS-58's six release
targets (linux/macos/windows × amd64/arm64) from one CI host with `mattn`
in the dependency graph means either a from-scratch matrix of per-target
build hosts, or a C cross-compiler toolchain (e.g. `zig cc`) wired into the
release pipeline and kept working. With `CGO_ENABLED=0`, the same
cross-compile is `GOOS=linux GOARCH=arm64 go build` — no toolchain, no
extra CI machinery, works from any host. This is the fight the ticket asks
about, and it's a real one, not a hypothetical: it reproduces on the first
try, unprompted, in a two-target check.

**2. Static Linux binaries are a second, separate cgo cost.** Even once
cross-compiling succeeds, statically linking a cgo binary against glibc is
its own known trap — `-extldflags -static` against glibc breaks NSS-based
lookups at runtime, not compile time — so the usual fix is musl (an Alpine
build container or a musl cross toolchain), which is another moving part
per target, on top of finding 1. Not reproduced here (no musl toolchain in
this spike's environment), but well-documented enough elsewhere to count
against cgo without needing to.

**3. `mattn/go-sqlite3` fails silently under `CGO_ENABLED=0`, not loudly.**
Building this spike's package with `CGO_ENABLED=0` *succeeds* — the driver
ships a stub for that case — but every call at runtime returns `"Binary was
compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a
stub"` (reproduced: `PRAGMA journal_mode=WAL` fails with exactly that
message). A CI leg or a contributor environment that forgets to force
`CGO_ENABLED=1` doesn't fail to build — it fails at first query, with an
error that doesn't obviously point back at the build flag.

**4. Binary size: pure Go costs about 2.8 MB.** A minimal program opening
one connection, stripped (`-ldflags="-s -w"`), is 3.6 MB with `mattn` (cgo,
native build) vs 6.3 MB with `modernc` (`CGO_ENABLED=0`) — the transpiled
amalgamation roughly doubles binary size. Real, but not close to a blocker
for a single self-contained CLI binary.

**5. Maintenance posture: both current, different risk shapes.**
`mattn/go-sqlite3` is at v1.14.50, requires Go ≥1.21, and is the
long-established hand-maintained cgo wrapper most Go projects reach for
first. `modernc.org/sqlite` is at v1.57.0, requires Go ≥1.25 — its much
faster version churn tracks a from-C-source transpilation (via `ccgo`) of
each upstream SQLite release rather than a hand-maintained binding, and its
Go-version floor has moved fast enough to be worth rechecking if gloss ever
needs to support an older toolchain in some contributor's environment. Both
are active; neither is the risk here — the transport story in finding 1 is.

## Decision

**Pure Go: `modernc.org/sqlite`.** ARCHITECTURE.md's own case for Go over
Rust rests on "the CLI in `--json` plumbing mode is the universal API ...
which Go distributes exceptionally well" — a promise a cgo dependency in
the projection layer would compromise for every release, on every target,
forever. The performance gap this spike measured is real (roughly 1.3–1.9x)
but stays well inside "fast enough for a local single-user cache" at every
scale tried, including a 100k-comment from-scratch refold. The
cross-compilation and static-linking cost (findings 1–2) has no comparable
"fine in practice" answer and compounds with every release target GLS-58
adds. No CGO exception needs writing into the release pipeline as a result
— that line item in GLS-58's definition of done is now moot rather than
satisfied.
