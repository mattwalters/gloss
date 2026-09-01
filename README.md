# writ
Open Source SDLC

![Writ quickstart](docs/static/posters/quickstart.png)

Start with [VISION.md](VISION.md) for what this is and why, then
[ARCHITECTURE.md](ARCHITECTURE.md) for how it's built and the reasoning
behind each decision.

## Install

With Go (1.25+):

```
go install github.com/writtendev/writ/cmd/writ@latest
```

Without Go:

```
curl -fsSL https://raw.githubusercontent.com/writtendev/writ/main/install.sh | sh
```

The install script downloads the release binary for your platform, verifies
the checksum, and places it in `~/.local/bin`. Pass `--bin-dir` to choose a
different directory. No `sudo`, no PATH edits.

Verify:

```
writ version
```
