# WRIT-69 spike: ref-count scaling — per-object refs vs per-writer chains

Measured cost curve for the two candidate ref layouts, against a real host
(GitHub over HTTPS, protocol v2) and a local `file://` control:

- **Per-object**: `refs/writ/<writer-id>/cobs/<type>/<object-id>` — one ref
  per (writer, object). Realistic mature-repo scale: 100k–500k refs.
- **Per-writer chains**: `refs/writ/<writer-id>/<type>` — one append chain
  per writer per type. Realistic scale: ~400–1,200 refs.

The question: git re-advertises every ref matching the fetch refspec on
every fetch — there is no incremental advertisement — so does the
per-object layout make every no-op `git fetch` pay O(total refs) forever,
and do hosts degrade or refuse at high ref counts?

Answer: yes, and the host's write path gives out long before the fetch
bytes do. GitHub returned `Internal Server Error` for every ref in a
9,000-ref push; chunked pushes degrade from ~35 ms/ref to ~70 ms/ref as
the repo's ref count grows (20k refs took 22.6 minutes to push at the 30k
tier); and by 100k refs a *no-op* fetch costs ~12 MB and ~14 s even over
`file://` with zero network. **Recommendation: per-writer chains. The
per-object layout stays comfortable only below ~10k total refs — one to
two orders of magnitude under its own realistic scale.**

## Method

Scripts checked in alongside this doc:

- `genrefs.go` — emits `create <ref> <sha>` lines for
  `git update-ref --stdin` in either layout. Per-object ids are
  sha256-derived 40-hex strings (realistic name entropy, in case anything
  on the wire compressed — nothing did; GitHub served the ls-refs response
  with no `Content-Encoding`). Writers: a pool of 50 for per-object;
  300 writers × 4 types for the 1,200-ref chain tier.
- `measure.sh` — the per-tier no-op fetch measurement: three timed runs of
  `git -c protocol.version=2 fetch <url> 'refs/writ/*:refs/writ/*'` with
  everything already up to date, then one traced run with
  `GIT_TRACE_PACKET` (byte accounting) and `GIT_TRACE_CURL_NO_DATA`.
- `tier.sh` / `batchpush.sh` — tier driver, and the chunked-push fallback
  GitHub forced (finding 2). `localtier.sh` runs the file://-only tiers
  (100k, 500k) that GitHub's write path put out of reach.

All refs point into a shared pool of 100 tiny unique commits.
Advertisement cost is per-ref, not per-commit, so object transfer is
deliberately negligible — see caveats. Tiers were built incrementally
(refs added, never recreated). Host: one private throwaway repo
(`writ-69-ref-scaling-spike`) under a personal GitHub account, used for
nothing else. Client: macOS, git 2.50.1 (Apple Git-155), residential
network; GitHub's server identified as `git/github-7cf87d205eb3-Linux`.
Control: a local bare repo with identical refs, fetched over `file://`
(protocol v2 both ways, confirmed in the packet traces).

Byte counts are pkt-line bytes received by the client (payload plus
4-byte length prefix) from `GIT_TRACE_PACKET`; with no content-encoding
on the response these are wire bytes, not an undercount.

## Results

### No-op fetch — the recurring cost every client pays forever

Median of 3 runs; advert bytes = the ls-refs ref lines (the total
response is ~550 bytes more for capabilities and flushes).

| tier (layout)        | refs    | GitHub (median) | file:// control | advert bytes |
|----------------------|---------|-----------------|-----------------|--------------|
| chains, 300 writers  | 1,200   | 0.96 s          | 0.11 s          | 87,300 (85 KB) |
| per-object           | 1,000   | 1.00 s          | 0.12 s          | 118,750 (116 KB) |
| per-object           | 10,000  | 1.20 s          | 0.44 s          | 1,187,500 (1.13 MB) |
| per-object           | 30,000  | 1.57 s          | 1.28 s          | 3,562,500 (3.40 MB) |
| per-object           | 100,000 | not reached (finding 2) | 13.6 s  | 11,875,000 (11.3 MB) |
| per-object           | 500,000 | not reached (finding 2) | 87.5 s  | 59,375,000 (56.6 MB) |

Advert cost is exactly linear per ref — 118.75 B/ref for the per-object
names, 72.75 B/ref for the shorter chain names — so the GitHub bytes at
100k/500k are the same numbers the control measured; only the wall time
is missing, and it is bounded below by the file:// column, which has no
network in it at all.

### Push — the write path

| operation | refs | result |
|---|---|---|
| chain layout, one push | 1,200 | 26.3 s |
| per-object, one push | 1,000 | 23.6 s |
| per-object, one push | 9,000 | **rejected — `Internal Server Error` on every ref** (finding 2) |
| per-object, 2k-ref chunks, 10k tier | +9,000 | 335 s total (chunks 64–82 s) |
| per-object, 2k-ref chunks, 30k tier | +20,000 | 1,355 s total (chunks 120–144 s) |

### Fresh clone, then first fetch of `refs/writ/*`

| tier | refs | clone | first writ fetch |
|---|---|---|---|
| chains | 1,200 | 1.1 s | 1.7 s |
| per-object | 1,000 | 1.1 s | 1.7 s |
| per-object | 10,000 | 1.1 s | 4.8 s |
| per-object | 30,000 | 1.3 s | 21.3 s |

### Narrow refspec escape hatch, measured once

At the 30k tier, fetching a single writer's namespace
(`refs/writ/w<id>/*`, 600 refs) cost 1.08 s and 72 KB — protocol v2's
ref-prefix filtering works, and the server only advertises the requested
prefix. It does not rescue the per-object layout, because writ's sync
model fetches all writers; see finding 4.

## Findings

**1. The recurring cost is real, linear on the wire, and superlinear on
the client.** Every no-op fetch re-downloads the full advertisement for
the refspec: 118.75 bytes per per-object ref, forever. At the realistic
100k–500k scale that is 11–57 MB per `git fetch` in which nothing
happened. Worse, wall time grows faster than the bytes: the file://
control (zero network) goes 0.44 s → 1.28 s → 13.6 s → 87.5 s across
10k → 30k → 100k → 500k. The 30k→100k step is 3.3× the refs for 10.6×
the time — client/local-transport ref processing is superlinear in this
range, so a fast pipe does not save the layout.

**2. GitHub's write path fails before its read path.** A single push of
9,000 ref creations was refused outright — every ref rejected with
`remote rejected ... (Internal Server Error)`, request ID
`FD6B:241D47:10C611D:181F338:6A94B1E7`, repo left untouched (still at
1,000 refs). Chunked to 2,000-ref pushes it succeeds, but per-ref cost
grows with the repo's total ref count: ~24 ms/ref at 1k refs, ~37 ms/ref
at the 10k tier, ~68 ms/ref at the 30k tier. Pushing the 30k tier's 20k
increment took 22.6 minutes of continuous pushing. Escalation to 100k on
GitHub was abandoned on that trend line (~35 s per 500-ref
writ-sized push and climbing); the 100k/500k rows come from the file://
control plus the exactly-linear byte math. Backfilling a mature repo's
history into per-object refs on a real host is an hours-long, error-prone
operation *by construction*.

**3. Fresh-clone onboarding degrades quadratically-ish too.** First fetch
of the writ refs into a clean clone: 1.7 s at 1k, 4.8 s at 10k, 21.3 s at
30k — the client is writing every ref locally as well as reading the
advertisement. At 100k+ this is minutes added to every new
clone/CI-runner that wants review data.

**4. Ref-prefix filtering is real but doesn't change the answer.**
A client that only wants one writer pays only for that writer (600 refs,
72 KB, 1.08 s at the 30k tier). But writ's fold needs all writers'
ops — `refs/writ/*` is the working refspec — so the full advertisement
is the steady-state cost. The prefix trick matters for a future
partial-sync feature, not for the layout decision.

**5. The chain layout is indistinguishable from free.** 1,200 chain refs:
0.96 s no-op fetch (85 KB), 26 s one-shot push of the entire namespace,
1.7 s first fetch on a fresh clone. Every number sits at the measurement
floor — the same ballpark as the 1k per-object tier, with headroom of two
orders of magnitude before it would reach the 100k regime measured above.
Chains also make the per-ref push tax (finding 2) irrelevant: appending
an op moves an existing ref instead of creating a new one, so the ref
count never grows with review activity.

## Caveats

- **Shared commit pool.** All refs point at 100 pooled commits, so packs
  are tiny and object negotiation is ~free. Real writ ops mean more
  objects, which makes every number here a *lower* bound — advertisement
  cost is per-ref and unaffected, object cost only adds on top.
- **One host, one network, one afternoon.** GitHub only (its 9k-push 500
  is today's behavior, not a documented limit); residential network;
  medians of 3 runs. GitLab/Gitea/Bitbucket were not measured. The
  file:// control exists precisely to separate protocol cost from host
  and network variance, and it shows the protocol cost alone is
  disqualifying at scale.
- **100k/500k GitHub rows are extrapolated, not measured** — bytes by
  exact linearity (118.75 B/ref, verified 1k→100k on the control),
  time bounded below by the file:// control. The reason they could not
  be measured directly (finding 2) is itself evidence against the layout.
- **`update-ref --stdin` batching** was used to create refs locally;
  real writers create refs one at a time, which is more transactions,
  not fewer.

## Recommendation

**Per-writer chains.** The per-object layout remains viable only up to
roughly 10k total refs — no-op fetch ~1.2 s / 1.1 MB, and pushes already
require chunking well before that — and is clearly degraded by 30k
(1.6 s and 3.4 MB per no-op fetch, 21 s first fetch on every fresh
clone, ~68 ms per pushed ref). Its own realistic scale is 100k–500k,
where a no-op fetch costs 11–57 MB and 14–88 s of pure protocol work
before network is even counted, and where this spike could not push the
ref set to GitHub in bounded time at all. The chain layout at its
realistic 1,200 refs is at the measurement floor on every axis and keeps
ref count constant as review activity grows. The per-object design's
costs are structural (per-ref advertisement, per-ref host-side
transactions), not tunable; freeze the spec on per-writer chains.
