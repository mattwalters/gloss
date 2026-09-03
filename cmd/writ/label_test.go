package main

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
)

func TestLabel_CLI_Commands(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ label list (initially empty)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label list failed with %d; stderr: %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("expected empty label list, got: %s", stdout.String())
	}

	// 2. writ label list --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "list", "-C", env.repoDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label list --json failed with %d; stderr: %s", code, stderr.String())
	}
	var envMap wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envMap); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if envMap.Kind != wire.KindLabelList {
		t.Fatalf("got kind %s, want %s", envMap.Kind, wire.KindLabelList)
	}

	// 3. writ label create
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"label", "create", "-C", env.repoDir,
		"-name", "bug",
		"-color", "#d73a4a",
		"-description", "Something isn't working",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label create failed with %d; stderr: %s", code, stderr.String())
	}

	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) bug$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected label create output: %q", createOut)
	}
	labelID := matches[1]

	// 4. writ label list has the label
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label list failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "bug") || !strings.Contains(stdout.String(), "#d73a4a") {
		t.Fatalf("label list output missing created label: %s", stdout.String())
	}

	// 5. writ label edit
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"label", "edit", "-C", env.repoDir,
		labelID,
		"-name", "defect",
		"-color", "#e2b93c",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label edit failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify edit by listing
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "defect") || !strings.Contains(stdout.String(), "#e2b93c") {
		t.Fatalf("expected updated defect label, got: %s", stdout.String())
	}

	// 6. Attach label to issue using human name
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "Bug 1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	issueID := strings.Fields(stdout.String())[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, "--add", "defect", issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue label --add defect failed: %s", stderr.String())
	}

	// Verify issue status renders "defect"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Labels:      defect") {
		t.Errorf("expected Labels: defect in issue status, got: %s", stdout.String())
	}
}

func TestLabel_Migrate(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	store, err := openStore(env.repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}

	// Create issue directly with legacy bare string label "legacy-tag"
	issID, err := store.Issues.Create(context.Background(), writ.NewIssue{Title: "Legacy Issue"})
	if err != nil {
		t.Fatalf("store.Issues.Create failed: %v", err)
	}
	if err := store.Issues.Label(context.Background(), issID, []string{"legacy-tag"}, nil); err != nil {
		t.Fatalf("store.Issues.Label legacy failed: %v", err)
	}
	_ = store.Close()

	// Verify issue has legacy-tag before migrate
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "legacy-tag") {
		t.Fatalf("expected legacy-tag before migrate, got: %s", stdout.String())
	}

	// Run writ label migrate
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "migrate", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label migrate failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Migrated 1 legacy label(s)") {
		t.Fatalf("unexpected migrate output: %s", stdout.String())
	}

	// Verify label was created
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("label list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "legacy-tag") {
		t.Fatalf("expected legacy-tag in label list, got: %s", stdout.String())
	}

	// Verify second migrate is idempotent (0 migrated)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"label", "migrate", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second label migrate failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Migrated 0 legacy label(s)") {
		t.Fatalf("expected 0 migrated on second run, got: %s", stdout.String())
	}
}
