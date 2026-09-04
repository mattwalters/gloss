package writ

import (
	"context"
	"fmt"
	"strings"

	"github.com/writtendev/writ/engine/projection"
)

// Re-exported filter, ordering, and grouping types.
type (
	// ReviewFilter specifies filter criteria when querying reviews.
	ReviewFilter = projection.ReviewFilter

	// IssueFilter specifies filter criteria when querying issues.
	IssueFilter = projection.IssueFilter

	// CommentFilter specifies filter criteria when querying comments.
	CommentFilter = projection.CommentFilter

	// ObjectFilter specifies filter criteria when querying collaborative objects cross-type.
	ObjectFilter = projection.ObjectFilter

	// OrderBy specifies the sort order for query results.
	OrderBy = projection.OrderBy

	// GroupKey represents a grouping dimension for issues.
	GroupKey = projection.GroupKey

	// Group represents a collection of issues belonging to a single group key.
	Group = projection.Group

	// ReviewResult represents a code review object along with its authorship and timestamps.
	ReviewResult = projection.ReviewResult

	// IssueResult represents an issue object along with its authorship and timestamps.
	IssueResult = projection.IssueResult

	// WorkflowStateFilter specifies filter criteria when querying workflow states.
	WorkflowStateFilter = projection.WorkflowStateFilter

	// WorkflowStateResult represents a workflow state object along with its authorship and timestamps.
	WorkflowStateResult = projection.WorkflowStateResult

	// LabelFilter specifies filter criteria when querying labels.
	LabelFilter = projection.LabelFilter

	// LabelResult represents a label object along with its authorship and timestamps.
	LabelResult = projection.LabelResult

	// DocumentFilter specifies filter criteria when querying documents.
	DocumentFilter = projection.DocumentFilter

	// DocumentResult represents a document object along with its authorship, timestamps, and ordered sections.
	DocumentResult = projection.DocumentResult

	// SectionResult represents a document section object along with its authorship and timestamps.
	SectionResult = projection.SectionResult

	// SettingsResult represents the repository settings along with its object ID and timestamps.
	SettingsResult = projection.SettingsResult

	// CommentResult represents a comment object along with its authorship, timestamps, and anchor resolutions.
	CommentResult = projection.CommentResult

	// ObjectResult represents summary metadata for any collaborative object cross-type.
	ObjectResult = projection.ObjectResult

	// ResolvedPosition describes the resolved anchor position for a comment side.
	ResolvedPosition = projection.ResolvedPosition

	// Author holds the author display name and email address derived from an object's operations.
	Author = projection.Author

	// RefreshStats reports the work performed during a Refresh pass.
	RefreshStats = projection.Stats

	// ObjectChange describes the modifications made to a collaborative object in an incremental refresh batch.
	ObjectChange = projection.ObjectChange
)

const (
	// OrderByCreatedAtAsc sorts results by created_at ascending.
	OrderByCreatedAtAsc = projection.OrderByCreatedAtAsc

	// OrderByCreatedAtDesc sorts results by created_at descending.
	OrderByCreatedAtDesc = projection.OrderByCreatedAtDesc

	// OrderByUpdatedAtAsc sorts results by updated_at ascending.
	OrderByUpdatedAtAsc = projection.OrderByUpdatedAtAsc

	// OrderByUpdatedAtDesc sorts results by updated_at descending.
	OrderByUpdatedAtDesc = projection.OrderByUpdatedAtDesc

	// OrderByTitleAsc sorts results by title ascending.
	OrderByTitleAsc = projection.OrderByTitleAsc

	// OrderByTitleDesc sorts results by title descending.
	OrderByTitleDesc = projection.OrderByTitleDesc

	// OrderByPriorityAsc sorts results by priority ascending (none < low < medium < high < urgent).
	OrderByPriorityAsc = projection.OrderByPriorityAsc

	// OrderByPriorityDesc sorts results by priority descending (urgent > high > medium > low > none).
	OrderByPriorityDesc = projection.OrderByPriorityDesc

	// OrderByPositionAsc sorts results by position ascending.
	OrderByPositionAsc = projection.OrderByPositionAsc

	// OrderByPositionDesc sorts results by position descending.
	OrderByPositionDesc = projection.OrderByPositionDesc

	// OrderByEstimateAsc sorts results by estimate ascending.
	OrderByEstimateAsc = projection.OrderByEstimateAsc

	// OrderByEstimateDesc sorts results by estimate descending.
	OrderByEstimateDesc = projection.OrderByEstimateDesc

	// GroupByState groups issues by their state string.
	GroupByState = projection.GroupByState

	// GroupByAssignee groups issues by assignee.
	GroupByAssignee = projection.GroupByAssignee

	// GroupByPriority groups issues by priority.
	GroupByPriority = projection.GroupByPriority
)

// Query provides read queries over collaborative objects, served from the projection SQLite cache.
type Query struct {
	store *Store
}

// Reviews executes a list and filter query over code reviews.
func (q *Query) Reviews(f ReviewFilter) ([]ReviewResult, error) {
	if q == nil || q.store == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	if len(f.Label) > 0 && q.store.Workspace != nil && q.store.Workspace.IsConfigured() {
		labels, err := q.Labels(LabelFilter{})
		if err == nil {
			expanded := make([]string, len(f.Label))
			copy(expanded, f.Label)
			seen := make(map[string]bool)
			for _, l := range expanded {
				seen[l] = true
			}
			for _, req := range f.Label {
				target := req
				if idx := strings.Index(req, "#"); idx >= 0 && idx < len(req)-1 {
					target = req[idx+1:]
				}
				for _, l := range labels {
					if strings.EqualFold(l.Label.Name, req) || strings.EqualFold(l.Label.Name, target) {
						if !seen[l.ObjectID] {
							seen[l.ObjectID] = true
							expanded = append(expanded, l.ObjectID)
						}
					}
					if l.ObjectID == req || l.ObjectID == target || strings.HasPrefix(l.ObjectID, target) {
						if !seen[l.Label.Name] {
							seen[l.Label.Name] = true
							expanded = append(expanded, l.Label.Name)
						}
					}
				}
			}
			f.Label = expanded
		}
	}
	return q.store.projection.Reviews(f)
}

func (q *Query) targetStoreForIssues(ctx context.Context) (*Store, error) {
	if q == nil || q.store == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}
	if q.store.Workspace != nil && q.store.Workspace.IsConfigured() {
		wsStore, err := q.store.Workspace.getStore(ctx)
		if err != nil {
			return nil, err
		}
		return wsStore, nil
	}
	return q.store, nil
}

// Issues executes a list and filter query over issues.
func (q *Query) Issues(f IssueFilter) ([]IssueResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return nil, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.Issues(f)
}

// Comments executes a list and filter query over comments.
func (q *Query) Comments(f CommentFilter) ([]CommentResult, error) {
	if q == nil || q.store == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return q.store.projection.Comments(f)
}

// Objects executes a cross-type summary query over collaborative objects.
func (q *Query) Objects(f ObjectFilter) ([]ObjectResult, error) {
	if q == nil || q.store == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return q.store.projection.Objects(f)
}

// Threads retrieves and structures all comments attached to a subject into a comment reply forest.
func (q *Query) Threads(subjectType, subjectID string) ([]CommentThread, error) {
	target := q.store
	if subjectType == "issue" {
		var err error
		target, err = q.targetStoreForIssues(context.Background())
		if err != nil {
			return nil, err
		}
	}
	if target == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.Threads(subjectType, subjectID)
}

// GroupIssues partitions issues matching the filter by the specified grouping key.
func (q *Query) GroupIssues(by GroupKey, f IssueFilter) ([]Group, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return nil, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.GroupIssues(by, f)
}

// Review fetches a single review by its object ID, returning ErrNotFound if not found.
func (q *Query) Review(id string) (ReviewResult, error) {
	if q == nil || q.store == nil {
		return ReviewResult{}, fmt.Errorf("writ: store is nil")
	}
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return ReviewResult{}, err
	}
	return q.store.projection.Review(id)
}

// Issue fetches a single issue by its object ID, returning ErrNotFound if not found.
func (q *Query) Issue(id string) (IssueResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return IssueResult{}, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return IssueResult{}, err
	}
	return target.projection.Issue(id)
}

// Object fetches summary metadata for a single collaborative object by its ID, returning ErrNotFound if not found.
func (q *Query) Object(id string) (ObjectResult, error) {
	if q == nil || q.store == nil {
		return ObjectResult{}, fmt.Errorf("writ: store is nil")
	}
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return ObjectResult{}, err
	}
	return q.store.projection.Object(id)
}

// WorkflowStates executes a list and filter query over workflow states.
func (q *Query) WorkflowStates(f WorkflowStateFilter) ([]WorkflowStateResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return nil, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.WorkflowStates(f)
}

// WorkflowState fetches a single workflow state by its object ID, returning ErrNotFound if not found.
func (q *Query) WorkflowState(id string) (WorkflowStateResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return WorkflowStateResult{}, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return WorkflowStateResult{}, err
	}
	return target.projection.WorkflowState(id)
}

// Labels executes a list and filter query over labels.
func (q *Query) Labels(f LabelFilter) ([]LabelResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return nil, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.Labels(f)
}

// Label fetches a single label by its object ID, returning ErrNotFound if not found.
func (q *Query) Label(id string) (LabelResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return LabelResult{}, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return LabelResult{}, err
	}
	return target.projection.Label(id)
}

// Documents executes a list and filter query over documents.
func (q *Query) Documents(f DocumentFilter) ([]DocumentResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return nil, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return nil, err
	}
	return target.projection.Documents(f)
}

// Document fetches a single document by its object ID, returning ErrNotFound if not found.
func (q *Query) Document(id string) (DocumentResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return DocumentResult{}, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return DocumentResult{}, err
	}
	return target.projection.Document(id)
}

// Section fetches a single document section by its object ID, returning ErrNotFound if not found.
func (q *Query) Section(id string) (SectionResult, error) {
	target, err := q.targetStoreForIssues(context.Background())
	if err != nil {
		return SectionResult{}, err
	}
	if err := target.maybeAutoRefresh(context.Background()); err != nil {
		return SectionResult{}, err
	}
	return target.projection.Section(id)
}

// Settings returns the current repository settings.
func (q *Query) Settings() (SettingsResult, error) {
	if err := q.store.maybeAutoRefresh(context.Background()); err != nil {
		return SettingsResult{}, err
	}
	return q.store.projection.Settings()
}
