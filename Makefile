PKG := github.com/writtendev/writ
APISURFACE := go run ./internal/cmd/apisurface

# Resolve the install dir the way the go tool does: GOBIN when set, else
# GOPATH/bin.
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# A local build names the commit it came from and whether the tree was dirty,
# so the version it reports is never a guess.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -X $(PKG)/internal/version.Version=$(VERSION)

# The one place Hugo's version is pinned. CI reads it back out via
# `make -s hugo-version` in .github/workflows/docs.yml.
HUGO_VERSION := 0.165.0

.PHONY: build test install api api-check cli-docs cli-docs-check snapshot release hugo-version docs docs-serve casts render-tape

build:
	go build ./...

test:
	go test ./...

install: ## Build and install writ into Go's bin dir
	go install -ldflags "$(LDFLAGS)" ./cmd/writ
	@printf 'installed %s to %s\n' '$(VERSION)' '$(GOBIN)/writ'
	@printf 'PATH resolves writ to: %s\n' \
	  "$$(command -v writ || echo '(nothing — is $(GOBIN) on your PATH?)')"

# api/engine.txt is the public API baseline: every exported symbol in engine
# and its public subpackages, as plain text. It is read in the PR diff, not by
# a tool, so an API change has to be visible to whoever reviews it. Generated
# from the source text alone — no export data, no absolute paths, nothing that
# varies with the Go version — which is what makes `api-check` below able to
# say "the baseline is stale" rather than "your toolchain differs from mine".
api: ## Regenerate api/engine.txt from the source
	@mkdir -p api
	$(APISURFACE) ./engine > api/engine.txt

api-check: ## Fail if api/engine.txt is stale (the CI gate)
	@set -e; \
	tmp=$$(mktemp "$${TMPDIR:-/tmp}/writ-api.XXXXXX"); \
	trap 'rm -f "$$tmp"' EXIT; \
	$(APISURFACE) ./engine > "$$tmp"; \
	if ! diff -u -L 'api/engine.txt (committed)' -L 'api/engine.txt (regenerated)' \
	     api/engine.txt "$$tmp"; then \
		echo; \
		echo 'api-check: api/engine.txt does not match the code.'; \
		echo 'api-check: run `make api` and commit the result. A line above that'; \
		echo 'api-check: changes or disappears is a breaking API change — say so'; \
		echo 'api-check: in the PR, and in CHANGELOG.md.'; \
		exit 1; \
	fi

# The CLI reference is generated from the command table in cmd/writ, not
# hand-written, so a flag cannot exist in code and be missing from the docs.
# This produces docs/cli.md; `make docs` below renders it (and the rest of
# docs/) into the site. Two stages, one direction: generate, then render.
cli-docs: ## Regenerate docs/cli.md from the command table
	go test ./cmd/writ -run TestDocsGolden -update-docs

cli-docs-check: ## Fail if docs/cli.md is stale (the CI gate)
	go test ./cmd/writ -run TestDocsGolden

# --------------------------------------------------------------------------
# Releases
# --------------------------------------------------------------------------

snapshot: ## Build the release binaries locally, publishing nothing (needs goreleaser)
	@command -v goreleaser >/dev/null || { \
	  echo 'snapshot: goreleaser (v2.6+) is not installed — see https://goreleaser.com/install/'; \
	  exit 1; }
	goreleaser release --snapshot --clean
	@bin=$$(ls -d dist/writ_$$(go env GOOS)_$$(go env GOARCH)*/writ 2>/dev/null | head -1); \
	  if [ -n "$$bin" ]; then \
	    printf 'built into dist/ — check the stamp with: %s version\n' "$$bin"; \
	  else \
	    printf 'built into dist/\n'; \
	  fi

# Semver grammar: the tag must be vMAJOR.MINOR.PATCH, optionally with a
# prerelease segment like -rc1. Build metadata (+…) is left out; nothing
# here has a use for it.
SEMVER_NUM := (0|[1-9][0-9]*)
SEMVER_ID := ($(SEMVER_NUM)|[0-9]*[A-Za-z-][0-9A-Za-z-]*)
SEMVER_RE := ^v$(SEMVER_NUM)\.$(SEMVER_NUM)\.$(SEMVER_NUM)(-$(SEMVER_ID)(\.$(SEMVER_ID))*)?

# The newest released section's version.
CHANGELOG_TOP = sed -n 's/^## \[\([0-9][^]]*\)\].*/\1/p' CHANGELOG.md | head -1

release: ## Tag main and push it, which starts the release build (VERSION=vX.Y.Z)
# VERSION must come from the command line specifically.
	@test '$(origin VERSION)' = 'command line' || { \
	  echo 'release: name the tag on the command line — make release VERSION=v0.1.0'; \
	  exit 1; }
	@printf '%s' '$(VERSION)' | grep -Eq '$(SEMVER_RE)$$' || { \
	  printf 'release: %s is not a version tag — vMAJOR.MINOR.PATCH, optionally -rc1\n' \
	    '$(VERSION)'; exit 1; }
	@test -z "$$(git status --porcelain)" || { \
	  echo 'release: the tree is dirty — commit or stash before tagging'; exit 1; }
# Cut from merged main, not a local commit.
	git fetch --quiet origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { \
	  echo 'release: HEAD is not origin/main — releases are cut from merged main'; \
	  exit 1; }
# CHANGELOG.md must have a section for this version.
	@test -f CHANGELOG.md || { echo 'release: CHANGELOG.md is missing'; exit 1; }
	@top=$$($(CHANGELOG_TOP)); \
	  test "$$top" = '$(VERSION:v%=%)' || { \
	    printf 'release: the newest section in CHANGELOG.md is [%s], not [%s]\n' \
	      "$$top" '$(VERSION:v%=%)'; \
	    echo 'release: write the section and merge it before tagging'; exit 1; }
# Known gap: main having a commit is not CI having finished with it, so a
# release cut a minute after a merge tags a commit whose run is still going.
# Checking that means asking the GitHub API, which is a different tool's job.
#
# Check origin for existing tag.
	@git ls-remote --exit-code --tags origin 'refs/tags/$(VERSION)' >/dev/null; \
	  case $$? in \
	  0) printf 'release: %s already exists on origin — pick the next version\n' \
	       '$(VERSION)'; exit 1 ;; \
	  2) ;; \
	  *) printf 'release: could not reach origin to check whether %s exists\n' \
	       '$(VERSION)'; exit 1 ;; \
	  esac
# A local tag on the same commit is fine (retrying a failed push); a local
# tag on a different commit is not.
	@if git rev-parse -q --verify 'refs/tags/$(VERSION)' >/dev/null \
	   && test "$$(git rev-parse 'refs/tags/$(VERSION)^{commit}')" != "$$(git rev-parse HEAD)"; then \
	  printf 'release: %s already exists here on another commit — a tag is never moved\n' \
	    '$(VERSION)'; exit 1; fi
	@git rev-parse -q --verify 'refs/tags/$(VERSION)' >/dev/null \
	  || git tag -a '$(VERSION)' -m 'writ $(VERSION)'
	git push origin 'refs/tags/$(VERSION)'
	@printf 'pushed %s — the release build takes it from here:\n' '$(VERSION)'
	@printf '  https://github.com/writtendev/writ/actions/workflows/release.yml\n'

# --------------------------------------------------------------------------
# Docs site
# --------------------------------------------------------------------------

CASTS_DIR := docs/static/casts
POSTERS_DIR := docs/static/posters
CAST_MAX_BYTES := 1310720
POSTER_MAX_BYTES := 327680
DEMO_BIN := .demo/bin
DEMO_RENDER_DIR := .demo/out
TAPES_DIR := docs/tapes

hugo-version: ## Print the pinned Hugo version (CI reads this)
	@printf '%s\n' '$(HUGO_VERSION)'

docs: ## Build the docs site locally (needs hugo)
	hugo --source docs --destination "$(CURDIR)/public"

docs-serve: ## Serve the docs site locally with live reload
	hugo server --source docs

render-tape:
	@test -n "$(TAPE)" || { echo 'render-tape: TAPE is required'; exit 1; }
	@command -v vhs >/dev/null || { \
	  echo 'render-tape: vhs is not installed — see https://github.com/charmbracelet/vhs'; \
	  exit 1; }
	@mkdir -p $(DEMO_BIN)
	go build -o $(DEMO_BIN)/writ ./cmd/writ
	@rm -rf $(DEMO_RENDER_DIR)
	@mkdir -p $(DEMO_RENDER_DIR)
	PATH="$(CURDIR)/$(DEMO_BIN):$$PATH" vhs $(TAPES_DIR)/$(TAPE).tape

casts: ## Render every tape under docs/tapes/ into docs/static/casts/ (needs vhs)
	@rm -rf $(CASTS_DIR) $(POSTERS_DIR)
	@mkdir -p $(CASTS_DIR) $(POSTERS_DIR)
	@for f in $(TAPES_DIR)/*.tape; do \
	  name=$$(basename "$$f" .tape); \
	  test "$$name" = "house" && continue; \
	  $(MAKE) render-tape TAPE="$$name" || exit 1; \
	  if [ -e $(DEMO_RENDER_DIR)/$$name.png ]; then \
	    size=$$(wc -c < $(DEMO_RENDER_DIR)/$$name.png | tr -d ' '); \
	    test "$$size" -le $(POSTER_MAX_BYTES) || { \
	      printf 'casts: %s.png came back %s bytes, over the %s cap\n' \
	        "$$name" "$$size" '$(POSTER_MAX_BYTES)'; exit 1; }; \
	    mv $(DEMO_RENDER_DIR)/$$name.png $(POSTERS_DIR)/$$name.png; \
	  fi; \
	  for ext in mp4 webm gif; do \
	    out=$(DEMO_RENDER_DIR)/$$name.$$ext; \
	    test -e "$$out" || continue; \
	    size=$$(wc -c < "$$out" | tr -d ' '); \
	    test "$$size" -le $(CAST_MAX_BYTES) || { \
	      printf 'casts: %s.%s came back %s bytes, over the %s cap\n' \
	        "$$name" "$$ext" "$$size" '$(CAST_MAX_BYTES)'; exit 1; }; \
	    cp "$$out" $(CASTS_DIR)/$$name.$$ext; \
	  done; \
	  printf 'rendered %s\n' "$$name"; \
	done
