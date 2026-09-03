---
title: "writ comment"
weight: 35
---

Manage comments on collaborative objects: edit and delete.

## edit

Edit the text of an existing comment. Emits a comment `edit` operation that updates the comment's content in the fold.

## delete

Delete an existing comment by recording a tombstone operation. The comment is marked as deleted in materialized state while preserving DAG history.
