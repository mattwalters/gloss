package projection

import (
	"strings"
)

// OrderBy specifies the sort order for query results.
type OrderBy string

const (
	OrderByCreatedAtAsc  OrderBy = "created_at_asc"
	OrderByCreatedAtDesc OrderBy = "created_at_desc"
	OrderByUpdatedAtAsc  OrderBy = "updated_at_asc"
	OrderByUpdatedAtDesc OrderBy = "updated_at_desc"
	OrderByTitleAsc      OrderBy = "title_asc"
	OrderByTitleDesc     OrderBy = "title_desc"
)

// ReviewFilter specifies filter criteria when querying reviews.
type ReviewFilter struct {
	Status         []string
	Author         []string
	Assignee       []string
	Label          []string // Filter by label names or canonical label object IDs.
	Text           string
	IncludeDeleted bool
	OrderBy        OrderBy
	Limit          int
	Offset         int
}

// IssueFilter specifies filter criteria when querying issues.
type IssueFilter struct {
	State          []string
	Author         []string
	Assignee       []string
	Label          []string // Filter by label names or canonical label object IDs.
	Text           string
	IncludeDeleted bool
	OrderBy        OrderBy
	Limit          int
	Offset         int
}

// CommentFilter specifies filter criteria when querying comments.
type CommentFilter struct {
	SubjectType    string
	SubjectID      string
	Author         []string
	Text           string
	IncludeDeleted bool
	Resolved       *bool
	TargetCommit   string
	OrderBy        OrderBy
	Limit          int
	Offset         int
}

// ObjectFilter specifies filter criteria when querying collaborative objects cross-type.
type ObjectFilter struct {
	Type           []string
	Author         []string
	Text           string
	IncludeDeleted bool
	OrderBy        OrderBy
	Limit          int
	Offset         int
}

// WorkflowStateFilter specifies filter criteria when querying workflow states.
type WorkflowStateFilter struct {
	Type    []string
	OrderBy OrderBy
	Limit   int
	Offset  int
}

// LabelFilter specifies filter criteria when querying labels.
type LabelFilter struct {
	OrderBy OrderBy
	Limit   int
	Offset  int
}

// escapeLike escapes special SQLite LIKE pattern characters (%, _, \) so that
// the text is matched literally as a substring.
func escapeLike(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' || c == '_' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}
