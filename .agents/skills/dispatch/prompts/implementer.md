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
failed. Do not invent your own brief.

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
thrashing.

Report back to the orchestrator in exactly this shape, and keep it
under ~15 lines — no diffs, no logs:

    TICKET: <id>
    RESULT: green | failed
    PR: <url>
    BRANCH: <name>
    SUMMARY: <2-3 sentences: what changed, where>
    FILES: <paths touched>
    NOTES: <anything you know is weak, or why it failed>
