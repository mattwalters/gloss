package scenario_test

import (
	"encoding/json"
	"testing"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/scenario"
)

// Steps may omit At — the runner defaults the clock rather than stamping ops
// with the zero time, which git rejects outright as an author date.
func TestStepsWithoutExplicitTime(t *testing.T) {
	body, err := json.Marshal(map[string]any{"title": "no explicit time"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	scenario.Run(t, scenario.Scenario{
		Name:    "no-explicit-time",
		Devices: []scenario.Device{aliceLaptop},
		Steps: []scenario.Step{
			scenario.Commit{
				Device:  aliceLaptop,
				Files:   map[string]string{"calc.go": initialCalcCode},
				Message: "initial calc implementation",
			},
			scenario.AppendOp{
				Device: aliceLaptop,
				Envelope: codec.Envelope{
					ObjectID:   "rev-no-time",
					ObjectType: "review",
					OpType:     "create",
					OpVersion:  1,
					Body:       body,
				},
			},
		},
	})
}
