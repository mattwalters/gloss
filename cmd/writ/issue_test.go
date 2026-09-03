package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
)

func TestIssue_RoundTrip_SingleRepo(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ issue create
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Bug: Crash on startup",
		"-description", "Crashes on nil pointer dereference",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed with %d; stderr: %s", code, stderr.String())
	}

	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Bug: Crash on startup$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected issue create output: %q", createOut)
	}
	issueID := matches[1]

	// 2. writ issue list
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list failed with %d; stderr: %s", code, stderr.String())
	}
	listOut := stdout.String()
	shortID := issueID[:8]
	if !strings.Contains(listOut, shortID) {
		t.Errorf("list output missing short ID %s: %s", shortID, listOut)
	}
	if !strings.Contains(listOut, "open") {
		t.Errorf("list output missing status 'open': %s", listOut)
	}
	if !strings.Contains(listOut, "Bug: Crash on startup") {
		t.Errorf("list output missing title: %s", listOut)
	}
	if !strings.Contains(listOut, "-") {
		t.Errorf("list output missing assignees '-': %s", listOut)
	}

	// 3. writ issue status <prefix> (prefix resolution)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, shortID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status with short ID failed with %d; stderr: %s", code, stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "Issue:       "+issueID) {
		t.Errorf("status output missing full issue ID: %s", statusOut)
	}
	if !strings.Contains(statusOut, "Title:       Bug: Crash on startup") {
		t.Errorf("status output missing Title: %s", statusOut)
	}
	if !strings.Contains(statusOut, "State:       open") {
		t.Errorf("status output missing State: open: %s", statusOut)
	}
	if !strings.Contains(statusOut, "Alice") {
		t.Errorf("status output missing Author Alice: %s", statusOut)
	}
	if !strings.Contains(statusOut, "Assignees:   -") {
		t.Errorf("status output missing Assignees: -: %s", statusOut)
	}

	// 4. writ issue assign <id> -add email:alice@example.com
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "assign", "-C", env.repoDir,
		issueID, "-add", "email:alice@example.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue assign failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify status shows assignee
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status after assign failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Assignees:   email:alice@example.com") {
		t.Errorf("status output missing Assignees: email:alice@example.com: %s", stdout.String())
	}

	// 5. writ issue assign -remove email:alice@example.com -add user:bob
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "assign", "-C", env.repoDir,
		issueID, "-remove", "email:alice@example.com", "-add", "user:bob",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue reassign failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status after reassign failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Assignees:   user:bob") {
		t.Errorf("status output missing Assignees: user:bob: %s", stdout.String())
	}

	// 6. writ issue status <id> closed -reason not_planned
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir,
		issueID, "closed", "-reason", "not_planned",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status transition to closed failed with %d; stderr: %s", code, stderr.String())
	}

	// 7. Verify status reflects closed and reason
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status after close failed: %s", stderr.String())
	}
	statusClosedOut := stdout.String()
	if !strings.Contains(statusClosedOut, "State:       closed") {
		t.Errorf("status missing State: closed: %s", statusClosedOut)
	}
	if !strings.Contains(statusClosedOut, "Reason:      not_planned") {
		t.Errorf("status missing Reason: not_planned: %s", statusClosedOut)
	}

	// 8. Verify issue list -state closed returns it, and -state open does not
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "closed"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list -state closed failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), shortID) {
		t.Errorf("issue list -state closed missing issue %s: %s", shortID, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "open"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list -state open failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), shortID) {
		t.Errorf("issue list -state open should not contain closed issue %s: %s", shortID, stdout.String())
	}
}

func TestIssue_Create_OptionalFlags(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}

	const ref1 = "11111111111111111111111111111111"
	const ref2 = "22222222222222222222222222222222"

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Pre-closed issue with links",
		"-state", "closed",
		"-fixes", ref1,
		"-relates", ref2,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create with optional flags failed with %d; stderr: %s", code, stderr.String())
	}

	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(closed\) Pre-closed issue with links$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected issue create output: %q", createOut)
	}
	issueID := matches[1]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "State:       closed") {
		t.Errorf("status missing State: closed: %s", statusOut)
	}
	if !strings.Contains(statusOut, "fixes "+ref1) {
		t.Errorf("status missing fixes link: %s", statusOut)
	}
	if !strings.Contains(statusOut, "relates "+ref2) {
		t.Errorf("status missing relates link: %s", statusOut)
	}
}

func TestIssue_WorkspaceRouting_DoD1(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	wsDir := filepath.Join(tempDir, "workspace")
	if err := os.Mkdir(wsDir, 0755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = wsDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init ws failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, wsDir)
	commitFile(t, wsDir, "README.md", "# Workspace", "initial ws commit")

	codeDir := filepath.Join(tempDir, "code")
	if err := os.Mkdir(codeDir, 0755); err != nil {
		t.Fatalf("mkdir code: %v", err)
	}
	cmd = exec.Command("git", "init")
	cmd.Dir = codeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init code failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, codeDir)
	commitFile(t, codeDir, "main.go", "package main", "initial code commit")

	setGitConfig(t, codeDir, "writ.workspace", wsDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", wsDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init ws failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"init", "-C", codeDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init code failed: %s", stderr.String())
	}

	// Create issue targeting codeDir
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", codeDir,
		"-title", "Routed Issue",
		"-description", "Should land in workspace repo",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create in codeDir failed with %d; stderr: %s", code, stderr.String())
	}

	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Routed Issue$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected issue create output: %q", createOut)
	}
	issueID := matches[1]

	// 1. Assert issue is visible from workspace repo's own store
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", wsDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list in wsDir failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), issueID[:8]) {
		t.Errorf("workspace repo issue list missing issue %s: %s", issueID[:8], stdout.String())
	}

	// 2. Assert issue is also visible when querying codeDir (through workspace routing)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", codeDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list in codeDir failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), issueID[:8]) {
		t.Errorf("codeDir issue list missing issue %s: %s", issueID[:8], stdout.String())
	}

	// 3. Assert no issue op landed in codeDir's refs/writ/*
	cmd = exec.Command("git", "for-each-ref", "refs/writ/ops/")
	cmd.Dir = codeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("for-each-ref in codeDir failed: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(line, "refs/writ/ops/issue/") {
			t.Errorf("unexpected issue op ref found in codeDir: %s", line)
		}
	}
}

func TestIssue_CrossRepoReference_DoD2(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	wsDir := filepath.Join(tempDir, "workspace")
	if err := os.Mkdir(wsDir, 0755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = wsDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init ws failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, wsDir)
	commitFile(t, wsDir, "README.md", "# Workspace", "initial ws commit")

	codeDir := filepath.Join(tempDir, "code")
	if err := os.Mkdir(codeDir, 0755); err != nil {
		t.Fatalf("mkdir code: %v", err)
	}
	cmd = exec.Command("git", "init")
	cmd.Dir = codeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init code failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, codeDir)
	commitFile(t, codeDir, "main.go", "package main", "initial code commit")

	setGitConfig(t, codeDir, "writ.workspace", wsDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", wsDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init ws failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"init", "-C", codeDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init code failed: %s", stderr.String())
	}

	// Create issue from codeDir
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", codeDir,
		"-title", "Issue with cross-repo link",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Issue with cross-repo link$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected create output: %q", createOut)
	}
	issueID := matches[1]

	// Open review in codeDir
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", codeDir,
		"-title", "Fix review in code repo",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed: %s", stderr.String())
	}
	revOut := strings.TrimSpace(stdout.String())
	revRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Fix review in code repo$`)
	revMatches := revRe.FindStringSubmatch(revOut)
	if len(revMatches) < 2 {
		t.Fatalf("unexpected review open output: %q", revOut)
	}
	reviewID := revMatches[1]

	// Get code repo's repo-id
	codeRepoIDBytes, err := identity.LoadRepoID(context.Background(), codeDir)
	if err != nil {
		t.Fatalf("load code repo id: %v", err)
	}
	codeRepoID := string(codeRepoIDBytes)

	// Case 1: Cross-repo link with explicit <repo-id>#<review-id>
	explicitTarget := codeRepoID + "#" + reviewID
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", codeDir,
		issueID, "-relation", "fixes", "-target", explicitTarget,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link explicit target failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", codeDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	statusOut := stdout.String()
	// Should show cross-repo with slug "code"
	if !strings.Contains(statusOut, "fixes "+explicitTarget+" (cross-repo code)") {
		t.Errorf("status output missing cross-repo slug: %s", statusOut)
	}

	// Case 2: Link with bare review ID -> engine auto-qualification
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", codeDir,
		issueID, "-relation", "relates", "-target", reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link bare target failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify stored target came back fully qualified
	store, err := writ.Open(codeDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	issRes, err := store.Query.Issue(issueID)
	_ = store.Close()
	if err != nil {
		t.Fatalf("query issue: %v", err)
	}
	var foundBareLink bool
	for _, l := range issRes.Issue.Links {
		if l.Relation == "relates" {
			foundBareLink = true
			if l.Target != explicitTarget {
				t.Errorf("bare link target = %q, want auto-qualified %q", l.Target, explicitTarget)
			}
		}
	}
	if !foundBareLink {
		t.Errorf("relates link not found in issue state")
	}

	// Case 3: Link unregistered repo-id -> displayed as unresolved and preserved verbatim
	unregisteredRef := "0123456789abcdef0123456789abcdef#fedcba9876543210fedcba9876543210"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", codeDir,
		issueID, "-relation", "fixes", "-target", unregisteredRef,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link unregistered ref failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", codeDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status query failed: %s", stderr.String())
	}
	unresStatusOut := stdout.String()
	if !strings.Contains(unresStatusOut, "fixes "+unregisteredRef+" (unresolved)") {
		t.Errorf("status missing unresolved link verbatim: %s", unresStatusOut)
	}
}

func TestIssue_ResolveIssueID_WorkspaceGlobal(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	wsDir := filepath.Join(tempDir, "workspace")
	if err := os.Mkdir(wsDir, 0755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = wsDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init ws failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, wsDir)
	commitFile(t, wsDir, "README.md", "# Workspace", "initial ws commit")

	codeDir := filepath.Join(tempDir, "code")
	if err := os.Mkdir(codeDir, 0755); err != nil {
		t.Fatalf("mkdir code: %v", err)
	}
	cmd = exec.Command("git", "init")
	cmd.Dir = codeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init code failed: %v (%s)", err, string(out))
	}
	setupSigningKey(t, codeDir)
	commitFile(t, codeDir, "main.go", "package main", "initial code commit")

	setGitConfig(t, codeDir, "writ.workspace", wsDir)

	_ = run(context.Background(), []string{"init", "-C", wsDir}, &bytes.Buffer{}, &bytes.Buffer{})
	_ = run(context.Background(), []string{"init", "-C", codeDir}, &bytes.Buffer{}, &bytes.Buffer{})

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"issue", "create", "-C", codeDir,
		"-title", "Global ID Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	issueID := strings.Split(createOut, " ")[0]

	wsRepoIDBytes, _ := identity.LoadRepoID(context.Background(), wsDir)
	wsRepoID := string(wsRepoIDBytes)
	codeRepoIDBytes, _ := identity.LoadRepoID(context.Background(), codeDir)
	codeRepoID := string(codeRepoIDBytes)

	// 1. Resolve with workspace repo ID: <ws-repo-id>#<issue-id> -> succeeds
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", codeDir,
		wsRepoID + "#" + issueID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status with global ws repo id failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Issue:       "+issueID) {
		t.Errorf("status output missing issue ID: %s", stdout.String())
	}

	// 2. Resolve with cross-repo ID: <code-repo-id>#<review-id> -> errors clearly "issue lives in repo <slug>"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", codeDir,
		codeRepoID + "#" + issueID,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for cross-repo issue reference, got %d", code)
	}
	if !strings.Contains(stderr.String(), "issue lives in repo code") {
		t.Errorf("stderr missing 'issue lives in repo code': %s", stderr.String())
	}

	// 3. Resolve with unknown repo ID -> not found error
	const unknownRepoRef = "00000000000000000000000000000000#12345678"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", codeDir,
		unknownRepoRef,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unknown repo reference, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no issue with id "+unknownRepoRef) {
		t.Errorf("stderr missing not found error for unknown repo ref: %s", stderr.String())
	}
}

func TestIssue_ErrorSurfaces(t *testing.T) {
	t.Run("unconfigured_identity", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "Test"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected exit code 1 for unconfigured identity, got %d", code)
		}
		if !strings.Contains(stderr.String(), "run 'writ init'") {
			t.Errorf("stderr does not advise 'run 'writ init'': %s", stderr.String())
		}
	})

	t.Run("unknown_issue_id", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "status", "-C", env.repoDir, "00000000000000000000000000000000"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected exit code 1 for unknown issue ID, got %d", code)
		}
		if !strings.Contains(stderr.String(), "writ: no issue with id 00000000000000000000000000000000") {
			t.Errorf("unexpected stderr: %s", stderr.String())
		}
	})

	t.Run("ambiguous_prefix", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		_ = run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "Issue 1"}, &bytes.Buffer{}, &bytes.Buffer{})
		_ = run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "Issue 2"}, &bytes.Buffer{}, &bytes.Buffer{})

		store, err := writ.Open(env.repoDir)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		issues, _ := store.Query.Issues(writ.IssueFilter{})
		_ = store.Close()

		if len(issues) >= 2 {
			id1 := issues[0].ObjectID
			id2 := issues[1].ObjectID
			var commonPrefix string
			for i := 0; i < len(id1) && i < len(id2); i++ {
				if id1[i] == id2[i] {
					commonPrefix += string(id1[i])
				} else {
					break
				}
			}
			if commonPrefix != "" {
				var stdout, stderr bytes.Buffer
				code := run(context.Background(), []string{"issue", "status", "-C", env.repoDir, commonPrefix}, &stdout, &stderr)
				if code != 1 {
					t.Fatalf("expected exit code 1 for ambiguous prefix, got %d", code)
				}
				if !strings.Contains(stderr.String(), "ambiguous issue ID prefix") {
					t.Errorf("stderr does not mention ambiguous prefix: %s", stderr.String())
				}
			}
		}
	})

	t.Run("missing_create_title", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "create", "-C", env.repoDir}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing -title, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-title is required") {
			t.Errorf("stderr missing '-title is required': %s", stderr.String())
		}
	})

	t.Run("bad_create_state", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "T", "-state", "invalid"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for invalid state, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid state") {
			t.Errorf("stderr missing invalid state message: %s", stderr.String())
		}
	})

	t.Run("bad_status_enum", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "status", "-C", env.repoDir, "12345678", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad status enum, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid status") {
			t.Errorf("stderr missing invalid status message: %s", stderr.String())
		}
	})

	t.Run("status_reason_without_state", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "status", "-C", env.repoDir, "12345678", "-reason", "why"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for -reason in view mode, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-reason is only valid when setting status") {
			t.Errorf("stderr missing -reason is only valid message: %s", stderr.String())
		}
	})

	t.Run("missing_assign_flags", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "assign", "-C", env.repoDir, "12345678"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for assign with no flags, got %d", code)
		}
		if !strings.Contains(stderr.String(), "at least one -add or -remove is required") {
			t.Errorf("stderr missing at least one -add or -remove message: %s", stderr.String())
		}
	})

	t.Run("missing_link_target", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "link", "-C", env.repoDir, "12345678", "-relation", "fixes"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing link target, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-target is required") {
			t.Errorf("stderr missing -target is required message: %s", stderr.String())
		}
	})

	t.Run("missing_link_relation", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "link", "-C", env.repoDir, "12345678", "-target", "12345678"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing link relation, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-relation is required") {
			t.Errorf("stderr missing -relation is required message: %s", stderr.String())
		}
	})

	t.Run("bad_link_relation", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "link", "-C", env.repoDir, "12345678", "-target", "12345678", "-relation", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad link relation, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid relation") {
			t.Errorf("stderr missing invalid relation message: %s", stderr.String())
		}
	})

	t.Run("bad_list_sort", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-sort", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad sort order, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid sort order") {
			t.Errorf("stderr missing invalid sort order message: %s", stderr.String())
		}
	})

	t.Run("negative_list_limit", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-limit", "-1"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for negative limit, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-limit must be non-negative") {
			t.Errorf("stderr missing -limit must be non-negative message: %s", stderr.String())
		}
	})
}

func TestIssue_HelpAndUsage(t *testing.T) {
	for _, subcmd := range []string{"create", "status", "assign", "list", "link", "label"} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"issue", subcmd, "-h"}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("issue %s -h returned %d, want 0", subcmd, code)
		}
		if !strings.Contains(stderr.String(), "Usage: writ issue "+subcmd) && !strings.Contains(stdout.String(), "Usage: writ issue "+subcmd) {
			t.Errorf("issue %s -h missing usage in output", subcmd)
		}
	}
}

func TestIssue_Label(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. Create an issue
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Issue for label tests",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	issueID := strings.Split(createOut, " ")[0]
	shortID := issueID[:8]

	// 2. View labels on an issue with no labels (human and --json)
	t.Run("view_empty", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label view empty failed with %d; stderr: %s", code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "" {
			t.Errorf("expected empty stdout for issue with no labels, got: %q", stdout.String())
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label --json view empty failed with %d; stderr: %s", code, stderr.String())
		}
		var envJSON struct {
			SchemaVersion int `json:"schema_version"`
			Kind          string `json:"kind"`
			Data          struct {
				ObjectID string   `json:"object_id"`
				Labels   []string `json:"labels"`
			} `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envJSON); err != nil {
			t.Fatalf("unmarshal json output: %v; raw: %s", err, stdout.String())
		}
		if envJSON.Kind != "issue.label" {
			t.Errorf("expected kind 'issue.label', got %q", envJSON.Kind)
		}
		if envJSON.Data.ObjectID != issueID {
			t.Errorf("expected object_id %q, got %q", issueID, envJSON.Data.ObjectID)
		}
		if len(envJSON.Data.Labels) != 0 {
			t.Errorf("expected empty labels array, got: %v", envJSON.Data.Labels)
		}
	})

	// 3. Add multiple labels using repeated -add
	t.Run("add_labels", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{
			"issue", "label", "-C", env.repoDir, issueID,
			"-add", "frontend", "-add", "bug",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label add failed: %s", stderr.String())
		}
		if !strings.Contains(stdout.String(), "updated labels") {
			t.Errorf("expected stdout to mention updated labels, got: %s", stdout.String())
		}
	})

	// 4. View labels verifying presence and sorted order (human and --json)
	t.Run("view_sorted_labels", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label view failed: %s", stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 2 || lines[0] != "bug" || lines[1] != "frontend" {
			t.Errorf("expected labels [bug, frontend], got: %v", lines)
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label view --json failed: %s", stderr.String())
		}
		var envJSON struct {
			Kind string `json:"kind"`
			Data struct {
				ObjectID string   `json:"object_id"`
				Labels   []string `json:"labels"`
			} `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envJSON); err != nil {
			t.Fatalf("unmarshal json output: %v", err)
		}
		if len(envJSON.Data.Labels) != 2 || envJSON.Data.Labels[0] != "bug" || envJSON.Data.Labels[1] != "frontend" {
			t.Errorf("expected json labels [bug, frontend], got: %v", envJSON.Data.Labels)
		}
	})

	// 5. Prefix resolution with short issue ID
	t.Run("prefix_resolution", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{"issue", "label", "-C", env.repoDir, shortID}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label with short ID failed: %s", stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 2 || lines[0] != "bug" || lines[1] != "frontend" {
			t.Errorf("expected labels [bug, frontend], got: %v", lines)
		}
	})

	// 6. Remove and add labels concurrently
	t.Run("concurrent_add_and_remove", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{
			"issue", "label", "-C", env.repoDir, issueID,
			"-remove", "bug", "-add", "documentation",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label update failed: %s", stderr.String())
		}
		if !strings.Contains(stdout.String(), "updated labels") {
			t.Errorf("expected stdout to mention updated labels, got: %s", stdout.String())
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label view after update failed: %s", stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 2 || lines[0] != "documentation" || lines[1] != "frontend" {
			t.Errorf("expected labels [documentation, frontend], got: %v", lines)
		}
	})

	// 7. Mutation with --json flag
	t.Run("mutation_with_json", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{
			"issue", "label", "-C", env.repoDir, issueID,
			"-add", "api", "--json",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue label mutation --json failed: %s", stderr.String())
		}
		var envJSON struct {
			Kind string `json:"kind"`
			Data struct {
				ObjectID string   `json:"object_id"`
				Labels   []string `json:"labels"`
			} `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envJSON); err != nil {
			t.Fatalf("unmarshal json: %v", err)
		}
		if len(envJSON.Data.Labels) != 3 || envJSON.Data.Labels[0] != "api" || envJSON.Data.Labels[1] != "documentation" || envJSON.Data.Labels[2] != "frontend" {
			t.Errorf("expected labels [api, documentation, frontend], got: %v", envJSON.Data.Labels)
		}
	})

	// 8. Validation and error cases
	t.Run("validation_errors", func(t *testing.T) {
		// Missing issue ID
		stdout.Reset()
		stderr.Reset()
		code := run(context.Background(), []string{"issue", "label", "-C", env.repoDir}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("expected exit code 2 for missing issue ID, got %d", code)
		}
		if !strings.Contains(stderr.String(), "issue ID is required") {
			t.Errorf("expected 'issue ID is required' in stderr, got: %s", stderr.String())
		}

		// Unexpected arguments
		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, issueID, "unexpected"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("expected exit code 2 for unexpected arguments, got %d", code)
		}
		if !strings.Contains(stderr.String(), "unexpected arguments") {
			t.Errorf("expected 'unexpected arguments' in stderr, got: %s", stderr.String())
		}

		// Nonexistent issue ID
		stdout.Reset()
		stderr.Reset()
		nonexistentID := "00000000000000000000000000000000"
		code = run(context.Background(), []string{"issue", "label", "-C", env.repoDir, nonexistentID}, &stdout, &stderr)
		if code == 0 {
			t.Errorf("expected nonzero exit code for nonexistent issue ID, got %d", code)
		}
	})
}
