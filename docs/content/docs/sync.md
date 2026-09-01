---
title: "writ sync"
weight: 50
---

Synchronize operations with git remotes: fetch remote operations, push local operations, and refresh the local projection cache.

## Synopsis

```
writ sync [--status] [--json] [remote...]
```

## What it does

`writ sync` ensures refspecs are configured, fetches remote writ refs, pushes local operations, and refreshes the SQLite projection cache. With `--status`, it reports the count of unpushed operations without performing network transport.
