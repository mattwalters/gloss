package scenario_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/scenario"
	writsync "github.com/writtendev/writ/engine/sync"
)

var (
	alice = scenario.Writer{
		Name:  "Alice",
		Email: "alice@example.test",
	}
	bob = scenario.Writer{
		Name:  "Bob",
		Email: "bob@example.test",
	}

	aliceLaptop = scenario.Device{
		Name:     "alice-laptop",
		Writer:   alice,
		WriterID: "0123456789abcdef",
	}
	aliceDesktop = scenario.Device{
		Name:     "alice-desktop",
		Writer:   alice,
		WriterID: "1122334455667788",
	}
	bobLaptop = scenario.Device{
		Name:     "bob-laptop",
		Writer:   bob,
		WriterID: "fedcba9876543210",
	}
)

const initialCalcCode = `package calc

func Add(a, b int) int {
	return a + b
}
`

const rebasedCalcCode = `package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`

func TestCanonical(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	commentBody, err := json.Marshal(map[string]any{
		"subject": map[string]any{
			"object_type": "review",
			"object_id":   "rev-canonical",
		},
		"text":   "Consider adding multiplication support as well.",
		"anchor": scenario.MakeAnchor("", "calc.go", initialCalcCode, 3, 5),
	})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}

	s := scenario.Scenario{
		Name:    "canonical",
		Devices: []scenario.Device{aliceLaptop, aliceDesktop, bobLaptop},
		Steps: []scenario.Step{
			// 1. Alice-laptop commits initial code and pushes to origin
			scenario.Commit{
				Device:  aliceLaptop,
				Files:   map[string]string{"calc.go": initialCalcCode},
				Message: "initial calc implementation",
				At:      baseTime,
			},
			scenario.PushBranch{
				Device: aliceLaptop,
				Branch: "main",
			},

			// 2. Alice-laptop opens review and adds anchored comment
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(1 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions",
						"description": "Initial draft of addition"
					}`),
				},
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(2 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-anchor",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body:       commentBody,
				},
			},
			scenario.Push{
				Device: aliceLaptop,
			},

			// 3. Bob-laptop fetches
			scenario.Fetch{
				Device: bobLaptop,
			},

			// 4. Bob-laptop replies to comment and approves review
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(3 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-reply",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": {
							"object_type": "review",
							"object_id": "rev-canonical"
						},
						"in_reply_to": "c-anchor",
						"text": "Great idea, will follow up!"
					}`),
				},
			},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(4 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "approval",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": "user:bob",
						"revision": "rev1",
						"verdict": "approve",
						"message": "Looks solid"
					}`),
				},
			},
			scenario.Push{
				Device: bobLaptop,
			},

			// 5. Alice-desktop has been offline throughout; edits same review concurrently (multi-device race)
			scenario.AppendOp{
				Device: aliceDesktop,
				At:     baseTime.Add(5 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions (polished title)"
					}`),
				},
			},

			// 6. Bob force-pushes rebased code branch after anchored comment exists
			scenario.Commit{
				Device:  bobLaptop,
				Files:   map[string]string{"calc.go": rebasedCalcCode},
				Message: "rebase and add doc comment",
				At:      baseTime.Add(6 * time.Minute),
			},
			scenario.PushBranch{
				Device: bobLaptop,
				Branch: "main",
				Force:  true,
			},

			// 7. Alice-desktop syncs last (pushes local offline edits)
			scenario.Push{
				Device: aliceDesktop,
			},

			// 8. Everyone fetches all updates
			scenario.Fetch{Device: aliceLaptop},
			scenario.Fetch{Device: bobLaptop},
			scenario.Fetch{Device: aliceDesktop},

			// 9. Converge: assert identical folded state across all 3 clones
			scenario.Converge{
				AnchorChecks: []scenario.AnchorCheck{
					{CommentID: "c-anchor", Branch: "main"},
				},
			},
		},
	}

	scenario.Run(t, s)
}

func TestChainRollback(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	s := scenario.Scenario{
		Name:    "chain-rollback",
		Devices: []scenario.Device{aliceLaptop, bobLaptop},
		Steps: []scenario.Step{
			// 1. Alice-laptop creates review rev-rollback (op1)
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime,
				Envelope: codec.Envelope{
					ObjectID:   "rev-rollback",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Op1 Title",
						"description": "Initial creation"
					}`),
				},
			},
			// 2. Alice-laptop updates review (op2)
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(1 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-rollback",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Op2 Title (Rewound Later)"
					}`),
				},
			},
			// 3. Alice pushes to origin (origin ref now at op2)
			scenario.Push{
				Device: aliceLaptop,
			},
			// 4. Bob fetches from origin (Bob tracking ref at op2)
			scenario.Fetch{
				Device: bobLaptop,
			},
			// 5. Alice rewinds local chain to op1 and force-pushes to origin
			scenario.ForcePushChain{
				Device:        aliceLaptop,
				ObjectType:    "review",
				TargetOpIndex: 0,
			},
			// 6. Bob fetches: must reject non-fast-forward update (spec/ref-layout.md §168)
			scenario.Fetch{
				Device:        bobLaptop,
				ExpectedError: writsync.ErrNonFastForward,
			},
			// 7. Alice recovers by restoring local ref to op2 and appending op3
			scenario.ResetLocalChain{
				Device:        aliceLaptop,
				ObjectType:    "review",
				TargetOpIndex: 1,
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(2 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-rollback",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Op3 Title (Fast-Forward Recovered)"
					}`),
				},
			},
			scenario.Push{
				Device: aliceLaptop,
			},
			// 8. Bob fetches fast-forwarded update
			scenario.Fetch{
				Device: bobLaptop,
			},
			// 9. Converge: both clones converge and no peer lost ops
			scenario.Converge{},
		},
	}

	scenario.Run(t, s)
}

func TestSyncOrderPermutation(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	commentBody, err := json.Marshal(map[string]any{
		"subject": map[string]any{
			"object_type": "review",
			"object_id":   "rev-canonical",
		},
		"text":   "Consider adding multiplication support as well.",
		"anchor": scenario.MakeAnchor("", "calc.go", initialCalcCode, 3, 5),
	})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}

	// Permuted sync order: same appends, reordered push/fetch
	s := scenario.Scenario{
		Name:    "sync-order-permutation",
		Devices: []scenario.Device{aliceLaptop, aliceDesktop, bobLaptop},
		Steps: []scenario.Step{
			// 1. Alice-laptop commits initial code
			scenario.Commit{
				Device:  aliceLaptop,
				Files:   map[string]string{"calc.go": initialCalcCode},
				Message: "initial calc implementation",
				At:      baseTime,
			},
			scenario.PushBranch{
				Device: aliceLaptop,
				Branch: "main",
			},

			// 2. Alice-laptop appends create review and anchored comment
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(1 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions",
						"description": "Initial draft of addition"
					}`),
				},
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(2 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-anchor",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body:       commentBody,
				},
			},

			// 3. Alice-desktop appends update offline FIRST before anyone pushed
			scenario.AppendOp{
				Device: aliceDesktop,
				At:     baseTime.Add(5 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions (polished title)"
					}`),
				},
			},

			// 4. Alice-laptop pushes
			scenario.Push{
				Device: aliceLaptop,
			},

			// 5. Bob fetches and appends comment and approval
			scenario.Fetch{
				Device: bobLaptop,
			},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(3 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-reply",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": {
							"object_type": "review",
							"object_id": "rev-canonical"
						},
						"in_reply_to": "c-anchor",
						"text": "Great idea, will follow up!"
					}`),
				},
			},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(4 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "approval",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": "user:bob",
						"revision": "rev1",
						"verdict": "approve",
						"message": "Looks solid"
					}`),
				},
			},

			// 6. Bob force-pushes code branch and pushes writ ops
			scenario.Commit{
				Device:  bobLaptop,
				Files:   map[string]string{"calc.go": rebasedCalcCode},
				Message: "rebase and add doc comment",
				At:      baseTime.Add(6 * time.Minute),
			},
			scenario.PushBranch{
				Device: bobLaptop,
				Branch: "main",
				Force:  true,
			},
			scenario.Push{
				Device: bobLaptop,
			},

			// 7. Alice-laptop fetches Bob's ops before Alice-desktop pushes
			scenario.Fetch{Device: aliceLaptop},

			// 8. Alice-desktop pushes
			scenario.Push{Device: aliceDesktop},

			// 9. Remaining fetches in permuted order
			scenario.Fetch{Device: bobLaptop},
			scenario.Fetch{Device: aliceLaptop},
			scenario.Fetch{Device: aliceDesktop},

			// 10. Converge: must equal canonical golden snapshot
			scenario.Converge{
				GoldenName: "canonical",
				AnchorChecks: []scenario.AnchorCheck{
					{CommentID: "c-anchor", Branch: "main"},
				},
			},
		},
	}

	scenario.Run(t, s)
}

type spyReporter struct {
	fatals  []string
	logs    []string
	tempDir string
}

func (s *spyReporter) Helper() {}
func (s *spyReporter) Fatalf(format string, args ...any) {
	s.fatals = append(s.fatals, format)
}
func (s *spyReporter) Logf(format string, args ...any) {
	s.logs = append(s.logs, format)
}
func (s *spyReporter) TempDir() string {
	return s.tempDir
}
func (s *spyReporter) Skip(args ...any) {}

func TestNegativeControl_MissingFetchFails(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	commentBody, err := json.Marshal(map[string]any{
		"subject": map[string]any{
			"object_type": "review",
			"object_id":   "rev-canonical",
		},
		"text":   "Consider adding multiplication support as well.",
		"anchor": scenario.MakeAnchor("", "calc.go", initialCalcCode, 3, 5),
	})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}

	// Canonical scenario with Alice-desktop's final fetch omitted
	s := scenario.Scenario{
		Name:    "canonical",
		Devices: []scenario.Device{aliceLaptop, aliceDesktop, bobLaptop},
		Steps: []scenario.Step{
			scenario.Commit{
				Device:  aliceLaptop,
				Files:   map[string]string{"calc.go": initialCalcCode},
				Message: "initial calc implementation",
				At:      baseTime,
			},
			scenario.PushBranch{Device: aliceLaptop, Branch: "main"},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(1 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body:       json.RawMessage(`{"title":"Add calculator functions"}`),
				},
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(2 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-anchor",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body:       commentBody,
				},
			},
			scenario.Push{Device: aliceLaptop},
			scenario.Fetch{Device: bobLaptop},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(3 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-reply",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": {"object_type": "review", "object_id": "rev-canonical"},
						"in_reply_to": "c-anchor",
						"text": "Great idea, will follow up!"
					}`),
				},
			},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(4 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "approval",
					OpVersion:  1,
					Body:       json.RawMessage(`{"subject":"user:bob","revision":"rev1","verdict":"approve"}`),
				},
			},
			scenario.Push{Device: bobLaptop},
			scenario.AppendOp{
				Device: aliceDesktop,
				At:     baseTime.Add(5 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body:       json.RawMessage(`{"title":"Add calculator functions (polished title)"}`),
				},
			},
			scenario.Commit{
				Device:  bobLaptop,
				Files:   map[string]string{"calc.go": rebasedCalcCode},
				Message: "rebase and add doc comment",
				At:      baseTime.Add(6 * time.Minute),
			},
			scenario.PushBranch{Device: bobLaptop, Branch: "main", Force: true},
			scenario.Push{Device: aliceDesktop},

			// Alice-laptop and Bob-laptop fetch, but Alice-desktop's fetch is OMITTED!
			scenario.Fetch{Device: aliceLaptop},
			scenario.Fetch{Device: bobLaptop},

			scenario.Converge{
				GoldenName:       "canonical",
				SkipGoldenUpdate: true,
				AnchorChecks: []scenario.AnchorCheck{
					{CommentID: "c-anchor", Branch: "main"},
				},
			},
		},
	}

	spy := &spyReporter{tempDir: t.TempDir()}
	scenario.Run(spy, s)

	if len(spy.fatals) == 0 {
		t.Fatalf("expected negative control (missing fetch) to fail with convergence mismatch, but it passed")
	}
}

func TestNegativeControl_MissingOpFails(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	commentBody, err := json.Marshal(map[string]any{
		"subject": map[string]any{
			"object_type": "review",
			"object_id":   "rev-canonical",
		},
		"text":   "Consider adding multiplication support as well.",
		"anchor": scenario.MakeAnchor("", "calc.go", initialCalcCode, 3, 5),
	})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}

	// Canonical scenario with Bob's approval op OMITTED -> golden mismatch!
	s := scenario.Scenario{
		Name:    "canonical",
		Devices: []scenario.Device{aliceLaptop, aliceDesktop, bobLaptop},
		Steps: []scenario.Step{
			scenario.Commit{
				Device:  aliceLaptop,
				Files:   map[string]string{"calc.go": initialCalcCode},
				Message: "initial calc implementation",
				At:      baseTime,
			},
			scenario.PushBranch{Device: aliceLaptop, Branch: "main"},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(1 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions",
						"description": "Initial draft of addition"
					}`),
				},
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				At:     baseTime.Add(2 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-anchor",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body:       commentBody,
				},
			},
			scenario.Push{Device: aliceLaptop},
			scenario.Fetch{Device: bobLaptop},
			scenario.AppendOp{
				Device: bobLaptop,
				At:     baseTime.Add(3 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "c-reply",
					ObjectType: "comment",
					OpType:     "create",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"subject": {
							"object_type": "review",
							"object_id": "rev-canonical"
						},
						"in_reply_to": "c-anchor",
						"text": "Great idea, will follow up!"
					}`),
				},
			},
			// Bob's approval op is deliberately omitted here!
			scenario.Push{Device: bobLaptop},
			scenario.AppendOp{
				Device: aliceDesktop,
				At:     baseTime.Add(5 * time.Minute),
				Envelope: codec.Envelope{
					ObjectID:   "rev-canonical",
					ObjectType: "review",
					OpType:     "update",
					OpVersion:  1,
					Body: json.RawMessage(`{
						"title": "Add calculator functions (polished title)"
					}`),
				},
			},
			scenario.Commit{
				Device:  bobLaptop,
				Files:   map[string]string{"calc.go": rebasedCalcCode},
				Message: "rebase and add doc comment",
				At:      baseTime.Add(6 * time.Minute),
			},
			scenario.PushBranch{Device: bobLaptop, Branch: "main", Force: true},
			scenario.Push{Device: aliceDesktop},
			scenario.Fetch{Device: aliceLaptop},
			scenario.Fetch{Device: bobLaptop},
			scenario.Fetch{Device: aliceDesktop},
			scenario.Converge{
				GoldenName:       "canonical",
				SkipGoldenUpdate: true,
				AnchorChecks: []scenario.AnchorCheck{
					{CommentID: "c-anchor", Branch: "main"},
				},
			},
		},
	}

	spy := &spyReporter{tempDir: t.TempDir()}
	scenario.Run(spy, s)

	if len(spy.fatals) == 0 {
		t.Fatalf("expected negative control (missing op) to fail with golden diff mismatch, but it passed")
	}
}
