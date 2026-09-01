# Quickstart

This guide walks you through setting up Writ in a git repository, creating a review, commenting on it, recording an approval, and syncing review operations to a collaborator across a remote repository.

## 1. Set Up Your Repository & SSH Signing Key

Writ stores all code review and issue operations directly inside your git repository as signed commits under `refs/writ/*`.

Initialize your git repository and ensure your SSH signing key and identity are configured:

```bash
git init
git config user.name "Alice"
git config user.email "alice@example.com"
git config gpg.format ssh
git config user.signingKey ~/.ssh/id_ed25519.pub
```

Make your initial commit:

```bash
echo "# My Project" > README.md
git add README.md
git commit -m "Initial commit"
git branch -M main
```

## 2. Initialize Writ

Run `writ init` to mint a unique writer ID and configure Writ's remote fetch refspecs:

```bash
writ init
```

Output:
```
Writer ID: 0123456789abcdef (minted)
Repo ID: a1b2c3d4e5f60718293a4b5c6d7e8f90 (minted)
Signing key: ~/.ssh/id_ed25519.pub (ssh)
No git remotes configured; fetch refspec will be added when a remote is configured.
```

## 3. Create a Feature and Open a Code Review

Create a feature branch with your changes:

```bash
git checkout -b feature
echo "package main" > main.go
git add main.go
git commit -m "Add main entry point"
```

Open a new code review comparing `main` and `feature`:

```bash
writ review open -title "Add main entry point" -base main -head feature
```

Output:
```
01918a3b5c6d7e8f90123456789abcde (open) Add main entry point
```

## 4. Add a Review Comment

Add a comment to the review using the review ID:

```bash
writ review comment 01918a3b5c6d7e8f90123456789abcde -m "Looks great, ready for review."
```

## 5. Record an Approval Verdict

Record an approval for the review:

```bash
writ review approve 01918a3b5c6d7e8f90123456789abcde -verdict approve -m "LGTM"
```

## 6. Sync Operations with Remote

Add a git remote and sync Writ review operations:

```bash
git remote add origin git@github.com:example/repo.git
writ sync origin
```

Output:
```
origin: pushed 3 ops, 1 object updated
```

## 7. Collaborator Clones and Lists Reviews

On another machine or clone, your collaborator initializes Writ and syncs:

```bash
git clone git@github.com:example/repo.git collab
cd collab
writ init
writ sync origin
```

Your collaborator can now inspect reviews and review status offline:

```bash
writ review list
```

Output:
```
01918a3b    open    Add main entry point    Alice    2026-08-31 16:00:00
```

And view detailed status including approvals and revisions:

```bash
writ review status 01918a3b
```
