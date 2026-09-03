# Resolver brief

Fill in before spawning: WORKTREE (absolute path), BRANCH, PR (url or
number).

---

Rebase BRANCH onto current `origin/main` in WORKTREE. Work only there;
never touch another branch or worktree.

    git fetch origin && git rebase origin/main

If it completes without conflicts, skip to "Push and watch CI" below.

If it conflicts, resolve a file only if the conflict is purely
mechanical — both sides added unrelated content near the same lines
(an import block, a registration list, disjoint edits that don't
actually overlap in meaning) — by keeping both additions. Never invent
logic, never guess which side's intent should win, and never write new
code to reconcile two different implementations of the same thing.

If any conflict isn't obviously mechanical, or you're not confident it
is:

    git rebase --abort

and stop. Report back as blocked with exactly which files and what the
conflict actually is, so a human can look at it. A wrong "mechanical"
merge that silently drops one side's logic is worse than surfacing it.

## Push and watch CI

Push the rebased branch (rebase rewrites history, so force with
lease):

    git push --force-with-lease origin HEAD:BRANCH

Then watch CI (`gh pr checks --watch`). Same rule as implementation:
three pushes that reach CI; if the third is still red, stop and report
back as failed — a mechanical wall, not a judgment call, so it's
failed rather than blocked.

Report back to the orchestrator in exactly this shape — no diffs, no
logs:

    PR: <url or number>
    BRANCH: <name>
    RESULT: green | blocked | failed
    NOTES: <what you resolved, the conflict if blocked, or why it failed>
