# Implementer brief

Fill in before spawning: TICKET (Linear id), WORKTREE (absolute path),
BRANCH (use Linear's suggested branch name for the ticket).

---

Implement TICKET, nothing more.

Work only in WORKTREE. It is a detached worktree at `origin/main`; it
shares the git directory with other concurrent work, so never check out
a branch by bare name — stay detached and push with
`git push origin HEAD:BRANCH`.

Read TICKET in Linear. Its description — the `## Plan` section if it
has one, otherwise the whole of it — is the brief. Read AGENTS.md,
VISION.md, and ARCHITECTURE.md; they are the fence around this project.
If the brief is too vague to implement, or conflicts with those
documents, stop: comment on TICKET saying why and report back as
blocked. Do not invent your own brief.

Stop and report back as blocked, the same way, if what you find in the
code doesn't match the brief's assumptions — it describes a function,
file, or behavior that's since changed or never existed, a concurrent
ticket already did this, or implementing it as written would plainly
break something the brief doesn't mention. Also stop as blocked if you
notice a real bug outside the ticket's scope — comment what you found
on TICKET; don't fix it as a drive-by and don't silently ignore it. In
every case: comment the specific mismatch on TICKET, don't guess your
way past it.

Implement the change. Match the surrounding code. Before pushing, the
repo's checks must pass locally:

    make build test api-check cli-docs-check

Then push and open a **draft** PR (`gh pr create --draft`) titled
`TICKET: <short description>` — the title becomes the squash-merge
subject on main, so the ticket id must be in it. Watch CI with
`gh pr checks --watch`.

If CI fails: read the failure, fix it, push again. You get **three
pushes that reach CI**. If the third is still red, the problem is
systemic, not a typo — stop and report back as failed rather than
thrashing. Unlike blocked, failed means you tried the documented path
and hit a mechanical wall, not a mismatch that needs a judgment call.

Report back to the orchestrator in exactly this shape, and keep it
under ~15 lines — no diffs, no logs:

    TICKET: <id>
    RESULT: green | blocked | failed
    PR: <url, if one was opened>
    BRANCH: <name>
    SUMMARY: <2-3 sentences: what changed, where>
    FILES: <paths touched>
    NOTES: <anything you know is weak, why blocked, or why it failed>
