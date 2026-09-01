package projection

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/engine/state"
)

// ErrNotFound is returned when an object is not found in the projection.
var ErrNotFound = errors.New("writ: object not found")

// Author holds the author display name and email address derived from an object's operations.
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ReviewResult represents a code review object along with its authorship and timestamps.
type ReviewResult struct {
	ObjectID  string      `json:"object_id"`
	Author    Author      `json:"author"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Review    state.Review `json:"review"`
}

// IssueResult represents an issue object along with its authorship and timestamps.
type IssueResult struct {
	ObjectID  string     `json:"object_id"`
	Author    Author     `json:"author"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Issue     state.Issue `json:"issue"`
}

// ResolvedPosition describes the resolved anchor position for a comment side.
type ResolvedPosition struct {
	Side      string `json:"side"`
	Outcome   string `json:"outcome"`
	Match     string `json:"match,omitempty"`
	Path      string `json:"path,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// CommentResult represents a comment object along with its authorship, timestamps,
// and anchor position resolutions.
//
// Note: The Resolved field represents anchor position resolution (where an anchor
// lands in a git tree), NOT comment thread resolution. Thread resolution is recorded
// on the folded Comment state (Comment.Resolved, Comment.ResolvedBy).
type CommentResult struct {
	ObjectID  string             `json:"object_id"`
	Author    Author             `json:"author"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Comment   state.Comment       `json:"comment"`
	Resolved  []ResolvedPosition `json:"resolved,omitempty"`
}

// ObjectResult represents summary metadata for any collaborative object cross-type.
type ObjectResult struct {
	ObjectID   string    `json:"object_id"`
	ObjectType string    `json:"object_type"`
	Author     Author    `json:"author"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	OpCount    int       `json:"op_count"`
	LastOpID   string    `json:"last_op_id"`
}

// Reviews executes a list and filter query over code reviews.
func (d *DB) Reviews(f ReviewFilter) ([]ReviewResult, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	var sb strings.Builder
	var args []any

	sb.WriteString("SELECT r.object_id, r.title, r.description, r.status, r.merge_commit, r.reason, ")
	sb.WriteString("o.author_name, o.author_email, o.created_at, o.updated_at ")
	sb.WriteString("FROM reviews r JOIN objects o ON o.object_id = r.object_id WHERE 1=1")

	if len(f.Status) > 0 {
		sb.WriteString(" AND r.status IN (" + placeholders(len(f.Status)) + ")")
		for _, s := range f.Status {
			args = append(args, s)
		}
	}

	if len(f.Author) > 0 {
		sb.WriteString(" AND (o.author_email IN (" + placeholders(len(f.Author)) + ") OR o.author_name IN (" + placeholders(len(f.Author)) + "))")
		for _, a := range f.Author {
			args = append(args, a)
		}
		for _, a := range f.Author {
			args = append(args, a)
		}
	}

	if len(f.Assignee) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM review_assignees ra WHERE ra.review_object_id = r.object_id AND ra.assignee IN (" + placeholders(len(f.Assignee)) + "))")
		for _, a := range f.Assignee {
			args = append(args, state.NormalizePerson(a))
		}
	}

	if len(f.Label) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM review_labels rl WHERE rl.review_object_id = r.object_id AND rl.label IN (" + placeholders(len(f.Label)) + "))")
		for _, l := range f.Label {
			args = append(args, l)
		}
	}

	if f.Text != "" {
		sb.WriteString(" AND (r.title LIKE ? ESCAPE '\\' OR r.description LIKE ? ESCAPE '\\')")
		escaped := "%" + escapeLike(f.Text) + "%"
		args = append(args, escaped, escaped)
	}

	switch f.OrderBy {
	case OrderByCreatedAtAsc:
		sb.WriteString(" ORDER BY o.created_at ASC, r.object_id ASC")
	case OrderByCreatedAtDesc:
		sb.WriteString(" ORDER BY o.created_at DESC, r.object_id DESC")
	case OrderByUpdatedAtAsc:
		sb.WriteString(" ORDER BY o.updated_at ASC, r.object_id ASC")
	case OrderByUpdatedAtDesc:
		sb.WriteString(" ORDER BY o.updated_at DESC, r.object_id DESC")
	case OrderByTitleAsc:
		sb.WriteString(" ORDER BY r.title ASC, r.object_id ASC")
	case OrderByTitleDesc:
		sb.WriteString(" ORDER BY r.title DESC, r.object_id DESC")
	default:
		sb.WriteString(" ORDER BY o.created_at ASC, r.object_id ASC")
	}

	appendLimitOffset(&sb, &args, f.Limit, f.Offset)

	rows, err := d.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("projection: query reviews: %w", err)
	}
	defer rows.Close()

	type rawReview struct {
		objectID    string
		title       string
		description string
		status      string
		mergeCommit string
		reason      string
		authorName  string
		authorEmail string
		createdAt   int64
		updatedAt   int64
	}

	var rawReviews []rawReview
	var objectIDs []string

	for rows.Next() {
		var rr rawReview
		if err := rows.Scan(
			&rr.objectID, &rr.title, &rr.description, &rr.status, &rr.mergeCommit, &rr.reason,
			&rr.authorName, &rr.authorEmail, &rr.createdAt, &rr.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("projection: scan review: %w", err)
		}
		rawReviews = append(rawReviews, rr)
		objectIDs = append(objectIDs, rr.objectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate reviews: %w", err)
	}

	if len(rawReviews) == 0 {
		return []ReviewResult{}, nil
	}

	// Batch load revisions
	revisionsMap := make(map[string][]state.Revision)
	revRows, err := d.queryIn("SELECT review_object_id, base, head FROM review_revisions WHERE review_object_id IN (?) ORDER BY review_object_id ASC, revision_index ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query review revisions: %w", err)
	}
	for revRows.Next() {
		var objID, base, head string
		if err := revRows.Scan(&objID, &base, &head); err != nil {
			revRows.Close()
			return nil, fmt.Errorf("projection: scan review revision: %w", err)
		}
		revisionsMap[objID] = append(revisionsMap[objID], state.Revision{Base: base, Head: head})
	}
	revRows.Close()

	// Batch load assignees
	assigneesMap := make(map[string][]string)
	asRows, err := d.queryIn("SELECT review_object_id, assignee FROM review_assignees WHERE review_object_id IN (?) ORDER BY review_object_id ASC, assignee ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query review assignees: %w", err)
	}
	for asRows.Next() {
		var objID, assignee string
		if err := asRows.Scan(&objID, &assignee); err != nil {
			asRows.Close()
			return nil, fmt.Errorf("projection: scan review assignee: %w", err)
		}
		assigneesMap[objID] = append(assigneesMap[objID], assignee)
	}
	asRows.Close()

	// Batch load labels
	labelsMap := make(map[string][]string)
	lblRows, err := d.queryIn("SELECT review_object_id, label FROM review_labels WHERE review_object_id IN (?) ORDER BY review_object_id ASC, label ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query review labels: %w", err)
	}
	for lblRows.Next() {
		var objID, label string
		if err := lblRows.Scan(&objID, &label); err != nil {
			lblRows.Close()
			return nil, fmt.Errorf("projection: scan review label: %w", err)
		}
		labelsMap[objID] = append(labelsMap[objID], label)
	}
	lblRows.Close()

	// Batch load links
	linksMap := make(map[string][]state.Link)
	lnkRows, err := d.queryIn("SELECT review_object_id, target, target_type, relation FROM review_links WHERE review_object_id IN (?) ORDER BY review_object_id ASC, target ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query review links: %w", err)
	}
	for lnkRows.Next() {
		var objID, target, targetType, relation string
		if err := lnkRows.Scan(&objID, &target, &targetType, &relation); err != nil {
			lnkRows.Close()
			return nil, fmt.Errorf("projection: scan review link: %w", err)
		}
		linksMap[objID] = append(linksMap[objID], state.Link{
			Target:     target,
			TargetType: targetType,
			Relation:   relation,
		})
	}
	lnkRows.Close()

	// Batch load approvals
	approvalsMap := make(map[string][]state.Approval)
	appRows, err := d.queryIn("SELECT review_object_id, subject, revision, verdict, message FROM approvals WHERE review_object_id IN (?) ORDER BY review_object_id ASC, subject ASC, revision ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query approvals: %w", err)
	}
	for appRows.Next() {
		var objID, subject, revision, verdict, message string
		if err := appRows.Scan(&objID, &subject, &revision, &verdict, &message); err != nil {
			appRows.Close()
			return nil, fmt.Errorf("projection: scan approval: %w", err)
		}
		approvalsMap[objID] = append(approvalsMap[objID], state.Approval{
			Subject:  subject,
			Revision: revision,
			Verdict:  verdict,
			Message:  message,
		})
	}
	appRows.Close()

	// Batch load ci_statuses
	ciMap := make(map[string][]state.CIStatus)
	ciRows, err := d.queryIn("SELECT review_object_id, revision, name, state, url, description, started_at, completed_at, external_id FROM ci_statuses WHERE review_object_id IN (?) ORDER BY review_object_id ASC, revision ASC, name ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query ci_statuses: %w", err)
	}
	for ciRows.Next() {
		var objID, revision, name, stateVal, url, description, startedAt, completedAt, externalID string
		if err := ciRows.Scan(&objID, &revision, &name, &stateVal, &url, &description, &startedAt, &completedAt, &externalID); err != nil {
			ciRows.Close()
			return nil, fmt.Errorf("projection: scan ci_status: %w", err)
		}
		ciMap[objID] = append(ciMap[objID], state.CIStatus{
			Revision:    revision,
			Name:        name,
			State:       stateVal,
			URL:         url,
			Description: description,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			ExternalID:  externalID,
		})
	}
	ciRows.Close()

	// Batch load unknown_ops
	unknownMap := make(map[string][]state.UnknownOp)
	uRows, err := d.queryIn("SELECT object_id, op_id, op_type, op_version FROM unknown_ops WHERE object_id IN (?) ORDER BY object_id ASC, op_index ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	for uRows.Next() {
		var objID, opID, opType string
		var opVersion int64
		if err := uRows.Scan(&objID, &opID, &opType, &opVersion); err != nil {
			uRows.Close()
			return nil, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownMap[objID] = append(unknownMap[objID], state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	uRows.Close()

	results := make([]ReviewResult, 0, len(rawReviews))
	for _, rr := range rawReviews {
		review := state.Review{
			Title:       rr.title,
			Description: rr.description,
			Status:      rr.status,
			MergeCommit: rr.mergeCommit,
			Reason:      rr.reason,
			Assignees:   assigneesMap[rr.objectID],
			Labels:      labelsMap[rr.objectID],
			Links:       linksMap[rr.objectID],
			Revisions:   revisionsMap[rr.objectID],
			Approvals:   approvalsMap[rr.objectID],
			CIStatuses:  ciMap[rr.objectID],
			UnknownOps:  unknownMap[rr.objectID],
		}
		results = append(results, ReviewResult{
			ObjectID:  rr.objectID,
			Author:    Author{Name: rr.authorName, Email: rr.authorEmail},
			CreatedAt: time.Unix(rr.createdAt, 0).UTC(),
			UpdatedAt: time.Unix(rr.updatedAt, 0).UTC(),
			Review:    review,
		})
	}

	return results, nil
}

// Issues executes a list and filter query over issues.
func (d *DB) Issues(f IssueFilter) ([]IssueResult, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	var sb strings.Builder
	var args []any

	sb.WriteString("SELECT i.object_id, i.title, i.description, i.state, i.reason, ")
	sb.WriteString("o.author_name, o.author_email, o.created_at, o.updated_at ")
	sb.WriteString("FROM issues i JOIN objects o ON o.object_id = i.object_id WHERE 1=1")

	if len(f.State) > 0 {
		sb.WriteString(" AND i.state IN (" + placeholders(len(f.State)) + ")")
		for _, s := range f.State {
			args = append(args, s)
		}
	}

	if len(f.Author) > 0 {
		sb.WriteString(" AND (o.author_email IN (" + placeholders(len(f.Author)) + ") OR o.author_name IN (" + placeholders(len(f.Author)) + "))")
		for _, a := range f.Author {
			args = append(args, a)
		}
		for _, a := range f.Author {
			args = append(args, a)
		}
	}

	if len(f.Assignee) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_object_id = i.object_id AND ia.assignee IN (" + placeholders(len(f.Assignee)) + "))")
		for _, a := range f.Assignee {
			args = append(args, state.NormalizePerson(a))
		}
	}

	if len(f.Label) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_object_id = i.object_id AND il.label IN (" + placeholders(len(f.Label)) + "))")
		for _, l := range f.Label {
			args = append(args, l)
		}
	}

	if f.Text != "" {
		sb.WriteString(" AND (i.title LIKE ? ESCAPE '\\' OR i.description LIKE ? ESCAPE '\\')")
		escaped := "%" + escapeLike(f.Text) + "%"
		args = append(args, escaped, escaped)
	}

	switch f.OrderBy {
	case OrderByCreatedAtAsc:
		sb.WriteString(" ORDER BY o.created_at ASC, i.object_id ASC")
	case OrderByCreatedAtDesc:
		sb.WriteString(" ORDER BY o.created_at DESC, i.object_id DESC")
	case OrderByUpdatedAtAsc:
		sb.WriteString(" ORDER BY o.updated_at ASC, i.object_id ASC")
	case OrderByUpdatedAtDesc:
		sb.WriteString(" ORDER BY o.updated_at DESC, i.object_id DESC")
	case OrderByTitleAsc:
		sb.WriteString(" ORDER BY i.title ASC, i.object_id ASC")
	case OrderByTitleDesc:
		sb.WriteString(" ORDER BY i.title DESC, i.object_id DESC")
	default:
		sb.WriteString(" ORDER BY o.created_at ASC, i.object_id ASC")
	}

	appendLimitOffset(&sb, &args, f.Limit, f.Offset)

	rows, err := d.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("projection: query issues: %w", err)
	}
	defer rows.Close()

	type rawIssue struct {
		objectID    string
		title       string
		description string
		state       string
		reason      string
		authorName  string
		authorEmail string
		createdAt   int64
		updatedAt   int64
	}

	var rawIssues []rawIssue
	var objectIDs []string

	for rows.Next() {
		var ri rawIssue
		if err := rows.Scan(
			&ri.objectID, &ri.title, &ri.description, &ri.state, &ri.reason,
			&ri.authorName, &ri.authorEmail, &ri.createdAt, &ri.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("projection: scan issue: %w", err)
		}
		rawIssues = append(rawIssues, ri)
		objectIDs = append(objectIDs, ri.objectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate issues: %w", err)
	}

	if len(rawIssues) == 0 {
		return []IssueResult{}, nil
	}

	// Batch load assignees
	assigneesMap := make(map[string][]string)
	asRows, err := d.queryIn("SELECT issue_object_id, assignee FROM issue_assignees WHERE issue_object_id IN (?) ORDER BY issue_object_id ASC, assignee ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query issue assignees: %w", err)
	}
	for asRows.Next() {
		var objID, assignee string
		if err := asRows.Scan(&objID, &assignee); err != nil {
			asRows.Close()
			return nil, fmt.Errorf("projection: scan issue assignee: %w", err)
		}
		assigneesMap[objID] = append(assigneesMap[objID], assignee)
	}
	asRows.Close()

	// Batch load labels
	labelsMap := make(map[string][]string)
	lblRows, err := d.queryIn("SELECT issue_object_id, label FROM issue_labels WHERE issue_object_id IN (?) ORDER BY issue_object_id ASC, label ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query issue labels: %w", err)
	}
	for lblRows.Next() {
		var objID, label string
		if err := lblRows.Scan(&objID, &label); err != nil {
			lblRows.Close()
			return nil, fmt.Errorf("projection: scan issue label: %w", err)
		}
		labelsMap[objID] = append(labelsMap[objID], label)
	}
	lblRows.Close()

	// Batch load links
	linksMap := make(map[string][]state.Link)
	lnkRows, err := d.queryIn("SELECT issue_object_id, target, target_type, relation FROM issue_links WHERE issue_object_id IN (?) ORDER BY issue_object_id ASC, target ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query issue links: %w", err)
	}
	for lnkRows.Next() {
		var objID, target, targetType, relation string
		if err := lnkRows.Scan(&objID, &target, &targetType, &relation); err != nil {
			lnkRows.Close()
			return nil, fmt.Errorf("projection: scan issue link: %w", err)
		}
		linksMap[objID] = append(linksMap[objID], state.Link{
			Target:     target,
			TargetType: targetType,
			Relation:   relation,
		})
	}
	lnkRows.Close()

	// Batch load unknown_ops
	unknownMap := make(map[string][]state.UnknownOp)
	uRows, err := d.queryIn("SELECT object_id, op_id, op_type, op_version FROM unknown_ops WHERE object_id IN (?) ORDER BY object_id ASC, op_index ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	for uRows.Next() {
		var objID, opID, opType string
		var opVersion int64
		if err := uRows.Scan(&objID, &opID, &opType, &opVersion); err != nil {
			uRows.Close()
			return nil, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownMap[objID] = append(unknownMap[objID], state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	uRows.Close()

	results := make([]IssueResult, 0, len(rawIssues))
	for _, ri := range rawIssues {
		issue := state.Issue{
			Title:       ri.title,
			Description: ri.description,
			State:       ri.state,
			Reason:      ri.reason,
			Assignees:   assigneesMap[ri.objectID],
			Labels:      labelsMap[ri.objectID],
			Links:       linksMap[ri.objectID],
			UnknownOps:  unknownMap[ri.objectID],
		}
		results = append(results, IssueResult{
			ObjectID:  ri.objectID,
			Author:    Author{Name: ri.authorName, Email: ri.authorEmail},
			CreatedAt: time.Unix(ri.createdAt, 0).UTC(),
			UpdatedAt: time.Unix(ri.updatedAt, 0).UTC(),
			Issue:     issue,
		})
	}

	return results, nil
}

// Comments executes a list and filter query over comments.
func (d *DB) Comments(f CommentFilter) ([]CommentResult, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	var sb strings.Builder
	var args []any

	sb.WriteString("SELECT c.object_id, c.subject_type, c.subject_id, c.text, c.in_reply_to, c.anchor, c.deleted, c.resolved, c.resolved_by, ")
	sb.WriteString("o.author_name, o.author_email, o.created_at, o.updated_at ")
	sb.WriteString("FROM comments c JOIN objects o ON o.object_id = c.object_id WHERE 1=1")

	if f.SubjectType != "" {
		sb.WriteString(" AND c.subject_type = ?")
		args = append(args, f.SubjectType)
	}
	if f.SubjectID != "" {
		sb.WriteString(" AND c.subject_id = ?")
		args = append(args, f.SubjectID)
	}

	if f.Resolved != nil {
		if *f.Resolved {
			sb.WriteString(" AND c.resolved = 1")
		} else {
			sb.WriteString(" AND (c.resolved = 0 OR c.resolved IS NULL)")
		}
	}

	if len(f.Author) > 0 {
		sb.WriteString(" AND (o.author_email IN (" + placeholders(len(f.Author)) + ") OR o.author_name IN (" + placeholders(len(f.Author)) + "))")
		for _, a := range f.Author {
			args = append(args, a)
		}
		for _, a := range f.Author {
			args = append(args, a)
		}
	}

	if f.Text != "" {
		sb.WriteString(" AND c.text LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(f.Text)+"%")
	}

	if !f.IncludeDeleted {
		sb.WriteString(" AND c.deleted = 0")
	}

	switch f.OrderBy {
	case OrderByCreatedAtAsc:
		sb.WriteString(" ORDER BY o.created_at ASC, c.object_id ASC")
	case OrderByCreatedAtDesc:
		sb.WriteString(" ORDER BY o.created_at DESC, c.object_id DESC")
	case OrderByUpdatedAtAsc:
		sb.WriteString(" ORDER BY o.updated_at ASC, c.object_id ASC")
	case OrderByUpdatedAtDesc:
		sb.WriteString(" ORDER BY o.updated_at DESC, c.object_id DESC")
	default:
		sb.WriteString(" ORDER BY o.created_at ASC, c.object_id ASC")
	}

	appendLimitOffset(&sb, &args, f.Limit, f.Offset)

	rows, err := d.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("projection: query comments: %w", err)
	}
	defer rows.Close()

	type rawComment struct {
		objectID    string
		subjectType string
		subjectID   string
		text        string
		inReplyTo   string
		anchor      string
		deleted     int
		resolved    sql.NullInt64
		resolvedBy  string
		authorName  string
		authorEmail string
		createdAt   int64
		updatedAt   int64
	}

	var rawComments []rawComment
	var objectIDs []string

	for rows.Next() {
		var rc rawComment
		if err := rows.Scan(
			&rc.objectID, &rc.subjectType, &rc.subjectID, &rc.text, &rc.inReplyTo, &rc.anchor, &rc.deleted, &rc.resolved, &rc.resolvedBy,
			&rc.authorName, &rc.authorEmail, &rc.createdAt, &rc.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("projection: scan comment: %w", err)
		}
		rawComments = append(rawComments, rc)
		objectIDs = append(objectIDs, rc.objectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate comments: %w", err)
	}

	if len(rawComments) == 0 {
		return []CommentResult{}, nil
	}

	// Batch load unknown_ops
	unknownMap := make(map[string][]state.UnknownOp)
	uRows, err := d.queryIn("SELECT object_id, op_id, op_type, op_version FROM unknown_ops WHERE object_id IN (?) ORDER BY object_id ASC, op_index ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	for uRows.Next() {
		var objID, opID, opType string
		var opVersion int64
		if err := uRows.Scan(&objID, &opID, &opType, &opVersion); err != nil {
			uRows.Close()
			return nil, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownMap[objID] = append(unknownMap[objID], state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	uRows.Close()

	// Determine target commit for resolutions:
	targetCommit := f.TargetCommit
	if targetCommit == "" {
		// Default to the sole code_tips entry if exactly one exists
		tipRows, err := d.db.Query("SELECT DISTINCT tip FROM code_tips WHERE tip != ''")
		if err == nil {
			var tips []string
			for tipRows.Next() {
				var tip string
				if err := tipRows.Scan(&tip); err == nil {
					tips = append(tips, tip)
				}
			}
			tipRows.Close()
			if len(tips) == 1 {
				targetCommit = tips[0]
			}
		}
	}

	// Batch load resolutions
	resolutionsMap := make(map[string][]ResolvedPosition)
	var resQuery string
	var extraArgs []any
	if targetCommit != "" {
		resQuery = "SELECT comment_object_id, side, outcome, match, path, start_line, end_line, reason FROM anchor_resolutions WHERE comment_object_id IN (?) AND target_commit = ? ORDER BY comment_object_id ASC, side ASC"
		extraArgs = []any{targetCommit}
	} else {
		resQuery = "SELECT comment_object_id, side, outcome, match, path, start_line, end_line, reason FROM anchor_resolutions WHERE comment_object_id IN (?) ORDER BY comment_object_id ASC, side ASC"
	}

	resRows, err := d.queryIn(resQuery, objectIDs, extraArgs...)
	if err != nil {
		return nil, fmt.Errorf("projection: query anchor resolutions: %w", err)
	}
	for resRows.Next() {
		var objID, side, outcome, match, path, reason string
		var startLine, endLine int
		if err := resRows.Scan(&objID, &side, &outcome, &match, &path, &startLine, &endLine, &reason); err != nil {
			resRows.Close()
			return nil, fmt.Errorf("projection: scan anchor resolution: %w", err)
		}
		resolutionsMap[objID] = append(resolutionsMap[objID], ResolvedPosition{
			Side:      side,
			Outcome:   outcome,
			Match:     match,
			Path:      path,
			StartLine: startLine,
			EndLine:   endLine,
			Reason:    reason,
		})
	}
	resRows.Close()

	results := make([]CommentResult, 0, len(rawComments))
	for _, rc := range rawComments {
		var anchor *resolve.Anchor
		if rc.anchor != "" && rc.anchor != "null" {
			a, err := resolve.ParseAnchor([]byte(rc.anchor))
			if err == nil {
				anchor = &a
			}
		}

		var resolvedPtr *bool
		if rc.resolved.Valid {
			val := rc.resolved.Int64 == 1
			resolvedPtr = &val
		}

		comment := state.Comment{
			Subject: state.CommentSubject{
				ObjectType: rc.subjectType,
				ObjectID:   rc.subjectID,
			},
			Text:       rc.text,
			InReplyTo:  rc.inReplyTo,
			Anchor:     anchor,
			Deleted:    rc.deleted == 1,
			Resolved:   resolvedPtr,
			ResolvedBy: rc.resolvedBy,
			UnknownOps: unknownMap[rc.objectID],
		}

		results = append(results, CommentResult{
			ObjectID:  rc.objectID,
			Author:    Author{Name: rc.authorName, Email: rc.authorEmail},
			CreatedAt: time.Unix(rc.createdAt, 0).UTC(),
			UpdatedAt: time.Unix(rc.updatedAt, 0).UTC(),
			Comment:   comment,
			Resolved:  resolutionsMap[rc.objectID],
		})
	}

	return results, nil
}

// Objects executes a cross-type summary query over collaborative objects.
func (d *DB) Objects(f ObjectFilter) ([]ObjectResult, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	var sb strings.Builder
	var args []any

	sb.WriteString("SELECT o.object_id, o.object_type, o.op_count, o.last_op_id, ")
	sb.WriteString("o.author_name, o.author_email, o.created_at, o.updated_at ")
	sb.WriteString("FROM objects o WHERE 1=1")

	if len(f.Type) > 0 {
		sb.WriteString(" AND o.object_type IN (" + placeholders(len(f.Type)) + ")")
		for _, t := range f.Type {
			args = append(args, t)
		}
	}

	if len(f.Author) > 0 {
		sb.WriteString(" AND (o.author_email IN (" + placeholders(len(f.Author)) + ") OR o.author_name IN (" + placeholders(len(f.Author)) + "))")
		for _, a := range f.Author {
			args = append(args, a)
		}
		for _, a := range f.Author {
			args = append(args, a)
		}
	}

	if f.Text != "" {
		escaped := "%" + escapeLike(f.Text) + "%"
		sb.WriteString(" AND (")
		sb.WriteString("EXISTS (SELECT 1 FROM reviews r WHERE r.object_id = o.object_id AND (r.title LIKE ? ESCAPE '\\' OR r.description LIKE ? ESCAPE '\\'))")
		sb.WriteString(" OR EXISTS (SELECT 1 FROM issues i WHERE i.object_id = o.object_id AND (i.title LIKE ? ESCAPE '\\' OR i.description LIKE ? ESCAPE '\\'))")
		sb.WriteString(" OR EXISTS (SELECT 1 FROM comments c WHERE c.object_id = o.object_id AND c.text LIKE ? ESCAPE '\\')")
		sb.WriteString(" OR EXISTS (SELECT 1 FROM projects p WHERE p.object_id = o.object_id AND (p.title LIKE ? ESCAPE '\\' OR p.description LIKE ? ESCAPE '\\'))")
		sb.WriteString(" OR EXISTS (SELECT 1 FROM cycles cy WHERE cy.object_id = o.object_id AND (cy.title LIKE ? ESCAPE '\\' OR cy.description LIKE ? ESCAPE '\\'))")
		sb.WriteString(" OR EXISTS (SELECT 1 FROM repos rp WHERE rp.object_id = o.object_id AND rp.slug LIKE ? ESCAPE '\\')")
		sb.WriteString(")")
		args = append(args, escaped, escaped, escaped, escaped, escaped, escaped, escaped, escaped, escaped, escaped)
	}

	if !f.IncludeDeleted {
		sb.WriteString(" AND (o.object_type != 'comment' OR EXISTS (SELECT 1 FROM comments c WHERE c.object_id = o.object_id AND c.deleted = 0))")
	}

	switch f.OrderBy {
	case OrderByCreatedAtAsc:
		sb.WriteString(" ORDER BY o.created_at ASC, o.object_id ASC")
	case OrderByCreatedAtDesc:
		sb.WriteString(" ORDER BY o.created_at DESC, o.object_id DESC")
	case OrderByUpdatedAtAsc:
		sb.WriteString(" ORDER BY o.updated_at ASC, o.object_id ASC")
	case OrderByUpdatedAtDesc:
		sb.WriteString(" ORDER BY o.updated_at DESC, o.object_id DESC")
	default:
		sb.WriteString(" ORDER BY o.created_at ASC, o.object_id ASC")
	}

	appendLimitOffset(&sb, &args, f.Limit, f.Offset)

	rows, err := d.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("projection: query objects: %w", err)
	}
	defer rows.Close()

	var results []ObjectResult
	for rows.Next() {
		var or ObjectResult
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&or.ObjectID, &or.ObjectType, &or.OpCount, &or.LastOpID,
			&or.Author.Name, &or.Author.Email, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("projection: scan object: %w", err)
		}
		or.CreatedAt = time.Unix(createdAt, 0).UTC()
		or.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		results = append(results, or)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate objects: %w", err)
	}

	if results == nil {
		return []ObjectResult{}, nil
	}

	return results, nil
}

// Object fetches summary metadata for a single collaborative object by its ID, returning ErrNotFound if not found.
func (d *DB) Object(objectID string) (ObjectResult, error) {
	if d == nil || d.db == nil {
		return ObjectResult{}, fmt.Errorf("projection: database is closed")
	}
	if objectID == "" {
		return ObjectResult{}, ErrNotFound
	}

	var (
		res          ObjectResult
		createdAtSec int64
		updatedAtSec int64
	)

	err := d.db.QueryRow(`
		SELECT object_id, object_type, op_count, last_op_id, author_name, author_email, created_at, updated_at
		FROM objects
		WHERE object_id = ?
	`, objectID).Scan(
		&res.ObjectID,
		&res.ObjectType,
		&res.OpCount,
		&res.LastOpID,
		&res.Author.Name,
		&res.Author.Email,
		&createdAtSec,
		&updatedAtSec,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return ObjectResult{}, ErrNotFound
		}
		return ObjectResult{}, fmt.Errorf("projection: query object %s: %w", objectID, err)
	}

	res.CreatedAt = time.Unix(createdAtSec, 0).UTC()
	res.UpdatedAt = time.Unix(updatedAtSec, 0).UTC()

	return res, nil
}

// Threads retrieves and structures all comments attached to a subject into a comment reply forest.
func (d *DB) Threads(subjectType, subjectID string) ([]state.CommentThread, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	comments, err := d.Comments(CommentFilter{
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		IncludeDeleted: true,
		OrderBy:        OrderByCreatedAtAsc,
	})
	if err != nil {
		return nil, fmt.Errorf("projection: threads query comments: %w", err)
	}

	if len(comments) == 0 {
		return nil, nil
	}

	type commentNode struct {
		res       CommentResult
		origIndex int
	}

	nodeMap := make(map[string]commentNode, len(comments))
	for i, c := range comments {
		nodeMap[c.ObjectID] = commentNode{res: c, origIndex: i}
	}

	// Build parent -> children map and identify roots
	parentMap := make(map[string]string, len(comments))
	for _, c := range comments {
		if c.Comment.InReplyTo != "" && c.Comment.InReplyTo != c.ObjectID {
			if _, parentExists := nodeMap[c.Comment.InReplyTo]; parentExists {
				parentMap[c.ObjectID] = c.Comment.InReplyTo
			}
		}
	}

	// Cycle detection
	visitState := make(map[string]int, len(comments))
	inCycle := make(map[string]bool)

	for _, c := range comments {
		objID := c.ObjectID
		if visitState[objID] != 0 {
			continue
		}
		var path []string
		curr := objID
		for curr != "" {
			if visitState[curr] == 1 {
				cycleStart := false
				for _, nodeID := range path {
					if nodeID == curr {
						cycleStart = true
					}
					if cycleStart {
						inCycle[nodeID] = true
					}
				}
				break
			}
			if visitState[curr] == 2 {
				break
			}
			visitState[curr] = 1
			path = append(path, curr)
			curr = parentMap[curr]
		}
		for _, nodeID := range path {
			visitState[nodeID] = 2
		}
	}

	for objID := range inCycle {
		delete(parentMap, objID)
	}

	childrenMap := make(map[string][]string, len(comments))
	var rootIDs []string
	for _, c := range comments {
		parentID, hasParent := parentMap[c.ObjectID]
		if hasParent {
			childrenMap[parentID] = append(childrenMap[parentID], c.ObjectID)
		} else {
			rootIDs = append(rootIDs, c.ObjectID)
		}
	}

	// Sibling sorting helper: created_at ASC, origIndex ASC, object_id ASC
	sortIDs := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			nI := nodeMap[ids[i]]
			nJ := nodeMap[ids[j]]
			if !nI.res.CreatedAt.Equal(nJ.res.CreatedAt) {
				return nI.res.CreatedAt.Before(nJ.res.CreatedAt)
			}
			if nI.origIndex != nJ.origIndex {
				return nI.origIndex < nJ.origIndex
			}
			return ids[i] < ids[j]
		})
	}

	sortIDs(rootIDs)

	var buildTree func(id string) state.CommentThread
	buildTree = func(id string) state.CommentThread {
		chIDs := childrenMap[id]
		sortIDs(chIDs)
		replies := make([]state.CommentThread, 0, len(chIDs))
		for _, chID := range chIDs {
			replies = append(replies, buildTree(chID))
		}
		node := nodeMap[id]
		return state.CommentThread{
			ObjectID:   id,
			Comment:    node.res.Comment,
			Replies:    replies,
			UnknownOps: node.res.Comment.UnknownOps,
		}
	}

	threads := make([]state.CommentThread, 0, len(rootIDs))
	for _, rootID := range rootIDs {
		threads = append(threads, buildTree(rootID))
	}

	return threads, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func appendLimitOffset(sb *strings.Builder, args *[]any, limit, offset int) {
	if limit > 0 && offset > 0 {
		sb.WriteString(" LIMIT ? OFFSET ?")
		*args = append(*args, limit, offset)
	} else if limit > 0 {
		sb.WriteString(" LIMIT ?")
		*args = append(*args, limit)
	} else if offset > 0 {
		sb.WriteString(" LIMIT -1 OFFSET ?")
		*args = append(*args, offset)
	}
}

// queryIn executes a query where placeholder `?` in `WHERE ... IN (?)` is expanded
// for the slice of string ids.
func (d *DB) queryIn(queryPattern string, ids []string, extraArgs ...any) (*sql.Rows, error) {
	if len(ids) == 0 {
		// Return an empty result set by querying with a false condition
		emptyQuery := strings.Replace(queryPattern, "(?)", "(NULL)", 1)
		return d.db.Query(emptyQuery, extraArgs...)
	}

	ph := placeholders(len(ids))
	query := strings.Replace(queryPattern, "(?)", "("+ph+")", 1)

	var args []any
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, extraArgs...)

	return d.db.Query(query, args...)
}

// Frontier returns the observed frontier of op commits for the given object ID:
// the op commits with no child dependencies within that object.
func (d *DB) Frontier(objectID string) ([]string, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}
	if objectID == "" {
		return nil, nil
	}

	rows, err := d.db.Query("SELECT op_id, parents FROM ops WHERE object_id = ? ORDER BY op_id ASC", objectID)
	if err != nil {
		return nil, fmt.Errorf("projection: query ops for frontier %s: %w", objectID, err)
	}
	defer rows.Close()

	allOps := make(map[string]bool)
	hasChildren := make(map[string]bool)

	for rows.Next() {
		var opID, parentsJSON string
		if err := rows.Scan(&opID, &parentsJSON); err != nil {
			return nil, fmt.Errorf("projection: scan op for frontier: %w", err)
		}
		allOps[opID] = true
		if len(parentsJSON) > 0 {
			var parents []string
			if err := json.Unmarshal([]byte(parentsJSON), &parents); err == nil {
				for _, p := range parents {
					hasChildren[p] = true
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate ops for frontier: %w", err)
	}

	var frontier []string
	for opID := range allOps {
		if !hasChildren[opID] {
			frontier = append(frontier, opID)
		}
	}
	sort.Strings(frontier)

	return frontier, nil
}

// Review fetches a single review by its object ID, returning ErrNotFound if not found.
func (d *DB) Review(objectID string) (ReviewResult, error) {
	if d == nil || d.db == nil {
		return ReviewResult{}, fmt.Errorf("projection: database is closed")
	}
	if objectID == "" {
		return ReviewResult{}, ErrNotFound
	}

	var rr struct {
		objectID    string
		title       string
		description string
		status      string
		mergeCommit string
		reason      string
		authorName  string
		authorEmail string
		createdAt   int64
		updatedAt   int64
	}

	err := d.db.QueryRow(
		"SELECT r.object_id, r.title, r.description, r.status, r.merge_commit, r.reason, o.author_name, o.author_email, o.created_at, o.updated_at FROM reviews r JOIN objects o ON o.object_id = r.object_id WHERE r.object_id = ?",
		objectID,
	).Scan(
		&rr.objectID, &rr.title, &rr.description, &rr.status, &rr.mergeCommit, &rr.reason,
		&rr.authorName, &rr.authorEmail, &rr.createdAt, &rr.updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReviewResult{}, ErrNotFound
		}
		return ReviewResult{}, fmt.Errorf("projection: query review %s: %w", objectID, err)
	}

	var revisions []state.Revision
	revRows, err := d.db.Query("SELECT base, head FROM review_revisions WHERE review_object_id = ? ORDER BY revision_index ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query review revisions: %w", err)
	}
	defer revRows.Close()
	for revRows.Next() {
		var base, head string
		if err := revRows.Scan(&base, &head); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan review revision: %w", err)
		}
		revisions = append(revisions, state.Revision{Base: base, Head: head})
	}
	if err := revRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate review revisions: %w", err)
	}

	var assignees []string
	asRows, err := d.db.Query("SELECT assignee FROM review_assignees WHERE review_object_id = ? ORDER BY assignee ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query review assignees: %w", err)
	}
	defer asRows.Close()
	for asRows.Next() {
		var assignee string
		if err := asRows.Scan(&assignee); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan review assignee: %w", err)
		}
		assignees = append(assignees, assignee)
	}
	if err := asRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate review assignees: %w", err)
	}

	var labels []string
	lblRows, err := d.db.Query("SELECT label FROM review_labels WHERE review_object_id = ? ORDER BY label ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query review labels: %w", err)
	}
	defer lblRows.Close()
	for lblRows.Next() {
		var label string
		if err := lblRows.Scan(&label); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan review label: %w", err)
		}
		labels = append(labels, label)
	}
	if err := lblRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate review labels: %w", err)
	}

	var links []state.Link
	lnkRows, err := d.db.Query("SELECT target, target_type, relation FROM review_links WHERE review_object_id = ? ORDER BY target ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query review links: %w", err)
	}
	defer lnkRows.Close()
	for lnkRows.Next() {
		var target, targetType, relation string
		if err := lnkRows.Scan(&target, &targetType, &relation); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan review link: %w", err)
		}
		links = append(links, state.Link{
			Target:     target,
			TargetType: targetType,
			Relation:   relation,
		})
	}
	if err := lnkRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate review links: %w", err)
	}

	var approvals []state.Approval
	appRows, err := d.db.Query("SELECT subject, revision, verdict, message FROM approvals WHERE review_object_id = ? ORDER BY subject ASC, revision ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query approvals: %w", err)
	}
	defer appRows.Close()
	for appRows.Next() {
		var subject, revision, verdict, message string
		if err := appRows.Scan(&subject, &revision, &verdict, &message); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan approval: %w", err)
		}
		approvals = append(approvals, state.Approval{
			Subject:  subject,
			Revision: revision,
			Verdict:  verdict,
			Message:  message,
		})
	}
	if err := appRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate approvals: %w", err)
	}

	var ciStatuses []state.CIStatus
	ciRows, err := d.db.Query("SELECT revision, name, state, url, description, started_at, completed_at, external_id FROM ci_statuses WHERE review_object_id = ? ORDER BY revision ASC, name ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query ci_statuses: %w", err)
	}
	defer ciRows.Close()
	for ciRows.Next() {
		var revision, name, stateVal, url, description, startedAt, completedAt, externalID string
		if err := ciRows.Scan(&revision, &name, &stateVal, &url, &description, &startedAt, &completedAt, &externalID); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan ci_status: %w", err)
		}
		ciStatuses = append(ciStatuses, state.CIStatus{
			Revision:    revision,
			Name:        name,
			State:       stateVal,
			URL:         url,
			Description: description,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			ExternalID:  externalID,
		})
	}
	if err := ciRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate ci_statuses: %w", err)
	}

	var unknownOps []state.UnknownOp
	uRows, err := d.db.Query("SELECT op_id, op_type, op_version FROM unknown_ops WHERE object_id = ? ORDER BY op_index ASC", objectID)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	defer uRows.Close()
	for uRows.Next() {
		var opID, opType string
		var opVersion int64
		if err := uRows.Scan(&opID, &opType, &opVersion); err != nil {
			return ReviewResult{}, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownOps = append(unknownOps, state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	if err := uRows.Err(); err != nil {
		return ReviewResult{}, fmt.Errorf("projection: iterate unknown_ops: %w", err)
	}

	return ReviewResult{
		ObjectID:  rr.objectID,
		Author:    Author{Name: rr.authorName, Email: rr.authorEmail},
		CreatedAt: time.Unix(rr.createdAt, 0).UTC(),
		UpdatedAt: time.Unix(rr.updatedAt, 0).UTC(),
		Review: state.Review{
			Title:       rr.title,
			Description: rr.description,
			Status:      rr.status,
			MergeCommit: rr.mergeCommit,
			Reason:      rr.reason,
			Assignees:   assignees,
			Labels:      labels,
			Links:       links,
			Revisions:   revisions,
			Approvals:   approvals,
			CIStatuses:  ciStatuses,
			UnknownOps:  unknownOps,
		},
	}, nil
}

// Issue fetches a single issue by its object ID, returning ErrNotFound if not found.
func (d *DB) Issue(objectID string) (IssueResult, error) {
	if d == nil || d.db == nil {
		return IssueResult{}, fmt.Errorf("projection: database is closed")
	}
	if objectID == "" {
		return IssueResult{}, ErrNotFound
	}

	var ri struct {
		objectID    string
		title       string
		description string
		state       string
		reason      string
		authorName  string
		authorEmail string
		createdAt   int64
		updatedAt   int64
	}

	err := d.db.QueryRow(
		"SELECT i.object_id, i.title, i.description, i.state, i.reason, o.author_name, o.author_email, o.created_at, o.updated_at FROM issues i JOIN objects o ON o.object_id = i.object_id WHERE i.object_id = ?",
		objectID,
	).Scan(
		&ri.objectID, &ri.title, &ri.description, &ri.state, &ri.reason,
		&ri.authorName, &ri.authorEmail, &ri.createdAt, &ri.updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IssueResult{}, ErrNotFound
		}
		return IssueResult{}, fmt.Errorf("projection: query issue %s: %w", objectID, err)
	}

	var assignees []string
	asRows, err := d.db.Query("SELECT assignee FROM issue_assignees WHERE issue_object_id = ? ORDER BY assignee ASC", objectID)
	if err != nil {
		return IssueResult{}, fmt.Errorf("projection: query issue assignees: %w", err)
	}
	defer asRows.Close()
	for asRows.Next() {
		var assignee string
		if err := asRows.Scan(&assignee); err != nil {
			return IssueResult{}, fmt.Errorf("projection: scan issue assignee: %w", err)
		}
		assignees = append(assignees, assignee)
	}
	if err := asRows.Err(); err != nil {
		return IssueResult{}, fmt.Errorf("projection: iterate issue assignees: %w", err)
	}

	var labels []string
	lblRows, err := d.db.Query("SELECT label FROM issue_labels WHERE issue_object_id = ? ORDER BY label ASC", objectID)
	if err != nil {
		return IssueResult{}, fmt.Errorf("projection: query issue labels: %w", err)
	}
	defer lblRows.Close()
	for lblRows.Next() {
		var label string
		if err := lblRows.Scan(&label); err != nil {
			return IssueResult{}, fmt.Errorf("projection: scan issue label: %w", err)
		}
		labels = append(labels, label)
	}
	if err := lblRows.Err(); err != nil {
		return IssueResult{}, fmt.Errorf("projection: iterate issue labels: %w", err)
	}

	var links []state.Link
	lnkRows, err := d.db.Query("SELECT target, target_type, relation FROM issue_links WHERE issue_object_id = ? ORDER BY target ASC", objectID)
	if err != nil {
		return IssueResult{}, fmt.Errorf("projection: query issue links: %w", err)
	}
	defer lnkRows.Close()
	for lnkRows.Next() {
		var target, targetType, relation string
		if err := lnkRows.Scan(&target, &targetType, &relation); err != nil {
			return IssueResult{}, fmt.Errorf("projection: scan issue link: %w", err)
		}
		links = append(links, state.Link{
			Target:     target,
			TargetType: targetType,
			Relation:   relation,
		})
	}
	if err := lnkRows.Err(); err != nil {
		return IssueResult{}, fmt.Errorf("projection: iterate issue links: %w", err)
	}

	var unknownOps []state.UnknownOp
	uRows, err := d.db.Query("SELECT op_id, op_type, op_version FROM unknown_ops WHERE object_id = ? ORDER BY op_index ASC", objectID)
	if err != nil {
		return IssueResult{}, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	defer uRows.Close()
	for uRows.Next() {
		var opID, opType string
		var opVersion int64
		if err := uRows.Scan(&opID, &opType, &opVersion); err != nil {
			return IssueResult{}, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownOps = append(unknownOps, state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	if err := uRows.Err(); err != nil {
		return IssueResult{}, fmt.Errorf("projection: iterate unknown_ops: %w", err)
	}

	return IssueResult{
		ObjectID:  ri.objectID,
		Author:    Author{Name: ri.authorName, Email: ri.authorEmail},
		CreatedAt: time.Unix(ri.createdAt, 0).UTC(),
		UpdatedAt: time.Unix(ri.updatedAt, 0).UTC(),
		Issue: state.Issue{
			Title:       ri.title,
			Description: ri.description,
			State:       ri.state,
			Reason:      ri.reason,
			Assignees:   assignees,
			Labels:      labels,
			Links:       links,
			UnknownOps:  unknownOps,
		},
	}, nil
}

// Repos returns all registered repositories from the repository registry in the projection cache.
func (d *DB) Repos() ([]state.RepoEntry, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	rows, err := d.db.Query("SELECT object_id, slug, is_workspace FROM repos ORDER BY object_id ASC")
	if err != nil {
		return nil, fmt.Errorf("projection: query repos: %w", err)
	}
	defer rows.Close()

	type rawRepo struct {
		objectID    string
		slug        string
		isWorkspace int
	}

	var rawRepos []rawRepo
	var objectIDs []string

	for rows.Next() {
		var rr rawRepo
		if err := rows.Scan(&rr.objectID, &rr.slug, &rr.isWorkspace); err != nil {
			return nil, fmt.Errorf("projection: scan repo: %w", err)
		}
		rawRepos = append(rawRepos, rr)
		objectIDs = append(objectIDs, rr.objectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: iterate repos: %w", err)
	}

	if len(rawRepos) == 0 {
		return []state.RepoEntry{}, nil
	}

	// Batch load remotes
	remotesMap := make(map[string][]string)
	remRows, err := d.queryIn("SELECT repo_object_id, remote FROM repo_remotes WHERE repo_object_id IN (?) ORDER BY repo_object_id ASC, remote ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query repo remotes: %w", err)
	}
	for remRows.Next() {
		var objID, remote string
		if err := remRows.Scan(&objID, &remote); err != nil {
			remRows.Close()
			return nil, fmt.Errorf("projection: scan repo remote: %w", err)
		}
		remotesMap[objID] = append(remotesMap[objID], remote)
	}
	remRows.Close()

	// Batch load unknown_ops
	unknownMap := make(map[string][]state.UnknownOp)
	uRows, err := d.queryIn("SELECT object_id, op_id, op_type, op_version FROM unknown_ops WHERE object_id IN (?) ORDER BY object_id ASC, op_index ASC", objectIDs)
	if err != nil {
		return nil, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	for uRows.Next() {
		var objID, opID, opType string
		var opVersion int64
		if err := uRows.Scan(&objID, &opID, &opType, &opVersion); err != nil {
			uRows.Close()
			return nil, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownMap[objID] = append(unknownMap[objID], state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	uRows.Close()

	results := make([]state.RepoEntry, 0, len(rawRepos))
	for _, rr := range rawRepos {
		results = append(results, state.RepoEntry{
			RepoID:      rr.objectID,
			Slug:        rr.slug,
			Remotes:     remotesMap[rr.objectID],
			IsWorkspace: rr.isWorkspace == 1,
			UnknownOps:  unknownMap[rr.objectID],
		})
	}

	return results, nil
}

// Repo fetches a single repository registry entry by its object ID, returning ErrNotFound if not found.
func (d *DB) Repo(objectID string) (state.RepoEntry, error) {
	if d == nil || d.db == nil {
		return state.RepoEntry{}, fmt.Errorf("projection: database is closed")
	}
	if objectID == "" {
		return state.RepoEntry{}, ErrNotFound
	}

	var rr struct {
		objectID    string
		slug        string
		isWorkspace int
	}

	err := d.db.QueryRow(
		"SELECT object_id, slug, is_workspace FROM repos WHERE object_id = ?",
		objectID,
	).Scan(&rr.objectID, &rr.slug, &rr.isWorkspace)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state.RepoEntry{}, ErrNotFound
		}
		return state.RepoEntry{}, fmt.Errorf("projection: query repo %s: %w", objectID, err)
	}

	var remotes []string
	remRows, err := d.db.Query("SELECT remote FROM repo_remotes WHERE repo_object_id = ? ORDER BY remote ASC", objectID)
	if err != nil {
		return state.RepoEntry{}, fmt.Errorf("projection: query repo remotes: %w", err)
	}
	defer remRows.Close()
	for remRows.Next() {
		var remote string
		if err := remRows.Scan(&remote); err != nil {
			return state.RepoEntry{}, fmt.Errorf("projection: scan repo remote: %w", err)
		}
		remotes = append(remotes, remote)
	}
	if err := remRows.Err(); err != nil {
		return state.RepoEntry{}, fmt.Errorf("projection: iterate repo remotes: %w", err)
	}

	var unknownOps []state.UnknownOp
	uRows, err := d.db.Query("SELECT op_id, op_type, op_version FROM unknown_ops WHERE object_id = ? ORDER BY op_index ASC", objectID)
	if err != nil {
		return state.RepoEntry{}, fmt.Errorf("projection: query unknown_ops: %w", err)
	}
	defer uRows.Close()
	for uRows.Next() {
		var opID, opType string
		var opVersion int64
		if err := uRows.Scan(&opID, &opType, &opVersion); err != nil {
			return state.RepoEntry{}, fmt.Errorf("projection: scan unknown op: %w", err)
		}
		unknownOps = append(unknownOps, state.UnknownOp{
			Commit:    opID,
			OpType:    opType,
			OpVersion: opVersion,
		})
	}
	if err := uRows.Err(); err != nil {
		return state.RepoEntry{}, fmt.Errorf("projection: iterate unknown_ops: %w", err)
	}

	return state.RepoEntry{
		RepoID:      rr.objectID,
		Slug:        rr.slug,
		Remotes:     remotes,
		IsWorkspace: rr.isWorkspace == 1,
		UnknownOps:  unknownOps,
	}, nil
}
