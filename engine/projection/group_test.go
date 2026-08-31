package projection_test

import (
	"testing"

	"github.com/writtendev/writ/engine/projection"
)

func TestGroupIssuesByState(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	groups, err := db.GroupIssues(projection.GroupByState, projection.IssueFilter{})
	if err != nil {
		t.Fatalf("GroupIssues(GroupByState): %v", err)
	}

	// Distinct states in seeded DB: closed (1), in_progress (1), open (2)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Alphabetical order: closed, in_progress, open
	if groups[0].Key != "closed" || groups[0].Count != 1 {
		t.Errorf("group 0: expected closed (1), got %s (%d)", groups[0].Key, groups[0].Count)
	}
	if groups[1].Key != "in_progress" || groups[1].Count != 1 {
		t.Errorf("group 1: expected in_progress (1), got %s (%d)", groups[1].Key, groups[1].Count)
	}
	if groups[2].Key != "open" || groups[2].Count != 2 {
		t.Errorf("group 2: expected open (2), got %s (%d)", groups[2].Key, groups[2].Count)
	}

	// Sum of counts must equal total matching issues
	totalCount := 0
	for _, g := range groups {
		totalCount += g.Count
		if len(g.Issues) != g.Count {
			t.Errorf("group %s count %d != len(issues) %d", g.Key, g.Count, len(g.Issues))
		}
	}
	if totalCount != 4 {
		t.Errorf("expected sum of counts = 4, got %d", totalCount)
	}
}

func TestGroupIssuesByAssignee(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	groups, err := db.GroupIssues(projection.GroupByAssignee, projection.IssueFilter{})
	if err != nil {
		t.Fatalf("GroupIssues(GroupByAssignee): %v", err)
	}

	// Seeded issues:
	// iss-1: assignees [alice, bob]
	// iss-2: assignees [bob]
	// iss-3: assignees [] (unassigned)
	// iss-4: assignees [] (unassigned)
	//
	// Expected groups:
	// "" (unassigned): iss-3, iss-4 (count 2)
	// "alice": iss-1 (count 1)
	// "bob": iss-1, iss-2 (count 2)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Group "": unassigned
	if groups[0].Key != "" || groups[0].Count != 2 {
		t.Errorf("group 0: expected '' (2), got %q (%d)", groups[0].Key, groups[0].Count)
	}
	unassignedIDs := []string{groups[0].Issues[0].ObjectID, groups[0].Issues[1].ObjectID}
	if unassignedIDs[0] != "iss-3" || unassignedIDs[1] != "iss-4" {
		t.Errorf("expected unassigned issues [iss-3, iss-4], got %v", unassignedIDs)
	}

	// Group "alice"
	if groups[1].Key != "alice" || groups[1].Count != 1 {
		t.Errorf("group 1: expected 'alice' (1), got %q (%d)", groups[1].Key, groups[1].Count)
	}
	if groups[1].Issues[0].ObjectID != "iss-1" {
		t.Errorf("expected alice's issue to be iss-1, got %s", groups[1].Issues[0].ObjectID)
	}

	// Group "bob"
	if groups[2].Key != "bob" || groups[2].Count != 2 {
		t.Errorf("group 2: expected 'bob' (2), got %q (%d)", groups[2].Key, groups[2].Count)
	}
	bobIDs := []string{groups[2].Issues[0].ObjectID, groups[2].Issues[1].ObjectID}
	if bobIDs[0] != "iss-1" || bobIDs[1] != "iss-2" {
		t.Errorf("expected bob's issues [iss-1, iss-2], got %v", bobIDs)
	}
}

func TestGroupIssuesWithFilter(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	// Filter by Label: bug (only iss-1)
	groups, err := db.GroupIssues(projection.GroupByAssignee, projection.IssueFilter{
		Label: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("GroupIssues with filter: %v", err)
	}

	// iss-1 is assigned to alice and bob
	// Expected groups: "alice" (1), "bob" (1)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups for bug label, got %d", len(groups))
	}
	if groups[0].Key != "alice" || groups[0].Count != 1 || groups[0].Issues[0].ObjectID != "iss-1" {
		t.Errorf("unexpected alice group: %+v", groups[0])
	}
	if groups[1].Key != "bob" || groups[1].Count != 1 || groups[1].Issues[0].ObjectID != "iss-1" {
		t.Errorf("unexpected bob group: %+v", groups[1])
	}
}

func TestGroupIssuesInvalidKey(t *testing.T) {
	db := setupSeededDB(t)
	defer db.Close()

	_, err := db.GroupIssues(projection.GroupKey("invalid"), projection.IssueFilter{})
	if err == nil {
		t.Errorf("expected error for invalid GroupKey, got nil")
	}
}
