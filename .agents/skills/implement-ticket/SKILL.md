---
name: implement-ticket
description: Take one or more Linear WRIT tickets from Todo/Implementing through a CI-green draft PR, using a fresh implementer subagent per ticket in an isolated git worktree. Use when asked to implement a specific ticket, pick up a ticket, or "just do the implementation" without also running review or merge. Called by the dispatch skill once per ticket in a wave; equally fine invoked standalone against a single ticket.
---

# Implement ticket

Given one or more ticket ids, take each through a CI-green draft PR
using a fresh implementer subagent. Independent tickets run
concurrently — spawn every target's subagent in the same batch of tool
calls. You do not write code, read diffs, or debug CI yourself: the
subagent does, and reports back.

If invoked with no ticket id, ask which ticket(s) to implement rather
than guessing.

## Per ticket

1. Move the ticket to `Implementing` in Linear, unless it's already
   there (a caller may have moved it during its own planning/approval
   step).
2. Make an isolated worktree outside the repo checkout:
   `git fetch origin && git worktree add --detach <runs-dir>/<TICKET> origin/main`.
   Never let two tickets share one worktree.
3. Spawn an implementer subagent with `prompts/implementer.md`, filling
   in TICKET, WORKTREE, BRANCH (Linear's suggested branch name for the
   ticket) — mid-tier model, high effort (Sonnet on Claude Code; see
   the `dispatch` skill's Models and effort table for other harnesses).

The implementer's contract, enforced by the prompt: implement the
ticket — its `## Plan` section if the description has one, otherwise
the whole description — match the surrounding code, pass
`make build test api-check cli-docs-check` locally, push, open a
**draft** PR titled `<TICKET>: <short description>` (the squash-merge
subject line, so the ticket id must be in it), and watch CI. It gets
three pushes that reach CI; a third red run means something systemic,
and it reports back failed instead of thrashing.

It also stops and reports back **blocked**, instead of guessing, if
what it finds doesn't match the brief: a stale assumption, a
concurrent ticket that already did this, or a real bug outside the
ticket's scope. Blocked and failed are not the same thing — blocked
means the brief needs a human's judgment call; failed means CI never
went green after three honest attempts. Keep that distinction when you
relay it upward; don't collapse both into one generic "it broke."

## On a blocked or failed report

For blocked, the implementer has already commented the specific
mismatch on the ticket. For failed, it hasn't — comment what happened
yourself. Either way: move the ticket to `Needs Attention` and tell
whoever is waiting on this — the human if you were invoked standalone,
or your caller if a skill invoked you. Don't retry past the
implementer's own three-attempt budget, and don't let one stuck ticket
stop the others you were given.

## Report, per ticket

Relay the implementer's report upward, unchanged:

    TICKET: <id>
    RESULT: green | blocked | failed
    PR: <url, if one was opened>
    BRANCH: <name>
    SUMMARY: <2-3 sentences: what changed, where>
    FILES: <paths touched>
    NOTES: <anything known weak, why blocked, or why it failed>

`RESULT: green` means CI is green on a draft PR — it says nothing
about review. This skill never runs review and never merges; see
`adversarial-review` and, for the full pipeline, `dispatch`.
