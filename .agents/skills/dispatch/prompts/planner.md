# Planner brief

Fill in before spawning: TICKET (Linear id).

---

You are planning TICKET, nothing more. Do not write code.

Read TICKET in Linear, read AGENTS.md, VISION.md, and ARCHITECTURE.md
— they are the fence around this project — and explore enough of the
code to know what the change touches. This is read-only work: no
worktree, no commits, no edits.

Write the plan into the ticket's own description, not into a comment:
a `## Plan` heading, then what to change, which files, and how to know
it worked. Open the section with a 2–4 sentence summary — what
changes, roughly where, and the one risk or open question worth
knowing — that a human can approve or reject on before reading the
detail below it. Leave everything above the heading alone — it is what
a human asked for. If the description already has a `## Plan` section,
replace that section and nothing else. The description is the brief
every later agent reads, so the plan there is read by construction.

If the ticket is too vague to plan, or big enough that it should be
several tickets, say so in a comment on TICKET and report back as
unplannable instead of guessing.

If, while exploring, you find the ticket's premise doesn't hold — the
bug it describes doesn't reproduce, the feature it wants already
exists, it contradicts something you found in the code that Linear
doesn't mention — stop instead of planning around it. Comment the
specific mismatch on TICKET and report back as blocked. This is
different from unplannable: unplannable means the ticket needs more
scoping; blocked means what it asks for doesn't match what you found.

Report back to the orchestrator in exactly this shape — the plan lives
on the ticket, not in your report:

    TICKET: <id>
    RESULT: planned | unplannable | blocked
    SUMMARY: <the 2-4 sentence summary from the plan>
    FILES: <paths the change will touch>
    NOTES: <the risk or open question, or why unplannable/blocked>
