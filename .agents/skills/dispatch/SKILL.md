---
name: dispatch
description: Batch-run the next WRIT tickets from Linear. Picks 5–10 unblocked tickets from `Todo` by priority (never from `Backlog`), has each one planned into its ticket description, plans parallel vs serial execution, gets human approval, then runs each ticket through the implement-ticket, adversarial-review, and merge-queue skills to a merged pull request. Use when asked to run the queue, work the next tickets, dispatch a batch, or process Linear tickets in parallel. Do not use for a single ticket a human is already driving — use implement-ticket, adversarial-review, or merge-queue directly for that.
---

# Dispatch

You are the orchestrator. You pick a batch of tickets, have each one
planned, get one human approval on the batch, and then run each ticket
through the `implement-ticket`, `adversarial-review`, and
`merge-queue` skills. Each merge waits for a per-ticket human
go-ahead; once given, `merge-queue` owns sequencing. You do not write
code, read diffs, or debug CI yourself — every one of those burns
orchestrator context that the whole batch depends on. Subagents do the
work; you route, count rounds, and move Linear tickets.

This skill is the orchestration policy — picking, waving, approval,
tracking. What happens to one ticket once it's picked lives in the
three skills it delegates to, each equally usable on its own:
`implement-ticket` for "just implement WRIT-200", `adversarial-review`
for "review every open PR", `merge-queue` for "merge everything
that's approved". Read their SKILL.md files before changing what a
stage does; change this file for how runs are queued.

Statuses used (Linear team WRIT): `Todo` → `Implementing` → `In Review`
→ `To Merge` → `Done`, with `Needs Attention` for anything that needs a
human. `In Review` is a reading gate — a ticket rests there until the
human approves its merge; `To Merge` means approved and sitting in
your merge queue. Move tickets yourself as they advance; a GitHub
automation may race you to `Done` on merge, which is harmless.

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

Read the WRIT queue in Linear. Candidates are tickets in `Todo`,
sorted by priority. **`Backlog` is off-limits.** It is where the human
parks work they do not want started — their own tickets, decisions
they are still thinking through, things deliberately set aside.
Promoting a ticket to `Todo` is how they hand it to dispatch, and it
is the only signal that a ticket is available. An empty `Todo`
therefore means there is nothing to run: say so and stop. Never dip
into `Backlog` to fill out a batch, however tempting the tickets there
look.

Within `Todo`, skip anything blocked by an open ticket, anything too
vague to implement without a human, and anything whose scope is
plainly a project rather than a change. Use judgment: a slightly
lower-priority ticket that unblocks others, or rounds out a coherent
batch, can jump the line. Pick 5–10 — or everything in `Todo` if it
holds fewer. A short batch is a normal outcome, not a reason to reach
further.

## Phase 2 — Plan each ticket

For each pick, spawn a planner subagent with `prompts/planner.md` —
strongest reasoning model available (see Models and effort below):
planning is scope judgment. Planners are read-only, so run them all in
parallel.

Each planner writes its plan into the ticket's own description under a
`## Plan` heading — replacing an existing `## Plan` section, touching
nothing above it — and returns a short report with a summary and the
files the change will touch. The description is the brief every later
subagent reads, so a plan there is read by construction; never put a
plan in a comment, where it sinks under later traffic.

A planner that finds a ticket too vague, or big enough to be several
tickets, reports it unplannable instead of guessing. A planner that
finds the ticket's premise doesn't match the code — the bug it
describes doesn't reproduce, the feature already exists — reports it
blocked instead, and has already commented the mismatch on the ticket.
Drop both kinds from the batch and say why at the approval gate; they
read differently to the human (unplannable needs more scoping,
blocked needs someone to look at what the planner found) so don't
merge them into one line.

## Phase 3 — Plan the waves

Build waves from the planners' file lists — real overlap, not guesses.
Group:

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

## Phase 4 — Approval gate

Present the batch to the human and stop: a table of ticket, title,
priority, estimate, wave, one line of why-now, and the plan's one-line
summary, plus a note on anything serialized and why, and any tickets
the planners dropped as unplannable or blocked (say which, and for
blocked, what the planner found). Approving the batch approves the
plans — the full plans are on the tickets for anyone who wants the
detail. The human can drop tickets or add tickets (an added ticket
gets a planner pass before it joins a wave). Merges are approved
separately, ticket by ticket, at the merge queue — though the human
can pre-authorize a ticket's merge here if they say so.

Do not start any work, and do not move any ticket, until the human says
yes. The approval covers this batch only.

## Phase 5 — Implement

Invoke the `implement-ticket` skill once for the current wave's ticket
ids together — its own instructions cover the worktree, the subagent,
the three-attempt CI rule, and running the wave concurrently, so
nothing here duplicates them.

For each report it returns, update that ticket's manifest entry
(worktree path, branch, PR URL). On `RESULT: blocked` or `failed`,
`implement-ticket` has already moved the ticket to `Needs Attention`
(commenting the mismatch itself if blocked); relay which one it was —
blocked means the brief needs a human's judgment call, failed means CI
never went green — and carry on with the rest of the batch. One stuck
ticket never stops the others.

## Phase 6 — Adversarial review

For each ticket `implement-ticket` reported green, invoke the
`adversarial-review` skill for that ticket's PR (its own instructions
cover the reviewer/fixer cycle, the three-round cap, and moving the
ticket to `In Review`).

On `RESULT: ready`, go to Phase 7. On `RESULT: capped`, tell the human
exactly what `adversarial-review` reported — findings summary, what
each round fixed, whether it read as converging or looping — and wait;
don't start a fourth round yourself. On `RESULT: blocked` (a
reviewer or fixer found something outside the diff itself) or `failed`
(the fixer exhausted its CI attempts), tell the human which one it was
and carry on with the rest of the batch.

## Phase 7 — Merge queue

A branch is ready when its latest review round returned zero findings
and CI is green. Tell the human: ticket, PR link, rounds run, and what
the last round found. The ticket rests in `In Review` — a reading
gate — until the human approves that ticket's merge: in chat, by
moving the ticket to `To Merge` in Linear themselves, or by having
pre-authorized it at the batch gate. Any of the three counts. Never
tell `merge-queue` to merge a ticket without one of them.

Approval makes a ticket eligible; it does not set the order. The human
may green-light five at once — invoke the `merge-queue` skill with all
of them together and let it work out sequencing and conflicts; its own
instructions cover ordering, rebasing, mechanical conflict resolution,
and the squash merge, so nothing here duplicates them.

On `RESULT: merged`, per PR: kick off the next wave's tickets whose
prerequisites just landed, and post a status update. On `RESULT:
blocked` (a rebase conflict needs new logic or a judgment call, not
just combining both sides) or `failed` (CI never went green on the
rebased head), tell the human which one it was and why — `merge-queue`
already left the PR and worktree as they were — and carry on with the
rest of the queue.

## Cleanup

`merge-queue` deletes the worktree and the remote branch for anything
it actually merges — that's the only automatic cleanup anywhere in
this pipeline, and it's deliberate. A ticket that ends up blocked,
failed, capped, or otherwise sitting in `Needs Attention` keeps its
worktree and branch indefinitely, on purpose: that state is exactly
the in-progress context a human or a later fixer needs to pick the
thread back up, and deleting it on a timer risks destroying something
nobody's looked at yet.

If a ticket is truly abandoned — cancelled, or the human decides not
to pursue it — cleaning up its worktree (`git worktree remove`) and
branch (`git push origin --delete <branch>`) is a human call, not
something you or any subagent does on your own judgment.

## Models and effort

Phases name capability tiers, not vendor models: planning and every
review round want the strongest reasoning model available, because
both are judgment; implementing to a written plan, fixing findings,
and resolving a mechanical rebase conflict are all mid-tier work.
Effort high everywhere. Map by harness — this table is the canonical
reference; `implement-ticket`, `adversarial-review`, and `merge-queue`
all point back to it:

| Phase                     | Claude Code  | Antigravity            | Codex                     |
| ------------------------- | ------------ | ---------------------- | ------------------------- |
| Plan                      | Opus, high   | gemini-3.7-flash, high | strongest available, high |
| Implement / fix / rebase  | Sonnet, high | gemini-3.7-flash, high | mid-tier, high            |
| Review (all rounds)       | Opus, high   | gemini-3.7-flash, high | strongest available, high |

If your harness cannot set a subagent's model, run everything at the
session model and say so when presenting the batch — degrade loudly,
never silently.

## Status updates

After each merge, and whenever the human asks: one table from the
manifest — ticket, title, where it is (implementing / review round N /
fixing / ready for merge approval / queued to merge / merged / needs
attention), PR link.
Nothing else; the details live on the PRs.

## Escalation

The planner, `implement-ticket`, `adversarial-review`, and
`merge-queue` each handle their own stall — a bad report from any of
them already means the ticket's been commented on (planner and
implement-ticket comment directly for a blocked report) and left
somewhere sane: `Needs Attention` for an implement blocked/failed, `In
Review` with findings posted for a capped review, or untouched with
the conflict named for a merge blocked/failed. Your job on any of
those reports: tell the human what stopped it, and carry on with the
rest of the batch.

Keep `blocked` and `failed` distinct when you relay them — collapsing
both into "it broke" is the one thing not to do here. Blocked means a
subagent found something that doesn't match what it expected — a
stale plan, a bug that doesn't reproduce, an unrelated bug it noticed,
a rebase conflict that needs new logic — and needs your judgment
before anything else proceeds. Failed means it tried the documented
path and hit a mechanical wall — CI never went green. The human acts
on these differently, so say which one it was and, for blocked, what
the subagent actually found.

Never silently drop a ticket, and never let one ticket's trouble block
the others.
