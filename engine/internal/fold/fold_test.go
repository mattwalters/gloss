package fold_test

import (
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
)

func TestFoldObjectTypeInference(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rules := []fold.Rule{
		{
			OpType:    "create",
			OpVersion: 1,
			Field:     "title",
			Strategy:  "lww",
		},
		{
			OpType:    "update",
			OpVersion: 1,
			Field:     "title",
			Strategy:  "lww",
		},
	}

	tests := []struct {
		name     string
		ops      []codec.Op
		wantType string
	}{
		{
			name: "ops[0] empty ObjectType, subsequent create op has ObjectType",
			ops: []codec.Op{
				{
					ID: "op-update",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "",
						OpType:     "update",
						OpVersion:  1,
						Body:       []byte(`{"title":"Updated Title"}`),
					},
					Parents: []string{"op-create"},
					Author:  codec.Identity{When: baseTime.Add(time.Minute)},
				},
				{
					ID: "op-create",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "issue",
						OpType:     "create",
						OpVersion:  1,
						Body:       []byte(`{"title":"Initial Title"}`),
					},
					Author: codec.Identity{When: baseTime},
				},
			},
			wantType: "issue",
		},
		{
			name: "ops[0] empty ObjectType, subsequent update op has ObjectType",
			ops: []codec.Op{
				{
					ID: "op-1",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "",
						OpType:     "update",
						OpVersion:  1,
						Body:       []byte(`{"title":"Title 1"}`),
					},
					Author: codec.Identity{When: baseTime},
				},
				{
					ID: "op-2",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "review",
						OpType:     "update",
						OpVersion:  1,
						Body:       []byte(`{"title":"Title 2"}`),
					},
					Parents: []string{"op-1"},
					Author:  codec.Identity{When: baseTime.Add(time.Minute)},
				},
			},
			wantType: "review",
		},
		{
			name: "subsequent create op takes precedence over first non-empty op",
			ops: []codec.Op{
				{
					ID: "op-update",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "generic",
						OpType:     "update",
						OpVersion:  1,
						Body:       []byte(`{"title":"Title"}`),
					},
					Parents: []string{"op-create"},
					Author:  codec.Identity{When: baseTime.Add(time.Minute)},
				},
				{
					ID: "op-create",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "issue",
						OpType:     "create",
						OpVersion:  1,
						Body:       []byte(`{"title":"Title"}`),
					},
					Author: codec.Identity{When: baseTime},
				},
			},
			wantType: "issue",
		},
		{
			name: "all ops have empty ObjectType",
			ops: []codec.Op{
				{
					ID: "op-1",
					Envelope: codec.Envelope{
						ObjectID:   "obj-1",
						ObjectType: "",
						OpType:     "update",
						OpVersion:  1,
						Body:       []byte(`{"title":"Title"}`),
					},
					Author: codec.Identity{When: baseTime},
				},
			},
			wantType: "",
		},
		{
			name:     "empty ops slice",
			ops:      nil,
			wantType: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := fold.Fold(tc.ops, rules)
			if err != nil {
				t.Fatalf("Fold returned unexpected error: %v", err)
			}
			if res.ObjectType != tc.wantType {
				t.Errorf("res.ObjectType = %q, want %q", res.ObjectType, tc.wantType)
			}
		})
	}
}
