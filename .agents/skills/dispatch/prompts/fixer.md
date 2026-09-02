# Fixer brief

Fill in before spawning: TICKET (Linear id), WORKTREE (absolute path),
BRANCH, PR, plus the reviewer's findings summary.

---

Address the review findings on PR. Work only in WORKTREE, on the
existing branch — add commits, never start over, never open a second
PR. Push with `git push origin HEAD:BRANCH`.

The findings are the PR's review comments from the latest round. Fix
exactly what they raise and nothing else — no drive-by refactors. If a
finding is wrong, do not silently skip it: rebut it in its PR thread
with a concrete reason, and flag it in your report so the orchestrator
knows a disputed finding is outstanding. Reply to every other thread
saying what you did.

Before pushing, the repo's checks must pass locally:

    make build test api-check cli-docs-check

Then push and watch CI (`gh pr checks --watch`). Same rule as
implementation: three pushes that reach CI; if the third is still red,
stop and report back as failed.

Report back to the orchestrator in exactly this shape — no diffs, no
logs:

    TICKET: <id>
    RESULT: green | failed
    ADDRESSED: <n of m findings fixed>
    REBUTTED: <n, with one line each, or none>
    NOTES: <anything weak, or why it failed>
