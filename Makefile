PKG := github.com/writtendev/writ
APIDIFF := go run golang.org/x/exp/cmd/apidiff@v0.0.0-20250128182459-e0ece0dbea4c

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

.PHONY: build test install api api-check docs docs-check snapshot release

build:
	go build ./...

test:
	go test ./...

install: ## Build and install writ into Go's bin dir
	go install -ldflags "$(LDFLAGS)" ./cmd/writ
	@printf 'installed %s to %s\n' '$(VERSION)' '$(GOBIN)/writ'
	@printf 'PATH resolves writ to: %s\n' \
	  "$$(command -v writ || echo '(nothing — is $(GOBIN) on your PATH?)')"

api:
	@mkdir -p api
	$(APIDIFF) -w api/engine.api $(PKG)/engine

api-check:
	@set -e; \
	out=$$($(APIDIFF) -incompatible api/engine.api $(PKG)/engine); \
	if [ -n "$$out" ]; then \
		echo "Incompatible API changes detected in $(PKG)/engine:"; \
		echo "$$out"; \
		exit 1; \
	fi

docs:
	go test ./cmd/writ -run TestDocsGolden -update-docs

docs-check:
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
