---
title: "writ issue"
weight: 40
---

Manage issues: create, list, check status, comment, assign, link, and label.

## create

Create a new issue with a title, optional description, and initial state.

## list

List issues, filtered by state, assignee, label, or text.

## status

View or update an issue's state (open, closed).

## comment

Add a comment to an issue, optionally as a reply to an existing comment.

## assign

Add or remove issue assignees.

## link

Manage cross-reference links between issues and other objects.

## label

Add or remove issue labels, or view the current labels on an issue.

## Public intake and bot attribution

Writ operations require push access to `refs/writ/<writer-id>/*`. Unauthenticated
public contributors cannot write ops directly into the repository.

For open-source projects or public bug intake, teams run an **intake bot**: a
designated writer with push credentials that bridges incoming webhooks, web
forms, email, or GitHub Issues into Writ operations.

To attribute external reporters truthfully without synthesizing fake email
addresses, intake bots use the `user:` person identifier scheme
(`user:<service>-<id>`, such as `user:github-octocat`). This accurately records
the reporter's origin identity while the bot signs the operation commit. Many
projects may also choose to keep GitHub Issues as the public front door and sync
accepted issues into Writ.
