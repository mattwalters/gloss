package projection

import (
	"fmt"
	"sort"
)

// GroupKey represents a grouping dimension for issues.
type GroupKey string

const (
	GroupByState    GroupKey = "state"
	GroupByAssignee GroupKey = "assignee"
)

// Group represents a collection of issues belonging to a single group key.
type Group struct {
	Key    string        `json:"key"`
	Count  int           `json:"count"`
	Issues []IssueResult `json:"issues"`
}

// GroupIssues partitions issues matching the filter by the specified grouping key.
// For GroupByState, issues are grouped by their state string.
// For GroupByAssignee, issues are grouped by assignee, with unassigned issues under key "",
// and multiply-assigned issues appearing under each of their assignees.
// Groups are returned sorted by Key ascending.
func (d *DB) GroupIssues(by GroupKey, f IssueFilter) ([]Group, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	issues, err := d.Issues(f)
	if err != nil {
		return nil, fmt.Errorf("projection: group issues query: %w", err)
	}

	switch by {
	case GroupByState:
		groupsMap := make(map[string][]IssueResult)
		for _, iss := range issues {
			groupsMap[iss.Issue.State] = append(groupsMap[iss.Issue.State], iss)
		}

		var keys []string
		for k := range groupsMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		groups := make([]Group, 0, len(keys))
		for _, k := range keys {
			groupIssues := groupsMap[k]
			groups = append(groups, Group{
				Key:    k,
				Count:  len(groupIssues),
				Issues: groupIssues,
			})
		}
		return groups, nil

	case GroupByAssignee:
		groupsMap := make(map[string][]IssueResult)
		for _, iss := range issues {
			if len(iss.Issue.Assignees) == 0 {
				groupsMap[""] = append(groupsMap[""], iss)
			} else {
				for _, assignee := range iss.Issue.Assignees {
					groupsMap[assignee] = append(groupsMap[assignee], iss)
				}
			}
		}

		var keys []string
		for k := range groupsMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		groups := make([]Group, 0, len(keys))
		for _, k := range keys {
			groupIssues := groupsMap[k]
			groups = append(groups, Group{
				Key:    k,
				Count:  len(groupIssues),
				Issues: groupIssues,
			})
		}
		return groups, nil

	default:
		return nil, fmt.Errorf("projection: unsupported group key %q", by)
	}
}
