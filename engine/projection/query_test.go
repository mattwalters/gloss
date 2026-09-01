package projection_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/writtendev/writ/engine/projection"
)

func setupSeededDB(t *testing.T) *projection.DB {
	t.Helper()
	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}

	rawDB := db.DB()

	// Insert objects
	insertObject(t, rawDB, "rev-1", "review", 2, "op-rev-1b", "Alice Smith", "alice@example.com", 1000, 1100)
	insertObject(t, rawDB, "rev-2", "review", 1, "op-rev-2", "Bob Jones", "bob@example.com", 1200, 1200)
	insertObject(t, rawDB, "rev-3", "review", 1, "op-rev-3", "Charlie Brown", "charlie@example.com", 1300, 1300)

	insertObject(t, rawDB, "iss-1", "issue", 3, "op-iss-1c", "Alice Smith", "alice@example.com", 2000, 2200)
	insertObject(t, rawDB, "iss-2", "issue", 1, "op-iss-2", "Bob Jones", "bob@example.com", 2100, 2100)
	insertObject(t, rawDB, "iss-3", "issue", 2, "op-iss-3b", "Charlie Brown", "charlie@example.com", 2300, 2400)
	insertObject(t, rawDB, "iss-4", "issue", 1, "op-iss-4", "Dave Wilson", "dave@example.com", 2500, 2500)

	insertObject(t, rawDB, "comm-1", "comment", 1, "op-comm-1", "Alice Smith", "alice@example.com", 3000, 3000)
	insertObject(t, rawDB, "comm-2", "comment", 1, "op-comm-2", "Bob Jones", "bob@example.com", 3100, 3100)
	insertObject(t, rawDB, "comm-3", "comment", 1, "op-comm-3", "Charlie Brown", "charlie@example.com", 3200, 3200)
	insertObject(t, rawDB, "comm-4", "comment", 1, "op-comm-4", "Dave Wilson", "dave@example.com", 3300, 3300)

	// Insert reviews
	execSQL(t, rawDB, "INSERT INTO reviews (object_id, title, description, status, merge_commit, reason) VALUES (?, ?, ?, ?, ?, ?)",
		"rev-1", "Fix 100% CPU in loop_worker", "Resolves high CPU usage during batching", "open", "", "")
	execSQL(t, rawDB, "INSERT INTO review_revisions (review_object_id, revision_index, base, head) VALUES (?, ?, ?, ?)",
		"rev-1", 0, "base-1", "head-1")
	execSQL(t, rawDB, "INSERT INTO review_assignees (review_object_id, assignee) VALUES (?, ?)", "rev-1", "alice")
	execSQL(t, rawDB, "INSERT INTO review_assignees (review_object_id, assignee) VALUES (?, ?)", "rev-1", "bob")
	execSQL(t, rawDB, "INSERT INTO approvals (review_object_id, subject, revision, verdict, message) VALUES (?, ?, ?, ?, ?)",
		"rev-1", "bob@example.com", "head-1", "approved", "LGTM")

	execSQL(t, rawDB, "INSERT INTO reviews (object_id, title, description, status, merge_commit, reason) VALUES (?, ?, ?, ?, ?, ?)",
		"rev-2", "Add feature foo_bar", "Implements foo_bar integration", "merged", "merge-sha-2", "")
	execSQL(t, rawDB, "INSERT INTO review_assignees (review_object_id, assignee) VALUES (?, ?)", "rev-2", "bob")
	execSQL(t, rawDB, "INSERT INTO reviews (object_id, title, description, status, merge_commit, reason) VALUES (?, ?, ?, ?, ?, ?)",
		"rev-3", "Refactor storage layer", "Clean up legacy drivers", "closed", "", "abandoned")

	// Insert issues
	execSQL(t, rawDB, "INSERT INTO issues (object_id, title, description, state, reason) VALUES (?, ?, ?, ?, ?)",
		"iss-1", "Memory leak in 100% workload", "Under 100% load, buffer_pool grows unbounded", "open", "")
	execSQL(t, rawDB, "INSERT INTO issue_assignees (issue_object_id, assignee) VALUES (?, ?)", "iss-1", "alice")
	execSQL(t, rawDB, "INSERT INTO issue_assignees (issue_object_id, assignee) VALUES (?, ?)", "iss-1", "bob")
	execSQL(t, rawDB, "INSERT INTO issue_labels (issue_object_id, label) VALUES (?, ?)", "iss-1", "bug")
	execSQL(t, rawDB, "INSERT INTO issue_labels (issue_object_id, label) VALUES (?, ?)", "iss-1", "perf")
	execSQL(t, rawDB, "INSERT INTO issue_links (issue_object_id, target, target_type, relation) VALUES (?, ?, ?, ?)",
		"iss-1", "writ#2", "issue", "blocks")

	execSQL(t, rawDB, "INSERT INTO issues (object_id, title, description, state, reason) VALUES (?, ?, ?, ?, ?)",
		"iss-2", "Support foo_bar in CLI", "CLI flag --foo_bar should be supported", "in_progress", "")
	execSQL(t, rawDB, "INSERT INTO issue_assignees (issue_object_id, assignee) VALUES (?, ?)", "iss-2", "bob")
	execSQL(t, rawDB, "INSERT INTO issue_labels (issue_object_id, label) VALUES (?, ?)", "iss-2", "feature")

	execSQL(t, rawDB, "INSERT INTO issues (object_id, title, description, state, reason) VALUES (?, ?, ?, ?, ?)",
		"iss-3", "Documentation typo in docs", "Fix typo in readme", "closed", "completed")
	execSQL(t, rawDB, "INSERT INTO issue_labels (issue_object_id, label) VALUES (?, ?)", "iss-3", "docs")

	execSQL(t, rawDB, "INSERT INTO issues (object_id, title, description, state, reason) VALUES (?, ?, ?, ?, ?)",
		"iss-4", "Unassigned issue for triage", "Needs investigation", "open", "")

	// Insert comments
	execSQL(t, rawDB, "INSERT INTO comments (object_id, subject_type, subject_id, text, in_reply_to, anchor, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"comm-1", "review", "rev-1", "Initial thought on loop_worker: 100% is high", "", "", 0)
	execSQL(t, rawDB, "INSERT INTO comments (object_id, subject_type, subject_id, text, in_reply_to, anchor, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"comm-2", "review", "rev-1", "Reply to initial thought", "comm-1", "", 0)
	execSQL(t, rawDB, "INSERT INTO comments (object_id, subject_type, subject_id, text, in_reply_to, anchor, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"comm-3", "review", "rev-1", "Nested reply under comm-2", "comm-2", "", 0)
	execSQL(t, rawDB, "INSERT INTO comments (object_id, subject_type, subject_id, text, in_reply_to, anchor, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"comm-4", "review", "rev-1", "Deleted comment in rev-1", "", "", 1)

	// Insert code_tips and anchor_resolutions
	execSQL(t, rawDB, "INSERT INTO code_tips (ref_name, tip) VALUES (?, ?)", "refs/heads/main", "tip-commit-1")
	execSQL(t, rawDB, "INSERT INTO anchor_resolutions (comment_object_id, target_commit, side, outcome, match, path, start_line, end_line, reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"comm-1", "tip-commit-1", "new", "resolved", "exact", "pkg/loop.go", 10, 15, "found match")

	// Insert repos
	execSQL(t, rawDB, "INSERT INTO repos (object_id, slug, is_workspace) VALUES (?, ?, ?)",
		"a1b2c3d4e5f60718293a4b5c6d7e8f90", "acme/backend", 0)
	execSQL(t, rawDB, "INSERT INTO repo_remotes (repo_object_id, remote) VALUES (?, ?)",
		"a1b2c3d4e5f60718293a4b5c6d7e8f90", "git@github.com:acme/backend.git")
	execSQL(t, rawDB, "INSERT INTO repos (object_id, slug, is_workspace) VALUES (?, ?, ?)",
		"00000000000000000000000000000001", "acme/workspace", 1)
	execSQL(t, rawDB, "INSERT INTO repo_remotes (repo_object_id, remote) VALUES (?, ?)",
		"00000000000000000000000000000001", "git@github.com:acme/workspace.git")

	return db
}

func insertObject(t *testing.T, db *sql.DB, objectID, objectType string, opCount int, lastOpID, authorName, authorEmail string, createdAt, updatedAt int64) {
	t.Helper()
	execSQL(t, db, "INSERT INTO objects (object_id, object_type, op_count, last_op_id, author_name, author_email, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		objectID, objectType, opCount, lastOpID, authorName, authorEmail, createdAt, updatedAt)
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("execSQL %q: %v", query, err)
	}
}

func TestReviewsFilter(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	tests := []struct {
		name    string
		filter  projection.ReviewFilter
		wantIDs []string
	}{
		{
			name:    "empty filter returns all reviews by created_at asc",
			filter:  projection.ReviewFilter{},
			wantIDs: []string{"rev-1", "rev-2", "rev-3"},
		},
		{
			name:    "filter by single status",
			filter:  projection.ReviewFilter{Status: []string{"open"}},
			wantIDs: []string{"rev-1"},
		},
		{
			name:    "filter by multiple statuses",
			filter:  projection.ReviewFilter{Status: []string{"merged", "closed"}},
			wantIDs: []string{"rev-2", "rev-3"},
		},
		{
			name:    "filter by author email",
			filter:  projection.ReviewFilter{Author: []string{"alice@example.com"}},
			wantIDs: []string{"rev-1"},
		},
		{
			name:    "filter by author name",
			filter:  projection.ReviewFilter{Author: []string{"Bob Jones"}},
			wantIDs: []string{"rev-2"},
		},
		{
			name:    "filter by substring text in title",
			filter:  projection.ReviewFilter{Text: "storage"},
			wantIDs: []string{"rev-3"},
		},
		{
			name:    "filter by substring text in description case-insensitive",
			filter:  projection.ReviewFilter{Text: "bAtChInG"},
			wantIDs: []string{"rev-1"},
		},
		{
			name:    "filter text with literal percent wildcard escaping",
			filter:  projection.ReviewFilter{Text: "100%"},
			wantIDs: []string{"rev-1"},
		},
		{
			name:    "filter text with literal underscore wildcard escaping",
			filter:  projection.ReviewFilter{Text: "foo_bar"},
			wantIDs: []string{"rev-2"},
		},
		{
			name:    "filter by single assignee",
			filter:  projection.ReviewFilter{Assignee: []string{"alice"}},
			wantIDs: []string{"rev-1"},
		},
		{
			name:    "filter by assignee matching multiple reviews",
			filter:  projection.ReviewFilter{Assignee: []string{"bob"}},
			wantIDs: []string{"rev-1", "rev-2"},
		},
		{
			name:    "filter text no match",
			filter:  projection.ReviewFilter{Text: "nonexistent"},
			wantIDs: []string{},
		},
		{
			name:    "order by created_at desc",
			filter:  projection.ReviewFilter{OrderBy: projection.OrderByCreatedAtDesc},
			wantIDs: []string{"rev-3", "rev-2", "rev-1"},
		},
		{
			name:    "pagination limit and offset",
			filter:  projection.ReviewFilter{Limit: 1, Offset: 1},
			wantIDs: []string{"rev-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := db.Reviews(tt.filter)
			if err != nil {
				t.Fatalf("Reviews failed: %v", err)
			}
			var gotIDs []string
			for _, r := range res {
				gotIDs = append(gotIDs, r.ObjectID)
			}
			if len(gotIDs) == 0 && len(tt.wantIDs) == 0 {
				return
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Errorf("got IDs %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestIssuesFilter(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	tests := []struct {
		name    string
		filter  projection.IssueFilter
		wantIDs []string
	}{
		{
			name:    "empty filter returns all issues by created_at asc",
			filter:  projection.IssueFilter{},
			wantIDs: []string{"iss-1", "iss-2", "iss-3", "iss-4"},
		},
		{
			name:    "filter by state",
			filter:  projection.IssueFilter{State: []string{"open"}},
			wantIDs: []string{"iss-1", "iss-4"},
		},
		{
			name:    "filter by single assignee",
			filter:  projection.IssueFilter{Assignee: []string{"alice"}},
			wantIDs: []string{"iss-1"},
		},
		{
			name:    "filter by assignee matching multiple issues",
			filter:  projection.IssueFilter{Assignee: []string{"bob"}},
			wantIDs: []string{"iss-1", "iss-2"},
		},
		{
			name:    "filter by label",
			filter:  projection.IssueFilter{Label: []string{"bug"}},
			wantIDs: []string{"iss-1"},
		},
		{
			name:    "filter by text in title with literal percent",
			filter:  projection.IssueFilter{Text: "100%"},
			wantIDs: []string{"iss-1"},
		},
		{
			name:    "filter by text in description with literal underscore",
			filter:  projection.IssueFilter{Text: "foo_bar"},
			wantIDs: []string{"iss-2"},
		},
		{
			name: "combined filter: state + label + assignee",
			filter: projection.IssueFilter{
				State:    []string{"open"},
				Assignee: []string{"alice"},
				Label:    []string{"bug"},
			},
			wantIDs: []string{"iss-1"},
		},
		{
			name:    "order by title asc",
			filter:  projection.IssueFilter{OrderBy: projection.OrderByTitleAsc},
			wantIDs: []string{"iss-3", "iss-1", "iss-2", "iss-4"},
		},
		{
			name:    "pagination limit 2 offset 1",
			filter:  projection.IssueFilter{Limit: 2, Offset: 1},
			wantIDs: []string{"iss-2", "iss-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := db.Issues(tt.filter)
			if err != nil {
				t.Fatalf("Issues failed: %v", err)
			}
			var gotIDs []string
			for _, r := range res {
				gotIDs = append(gotIDs, r.ObjectID)
			}
			if len(gotIDs) == 0 && len(tt.wantIDs) == 0 {
				return
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Errorf("got IDs %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestCommentsFilterAndResolutions(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	// Default IncludeDeleted: false should exclude comm-4
	res, err := db.Comments(projection.CommentFilter{SubjectID: "rev-1"})
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 non-deleted comments, got %d", len(res))
	}

	// First comment should have resolved position from code_tips default
	if res[0].ObjectID == "comm-1" {
		if len(res[0].Resolved) != 1 {
			t.Fatalf("expected 1 resolution for comm-1, got %d", len(res[0].Resolved))
		}
		if res[0].Resolved[0].Path != "pkg/loop.go" || res[0].Resolved[0].StartLine != 10 {
			t.Errorf("unexpected resolution: %+v", res[0].Resolved[0])
		}
	}

	// IncludeDeleted: true should include comm-4
	allRes, err := db.Comments(projection.CommentFilter{SubjectID: "rev-1", IncludeDeleted: true})
	if err != nil {
		t.Fatalf("Comments with IncludeDeleted: %v", err)
	}
	if len(allRes) != 4 {
		t.Fatalf("expected 4 comments including deleted, got %d", len(allRes))
	}

	// Substring search with wildcards
	txtRes, err := db.Comments(projection.CommentFilter{Text: "100%"})
	if err != nil {
		t.Fatalf("Comments by Text: %v", err)
	}
	if len(txtRes) != 1 || txtRes[0].ObjectID != "comm-1" {
		t.Errorf("expected only comm-1, got %v", txtRes)
	}
}

func TestObjectsCrossTypeFilter(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	// Filter by type
	revObjs, err := db.Objects(projection.ObjectFilter{Type: []string{"review"}})
	if err != nil {
		t.Fatalf("Objects(review): %v", err)
	}
	if len(revObjs) != 3 {
		t.Fatalf("expected 3 review objects, got %d", len(revObjs))
	}

	issObjs, err := db.Objects(projection.ObjectFilter{Type: []string{"issue"}})
	if err != nil {
		t.Fatalf("Objects(issue): %v", err)
	}
	if len(issObjs) != 4 {
		t.Fatalf("expected 4 issue objects, got %d", len(issObjs))
	}

	// Cross-type text search
	crossObjs, err := db.Objects(projection.ObjectFilter{Text: "100%"})
	if err != nil {
		t.Fatalf("Objects(100%%): %v", err)
	}
	// rev-1 (title), iss-1 (title/desc), comm-1 (text)
	if len(crossObjs) != 3 {
		t.Fatalf("expected 3 objects matching '100%%', got %d (%+v)", len(crossObjs), crossObjs)
	}
}

func TestThreadsAssembly(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	threads, err := db.Threads("review", "rev-1")
	if err != nil {
		t.Fatalf("Threads: %v", err)
	}

	// We expect 2 root threads:
	// Root 1: comm-1 -> Reply: comm-2 -> Reply: comm-3
	// Root 2: comm-4 (deleted)
	if len(threads) != 2 {
		t.Fatalf("expected 2 root threads, got %d", len(threads))
	}

	root1 := threads[0]
	if root1.ObjectID != "comm-1" {
		t.Errorf("expected first root to be comm-1, got %s", root1.ObjectID)
	}
	if len(root1.Replies) != 1 {
		t.Fatalf("expected comm-1 to have 1 reply, got %d", len(root1.Replies))
	}

	reply1 := root1.Replies[0]
	if reply1.ObjectID != "comm-2" {
		t.Errorf("expected reply to be comm-2, got %s", reply1.ObjectID)
	}
	if len(reply1.Replies) != 1 {
		t.Fatalf("expected comm-2 to have 1 reply, got %d", len(reply1.Replies))
	}

	nestedReply := reply1.Replies[0]
	if nestedReply.ObjectID != "comm-3" {
		t.Errorf("expected nested reply to be comm-3, got %s", nestedReply.ObjectID)
	}
	if len(nestedReply.Replies) != 0 {
		t.Errorf("expected comm-3 to have 0 replies, got %d", len(nestedReply.Replies))
	}

	root2 := threads[1]
	if root2.ObjectID != "comm-4" {
		t.Errorf("expected second root to be comm-4, got %s", root2.ObjectID)
	}
	if !root2.Comment.Deleted {
		t.Errorf("expected comm-4 to have Deleted = true")
	}
}

func TestReposQuery(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	repos, err := db.Repos()
	if err != nil {
		t.Fatalf("Repos query failed: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	r1 := repos[0]
	if r1.RepoID != "00000000000000000000000000000001" || r1.Slug != "acme/workspace" || !r1.IsWorkspace {
		t.Errorf("unexpected workspace repo: %+v", r1)
	}
	if len(r1.Remotes) != 1 || r1.Remotes[0] != "git@github.com:acme/workspace.git" {
		t.Errorf("unexpected workspace remotes: %v", r1.Remotes)
	}

	r2 := repos[1]
	if r2.RepoID != "a1b2c3d4e5f60718293a4b5c6d7e8f90" || r2.Slug != "acme/backend" || r2.IsWorkspace {
		t.Errorf("unexpected backend repo: %+v", r2)
	}

	single, err := db.Repo("a1b2c3d4e5f60718293a4b5c6d7e8f90")
	if err != nil {
		t.Fatalf("Repo query failed: %v", err)
	}
	if single.Slug != "acme/backend" {
		t.Errorf("single slug = %q, want 'acme/backend'", single.Slug)
	}

	_, errMissing := db.Repo("nonexistent")
	if errMissing == nil {
		t.Fatalf("expected ErrNotFound for missing repo, got nil")
	}
}
