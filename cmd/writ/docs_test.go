package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "update docs/cli.md")

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		// Fallback to relative path from cmd/writ
		return filepath.Clean(filepath.Join(".", "..", ".."))
	}
	return strings.TrimSpace(string(out))
}

func TestDocsGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDocs(&buf); err != nil {
		t.Fatalf("renderDocs: %v", err)
	}

	repoRoot := findRepoRoot(t)
	docsPath := filepath.Join(repoRoot, "docs", "cli.md")

	if *updateDocs {
		if err := os.WriteFile(docsPath, buf.Bytes(), 0644); err != nil {
			t.Fatalf("writing %s: %v", docsPath, err)
		}
		t.Logf("Updated %s", docsPath)
		return
	}

	existing, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run 'make cli-docs' to generate)", docsPath, err)
	}

	if !bytes.Equal(existing, buf.Bytes()) {
		t.Fatalf("docs/cli.md does not match generated docs from command table.\nRun 'make cli-docs' or 'go generate ./cmd/writ' to regenerate.")
	}
}
