APIDIFF := go run golang.org/x/exp/cmd/apidiff@v0.0.0-20250128182459-e0ece0dbea4c

.PHONY: build test api api-check docs docs-check

build:
	go build ./...

test:
	go test ./...

api:
	@mkdir -p api
	$(APIDIFF) -w api/engine.api github.com/writtendev/writ/engine

api-check:
	@set -e; \
	out=$$($(APIDIFF) -incompatible api/engine.api github.com/writtendev/writ/engine); \
	if [ -n "$$out" ]; then \
		echo "Incompatible API changes detected in github.com/writtendev/writ/engine:"; \
		echo "$$out"; \
		exit 1; \
	fi

docs:
	go test ./cmd/writ -run TestDocsGolden -update-docs

docs-check:
	go test ./cmd/writ -run TestDocsGolden

