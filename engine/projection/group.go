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
			if strings.EqualFold(stateRef, "open") {
				var matchedID string
				for _, ws := range knownStates {
					if strings.EqualFold(ws.WorkflowState.Name, "open") || ws.WorkflowState.Type == "unstarted" {
						matchedID = ws.ObjectID
						break
					}
				}
				if matchedID != "" {
					knownStateIssues[matchedID] = append(knownStateIssues[matchedID], iss)
					continue
				}
			} else if strings.EqualFold(stateRef, "closed") {
				var matchedID string
				for _, ws := range knownStates {
					if strings.EqualFold(ws.WorkflowState.Name, "closed") || ws.WorkflowState.Type == "completed" {
						matchedID = ws.ObjectID
						break
					}
				}
				if matchedID != "" {
					knownStateIssues[matchedID] = append(knownStateIssues[matchedID], iss)
					continue
				}
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

	default:
		return nil, fmt.Errorf("projection: unsupported group key %q", by)
	}
}
