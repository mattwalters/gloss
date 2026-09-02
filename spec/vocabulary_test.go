package spec_test

import (
	"reflect"
	"testing"

	"github.com/writtendev/writ/spec"
)

func TestVocabularyEnumsExtractedFromSchemas(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "ReviewStatuses",
			got:  spec.ReviewStatuses(),
			want: []string{"draft", "open", "closed", "merged"},
		},
		{
			name: "ApprovalVerdicts",
			got:  spec.ApprovalVerdicts(),
			want: []string{"approve", "request-changes", "none"},
		},
		{
			name: "CIStatusStates",
			got:  spec.CIStatusStates(),
			want: []string{"pending", "success", "failure", "error", "cancelled", "neutral", "skipped"},
		},
		{
			name: "LinkRelations",
			got:  spec.LinkRelations(),
			want: []string{"fixes", "relates", "none"},
		},
		{
			name: "IssueStates",
			got:  spec.IssueStates(),
			want: []string{"open", "closed"},
		},
		{
			name: "ProjectStatuses",
			got:  spec.ProjectStatuses(),
			want: []string{"planned", "active", "paused", "completed", "canceled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestFormatOptions(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"single"}, "single"},
		{[]string{"open", "closed"}, "open or closed"},
		{[]string{"approve", "request-changes", "none"}, "approve, request-changes, or none"},
		{[]string{"draft", "open", "closed", "merged"}, "draft, open, closed, or merged"},
	}

	for _, tt := range tests {
		got := spec.FormatOptions(tt.input)
		if got != tt.want {
			t.Errorf("FormatOptions(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
