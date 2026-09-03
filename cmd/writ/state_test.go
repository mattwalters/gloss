package main

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
)

func TestState_CLI_Commands(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ state list (defaults seeded)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"state", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("state list failed with %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Backlog") || !strings.Contains(out, "Todo") || !strings.Contains(out, "In Progress") || !strings.Contains(out, "Done") || !strings.Contains(out, "Canceled") {
		t.Fatalf("state list missing expected default states: %s", out)
	}

	// 2. writ state list --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"state", "list", "-C", env.repoDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("state list --json failed with %d; stderr: %s", code, stderr.String())
	}
	var envMap wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envMap); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if envMap.Kind != wire.KindStateList {
		t.Fatalf("got kind %s, want %s", envMap.Kind, wire.KindStateList)
	}

	// 3. writ state create
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"state", "create", "-C", env.repoDir,
		"-name", "In Review",
		"-type", "started",
		"-position", "f",
		"-color", "#f2c94c",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("state create failed with %d; stderr: %s", code, stderr.String())
	}

	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(started\) In Review$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected state create output: %q", createOut)
	}
	stateID := matches[1]

	// 4. writ state update
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"state", "update", "-C", env.repoDir,
		stateID,
		"-name", "Peer Review",
		"-color", "#e2b93c",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("state update failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("unexpected state update output: %q", stdout.String())
	}

	// Verify state list reflects updated name
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"state", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("state list failed: %d", code)
	}
	if !strings.Contains(stdout.String(), "Peer Review") {
		t.Fatalf("expected Peer Review in list: %s", stdout.String())
	}

	// 5. Create issue referencing the new state by name
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Review my code",
		"-state", "Peer Review",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(Peer Review)") {
		t.Fatalf("expected issue to show Peer Review: %s", stdout.String())
	}

	issueRe := regexp.MustCompile(`^([0-9a-f]{32})`)
	issMatches := issueRe.FindStringSubmatch(strings.TrimSpace(stdout.String()))
	if len(issMatches) < 2 {
		t.Fatalf("could not extract issue ID: %s", stdout.String())
	}
	issueID := issMatches[1]

	// 6. Transition issue state via writ issue status <id> <name>
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir,
		issueID, "Done",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status transition failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Done") {
		t.Fatalf("expected output to contain Done: %s", stdout.String())
	}

	// 7. View issue status
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir,
		issueID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status view failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "State:       Done") {
		t.Fatalf("expected issue status view to show Done: %s", stdout.String())
	}
}
