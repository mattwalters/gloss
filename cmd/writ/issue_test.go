package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
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
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(Todo\) Bug: Crash on startup$`)
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
	if !strings.Contains(listOut, "Todo") {
		t.Errorf("list output missing status 'Todo': %s", listOut)
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
	if !strings.Contains(statusOut, "State:       Todo") {
		t.Errorf("status output missing State: Todo: %s", statusOut)
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

	// 7. Verify status reflects closed and reason (resolved to Done workflow state)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status after close failed: %s", stderr.String())
	}
	statusClosedOut := stdout.String()
	if !strings.Contains(statusClosedOut, "State:       Done") {
		t.Errorf("status missing State: Done: %s", statusClosedOut)
	}
	if !strings.Contains(statusClosedOut, "Reason:      not_planned") {
		t.Errorf("status missing Reason: not_planned: %s", statusClosedOut)
	}

	// 8. Verify issue list -state closed and -state Done return it, and -state open / -state Todo do not
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
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "Done"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list -state Done failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), shortID) {
		t.Errorf("issue list -state Done missing issue %s: %s", shortID, stdout.String())
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

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "Todo"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list -state Todo failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), shortID) {
		t.Errorf("issue list -state Todo should not contain closed issue %s: %s", shortID, stdout.String())
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
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(Done\) Pre-closed issue with links$`)
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
	if !strings.Contains(statusOut, "State:       Done") {
		t.Errorf("status missing State: Done: %s", statusOut)
	}
	if !strings.Contains(statusOut, "fixes "+ref1) {
		t.Errorf("status missing fixes link: %s", statusOut)
	}
	if !strings.Contains(statusOut, "relates "+ref2) {
		t.Errorf("status missing relates link: %s", statusOut)
	}
}

// TestIssue_QualifiedLinkReference covers the link-target resolution
// behaviour that survives the removal of workspace routing (WRIT-181):
// qualified references (<repo-id>#<object-id>) still parse and still
// display, but resolution against a repo other than the one writ is
// standing in is no longer possible — a bare link target is never
// auto-qualified, and any reference this store cannot recognize as its
// own is displayed unresolved, verbatim, per spec/identifiers.md.
func TestIssue_QualifiedLinkReference(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)
	commitFile(t, env.repoDir, "main.go", "package main", "initial commit")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Issue with a qualified link",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(Todo\) Issue with a qualified link$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected create output: %q", createOut)
	}
	issueID := matches[1]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Fix review",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed: %s", stderr.String())
	}
	revOut := strings.TrimSpace(stdout.String())
	revRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Fix review$`)
	revMatches := revRe.FindStringSubmatch(revOut)
	if len(revMatches) < 2 {
		t.Fatalf("unexpected review open output: %q", revOut)
	}
	reviewID := revMatches[1]

	repoIDBytes, err := identity.LoadRepoID(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("load repo id: %v", err)
	}
	repoID := string(repoIDBytes)

	// Case 1: a reference explicitly qualified with this repo's own repo-id
	// resolves locally — qualification is still correct, it just never
	// means "elsewhere" anymore.
	selfQualified := repoID + "#" + reviewID
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", env.repoDir,
		issueID, "-relation", "fixes", "-target", selfQualified,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link self-qualified target failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "fixes "+selfQualified+" (local)") {
		t.Errorf("status output missing local resolution for self-qualified link: %s", statusOut)
	}

	// Case 2: a bare link target stays bare — there is no second store left
	// to auto-qualify it against.
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", env.repoDir,
		issueID, "-relation", "relates", "-target", reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link bare target failed with %d; stderr: %s", code, stderr.String())
	}

	store, err := writ.Open(env.repoDir)
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
			if l.Target != reviewID {
				t.Errorf("bare link target = %q, want it to stay bare (%q)", l.Target, reviewID)
			}
		}
	}
	if !foundBareLink {
		t.Errorf("relates link not found in issue state")
	}

	// Case 3: a qualified reference to a repo-id this store does not
	// recognize as its own is displayed unresolved, preserved verbatim.
	foreignRef := "0123456789abcdef0123456789abcdef#fedcba9876543210fedcba9876543210"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "link", "-C", env.repoDir,
		issueID, "-relation", "fixes", "-target", foreignRef,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue link foreign ref failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status query failed: %s", stderr.String())
	}
	unresStatusOut := stdout.String()
	if !strings.Contains(unresStatusOut, "fixes "+foreignRef+" (unresolved)") {
		t.Errorf("status missing unresolved link verbatim: %s", unresStatusOut)
	}
}

// TestIssue_ResolveIssueID_Qualified covers resolving an issue ID prefix
// itself (not just a link target) when passed as a qualified reference:
// self-qualified succeeds, and a reference to any other repo-id is not
// found — there is no registry left to consult.
func TestIssue_ResolveIssueID_Qualified(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)
	commitFile(t, env.repoDir, "main.go", "package main", "initial commit")

	if code := run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("init failed")
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Qualified ID Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	issueID := strings.Split(createOut, " ")[0]

	repoIDBytes, _ := identity.LoadRepoID(context.Background(), env.repoDir)
	repoID := string(repoIDBytes)

	// 1. Resolve with this repo's own repo-id: <repo-id>#<issue-id> -> succeeds
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir,
		repoID + "#" + issueID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status with self-qualified repo id failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Issue:       "+issueID) {
		t.Errorf("status output missing issue ID: %s", stdout.String())
	}

	// 2. Resolve with an unknown repo-id -> not found error
	const unknownRepoRef = "00000000000000000000000000000000#12345678"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir,
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

	// status_reason_with_position_no_state guards against a regression where
	// `writ issue status <id> -position <p> -reason <r>` (no explicit new
	// state) passed flag validation and then silently dropped -reason: the
	// reposition-only path routes through Issues.Update, whose IssueEdit has
	// no reason field. -reason must be rejected up front instead, and that
	// must hold regardless of whether the issue already has a folded state
	// -- a user should not get different semantics from the same flags
	// depending on the issue's current state.
	t.Run("status_reason_with_position_no_state", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})
		commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"issue", "create", "-C", env.repoDir,
			"-title", "Stated Issue",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue create failed with %d; stderr: %s", code, stderr.String())
		}
		issueID := strings.Fields(stdout.String())[0]

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"issue", "status", "-C", env.repoDir, issueID,
			"-position", "V", "-reason", "pulled forward",
		}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for -reason with -position and no explicit state, got %d; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "-reason is only valid when setting status") {
			t.Errorf("stderr missing -reason is only valid message: %s", stderr.String())
		}

		// Confirm nothing was applied: neither the reason nor the position.
		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("issue status --json failed with %d; stderr: %s", code, stderr.String())
		}
		var wire1 struct {
			Data wire.Issue `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &wire1); err != nil {
			t.Fatalf("unmarshal json: %v", err)
		}
		if wire1.Data.Reason != "" {
			t.Errorf("expected reason not to be recorded, got %q", wire1.Data.Reason)
		}
		if wire1.Data.Position == "V" {
			t.Errorf("expected position not to be applied when -reason is rejected, got %q", wire1.Data.Position)
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

	// Create label objects used in subsequent subtests
	for _, lbl := range []string{"frontend", "bug", "documentation", "api"} {
		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{"label", "create", "-C", env.repoDir, "-name", lbl}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("label create %s failed: %s", lbl, stderr.String())
		}
	}

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

func TestIssueComment_Workflow(t *testing.T) {
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
		"-title", "Investigate timeout on worker shutdown",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	issueID := strings.Split(createOut, " ")[0]
	if len(issueID) != 32 {
		t.Fatalf("unexpected issue ID: %q", issueID)
	}

	// Verify status initially has no Comments section
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), "Comments:") {
		t.Errorf("status should not contain Comments: when empty, got: %s", stdout.String())
	}

	// Verify status --json initially has "comments":[]
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed: %s", stderr.String())
	}
	var envJSON wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envJSON); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	issueJSON, ok := envJSON.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data map in json envelope")
	}
	commentsField, hasComments := issueJSON["comments"]
	if !hasComments {
		t.Fatalf("expected 'comments' field in issue json")
	}
	commentsArr, ok := commentsField.([]any)
	if !ok || len(commentsArr) != 0 {
		t.Fatalf("expected empty comments array in issue json, got: %v", commentsField)
	}

	// 2. Post first root comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-m", "Worker processes are hanging during SIGTERM",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment failed: %s", stderr.String())
	}
	commentID1 := strings.TrimSpace(stdout.String())
	if len(commentID1) != 32 {
		t.Fatalf("unexpected comment ID format: %q", commentID1)
	}

	// 3. Post reply using prefix matching for issue ID and comment ID
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID[:8],
		"-m", "Represents a leak in the channel drain loop",
		"-reply-to", commentID1[:8],
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment reply failed: %s", stderr.String())
	}
	replyID1 := strings.TrimSpace(stdout.String())
	if len(replyID1) != 32 {
		t.Fatalf("unexpected reply comment ID format: %q", replyID1)
	}

	// 4. Post second root comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-m", "Added reproduction test case in repro_test.go",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second issue comment failed: %s", stderr.String())
	}
	commentID2 := strings.TrimSpace(stdout.String())

	// 5. Verify human status rendering with hierarchy
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	statusStr := stdout.String()
	if !strings.Contains(statusStr, "Comments:\n") {
		t.Errorf("expected 'Comments:' header in status output: %s", statusStr)
	}
	expectedRoot1 := fmt.Sprintf("  [%s] Worker processes are hanging during SIGTERM", commentID1)
	expectedReply1 := fmt.Sprintf("    [%s] Represents a leak in the channel drain loop", replyID1)
	expectedRoot2 := fmt.Sprintf("  [%s] Added reproduction test case in repro_test.go", commentID2)
	if !strings.Contains(statusStr, expectedRoot1) {
		t.Errorf("missing root comment 1 in status: %s", statusStr)
	}
	if !strings.Contains(statusStr, expectedReply1) {
		t.Errorf("missing reply 1 in status: %s", statusStr)
	}
	if !strings.Contains(statusStr, expectedRoot2) {
		t.Errorf("missing root comment 2 in status: %s", statusStr)
	}

	// 6. Verify status --json contains full threaded tree
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed: %s", stderr.String())
	}
	var statusEnv struct {
		Kind string     `json:"kind"`
		Data wire.Issue `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statusEnv); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	var thread1, thread2 *wire.CommentThread
	for i := range statusEnv.Data.Comments {
		if statusEnv.Data.Comments[i].ObjectID == commentID1 {
			thread1 = &statusEnv.Data.Comments[i]
		} else if statusEnv.Data.Comments[i].ObjectID == commentID2 {
			thread2 = &statusEnv.Data.Comments[i]
		}
	}
	if thread1 == nil {
		t.Fatalf("thread 1 (%s) not found in comments json", commentID1)
	}
	if thread2 == nil {
		t.Fatalf("thread 2 (%s) not found in comments json", commentID2)
	}
	if len(thread1.Replies) != 1 {
		t.Fatalf("expected 1 reply under thread 1, got %d", len(thread1.Replies))
	}
	if thread1.Replies[0].ObjectID != replyID1 {
		t.Errorf("expected reply object_id %s, got %s", replyID1, thread1.Replies[0].ObjectID)
	}
	if len(thread2.Replies) != 0 {
		t.Fatalf("expected 0 replies under thread 2, got %d", len(thread2.Replies))
	}

	// 7. Resolve first thread with -resolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-reply-to", replyID1[:8],
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment -resolve failed: %s", stderr.String())
	}
	resolveOut := strings.TrimSpace(stdout.String())
	if resolveOut != fmt.Sprintf("%s (resolved)", commentID1) {
		t.Errorf("expected '%s (resolved)', got %q", commentID1, resolveOut)
	}

	// Verify store state
	store, err := writ.Open(env.repoDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	comments, err := store.Query.Comments(writ.CommentFilter{SubjectType: "issue", SubjectID: issueID})
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	for _, c := range comments {
		if c.ObjectID == commentID1 {
			if !c.Comment.IsResolved() {
				t.Errorf("expected comment 1 to be resolved")
			}
			if c.Comment.ResolvedBy != "email:alice@example.com" {
				t.Errorf("expected resolvedBy 'email:alice@example.com', got %q", c.Comment.ResolvedBy)
			}
		}
	}

	// Verify human status displays (resolved by email:alice@example.com)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("[%s] Worker processes are hanging during SIGTERM (resolved by email:alice@example.com)", commentID1)) {
		t.Errorf("expected resolved annotation in status output: %s", stdout.String())
	}

	// 8. Post reply with -unresolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-reply-to", commentID1,
		"-m", "Reopening: still reproduces on Go 1.26",
		"-unresolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment reply with -unresolve failed: %s", stderr.String())
	}
	replyID2 := strings.TrimSpace(stdout.String())
	if len(replyID2) != 32 {
		t.Fatalf("unexpected replyID2: %q", replyID2)
	}

	// Verify thread is now unresolved
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), "(resolved") {
		t.Errorf("expected thread to be unresolved, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("[%s] Reopening: still reproduces on Go 1.26", replyID2)) {
		t.Errorf("expected reply 2 in status: %s", stdout.String())
	}

	// 9. Direct targeting using comment ID as positional argument
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, commentID1[:8],
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment direct resolve failed: %s", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != fmt.Sprintf("%s (resolved)", commentID1) {
		t.Errorf("expected '%s (resolved)', got %q", commentID1, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, commentID1[:8],
		"-unresolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment direct unresolve failed: %s", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != fmt.Sprintf("%s (unresolved)", commentID1) {
		t.Errorf("expected '%s (unresolved)', got %q", commentID1, stdout.String())
	}

	// Direct reply using comment ID as positional argument
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, commentID1,
		"-m", "Direct reply via comment ID",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment direct reply failed: %s", stderr.String())
	}
	directReplyID := strings.TrimSpace(stdout.String())
	if len(directReplyID) != 32 {
		t.Fatalf("unexpected directReplyID: %q", directReplyID)
	}
}

func TestIssueComment_ResolverIdentity(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Test Identity Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	issueID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-m", "Need resolution",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment failed: %s", stderr.String())
	}
	commentID := strings.TrimSpace(stdout.String())

	// Configure invalid personId that has no scheme (e.g. "alice"), causing PersonIDErr
	setGitConfig(t, env.repoDir, identity.PersonIDKey, "alice_without_scheme")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-reply-to", commentID,
		"-resolve",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure with invalid person identity, got 0")
	}
	if !strings.Contains(stderr.String(), "writ.personId") || !strings.Contains(stderr.String(), "missing scheme") {
		t.Errorf("expected diagnosis naming writ.personId and missing scheme in stderr, got: %s", stderr.String())
	}
}

func TestIssueComment_UsageErrors(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Usage Errors Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	issueID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	// 1. Missing issue ID
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2 for missing issue ID, got %d", code)
	}
	if !strings.Contains(stderr.String(), "issue ID is required") {
		t.Errorf("expected 'issue ID is required' in stderr, got: %s", stderr.String())
	}

	// 2. Unexpected arguments
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir, issueID, "unexpected", "-m", "hi"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2 for unexpected arguments, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Errorf("expected 'unexpected arguments' in stderr, got: %s", stderr.String())
	}

	// 3. Mutually exclusive -resolve and -unresolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir, issueID, "-resolve", "-unresolve"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2 for mutually exclusive resolve flags, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot specify both -resolve and -unresolve") {
		t.Errorf("expected 'cannot specify both -resolve and -unresolve' in stderr, got: %s", stderr.String())
	}

	// 4. Missing -m without resolve/unresolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2 for missing -m, got %d", code)
	}
	if !strings.Contains(stderr.String(), "-m is required") {
		t.Errorf("expected '-m is required' in stderr, got: %s", stderr.String())
	}

	// 5. Resolve without comment or thread target
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir, issueID, "-resolve"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("expected failure when resolving issue ID directly without comment target, got 0")
	}
	if !strings.Contains(stderr.String(), "comment or thread ID is required to resolve") {
		t.Errorf("expected 'comment or thread ID is required to resolve' in stderr, got: %s", stderr.String())
	}

	// 6. Unknown issue ID
	stdout.Reset()
	stderr.Reset()
	nonexistentID := "00000000000000000000000000000000"
	code = run(context.Background(), []string{"issue", "comment", "-C", env.repoDir, nonexistentID, "-m", "text"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("expected failure for nonexistent issue ID, got 0")
	}
}

func TestIssueComment_ScopedCommentLookup(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// Create an issue and an issue comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Scoped Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	issueID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueID,
		"-m", "Issue comment text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue comment failed: %s", stderr.String())
	}
	issueCommentID := strings.TrimSpace(stdout.String())

	// Open a review and add a review comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Scoped Review",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed: %s", stderr.String())
	}
	reviewID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-m", "Review comment text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment failed: %s", stderr.String())
	}
	reviewCommentID := strings.TrimSpace(stdout.String())

	// 1. Attempt to resolve review comment via writ issue comment: must fail
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, reviewCommentID,
		"-resolve",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure resolving review comment via issue comment, got code 0")
	}
	if !strings.Contains(stderr.String(), "no issue with id") {
		t.Errorf("expected 'no issue with id' error in stderr, got: %s", stderr.String())
	}

	// Verify review comment is still unresolved
	store, err := openStore(env.repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	rcs, err := store.Query.Comments(writ.CommentFilter{
		SubjectType: "review",
		SubjectID:   reviewID,
	})
	if err != nil {
		t.Fatalf("query review comments failed: %v", err)
	}
	if len(rcs) != 1 || rcs[0].Comment.IsResolved() {
		t.Errorf("expected review comment to remain unresolved")
	}

	// 2. Direct comment lookup with issue comment prefix must succeed even though review comments exist
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issueCommentID[:8],
		"-m", "Reply to issue comment via prefix",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected direct issue comment prefix lookup to succeed, got %d: %s", code, stderr.String())
	}
}

func TestIssueComment_ReplyToNotFound(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// Create two issues with one comment each
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Issue One",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue 1 create failed: %s", stderr.String())
	}
	issue1ID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue1ID,
		"-m", "Comment on Issue One",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue 1 comment failed: %s", stderr.String())
	}
	comment1ID := strings.TrimSpace(stdout.String())

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Issue Two",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue 2 create failed: %s", stderr.String())
	}
	issue2ID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue2ID,
		"-m", "Comment on Issue Two",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue 2 comment failed: %s", stderr.String())
	}
	comment2ID := strings.TrimSpace(stdout.String())

	// Valid reply-to on issue 1 succeeds
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue1ID,
		"-reply-to", comment1ID[:8],
		"-m", "Valid reply on issue 1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected valid reply to succeed, got %d: %s", code, stderr.String())
	}

	// 1. Cross-issue resolution: try resolving comment2ID on issue1ID
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue1ID,
		"-reply-to", comment2ID,
		"-resolve",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for cross-issue comment resolution, got code 0")
	}
	expectedErrMsg := fmt.Sprintf("comment %q not found on issue", comment2ID)
	if !strings.Contains(stderr.String(), expectedErrMsg) {
		t.Errorf("expected stderr to contain %q, got: %s", expectedErrMsg, stderr.String())
	}

	// Verify comment 2 on issue 2 was NOT resolved
	store, err := openStore(env.repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	c2s, err := store.Query.Comments(writ.CommentFilter{
		SubjectType: "issue",
		SubjectID:   issue2ID,
	})
	if err != nil {
		t.Fatalf("query issue 2 comments failed: %v", err)
	}
	if len(c2s) != 1 || c2s[0].Comment.IsResolved() {
		t.Errorf("expected comment on issue 2 to remain unresolved")
	}

	// 2. Nonexistent comment prefix in -reply-to
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue1ID,
		"-reply-to", "nonexistent123",
		"-m", "reply text",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for nonexistent -reply-to, got code 0")
	}
	expectedNonexistentMsg := `comment "nonexistent123" not found on issue`
	if !strings.Contains(stderr.String(), expectedNonexistentMsg) {
		t.Errorf("expected stderr to contain %q, got: %s", expectedNonexistentMsg, stderr.String())
	}

	// 3. Nonexistent comment prefix in -reply-to with -resolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "comment", "-C", env.repoDir, issue1ID,
		"-reply-to", "nonexistent456",
		"-resolve",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for nonexistent -reply-to -resolve, got code 0")
	}
	expectedNonexistentMsg2 := `comment "nonexistent456" not found on issue`
	if !strings.Contains(stderr.String(), expectedNonexistentMsg2) {
		t.Errorf("expected stderr to contain %q, got: %s", expectedNonexistentMsg2, stderr.String())
	}
}

func TestIssue_WorkflowStateDefaultsAndFiltering(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "init")

	// 1. Issue created without --state defaults to Todo (unstarted)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "create", "-C", env.repoDir, "-title", "Fix bug"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed: %s", stderr.String())
	}
	createOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(Todo\) Fix bug$`)
	matches := idRe.FindStringSubmatch(createOut)
	if len(matches) < 2 {
		t.Fatalf("expected (Todo) in issue create output, got %q", createOut)
	}
	issueID := matches[1]

	// 2. Issue list shows Todo, and -state Todo / -state open match it
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "Todo"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), issueID[:8]) {
		t.Fatalf("issue list -state Todo failed to return issue: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "open"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), issueID[:8]) {
		t.Fatalf("issue list -state open failed to return issue in Todo: %s", stdout.String())
	}

	// 3. writ issue status <id> closed resolves to completed workflow state (Done)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID, "closed"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status closed failed: %s", stderr.String())
	}

	// 4. View status shows Done
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "State:       Done") {
		t.Fatalf("expected State: Done, got: %s", stdout.String())
	}

	// 5. Querying -state Done and -state closed returns it; -state open / -state Todo returns 0
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "Done"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), issueID[:8]) {
		t.Fatalf("issue list -state Done failed to return closed issue: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "closed"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), issueID[:8]) {
		t.Fatalf("issue list -state closed failed to return issue in Done: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir, "-state", "open"}, &stdout, &stderr)
	if code != 0 || strings.Contains(stdout.String(), issueID[:8]) {
		t.Fatalf("issue list -state open should not return closed issue: %s", stdout.String())
	}
}

func TestIssueFieldsPriorityEstimatePosition(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. Create issue with priority, estimate, position
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Urgent Task",
		"-priority", "urgent",
		"-estimate", "3.5",
		"-position", "V",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed with %d; stderr: %s", code, stderr.String())
	}

	issue1ID := strings.Fields(stdout.String())[0]

	// 2. View status --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issue1ID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed with %d: %s", code, stderr.String())
	}

	var wire1 struct {
		Data wire.Issue `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &wire1); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if wire1.Data.Priority != 1 {
		t.Errorf("expected priority 1 (urgent), got %d", wire1.Data.Priority)
	}
	if wire1.Data.Estimate == nil || *wire1.Data.Estimate != 3.5 {
		t.Errorf("expected estimate 3.5, got %v", wire1.Data.Estimate)
	}
	if wire1.Data.Position != "V" {
		t.Errorf("expected position 'V', got %q", wire1.Data.Position)
	}

	// 3. Update issue via `writ issue update`
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "update", "-C", env.repoDir, issue1ID,
		"-priority", "high",
		"-estimate", "5.0",
		"-position", "aV",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue update failed with %d: %s", code, stderr.String())
	}

	// Verify update in status --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issue1ID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed with %d: %s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &wire1); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if wire1.Data.Priority != 2 {
		t.Errorf("expected updated priority 2 (high), got %d", wire1.Data.Priority)
	}
	if wire1.Data.Estimate == nil || *wire1.Data.Estimate != 5.0 {
		t.Errorf("expected updated estimate 5.0, got %v", wire1.Data.Estimate)
	}
	if wire1.Data.Position != "aV" {
		t.Errorf("expected updated position 'aV', got %q", wire1.Data.Position)
	}

	// 4. Update position via `writ issue status`
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir, issue1ID, "closed",
		"-position", "k",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status with position failed with %d: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issue1ID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed with %d: %s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &wire1); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if wire1.Data.State == "" {
		t.Errorf("expected non-empty state, got %s", wire1.Data.State)
	}
	if wire1.Data.Position != "k" {
		t.Errorf("expected updated position 'k', got %q", wire1.Data.Position)
	}

	// 5. Create a second issue with priority low
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Low Priority Task",
		"-priority", "low",
		"-estimate", "1.0",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed with %d: %s", code, stderr.String())
	}
	issue2ID := strings.Fields(stdout.String())[0]

	// 6. Test list filter by -priority high
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "list", "-C", env.repoDir,
		"-priority", "high",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list -priority high failed with %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), issue1ID[:8]) {
		t.Errorf("expected issue1 (%s) in high priority list: %s", issue1ID[:8], stdout.String())
	}
	if strings.Contains(stdout.String(), issue2ID[:8]) {
		t.Errorf("issue2 (%s) should not appear in high priority list: %s", issue2ID[:8], stdout.String())
	}

	// 7. Test tabular view includes priority and estimate
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue list failed with %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "high") {
		t.Errorf("expected tabular output to show 'high': %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "low") {
		t.Errorf("expected tabular output to show 'low': %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "5") {
		t.Errorf("expected tabular output to show estimate '5': %s", stdout.String())
	}

	// 8. Test invalid estimates (NaN, +Inf, -Inf, negative, non-number) fail with exit code 2
	for _, badEst := range []string{"NaN", "+Inf", "-Inf", "-1.5", "abc"} {
		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"issue", "create", "-C", env.repoDir,
			"-title", "Bad Estimate Issue",
			"-estimate", badEst,
		}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("issue create -estimate %s got code %d, want 2; stderr: %s", badEst, code, stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"issue", "update", "-C", env.repoDir, issue1ID,
			"-estimate", badEst,
		}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("issue update -estimate %s got code %d, want 2; stderr: %s", badEst, code, stderr.String())
		}
	}
}

// TestIssueStatus_RepositionOnly_StatelessIssue guards against a regression
// where `writ issue status <id> -position <p>` on an issue with no
// `set-state` op (folded state == "") passed the empty state into
// SetState, which rejects it. Repositioning must not require a state.
//
// A stateless issue arises from a repo whose `writ init` ran before a
// signing key was configured: default workflow-state seeding is attempted
// but silently fails (no signing key to commit with), so `issue create`
// -- run later, once signing is configured -- finds no workflow states to
// default into and leaves the issue without a set-state op.
func TestIssueStatus_RepositionOnly_StatelessIssue(t *testing.T) {
	env := setupTestCLIEnv(t)
	setGitConfig(t, env.repoDir, "user.name", "Alice")
	setGitConfig(t, env.repoDir, "user.email", "alice@example.com")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init (no signing key) failed with %d; stderr: %s", code, stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// Configure signing only now, so default workflow-state seeding above
	// stayed a no-op: this issue is created with no workflow states to
	// default into, hence no set-state op.
	setupSigningKey(t, env.repoDir)

	// Create an issue without -state: it folds to state == "".
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "create", "-C", env.repoDir,
		"-title", "Stateless Issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue create failed with %d; stderr: %s", code, stderr.String())
	}
	issueID := strings.Fields(stdout.String())[0]

	// Reposition-only: no explicit new state is passed.
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"issue", "status", "-C", env.repoDir, issueID,
		"-position", "V",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status -position on stateless issue failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"issue", "status", "-C", env.repoDir, issueID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("issue status --json failed with %d; stderr: %s", code, stderr.String())
	}
	var wire1 struct {
		Data wire.Issue `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &wire1); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if wire1.Data.State != "" {
		t.Errorf("expected state to remain empty, got %q", wire1.Data.State)
	}
	if wire1.Data.Position != "V" {
		t.Errorf("expected position 'V', got %q", wire1.Data.Position)
	}
}




