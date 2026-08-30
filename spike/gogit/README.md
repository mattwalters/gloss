# gogit-bench

WRIT-4 spike tool. Measures whether go-git's local object I/O — the "local
half" of the hybrid approach settled in `ARCHITECTURE.md` (go-git for
objects, system git for transport) — holds up against a large, real
repository's existing object store: object-write throughput, ref-update
behavior at `refs/writ/*` scale (thousands of refs), and DAG-walk
performance.

This is throwaway spike tooling, not engine code. It writes and deletes its
own synthetic refs/commits under `refs/writ/bench-writer/cobs/spike/*` and
cleans them up when it finishes, but point it at a scratch clone, not a repo
you care about — it also runs `git pack-refs` on the target.

## Usage

Get a big guinea-pig repo (bare, so there's no working-tree checkout cost):

```
git clone --bare https://github.com/FFmpeg/FFmpeg.git ffmpeg-bare.git
```

(FFmpeg was used for the WRIT-4 measurements: 126k+ commits, ~500MB, a
reasonable stand-in for "large real repo" without linux-kernel-scale clone
times.)

Then:

```
go run . --repo ./ffmpeg-bare.git --ops 5000 --refs 5000
```

- `--ops` — number of synthetic op-commits to write, chained as one
  writer's history, each with its own ref (measures combined
  commit-write + ref-create throughput, the shape of one writ write).
- `--refs` — additional refs to create on top of `--ops`, to reach the
  scale the many-refs benchmark measures enumeration/lookup/update against.

Findings from running this against FFmpeg are recorded in
`docs/spikes/writ-4-go-git-capability.md`.
