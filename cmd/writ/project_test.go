package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
)

func TestProject_CLI_Commands(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ project create
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "create", "-C", env.repoDir,
		"-t", "Authentication Redesign",
		"-description", "Redesign auth flow to support SAML and OIDC",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project create failed with %d; stderr: %s", code, stderr.String())
	}
	projectID := strings.TrimSpace(stdout.String())
	if projectID == "" {
		t.Fatalf("project create returned empty ID")
	}

	// 2. writ project list
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"project", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project list failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Authentication Redesign") {
		t.Fatalf("project list missing created project: %s", stdout.String())
	}

	// 3. writ project list --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"project", "list", "-C", env.repoDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project list --json failed with %d; stderr: %s", code, stderr.String())
	}
	var listEnv wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &listEnv); err != nil {
		t.Fatalf("unmarshal list json: %v", err)
	}
	if listEnv.Kind != wire.KindProjectList {
		t.Fatalf("expected kind %s, got %s", wire.KindProjectList, listEnv.Kind)
	}

	// 4. writ project update
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "update", "-C", env.repoDir, projectID,
		"-t", "Authentication & SSO Redesign",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project update failed with %d; stderr: %s", code, stderr.String())
	}

	// 5. writ project status
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "status", "-C", env.repoDir, projectID, "active",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project status failed with %d; stderr: %s", code, stderr.String())
	}

	// 5b. writ project status: invalid status is refused
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "status", "-C", env.repoDir, projectID, "bogus",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("project status with invalid status unexpectedly succeeded")
	}

	// 6. writ project add
	issueRef1 := "0123456789abcdef0123456789abcdef"
	issueRef2 := "abcdef0123456789abcdef0123456789"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "add", "-C", env.repoDir, projectID, issueRef1, issueRef2,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project add failed with %d; stderr: %s", code, stderr.String())
	}

	// 7. writ project show
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"project", "show", "-C", env.repoDir, projectID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project show failed with %d; stderr: %s", code, stderr.String())
	}
	showOut := stdout.String()
	if !strings.Contains(showOut, "Authentication & SSO Redesign") {
		t.Fatalf("project show missing updated title: %s", showOut)
	}
	if !strings.Contains(showOut, "active") {
		t.Fatalf("project show missing status: %s", showOut)
	}
	if !strings.Contains(showOut, issueRef1) || !strings.Contains(showOut, issueRef2) {
		t.Fatalf("project show missing added issues: %s", showOut)
	}

	// 8. writ project show --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"project", "show", "-C", env.repoDir, projectID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project show --json failed with %d; stderr: %s", code, stderr.String())
	}
	var showEnv wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &showEnv); err != nil {
		t.Fatalf("unmarshal show json: %v", err)
	}
	if showEnv.Kind != wire.KindProjectShow {
		t.Fatalf("expected kind %s, got %s", wire.KindProjectShow, showEnv.Kind)
	}

	// 9. writ project remove
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"project", "remove", "-C", env.repoDir, projectID, issueRef1,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project remove failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify removed issue is gone from project show
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"project", "show", "-C", env.repoDir, projectID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project show after remove failed with %d; stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), issueRef1) {
		t.Fatalf("removed issue still present in project show: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), issueRef2) {
		t.Fatalf("expected remaining issue in project show: %s", stdout.String())
	}
}

func TestGolden_ProjectList(t *testing.T) {
	repoDir := loadFixtureRepo(t, "project-membership-races")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"project", "list", "-C", repoDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project list --json failed with %d; stderr: %s", code, stderr.String())
	}

	compareOrUpdateGolden(t, "project_list.json", stdout.Bytes())
}

func TestGolden_ProjectShow(t *testing.T) {
	repoDir := loadFixtureRepo(t, "project-membership-races")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"project", "show", "-C", repoDir, "p-races", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project show --json failed with %d; stderr: %s", code, stderr.String())
	}

	compareOrUpdateGolden(t, "project_show.json", stdout.Bytes())
}
