package spec

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type vocabulary struct {
	reviewStatuses     []string
	approvalVerdicts   []string
	ciStatusStates     []string
	linkRelations      []string
	projectStatuses    []string
	workflowStateTypes []string
	estimateScales     []string
}

func parseSchemaEnum(schemaFile string, jsonPath ...string) []string {
	raw, err := FS.ReadFile("schemas/" + schemaFile)
	if err != nil {
		panic(fmt.Errorf("spec: read schema %s: %w", schemaFile, err))
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(fmt.Errorf("spec: unmarshal schema %s: %w", schemaFile, err))
	}
	curr := doc
	for _, p := range jsonPath {
		m, ok := curr.(map[string]any)
		if !ok {
			panic(fmt.Errorf("spec: schema %s: path %v not a map at %s", schemaFile, jsonPath, p))
		}
		curr, ok = m[p]
		if !ok {
			panic(fmt.Errorf("spec: schema %s: key %s not found in path %v", schemaFile, p, jsonPath))
		}
	}
	enumArr, ok := curr.([]any)
	if !ok {
		panic(fmt.Errorf("spec: schema %s: enum at path %v is not an array", schemaFile, jsonPath))
	}
	res := make([]string, len(enumArr))
	for i, item := range enumArr {
		s, ok := item.(string)
		if !ok {
			panic(fmt.Errorf("spec: schema %s: enum item at %d is not a string", schemaFile, i))
		}
		res[i] = s
	}
	return res
}

var vocabOnce = sync.OnceValue(func() vocabulary {
	return vocabulary{
		reviewStatuses:     parseSchemaEnum("review-ops.schema.json", "$defs", "set_status_body", "properties", "status", "enum"),
		approvalVerdicts:   parseSchemaEnum("review-ops.schema.json", "$defs", "approval_body", "properties", "verdict", "enum"),
		ciStatusStates:     parseSchemaEnum("review-ops.schema.json", "$defs", "ci_status_body", "properties", "state", "enum"),
		linkRelations:      parseSchemaEnum("review-ops.schema.json", "$defs", "link_body", "properties", "relation", "enum"),
		projectStatuses:    parseSchemaEnum("project-ops.schema.json", "$defs", "set_status_body", "properties", "status", "enum"),
		workflowStateTypes: parseSchemaEnum("workflow-state-ops.schema.json", "$defs", "state_type", "enum"),
		estimateScales:     parseSchemaEnum("settings-ops.schema.json", "$defs", "set_body", "properties", "estimate_scale", "enum"),
	}
})

// ReviewStatuses returns the accepted review status enum values defined in review-ops.schema.json.
func ReviewStatuses() []string {
	return slices.Clone(vocabOnce().reviewStatuses)
}

// ApprovalVerdicts returns the accepted approval verdict enum values defined in review-ops.schema.json.
func ApprovalVerdicts() []string {
	return slices.Clone(vocabOnce().approvalVerdicts)
}

// CIStatusStates returns the accepted CI status state enum values defined in review-ops.schema.json.
func CIStatusStates() []string {
	return slices.Clone(vocabOnce().ciStatusStates)
}

// LinkRelations returns the accepted link relation enum values defined in review-ops.schema.json.
func LinkRelations() []string {
	return slices.Clone(vocabOnce().linkRelations)
}

// ProjectStatuses returns the accepted project status enum values defined in project-ops.schema.json.
func ProjectStatuses() []string {
	return slices.Clone(vocabOnce().projectStatuses)
}

// WorkflowStateTypes returns the accepted workflow state type enum values defined in workflow-state-ops.schema.json.
func WorkflowStateTypes() []string {
	return slices.Clone(vocabOnce().workflowStateTypes)
}

// EstimateScales returns the accepted estimate scale enum values defined in settings-ops.schema.json.
func EstimateScales() []string {
	return slices.Clone(vocabOnce().estimateScales)
}

// FormatOptions formats a slice of enum options into a human-readable list,
// e.g. "open or closed", "approve, request-changes, or none",
// "draft, open, closed, or merged".
func FormatOptions(options []string) string {
	switch len(options) {
	case 0:
		return ""
	case 1:
		return options[0]
	case 2:
		return options[0] + " or " + options[1]
	default:
		return strings.Join(options[:len(options)-1], ", ") + ", or " + options[len(options)-1]
	}
}

// IssuePriorityNames returns the closed priority vocabulary names in index order:
// 0: "none", 1: "urgent", 2: "high", 3: "medium", 4: "low".
func IssuePriorityNames() []string {
	return []string{"none", "urgent", "high", "medium", "low"}
}

// ParseIssuePriority parses a priority string, accepting symbolic names
// ("none", "urgent", "high", "medium", "low") or numeric digits ("0"-"4").
func ParseIssuePriority(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "0":
		return 0, nil
	case "urgent", "1":
		return 1, nil
	case "high", "2":
		return 2, nil
	case "medium", "3":
		return 3, nil
	case "low", "4":
		return 4, nil
	default:
		return 0, fmt.Errorf("spec: invalid issue priority %q (expected none, urgent, high, medium, low, or 0-4)", s)
	}
}

// FormatIssuePriority returns the canonical lowercase symbolic name for a priority level.
func FormatIssuePriority(p int) string {
	switch p {
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

