package projection

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/engine/state"
)

// materializeObject folds ops for a single collaborative object and records
// typed rows in the projection tables inside tx.
func materializeObject(tx *sql.Tx, objectID string, ops []codec.Op) error {
	// Clean up existing state for this object
	if err := deleteObjectState(tx, objectID); err != nil {
		return err
	}

	if len(ops) == 0 {
		return nil
	}

	orderedOps, err := dag.Order(ops)
	if err != nil {
		return fmt.Errorf("projection: order ops for object %s: %w", objectID, err)
	}

	firstOp := orderedOps[0]
	lastOp := orderedOps[len(orderedOps)-1]
	authorName := firstOp.Author.Name
	authorEmail := firstOp.Author.Email
	createdAt := firstOp.Author.When.UTC().Unix()
	updatedAt := lastOp.Author.When.UTC().Unix()
	lastOpID := lastOp.ID
	objectType := determineObjectType(orderedOps)

	// Insert objects row
	_, err = tx.Exec(
		"INSERT INTO objects (object_id, object_type, op_count, last_op_id, author_name, author_email, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		objectID, objectType, len(ops), lastOpID, authorName, authorEmail, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("projection: insert object %s: %w", objectID, err)
	}

	switch objectType {
	case "review":
		review, err := state.FoldReview(ops)
		if err != nil {
			return fmt.Errorf("projection: fold review %s: %w", objectID, err)
		}

		_, err = tx.Exec(
			"INSERT INTO reviews (object_id, title, description, status, merge_commit, reason) VALUES (?, ?, ?, ?, ?, ?)",
			objectID, review.Title, review.Description, review.Status, review.MergeCommit, review.Reason,
		)
		if err != nil {
			return fmt.Errorf("projection: insert review %s: %w", objectID, err)
		}

		for i, rev := range review.Revisions {
			_, err = tx.Exec(
				"INSERT INTO review_revisions (review_object_id, revision_index, base, head) VALUES (?, ?, ?, ?)",
				objectID, i, rev.Base, rev.Head,
			)
			if err != nil {
				return fmt.Errorf("projection: insert review revision %s [%d]: %w", objectID, i, err)
			}
		}

		for _, a := range review.Assignees {
			_, err = tx.Exec(
				"INSERT INTO review_assignees (review_object_id, assignee) VALUES (?, ?)",
				objectID, a,
			)
			if err != nil {
				return fmt.Errorf("projection: insert review assignee %s (%s): %w", objectID, a, err)
			}
		}

		for _, label := range review.Labels {
			_, err = tx.Exec(
				"INSERT INTO review_labels (review_object_id, label) VALUES (?, ?)",
				objectID, label,
			)
			if err != nil {
				return fmt.Errorf("projection: insert review label %s (%s): %w", objectID, label, err)
			}
		}

		for _, link := range review.Links {
			_, err = tx.Exec(
				"INSERT INTO review_links (review_object_id, target, target_type, relation) VALUES (?, ?, ?, ?)",
				objectID, link.Target, link.TargetType, link.Relation,
			)
			if err != nil {
				return fmt.Errorf("projection: insert review link %s (%s): %w", objectID, link.Target, err)
			}
		}

		for _, app := range review.Approvals {
			_, err = tx.Exec(
				"INSERT INTO approvals (review_object_id, subject, revision, verdict, message) VALUES (?, ?, ?, ?, ?)",
				objectID, app.Subject, app.Revision, app.Verdict, app.Message,
			)
			if err != nil {
				return fmt.Errorf("projection: insert approval %s (%s, %s): %w", objectID, app.Subject, app.Revision, err)
			}
		}

		for _, ci := range review.CIStatuses {
			_, err = tx.Exec(
				"INSERT INTO ci_statuses (review_object_id, revision, name, state, url, description, started_at, completed_at, external_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				objectID, ci.Revision, ci.Name, ci.State, ci.URL, ci.Description, ci.StartedAt, ci.CompletedAt, ci.ExternalID,
			)
			if err != nil {
				return fmt.Errorf("projection: insert ci_status %s (%s, %s): %w", objectID, ci.Revision, ci.Name, err)
			}
		}

		for i, u := range review.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	case "comment":
		comment, err := state.FoldComment(ops)
		if err != nil {
			return fmt.Errorf("projection: fold comment %s: %w", objectID, err)
		}

		var anchorStr string
		if comment.Anchor != nil {
			b, err := json.Marshal(comment.Anchor)
			if err != nil {
				return fmt.Errorf("projection: marshal anchor for comment %s: %w", objectID, err)
			}
			anchorStr = string(b)
		}

		deletedInt := 0
		if comment.Deleted {
			deletedInt = 1
		}

		var resolvedVal any
		if comment.Resolved != nil {
			if *comment.Resolved {
				resolvedVal = 1
			} else {
				resolvedVal = 0
			}
		}

		_, err = tx.Exec(
			"INSERT INTO comments (object_id, subject_type, subject_id, text, in_reply_to, anchor, deleted, resolved, resolved_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			objectID, comment.Subject.ObjectType, comment.Subject.ObjectID, comment.Text, comment.InReplyTo, anchorStr, deletedInt, resolvedVal, comment.ResolvedBy,
		)
		if err != nil {
			return fmt.Errorf("projection: insert comment %s: %w", objectID, err)
		}

		for i, u := range comment.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	case "issue":
		issue, err := state.FoldIssue(ops)
		if err != nil {
			return fmt.Errorf("projection: fold issue %s: %w", objectID, err)
		}

		_, err = tx.Exec(
			"INSERT INTO issues (object_id, title, description, state, reason) VALUES (?, ?, ?, ?, ?)",
			objectID, issue.Title, issue.Description, issue.State, issue.Reason,
		)
		if err != nil {
			return fmt.Errorf("projection: insert issue %s: %w", objectID, err)
		}

		for _, assignee := range issue.Assignees {
			_, err = tx.Exec(
				"INSERT INTO issue_assignees (issue_object_id, assignee) VALUES (?, ?)",
				objectID, assignee,
			)
			if err != nil {
				return fmt.Errorf("projection: insert issue assignee %s (%s): %w", objectID, assignee, err)
			}
		}

		for _, label := range issue.Labels {
			_, err = tx.Exec(
				"INSERT INTO issue_labels (issue_object_id, label) VALUES (?, ?)",
				objectID, label,
			)
			if err != nil {
				return fmt.Errorf("projection: insert issue label %s (%s): %w", objectID, label, err)
			}
		}

		for _, link := range issue.Links {
			_, err = tx.Exec(
				"INSERT INTO issue_links (issue_object_id, target, target_type, relation) VALUES (?, ?, ?, ?)",
				objectID, link.Target, link.TargetType, link.Relation,
			)
			if err != nil {
				return fmt.Errorf("projection: insert issue link %s (%s): %w", objectID, link.Target, err)
			}
		}

		for i, u := range issue.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	case "project":
		project, err := state.FoldProject(ops)
		if err != nil {
			return fmt.Errorf("projection: fold project %s: %w", objectID, err)
		}

		_, err = tx.Exec(
			"INSERT INTO projects (object_id, title, description, status, reason) VALUES (?, ?, ?, ?, ?)",
			objectID, project.Title, project.Description, project.Status, project.Reason,
		)
		if err != nil {
			return fmt.Errorf("projection: insert project %s: %w", objectID, err)
		}

		for _, iss := range project.Issues {
			_, err = tx.Exec(
				"INSERT INTO project_issues (project_object_id, issue) VALUES (?, ?)",
				objectID, iss,
			)
			if err != nil {
				return fmt.Errorf("projection: insert project issue %s (%s): %w", objectID, iss, err)
			}
		}

		for i, u := range project.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	case "cycle":
		cycle, err := state.FoldCycle(ops)
		if err != nil {
			return fmt.Errorf("projection: fold cycle %s: %w", objectID, err)
		}

		_, err = tx.Exec(
			"INSERT INTO cycles (object_id, title, description, starts_at, ends_at) VALUES (?, ?, ?, ?, ?)",
			objectID, cycle.Title, cycle.Description, cycle.StartsAt, cycle.EndsAt,
		)
		if err != nil {
			return fmt.Errorf("projection: insert cycle %s: %w", objectID, err)
		}

		for _, iss := range cycle.Issues {
			_, err = tx.Exec(
				"INSERT INTO cycle_issues (cycle_object_id, issue) VALUES (?, ?)",
				objectID, iss,
			)
			if err != nil {
				return fmt.Errorf("projection: insert cycle issue %s (%s): %w", objectID, iss, err)
			}
		}

		for i, u := range cycle.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	case "repo":
		repoEntry, err := state.FoldRepo(ops)
		if err != nil {
			return fmt.Errorf("projection: fold repo %s: %w", objectID, err)
		}

		isWorkspaceInt := 0
		if repoEntry.IsWorkspace {
			isWorkspaceInt = 1
		}

		_, err = tx.Exec(
			"INSERT INTO repos (object_id, slug, is_workspace) VALUES (?, ?, ?)",
			objectID, repoEntry.Slug, isWorkspaceInt,
		)
		if err != nil {
			return fmt.Errorf("projection: insert repo %s: %w", objectID, err)
		}

		for _, remote := range repoEntry.Remotes {
			_, err = tx.Exec(
				"INSERT INTO repo_remotes (repo_object_id, remote) VALUES (?, ?)",
				objectID, remote,
			)
			if err != nil {
				return fmt.Errorf("projection: insert repo remote %s (%s): %w", objectID, remote, err)
			}
		}

		for i, u := range repoEntry.UnknownOps {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, u.Commit, u.ObjectType, u.OpType, u.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unknown op %s: %w", u.Commit, err)
			}
		}

	default:
		// Preserved-but-unreduced ops: record in unknown_ops
		for i, op := range ops {
			_, err = tx.Exec(
				"INSERT OR REPLACE INTO unknown_ops (object_id, op_id, object_type, op_type, op_version, op_index) VALUES (?, ?, ?, ?, ?, ?)",
				objectID, op.ID, op.ObjectType, op.OpType, op.OpVersion, i,
			)
			if err != nil {
				return fmt.Errorf("projection: insert unreduced op %s: %w", op.ID, err)
			}
		}
	}

	return nil
}

func determineObjectType(ops []codec.Op) string {
	for _, op := range ops {
		if op.OpType == "create" && op.ObjectType != "" {
			return op.ObjectType
		}
	}
	for _, op := range ops {
		if op.ObjectType != "" {
			return op.ObjectType
		}
	}
	if len(ops) > 0 {
		return ops[0].ObjectType
	}
	return ""
}

func deleteObjectState(tx *sql.Tx, objectID string) error {
	queries := []string{
		"DELETE FROM objects WHERE object_id = ?",
		"DELETE FROM unknown_ops WHERE object_id = ?",
		"DELETE FROM reviews WHERE object_id = ?",
		"DELETE FROM review_revisions WHERE review_object_id = ?",
		"DELETE FROM review_assignees WHERE review_object_id = ?",
		"DELETE FROM review_labels WHERE review_object_id = ?",
		"DELETE FROM review_links WHERE review_object_id = ?",
		"DELETE FROM approvals WHERE review_object_id = ?",
		"DELETE FROM ci_statuses WHERE review_object_id = ?",
		"DELETE FROM comments WHERE object_id = ?",
		"DELETE FROM anchor_resolutions WHERE comment_object_id = ?",
		"DELETE FROM issues WHERE object_id = ?",
		"DELETE FROM issue_assignees WHERE issue_object_id = ?",
		"DELETE FROM issue_labels WHERE issue_object_id = ?",
		"DELETE FROM issue_links WHERE issue_object_id = ?",
		"DELETE FROM projects WHERE object_id = ?",
		"DELETE FROM project_issues WHERE project_object_id = ?",
		"DELETE FROM cycles WHERE object_id = ?",
		"DELETE FROM cycle_issues WHERE cycle_object_id = ?",
		"DELETE FROM repos WHERE object_id = ?",
		"DELETE FROM repo_remotes WHERE repo_object_id = ?",
	}
	for _, q := range queries {
		if _, err := tx.Exec(q, objectID); err != nil {
			return fmt.Errorf("projection: delete object state (%s): %w", q, err)
		}
	}
	return nil
}

type commentToResolve struct {
	objectID   string
	anchorJSON string
}

// materializeAnchors resolves comment anchors against current target commits in code_tips.
func materializeAnchors(tx *sql.Tx, s storage.Storer) (int, error) {
	// 1. Prune resolutions whose target_commit is no longer in code_tips
	if _, err := tx.Exec("DELETE FROM anchor_resolutions WHERE target_commit NOT IN (SELECT tip FROM code_tips)"); err != nil {
		return 0, fmt.Errorf("projection: prune stale anchor resolutions: %w", err)
	}

	// 2. Fetch distinct target commits from code_tips
	rows, err := tx.Query("SELECT DISTINCT tip FROM code_tips WHERE tip != ''")
	if err != nil {
		return 0, fmt.Errorf("projection: query code_tips: %w", err)
	}
	var targetCommits []string
	for rows.Next() {
		var tip string
		if err := rows.Scan(&tip); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("projection: scan code_tips: %w", err)
		}
		targetCommits = append(targetCommits, tip)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("projection: iterate code_tips: %w", err)
	}

	if len(targetCommits) == 0 {
		return 0, nil
	}

	// 3. Fetch all comments with non-empty anchor
	cRows, err := tx.Query("SELECT object_id, anchor FROM comments WHERE anchor != '' AND anchor != 'null'")
	if err != nil {
		return 0, fmt.Errorf("projection: query comments with anchors: %w", err)
	}
	var comments []commentToResolve
	for cRows.Next() {
		var c commentToResolve
		if err := cRows.Scan(&c.objectID, &c.anchorJSON); err != nil {
			_ = cRows.Close()
			return 0, fmt.Errorf("projection: scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	if err := cRows.Err(); err != nil {
		return 0, fmt.Errorf("projection: iterate comments: %w", err)
	}

	if len(comments) == 0 {
		return 0, nil
	}

	treeCache := make(map[string]*resolve.Tree)
	resolvedCount := 0

	for _, targetCommit := range targetCommits {
		// Find comments that already have resolutions for this targetCommit
		resRows, err := tx.Query("SELECT DISTINCT comment_object_id FROM anchor_resolutions WHERE target_commit = ?", targetCommit)
		if err != nil {
			return resolvedCount, fmt.Errorf("projection: query existing resolutions: %w", err)
		}
		existing := make(map[string]bool)
		for resRows.Next() {
			var objID string
			if err := resRows.Scan(&objID); err != nil {
				_ = resRows.Close()
				return resolvedCount, fmt.Errorf("projection: scan existing resolution: %w", err)
			}
			existing[objID] = true
		}
		if err := resRows.Err(); err != nil {
			return resolvedCount, fmt.Errorf("projection: iterate existing resolutions: %w", err)
		}

		for _, comm := range comments {
			if existing[comm.objectID] {
				continue
			}

			targetTree, ok := treeCache[targetCommit]
			if !ok {
				treeFiles, err := materializeCommitTree(s, targetCommit)
				if err != nil {
					return resolvedCount, fmt.Errorf("projection: materialize tree %s: %w", targetCommit, err)
				}
				algo := resolve.SHA1
				if len(targetCommit) == 64 {
					algo = resolve.SHA256
				}
				targetTree = resolve.NewTree(treeFiles, algo)
				treeCache[targetCommit] = targetTree
			}

			anchor, err := resolve.ParseAnchor([]byte(comm.anchorJSON))
			if err != nil {
				return resolvedCount, fmt.Errorf("projection: parse anchor for comment %s: %w", comm.objectID, err)
			}

			res := resolve.Resolve(anchor, targetTree)

			if res.Old != nil {
				startLine, endLine := 0, 0
				if res.Old.Range != nil {
					startLine = res.Old.Range.Start
					endLine = res.Old.Range.End
				}
				_, err = tx.Exec(
					"INSERT OR REPLACE INTO anchor_resolutions (comment_object_id, target_commit, side, outcome, match, path, start_line, end_line, reason) VALUES (?, ?, 'old', ?, ?, ?, ?, ?, ?)",
					comm.objectID, targetCommit, res.Old.Outcome, res.Old.Match, res.Old.Path, startLine, endLine, res.Old.Reason,
				)
				if err != nil {
					return resolvedCount, fmt.Errorf("projection: insert anchor resolution old (%s, %s): %w", comm.objectID, targetCommit, err)
				}
				resolvedCount++
			}

			if res.New != nil {
				startLine, endLine := 0, 0
				if res.New.Range != nil {
					startLine = res.New.Range.Start
					endLine = res.New.Range.End
				}
				_, err = tx.Exec(
					"INSERT OR REPLACE INTO anchor_resolutions (comment_object_id, target_commit, side, outcome, match, path, start_line, end_line, reason) VALUES (?, ?, 'new', ?, ?, ?, ?, ?, ?)",
					comm.objectID, targetCommit, res.New.Outcome, res.New.Match, res.New.Path, startLine, endLine, res.New.Reason,
				)
				if err != nil {
					return resolvedCount, fmt.Errorf("projection: insert anchor resolution new (%s, %s): %w", comm.objectID, targetCommit, err)
				}
				resolvedCount++
			}
		}
	}

	return resolvedCount, nil
}

func materializeCommitTree(s storage.Storer, commitHash string) (map[string][]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("nil storer")
	}
	commit, err := object.GetCommit(s, plumbing.NewHash(commitHash))
	if err != nil {
		return nil, fmt.Errorf("lookup commit %s: %w", commitHash, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("lookup tree for commit %s: %w", commitHash, err)
	}

	files := make(map[string][]byte)
	err = tree.Files().ForEach(func(f *object.File) error {
		contents, err := f.Contents()
		if err != nil {
			return err
		}
		files[f.Name] = []byte(contents)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read tree files for commit %s: %w", commitHash, err)
	}
	return files, nil
}
