package projection

import (
	"fmt"
	"sort"
	"strings"
)

// GroupKey represents a grouping dimension for issues.
type GroupKey string

const (
	GroupByState    GroupKey = "state"
	GroupByAssignee GroupKey = "assignee"
	GroupByPriority GroupKey = "priority"
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
// For GroupByPriority, issues are grouped by priority bucket in semantic order (urgent -> high -> medium -> low -> none).
// Groups are returned sorted by Key ascending (or semantic order for priority).
func (d *DB) GroupIssues(by GroupKey, f IssueFilter) ([]Group, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("projection: database is closed")
	}

	if (by == GroupByState || by == GroupByPriority) && f.OrderBy == "" {
		f.OrderBy = OrderByPositionAsc
	}

	issues, err := d.Issues(f)
	if err != nil {
		return nil, fmt.Errorf("projection: group issues query: %w", err)
	}

	switch by {
	case GroupByState:
		knownStates, err := d.WorkflowStates(WorkflowStateFilter{})
		if err != nil {
			return nil, fmt.Errorf("projection: group issues query workflow states: %w", err)
		}

		if len(knownStates) == 0 {
			// Backward compatibility fallback when no workflow states are defined
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
		}

		// When workflow states exist:
		stateByID := make(map[string]WorkflowStateResult, len(knownStates))
		stateByName := make(map[string]WorkflowStateResult, len(knownStates))
		for _, ws := range knownStates {
			stateByID[ws.ObjectID] = ws
			if ws.WorkflowState.Name != "" {
				stateByName[strings.ToLower(ws.WorkflowState.Name)] = ws
			}
		}

		knownStateIssues := make(map[string][]IssueResult, len(knownStates))
		var unknownIssues []IssueResult

		for _, iss := range issues {
			stateRef := iss.Issue.State
			if ws, ok := stateByID[stateRef]; ok {
				knownStateIssues[ws.ObjectID] = append(knownStateIssues[ws.ObjectID], iss)
				continue
			}
			if ws, ok := stateByName[strings.ToLower(stateRef)]; ok {
				knownStateIssues[ws.ObjectID] = append(knownStateIssues[ws.ObjectID], iss)
				continue
			}

			unknownIssues = append(unknownIssues, iss)
		}

		var groups []Group
		for _, ws := range knownStates {
			list := knownStateIssues[ws.ObjectID]
			if len(list) > 0 {
				key := ws.WorkflowState.Name
				if key == "" {
					key = ws.ObjectID
				}
				groups = append(groups, Group{
					Key:    key,
					Count:  len(list),
					Issues: list,
				})
			}
		}

		if len(unknownIssues) > 0 {
			groups = append(groups, Group{
				Key:    "Unknown",
				Count:  len(unknownIssues),
				Issues: unknownIssues,
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

	case GroupByPriority:
		buckets := []int{1, 2, 3, 4, 0}
		groupsMap := make(map[int][]IssueResult)
		for _, iss := range issues {
			p := iss.Issue.Priority
			if p < 0 || p > 4 {
				p = 0
			}
			groupsMap[p] = append(groupsMap[p], iss)
		}

		var groups []Group
		for _, p := range buckets {
			list := groupsMap[p]
			if len(list) > 0 {
				groups = append(groups, Group{
					Key:    formatPriority(p),
					Count:  len(list),
					Issues: list,
				})
			}
		}
		return groups, nil

	default:
		return nil, fmt.Errorf("projection: unsupported group key %q", by)
	}
}

func formatPriority(priority int) string {
	switch priority {
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 3:
		return "medium"
	case 4:
		return "low"
	default:
		return "none"
	}
}
