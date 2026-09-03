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

If addressing a finding would take you outside what it or TICKET's
brief actually authorizes — the real fix means touching code the brief
never scoped, contradicts the brief, or reveals the ticket's premise
doesn't hold — stop instead of expanding scope on your own judgment.
Comment the specific mismatch on the PR and report back as blocked.
This is different from rebutting: a rebuttal disputes one finding and
you still finish the round; blocked means you can't finish the round
without a call that isn't yours to make.

Before pushing, the repo's checks must pass locally:

    make build test api-check cli-docs-check

Then push and watch CI (`gh pr checks --watch`). Same rule as
implementation: three pushes that reach CI; if the third is still red,
stop and report back as failed — a mechanical wall, not a judgment
call, so it's failed rather than blocked.

Report back to the orchestrator in exactly this shape — no diffs, no
logs:

    TICKET: <id>
    RESULT: green | blocked | failed
    ADDRESSED: <n of m findings fixed>
    REBUTTED: <n, with one line each, or none>
    NOTES: <anything weak, why blocked, or why it failed>
