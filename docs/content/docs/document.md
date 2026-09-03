---
title: "writ doc"
weight: 45
---

Manage collaborative documents and sections: create, list, show, edit, link, and section management.

Writ documents represent long-form collaborative texts such as RFCs, design docs, specifications, and project plans stored directly in git as signed, append-only operations under `refs/writ/<writer-id>/document` and `refs/writ/<writer-id>/section`.

## create

Create a new document collaborative object with a title, optional initial cross-reference links, and labels.

```bash
writ doc create -t "RFC: Architecture Overview"
writ doc create -t "Design Doc" --link issue-42:plan --label architecture
```

## list

List documents in the repository or workspace, optionally filtered by label.

```bash
writ doc list
writ doc list --label rfc --json
```

## show

Display a document's metadata and its ordered sections. If concurrent edits have produced conflicting versions of a section body, visual conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`) are displayed.

```bash
writ doc show 01J8ABC
writ doc show 01J8ABC --json
```

## edit

Update document metadata, such as its title or attached labels.

```bash
writ doc edit 01J8ABC -t "RFC: Architecture Overview (v2)"
writ doc edit 01J8ABC --label approved --remove-label draft
```

## link

Attach or update cross-reference links from a document to issues, reviews, or external entities.

```bash
writ doc link 01J8ABC --target issue-105 --relation implementation-plan
```

## section

Manage sections within a document. Documents are composed of ordered sections positioned via fractional indexing (`engine/order`).

### section add

Append or position a new section in a document. The section body can be provided directly via `-m` or read from a file with `-F` (`-` reads from standard input).

```bash
writ doc section add 01J8ABC -t "Motivation" -m "We need documents in git."
writ doc section add 01J8ABC -t "Specification" -F spec.md --after 01J8SEC
```

### section edit

Update an existing section's body text. Committing a new body observes prior heads and resolves any existing edit conflicts.

```bash
writ doc section edit 01J8SEC -m "Updated section body text."
writ doc section edit 01J8SEC -F updated_spec.md
```

### section move

Reorder a section between siblings using `--after` and/or `--before`.

```bash
writ doc section move 01J8SEC --after 01J8FIRST
writ doc section move 01J8SEC --before 01J8LAST
```

### section delete

Soft-delete (tombstone) a section. Tombstoned sections are excluded from the active document structure while preserving causal history.

```bash
writ doc section delete 01J8SEC
```
