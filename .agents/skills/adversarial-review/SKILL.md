---
name: adversarial-review
description: Run adversarial review rounds on one or more open pull requests — a fresh reviewer subagent each round, a fixer subagent addressing findings, capped at three rounds — and report which are ready to merge. Use when asked to adversarially review a PR, run a review cycle, or review every open PR. Called by the dispatch skill right after implement-ticket reports a ticket green; equally fine invoked standalone against any open PR, ticket-linked or not.
---

# Adversarial review

Given one or more targets — a PR, a ticket id, or the literal "all open
PRs" — run this cycle on each, independently: fresh reviewer subagent
→ if it finds anything, fixer subagent → fresh reviewer again, up to
three rounds. You never read the diff, a test log, or a CI log
yourself; subagents do that and report back counts and one line per
finding.

If invoked with no target, **ask** which PR(s) or ticket(s) to review
— never default to scanning the repo. "All open PRs" is a valid
explicit target if the caller says it: expand it with
`gh pr list --state open` and run one independent cycle per PR.

## Resolving a target to a brief

A reviewer needs the ticket's brief — its description, `## Plan`
section if present — to check the PR against. Given a ticket id, find
its open PR. Given a PR, find its linked ticket from the title's
`<TICKET>: ...` convention or the branch name. If a PR has no
resolvable ticket (a human's PR with no Linear link), review it
against just this repo's conventions (AGENTS.md, ARCHITECTURE.md) and
say in your report that there was no ticket brief to check against.

## Cycle, per target

1. Move the resolved ticket to `In Review`, if one resolved.
2. Spawn a **fresh** reviewer subagent with `prompts/reviewer.md` —
   strongest model available, high effort, every round, not just later
   ones (see the `dispatch` skill's Models and effort table for the
   harness mapping) — filling in TICKET (or "none"), PR, ROUND. It
   reads the diff with hostile eyes, posts findings as PR review
   comments rated major/medium/minor, and returns only counts and one
   line per finding.
3. **Reviewer reports blocked** — it found something that isn't a
   diff-level finding (the PR doesn't do what the brief describes, a
   diverged base, a serious pre-existing bug) — stop the cycle for this
   target right there. Don't spawn a fixer; there's nothing for it to
   fix. Go to "On a blocked or failed report" below.
4. **Zero findings** → this target is ready. Report it and stop.
5. **Any findings** → spawn a fixer subagent with `prompts/fixer.md` in
   a worktree on the PR's branch — reuse one at `<runs-dir>/<TICKET>`
   if it already exists (a caller may have left one from implementing),
   otherwise make one:
   `git fetch origin && git worktree add <runs-dir>/<TICKET-or-PR#> origin/<branch>`.
   The fixer addresses every finding, or rebuts one in the PR thread
   with a concrete reason and flags it in its report as disputed. It
   gets CI green again under the implementer's same three-attempt
   rule. Then run the next round with another fresh reviewer.

   If the fixer instead reports **blocked** — a finding can't be
   addressed without a judgment call outside its brief — stop the cycle
   here too, same as a blocked reviewer report.

Hard cap: three rounds per target. If round three still has findings —
any severity — stop, leave the PR as it stands, and report it to
whoever is waiting: the findings summary, what each round fixed, and
your read on whether it's converging or looping. Never run a fourth
round unattended; a human decides whether another cycle is warranted.

A clean round is a good round — zero findings is an acceptable and
expected result for a small, correct change, not a sign the review was
skipped.

## On a blocked or failed report

Blocked (from either subagent) means something about the target
doesn't match what the review expected and needs a human's judgment
call before this cycle can mean anything — the mismatch is already
commented on the PR. Failed means the fixer never got CI green after
three honest attempts — a mechanical wall. Don't collapse the two when
you relay this: say which one it was and why. Either way, stop this
target's cycle, and don't let it block review of the others you were
given.

## Report, per target

    TICKET: <id or none>
    PR: <url>
    RESULT: ready | blocked | capped | failed
    ROUNDS: <n>
    LAST_ROUND_FINDINGS: <count by severity, or 0 — omit if blocked>
    NOTES: <the mismatch if blocked, disputed findings, why capped/failed>

`RESULT: ready` means zero findings on the latest round and CI green —
it is not a merge. This skill never merges anything; a human (directly,
or via the `dispatch` skill's merge queue) still approves that
separately.
