package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
)

func setupRepoWithComment(t *testing.T) (string, string, string) {
	t.Helper()
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)
	commitFile(t, env.repoDir, "file.txt", "v1", "initial")

	var stdout, stderr bytes.Buffer

	// 1. Init
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init failed with code %d: %s", code, stderr.String())
	}

	// 2. Open review
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Review for comment tests",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed with code %d: %s", code, stderr.String())
	}
	reviewID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	// 3. Post comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-m", "Initial comment text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment failed with code %d: %s", code, stderr.String())
	}
	commentID := strings.TrimSpace(stdout.String())

	return env.repoDir, reviewID, commentID
}

func TestCommentEdit_Human(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "edit", "-C", repoDir, commentID,
		"-m", "Updated comment text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment edit failed with code %d: %s", code, stderr.String())
	}

	wantOut := commentID + " (edited)\n"
	if stdout.String() != wantOut {
		t.Errorf("stdout = %q, want %q", stdout.String(), wantOut)
	}

	// Verify folded state in store
	store, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	comments, err := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Query.Comments failed: %v", err)
	}

	var found *writ.CommentResult
	for i := range comments {
		if comments[i].ObjectID == commentID {
			found = &comments[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("comment %s not found in query results", commentID)
	}
	if found.Comment.Text != "Updated comment text" {
		t.Errorf("comment text = %q, want %q", found.Comment.Text, "Updated comment text")
	}
}

func TestCommentEdit_JSON(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "edit", "-C", repoDir, commentID,
		"-m", "Updated JSON comment text",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment edit --json failed with code %d: %s", code, stderr.String())
	}

	var env wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v; raw: %s", err, stdout.String())
	}

	if env.SchemaVersion != wire.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", env.SchemaVersion, wire.CurrentSchemaVersion)
	}
	if env.Kind != wire.KindCommentEdit {
		t.Errorf("Kind = %q, want %q", env.Kind, wire.KindCommentEdit)
	}

	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal env.Data: %v", err)
	}
	var comment wire.Comment
	if err := json.Unmarshal(dataBytes, &comment); err != nil {
		t.Fatalf("unmarshal wire.Comment: %v", err)
	}

	if comment.ObjectID != commentID {
		t.Errorf("ObjectID = %q, want %q", comment.ObjectID, commentID)
	}
	if comment.Text != "Updated JSON comment text" {
		t.Errorf("Text = %q, want %q", comment.Text, "Updated JSON comment text")
	}
	if comment.Deleted {
		t.Errorf("Deleted = true, want false")
	}
}

func TestCommentEdit_Prefix(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)
	prefix := commentID[:8]

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "edit", "-C", repoDir, prefix,
		"-m", "Prefix edited comment text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment edit with prefix failed with code %d: %s", code, stderr.String())
	}

	wantOut := commentID + " (edited)\n"
	if stdout.String() != wantOut {
		t.Errorf("stdout = %q, want %q", stdout.String(), wantOut)
	}

	store, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	comments, err := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Query.Comments failed: %v", err)
	}
	for _, c := range comments {
		if c.ObjectID == commentID {
			if c.Comment.Text != "Prefix edited comment text" {
				t.Errorf("Text = %q, want %q", c.Comment.Text, "Prefix edited comment text")
			}
			return
		}
	}
	t.Fatalf("comment %s not found", commentID)
}

func TestCommentDelete_Human(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "delete", "-C", repoDir, commentID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment delete failed with code %d: %s", code, stderr.String())
	}

	wantOut := commentID + " (deleted)\n"
	if stdout.String() != wantOut {
		t.Errorf("stdout = %q, want %q", stdout.String(), wantOut)
	}

	// Verify folded state in store
	store, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	// Default query should omit tombstoned comments
	comments, err := store.Query.Comments(writ.CommentFilter{})
	if err != nil {
		t.Fatalf("Query.Comments failed: %v", err)
	}
	for _, c := range comments {
		if c.ObjectID == commentID {
			t.Errorf("comment %s should not be returned by default query", commentID)
		}
	}

	// Query with IncludeDeleted: true should show Deleted == true
	allComments, err := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Query.Comments with IncludeDeleted failed: %v", err)
	}
	var found *writ.CommentResult
	for i := range allComments {
		if allComments[i].ObjectID == commentID {
			found = &allComments[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("comment %s not found in IncludeDeleted query", commentID)
	}
	if !found.Comment.Deleted {
		t.Errorf("expected Comment.Deleted == true")
	}
}

func TestCommentDelete_JSON(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "delete", "-C", repoDir, commentID,
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment delete --json failed with code %d: %s", code, stderr.String())
	}

	var env wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v; raw: %s", err, stdout.String())
	}

	if env.SchemaVersion != wire.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", env.SchemaVersion, wire.CurrentSchemaVersion)
	}
	if env.Kind != wire.KindCommentDelete {
		t.Errorf("Kind = %q, want %q", env.Kind, wire.KindCommentDelete)
	}

	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal env.Data: %v", err)
	}
	var comment wire.Comment
	if err := json.Unmarshal(dataBytes, &comment); err != nil {
		t.Fatalf("unmarshal wire.Comment: %v", err)
	}

	if comment.ObjectID != commentID {
		t.Errorf("ObjectID = %q, want %q", comment.ObjectID, commentID)
	}
	if !comment.Deleted {
		t.Errorf("Deleted = false, want true")
	}
}

func TestCommentDelete_Prefix(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)
	prefix := commentID[:8]

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "delete", "-C", repoDir, prefix,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment delete with prefix failed with code %d: %s", code, stderr.String())
	}

	wantOut := commentID + " (deleted)\n"
	if stdout.String() != wantOut {
		t.Errorf("stdout = %q, want %q", stdout.String(), wantOut)
	}

	store, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store.Close()

	allComments, err := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Query.Comments failed: %v", err)
	}
	for _, c := range allComments {
		if c.ObjectID == commentID {
			if !c.Comment.Deleted {
				t.Errorf("expected Comment.Deleted == true")
			}
			return
		}
	}
	t.Fatalf("comment %s not found", commentID)
}

func TestComment_Validation(t *testing.T) {
	repoDir, _, commentID := setupRepoWithComment(t)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantErrSub string
	}{
		{
			name:       "edit_missing_id",
			args:       []string{"comment", "edit", "-C", repoDir},
			wantCode:   2,
			wantErrSub: "comment ID is required",
		},
		{
			name:       "delete_missing_id",
			args:       []string{"comment", "delete", "-C", repoDir},
			wantCode:   2,
			wantErrSub: "comment ID is required",
		},
		{
			name:       "edit_missing_m",
			args:       []string{"comment", "edit", "-C", repoDir, commentID},
			wantCode:   2,
			wantErrSub: "-m is required",
		},
		{
			name:       "edit_empty_m",
			args:       []string{"comment", "edit", "-C", repoDir, commentID, "-m", ""},
			wantCode:   2,
			wantErrSub: "-m is required",
		},
		{
			name:       "edit_unexpected_args",
			args:       []string{"comment", "edit", "-C", repoDir, commentID, "extra", "-m", "text"},
			wantCode:   2,
			wantErrSub: "unexpected arguments",
		},
		{
			name:       "delete_unexpected_args",
			args:       []string{"comment", "delete", "-C", repoDir, commentID, "extra"},
			wantCode:   2,
			wantErrSub: "unexpected arguments",
		},
		{
			name:       "edit_invalid_flag",
			args:       []string{"comment", "edit", "-C", repoDir, "-invalid"},
			wantCode:   2,
			wantErrSub: "flag provided but not defined",
		},
		{
			name:       "delete_invalid_flag",
			args:       []string{"comment", "delete", "-C", repoDir, "-invalid"},
			wantCode:   2,
			wantErrSub: "flag provided but not defined",
		},
		{
			name:       "comment_no_args",
			args:       []string{"comment", "-C", repoDir},
			wantCode:   2,
			wantErrSub: "Usage: writ comment",
		},
		{
			name:       "comment_unknown_subcommand",
			args:       []string{"comment", "-C", repoDir, "bogus"},
			wantCode:   2,
			wantErrSub: "unknown subcommand \"bogus\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d; stderr: %s", code, tc.wantCode, stderr.String())
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, tc.wantErrSub) {
				t.Errorf("output missing %q; got:\n%s", tc.wantErrSub, combined)
			}
		})
	}
}

func TestComment_Errors(t *testing.T) {
	t.Run("nonexistent_id", func(t *testing.T) {
		repoDir, _, _ := setupRepoWithComment(t)

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"comment", "edit", "-C", repoDir, "nonexistent-id", "-m", "text",
		}, &stdout, &stderr)
		if code != 1 {
			t.Errorf("comment edit nonexistent exited with %d, want 1; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "no comment with id nonexistent-id") {
			t.Errorf("stderr missing expected message; got: %s", stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"comment", "delete", "-C", repoDir, "nonexistent-id",
		}, &stdout, &stderr)
		if code != 1 {
			t.Errorf("comment delete nonexistent exited with %d, want 1; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "no comment with id nonexistent-id") {
			t.Errorf("stderr missing expected message; got: %s", stderr.String())
		}
	})

	t.Run("ambiguous_prefix", func(t *testing.T) {
		repoDir := loadFixtureRepo(t, "fold-comment-threads")
		setupSigningKey(t, repoDir)

		var initOut, initErr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", repoDir}, &initOut, &initErr)
		if code != 0 {
			t.Fatalf("init failed: %s", initErr.String())
		}

		// "c-" matches c-root, c-del-target, c-reply in fold-comment-threads
		var stdout, stderr bytes.Buffer
		code = run(context.Background(), []string{
			"comment", "edit", "-C", repoDir, "c-", "-m", "ambiguous",
		}, &stdout, &stderr)
		if code != 1 {
			t.Errorf("comment edit ambiguous prefix exited with %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "ambiguous comment ID prefix") {
			t.Errorf("stderr missing 'ambiguous comment ID prefix': %s", stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"comment", "delete", "-C", repoDir, "c-",
		}, &stdout, &stderr)
		if code != 1 {
			t.Errorf("comment delete ambiguous prefix exited with %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "ambiguous comment ID prefix") {
			t.Errorf("stderr missing 'ambiguous comment ID prefix': %s", stderr.String())
		}
	})
}

func TestComment_RoundTrip(t *testing.T) {
	repoDir, reviewID, commentID := setupRepoWithComment(t)

	// 1. Edit comment
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"comment", "edit", "-C", repoDir, commentID,
		"-m", "Round trip edited text",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment edit failed with %d: %s", code, stderr.String())
	}

	// Verify state in store
	store, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}

	comments, err := store.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
	if err != nil {
		store.Close()
		t.Fatalf("Query.Comments failed: %v", err)
	}
	if len(comments) != 1 {
		store.Close()
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Comment.Text != "Round trip edited text" {
		store.Close()
		t.Errorf("text = %q, want %q", comments[0].Comment.Text, "Round trip edited text")
	}
	if comments[0].Comment.Deleted {
		store.Close()
		t.Errorf("comment should not be deleted")
	}
	store.Close()

	// 2. Delete comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"comment", "delete", "-C", repoDir, commentID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("comment delete failed with %d: %s", code, stderr.String())
	}

	// Verify state in store after delete
	store2, err := openStore(repoDir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	defer store2.Close()

	activeComments, err := store2.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
	if err != nil {
		t.Fatalf("Query.Comments active failed: %v", err)
	}
	if len(activeComments) != 0 {
		t.Errorf("expected 0 active comments, got %d", len(activeComments))
	}

	allComments, err := store2.Query.Comments(writ.CommentFilter{SubjectID: reviewID, IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Query.Comments all failed: %v", err)
	}
	if len(allComments) != 1 {
		t.Fatalf("expected 1 comment including deleted, got %d", len(allComments))
	}
	if !allComments[0].Comment.Deleted {
		t.Errorf("expected Comment.Deleted == true")
	}
	if allComments[0].Comment.Text != "Round trip edited text" {
		t.Errorf("text = %q, want %q", allComments[0].Comment.Text, "Round trip edited text")
	}
}
