---
name: merge-queue
description: Merge every eligible pull request — approved via GitHub review, or with its linked WRIT ticket in To Merge, and CI green — rebasing each onto main in an order chosen to minimize conflicts, resolving purely mechanical rebase conflicts itself, and surfacing anything needing new logic or a judgment call to the human instead of guessing. Use when asked to run the merge queue, merge everything that's approved, or merge eligible PRs. Called by the dispatch skill for its own batch's approved tickets; equally fine invoked standalone against any repo's open PRs.
---

# Merge queue

Given one or more targets — a PR, a ticket id, or the literal "all
eligible PRs" — merge each into main, in an order chosen to minimize
conflicts, as fast as CI allows. You do not write code, read a diff,
or invent a resolution to a real conflict yourself: a resolver
subagent handles each rebase, and stops instead of guessing when a
conflict needs a decision that isn't mechanical.

If invoked with no target, **ask** which PR(s) or ticket(s) to merge —
never default to scanning the repo. "All eligible PRs" is a valid
explicit target: see "Finding eligible PRs" below.

## Eligibility

Never merge anything without one of these:

- **No linked Linear ticket**: the PR carries at least one **approved**
  GitHub review.
- **A linked WRIT ticket** (from the title's `<TICKET>: ...` convention
  or the branch name): that ticket is in `To Merge` — moved there
  directly by a human, or by you once you've confirmed the human
  approved it (in chat, or pre-authorized at a `dispatch` batch gate).

Either way, CI must show green on the PR's current head before you
queue it. That's a sanity check, not the real gate — rebasing needs a
fresh CI run regardless of what the pre-rebase head showed.

## Finding eligible PRs

For "all eligible PRs": list open PRs (`gh pr list --state open`) and
keep the ones meeting the rule above. For an explicit ticket or PR
target from a caller, just confirm it's eligible — don't second-guess
a target you were already given.

## Ordering

Use the same lightweight file-overlap approach `dispatch`'s wave
planning uses — file lists per PR (`gh pr diff <number> --name-only`),
never diff content. Order:

1. Serial chains first, in dependency order (one PR plainly building
   on another).
2. Then whatever overlaps least with what's already merged this run.
   Main moves after every merge, so re-evaluate before picking the
   next PR — don't compute the whole order up front and stick to it
   blindly.

Trivial overlap (both touch a registration list, an import block) is
not a reason to serialize — that's exactly what the rebase absorbs.

## Per PR

1. Move a chat-or-pre-authorized ticket to `To Merge` if it isn't
   already there (skip if the PR has no linked ticket).
2. Reuse the worktree at `<runs-dir>/<TICKET>` if one exists (a caller
   may have left one from implementing or reviewing); otherwise make
   one on the PR's branch:
   `git fetch origin && git worktree add <runs-dir>/<TICKET-or-PR#> origin/<branch>`.
3. Spawn a **fresh** resolver subagent with `prompts/resolver.md` —
   mid-tier model, high effort (see the `dispatch` skill's Models and
   effort table for the harness mapping) — filling in WORKTREE,
   BRANCH, PR. It rebases onto current `origin/main`, resolves any
   purely mechanical conflict itself, pushes, and watches CI under the
   same three-attempt rule as implementing.
4. **`RESULT: green`** → mark the PR ready and
   `gh pr merge <number> --squash --delete-branch`. This repo doesn't
   auto-delete branches on merge, so pass the flag yourself every
   time — the squash commit is all of the branch's content that
   survives, and a remote branch with nothing left to give is just
   clutter. Move the ticket to `Done` (a GitHub automation may race you
   there, which is harmless) and delete the worktree
   (`git worktree remove <path>`). Move to the next PR.
5. **`RESULT: blocked`** — the resolver found a conflict that needs new
   logic or a judgment call, not just combining both sides. Stop this
   PR's merge, tell the human exactly what it found (files, the nature
   of the conflict), and leave the PR and worktree as they are. Move
   on to the next PR — one blocked merge never stalls the rest of the
   queue.
6. **`RESULT: failed`** — CI never went green on the rebased head after
   three attempts. Handle it like blocked for queue purposes (stop
   this one, tell the human, move on), but say which one it was — they
   read differently: blocked needs a decision, failed hit a mechanical
   wall.

## Report

One line per PR as you go, plus a final table if you merged more than
one: PR, ticket, `RESULT` (merged | blocked | failed), one line of why
for anything not merged.

This skill never plans, implements, or reviews anything — it only
merges what's already eligible. See `implement-ticket` and
`adversarial-review` for the stages before this one, and `dispatch`
for the full pipeline.
