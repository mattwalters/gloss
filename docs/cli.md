---
title: "CLI Reference"
slug: "cli"
---

# CLI Reference

`writ` is an open SDLC layer that stores code review and issues inside git.

## Table of Contents

- [`writ init`](#writ-init)
- [`writ issue create`](#writ-issue-create)
- [`writ issue status`](#writ-issue-status)
- [`writ issue assign`](#writ-issue-assign)
- [`writ issue list`](#writ-issue-list)
- [`writ issue link`](#writ-issue-link)
- [`writ review open`](#writ-review-open)
- [`writ review comment`](#writ-review-comment)
- [`writ review approve`](#writ-review-approve)
- [`writ review status`](#writ-review-status)
- [`writ review list`](#writ-review-list)
- [`writ sync`](#writ-sync)
- [`writ version`](#writ-version)
- [`writ completion`](#writ-completion)
- [`writ help`](#writ-help)

## Commands

### `writ init`

Initialize writ configuration (writer ID and remote fetch refspecs)

#### Synopsis

```
Usage: writ init [-C <dir>] [remote...]
```

#### Description

Initialize writ repository configuration by resolving or minting a writer ID,
verifying SSH signing key configuration, and adding fetch refspecs for git remotes.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>

#### Examples

```bash
writ init
writ init origin
```

### `writ issue create`

Create a new issue

#### Synopsis

```
Usage: writ issue create [-C <dir>] -title <t> [-description <d>] [-state open|closed] [-fixes <ref>]... [-relates <ref>]...
```

#### Description

Create a new issue.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-title <t>`: Issue title <t> (required)
- `-description <d>`: Issue description <d>
- `-state <state>`: Initial issue state <state> (open or closed)
- `-fixes <ref>`: Add a 'fixes' cross-reference link <ref> (repeatable)
- `-relates <ref>`: Add a 'relates' cross-reference link <ref> (repeatable)

#### Examples

```bash
writ issue create -title "Fix memory leak"
writ issue create -title "Bug in parser" -fixes 01J8ABC
```

### `writ issue status`

View or update issue status

#### Synopsis

```
Usage: writ issue status [-C <dir>] <id> [<state>] [-reason <r>] [--json]
```

#### Description

View or update issue status.

States:
  open, closed

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-reason <r>`: Reason <r> for status change
- `-json`: Output result as JSON (view mode only)

#### Examples

```bash
writ issue status 01J8ABC
writ issue status 01J8ABC closed -reason "resolved in #42"
writ issue status 01J8ABC --json
```

### `writ issue assign`

Add or remove issue assignees

#### Synopsis

```
Usage: writ issue assign [-C <dir>] <id> [-add <a>]... [-remove <a>]...
```

#### Description

Add or remove issue assignees.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-add <a>`: Add assignee <a> email or ID (repeatable)
- `-remove <a>`: Remove assignee <a> email or ID (repeatable)

#### Examples

```bash
writ issue assign 01J8ABC -add alice@example.com
writ issue assign 01J8ABC -remove bob@example.com
```

### `writ issue list`

List issues

#### Synopsis

```
Usage: writ issue list [-C <dir>] [-state <s>]... [-assignee <a>]... [-label <l>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>] [--json]
```

#### Description

List issues.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-state <s>`: Filter by issue state <s> (repeatable)
- `-assignee <a>`: Filter by assignee <a> name or email (repeatable)
- `-label <l>`: Filter by label <l> (repeatable)
- `-author <a>`: Filter by author <a> name or email (repeatable)
- `-text <q>`: Filter by text <q> match in title or description
- `-limit N`: Maximum number N of issues to return
- `-sort <order>`: Sort order <order> (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)
- `-json`: Output result as JSON

#### Examples

```bash
writ issue list
writ issue list -state open
writ issue list -assignee alice@example.com --json
```

### `writ issue link`

Manage issue cross-reference links

#### Synopsis

```
Usage: writ issue link [-C <dir>] <id> -target <ref> -relation fixes|relates|none [-target-type <t>]
```

#### Description

Manage issue cross-reference links.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-target <ref>`: Target reference <ref> (required, e.g. <repo-id>#<object-id> or <object-id>)
- `-relation <rel>`: Link relation <rel>: fixes, relates, or none (required)
- `-target-type <t>`: Target object type <t>

#### Examples

```bash
writ issue link 01J8ABC -target 01J8DEF -relation fixes
writ issue link 01J8ABC -target other-repo#01J8DEF -relation relates
```

### `writ review open`

Create a new code review

#### Synopsis

```
Usage: writ review open [-C <dir>] -title <t> [-description <d>] [-base <ref> -head <ref>] [-draft]
```

#### Description

Create a new code review.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-title <t>`: Review title <t> (required)
- `-description <d>`: Review description <d>
- `-base <ref>`: Base revision <ref> commit or ref
- `-head <ref>`: Head revision <ref> commit or ref
- `-draft`: Create review in draft state

#### Examples

```bash
writ review open -title "Add feature X"
writ review open -title "Add feature X" -base main -head feature-x
writ review open -title "WIP: feature" -draft
```

### `writ review comment`

Add a comment to a review

#### Synopsis

```
Usage: writ review comment [-C <dir>] <id> -m <text> [-reply-to <comment-id>]
```

#### Description

Add a comment to a review.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-m <text>`: Comment message text <text> (required)
- `-reply-to <comment-id>`: Comment ID <comment-id> to reply to

#### Examples

```bash
writ review comment 01J8ABC -m "Looks good to me"
writ review comment 01J8ABC -m "Addressed feedback" -reply-to 01J8DEF
```

### `writ review approve`

Record a review verdict

#### Synopsis

```
Usage: writ review approve [-C <dir>] <id> [-verdict approve|request-changes|none] [-revision <ref>] [-m <msg>] [-subject <s>]
```

#### Description

Record a review verdict.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-verdict approve|request-changes|none`: Verdict approve|request-changes|none (default: approve)
- `-revision <ref>`: Revision commit ref or SHA <ref> (defaults to latest head)
- `-m <msg>`: Verdict message <msg>
- `-subject <s>`: Subject identity <s> (defaults to writer email or writer ID)

#### Examples

```bash
writ review approve 01J8ABC
writ review approve 01J8ABC -verdict request-changes -m "Please fix tests"
```

### `writ review status`

View or update review status

#### Synopsis

```
Usage: writ review status [-C <dir>] <id> [<state>] [-reason <r>] [-merge-commit <ref>] [--json]
```

#### Description

View or update review status.

States:
  draft, open, closed, merged

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-reason <r>`: Reason <r> for status change
- `-merge-commit <ref>`: Merge commit ref or SHA <ref> (valid when setting status to merged)
- `-json`: Output result as JSON (view mode only)

#### Examples

```bash
writ review status 01J8ABC
writ review status 01J8ABC closed -reason "superseded"
writ review status 01J8ABC merged -merge-commit main
writ review status 01J8ABC --json
```

### `writ review list`

List code reviews

#### Synopsis

```
Usage: writ review list [-C <dir>] [-status <s>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>] [--json]
```

#### Description

List code reviews.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-status <s>`: Filter by review status <s> (repeatable)
- `-author <a>`: Filter by author <a> name or email (repeatable)
- `-text <q>`: Filter by text <q> match in title or description
- `-limit N`: Maximum number N of reviews to return
- `-sort <order>`: Sort order <order> (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)
- `-json`: Output result as JSON

#### Examples

```bash
writ review list
writ review list -status open
writ review list -status open -status draft --json
```

### `writ sync`

Synchronize operations with git remotes

#### Synopsis

```
Usage: writ sync [-C <dir>] [--status] [--json] [remote...]
```

#### Description

Synchronize collaborative SDLC operations with one or more git remotes.

Fetch remote operations, push local operations, and refresh the local projection cache.
With no remote specified, defaults to 'origin' or the sole configured remote.

#### Flags

- `-C <dir>`: Run as if writ was started in <dir>
- `-status`: Report unpushed ops count without network transport
- `-json`: Output result as JSON

#### Exit Codes

- `0`: Success
- `1`: Transport or unclassified git failure
- `2`: Usage error (bad flag, no resolvable default remote)
- `3`: Unknown or unconfigured remote
- `4`: Rejected non-fast-forward update
- `5`: Not a git repository / store cannot be opened
- `6`: Authentication or credentials failure
- `7`: Network or remote unreachable

#### Examples

```bash
writ sync
writ sync origin
writ sync --status
writ sync --json
```

### `writ version`

Print the writ version

#### Synopsis

```
Usage: writ version
```

#### Description

Print the version of the writ binary.

#### Examples

```bash
writ version
```

### `writ completion`

Generate shell completion scripts

#### Synopsis

```
Usage: writ completion <shell>
```

#### Description

Generate shell completion scripts for bash, zsh, or fish.

Supported shells: bash, zsh, fish.

#### Examples

```bash
writ completion bash > /etc/bash_completion.d/writ
writ completion zsh > "${fpath[1]}/_writ"
writ completion fish > ~/.config/fish/completions/writ.fish
```

### `writ help`

Show help for commands

#### Synopsis

```
Usage: writ help [<command> [<subcommand>]]
```

#### Description

Show detailed help and examples for a command or subcommand.

#### Examples

```bash
writ help
writ help review
writ help review open
```

