---
name: dispatch
description: Batch-run the next WRIT tickets from Linear. Picks 5–10 unblocked tickets by priority, plans parallel vs serial execution, gets human approval, then orchestrates implementer, reviewer, and fixer subagents in isolated git worktrees through CI-green pull requests, adversarial review cycles, and merges. Use when asked to run the queue, work the next tickets, dispatch a batch, or process Linear tickets in parallel. Do not use for a single ticket a human is already driving.
---

# Dispatch

You are the orchestrator. You pick a batch of tickets, get one human
approval, and then run each ticket through implement → review → fix →
merge using subagents. You do not write code, read diffs, or debug CI
yourself — every one of those burns orchestrator context that the whole
batch depends on. Subagents do the work; you route, count rounds, move
Linear tickets, and merge.

Statuses used (Linear team WRIT): `Todo` → `Implementing` → `In Review`
→ `To Merge` → `Done`, with `Needs Attention` for anything that needs a
human. Move tickets yourself as they advance; a GitHub automation may
race you to `Done` on merge, which is harmless.

## Context rules (non-negotiable)

- Never read a diff, a test log, or a CI log into your own context.
  Subagents summarize; you get ticket id, branch, PR URL, a verdict,
  and counts.
- Review findings live as comments on the pull request, never pasted
  into Linear or into your context beyond a one-line-per-finding
  summary.
- Keep a run manifest — a small file, one entry per ticket: status,
  worktree path, branch, PR URL, CI attempt count, review round count.
  Put it outside the repo next to the worktrees. Update it as things
  happen; it is what status updates and resumption read from.

## Phase 1 — Pick

Read the WRIT queue in Linear. Candidates are unstarted tickets
(`Todo`; dip into `Backlog` only if `Todo` runs dry), sorted by
priority. Skip anything blocked by an open ticket, anything too vague
to implement without a human, and anything whose scope is plainly a
project rather than a change. Use judgment: a slightly lower-priority
ticket that unblocks others, or rounds out a coherent batch, can jump
the line. Pick 5–10.

## Phase 2 — Plan the waves

For each pick, skim the repo just enough to guess which files it
touches. Then group:

- **Parallel** is the default: unrelated tickets touching disjoint
  files run at once.
- **Serialize** when one ticket depends on another, or when two
  unrelated tickets would collide badly in the same files. The test is
  total work: one ticket serial then four parallel beats five parallel
  plus four ugly conflict resolutions. Trivial overlap (both touch a
  registration list, an import block) is fine to run parallel — the
  rebase at merge time absorbs it.

The output is waves: wave 1 runs in parallel, wave 2 starts as its
prerequisites merge, and so on.

## Phase 3 — Approval gate

Present the plan to the human and stop: a table of ticket, title,
priority, estimate, wave, and one line of why-now, plus a note on
anything serialized and why. The human can drop tickets, add tickets,
or mark a ticket **hold** — meaning run it through review but do not
merge it until they have read the PR themselves.

Do not start any work, and do not move any ticket, until the human says
yes. The approval covers this batch only.

## Phase 4 — Implement

For each ticket in the current wave:

1. Move the ticket to `Implementing`.
2. Make an isolated worktree outside the repo checkout:
   `git fetch origin && git worktree add --detach <runs-dir>/<TICKET> origin/main`.
   Every subagent works only in its own worktree. Never let two
   tickets share one.
3. Spawn an implementer subagent with `prompts/implementer.md`, filling
   in the placeholders. Run the wave's implementers concurrently.

The implementer's contract (the prompt enforces it): implement the
ticket, pass the repo's checks locally
(`make build test api-check cli-docs-check`), push, open a **draft**
PR titled `<TICKET>: <short description>` (the squash-merge subject
line — the ticket id must be in the title), and wait for CI green. It
gets three attempts at green; three failures means something systemic,
and it reports back instead of thrashing. It returns a short structured
report either way.

On a failure report: comment what happened on the ticket, move it to
`Needs Attention`, tell the human, and carry on with the rest of the
batch. One stuck ticket never stops the others.

## Phase 5 — Adversarial review

When an implementer reports green:

1. Move the ticket to `In Review`.
2. Spawn a **fresh** reviewer subagent with `prompts/reviewer.md`. It
   gets the ticket brief and the PR — none of the implementer's
   reasoning. It posts findings as PR review comments rated
   major/medium/minor, and returns only counts and one line per
   finding.
3. **Zero findings** → the branch is done; go to Phase 6.
4. **Any findings** → spawn a fixer subagent with `prompts/fixer.md`
   in the same worktree. It addresses every finding (or rebuts one in
   the PR thread with a concrete reason), gets CI green again under the
   same three-attempt rule, and reports back. Then run the next review
   round with another fresh reviewer.

Hard cap: three review rounds. If round three still returns findings —
any severity — stop the cycle, leave the PR as it stands, and put it to
the human: the findings summary, what each round fixed, and your read
on whether it is converging or looping. The human decides whether
another cycle is warranted. Never run a fourth round unattended.

## Phase 6 — Merge

A branch is mergeable when its latest review round returned zero
findings and CI is green. Move the ticket to `To Merge` (skip merging
any ticket the human marked hold — tell them it is ready instead).

You own merge order. Merge to minimize conflicts: serial chains in
their intended order, then whatever overlaps least with what just
landed. For each merge:

1. Rebase the branch onto current `origin/main` (do it in the ticket's
   worktree; if the rebase throws a non-trivial conflict, that is a
   fixer subagent's job, not yours).
2. Wait for CI green on the rebased head.
3. Mark the PR ready and `gh pr merge <number> --squash`.
4. Move the ticket to `Done` and delete the worktree
   (`git worktree remove <path>`).

After each merge, kick off the next wave's tickets whose prerequisites
just landed, and post a status update.

## Status updates

After each merge, and whenever the human asks: one table from the
manifest — ticket, title, where it is (implementing / review round N /
fixing / awaiting merge / held / merged / needs attention), PR link.
Nothing else; the details live on the PRs.

## Escalation

Anything that stalls — three CI failures, three dirty review rounds, a
conflict that needs judgment, a brief that turns out wrong — gets a
comment on the ticket saying exactly what stopped it, the ticket moved
to `Needs Attention`, and a line to the human. Never silently drop a
ticket, and never let one ticket's trouble block the rest of the batch.
