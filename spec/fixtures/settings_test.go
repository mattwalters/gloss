package fixtures_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/codec/canonicaljson"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/spec/fixtures"
)

// TestSettingsFamily registers the settings fixture family and runs all descriptions
// carrying settings collaborative objects through the typed FoldSettings golden test harness.
func TestSettingsFamily(t *testing.T) {
	fixtures.Run(t, fixtures.Family{
		Name:      "settings",
		GoldenDir: "testdata/golden/settings",
		Filter: func(desc *fixtures.Description) bool {
			if !strings.HasPrefix(desc.Name, "settings-") {
				return false
			}
			for _, ref := range desc.Refs {
				for _, gen := range ref.History {
					for _, c := range gen.Commits {
						if c.Op != nil && c.Op.ObjectType == "settings" {
							return true
						}
					}
				}
			}
			return false
		},
		Runner: runSettingsFixture,
	})
}

type SettingsGolden struct {
	Objects []SettingsObjectGolden `json:"objects"`
}

type SettingsObjectGolden struct {
	ObjectID string        `json:"object_id"`
	Settings writ.Settings `json:"settings"`
}

func runSettingsFixture(t *testing.T, fix *fixtures.Fixture) ([]byte, error) {
	t.Helper()

	store, err := dag.OpenRepo(fix.Repo, identity.Identity{})
	if err != nil {
		return nil, fmt.Errorf("dag.OpenRepo failed: %w", err)
	}

	enumRes, err := store.Enumerate()
	if err != nil {
		return nil, fmt.Errorf("store.Enumerate failed: %w", err)
	}

	var golden SettingsGolden

	opsByObject := enumRes.Ops
	if len(opsByObject) == 0 {
		opsByObject = make(map[string][]codec.Op)
		seenCommits := make(map[string]bool)
		cIdx := 0
		for _, ref := range fix.Description.Refs {
			isControl := strings.HasSuffix(ref.Name, "-control")
			for _, gen := range ref.History {
				gs := fix.Manifest.Generations[cIdx]
				cIdx++
				if isControl {
					continue
				}
				for ci := range gen.Commits {
					cState := gs.Commits[ci]
					if seenCommits[cState.SHA] {
						continue
					}
					seenCommits[cState.SHA] = true
					commitObj, err := fix.Repo.CommitObject(plumbing.NewHash(cState.SHA))
					if err != nil {
						return nil, fmt.Errorf("lookup commit %s: %w", cState.SHA, err)
					}
					pureCommit, err := codec.FromGitCommit(fix.Repo.Storer, commitObj)
					if err != nil {
						return nil, fmt.Errorf("from git commit %s: %w", cState.SHA, err)
					}
					op, err := codec.DecodeCommit(pureCommit)
					if err != nil {
						continue
					}
					opsByObject[op.ObjectID] = append(opsByObject[op.ObjectID], op)
				}
			}
		}
	}

	var objectIDs []string
	for objID := range opsByObject {
		objectIDs = append(objectIDs, objID)
	}
	sort.Strings(objectIDs)

	r := rand.New(rand.NewSource(42))

	for _, objID := range objectIDs {
		codecOps := opsByObject[objID]
		var settingsOps []codec.Op
		for _, op := range codecOps {
			if op.ObjectType == "settings" {
				settingsOps = append(settingsOps, op)
			}
		}
		if len(settingsOps) == 0 {
			continue
		}

		settingsState, err := writ.FoldSettings(settingsOps)
		if err != nil {
			return nil, fmt.Errorf("writ.FoldSettings for object %s in %s: %w", objID, fix.Name, err)
		}

		objectState, err := writ.Fold(codecOps, writ.SettingsRules())
		if err != nil {
			return nil, fmt.Errorf("writ.Fold for object %s in %s: %w", objID, fix.Name, err)
		}
		assertSettingsFoldAgreement(t, settingsState, objectState, fix.Name, objID)

		expectedJSON, err := canonicaljson.Marshal(mustJSON(t, settingsState))
		if err != nil {
			return nil, fmt.Errorf("canonicalizing settings state for %s: %w", objID, err)
		}

		for i := 0; i < 100; i++ {
			shuffled := make([]codec.Op, len(settingsOps))
			copy(shuffled, settingsOps)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			shuffledSettings, err := writ.FoldSettings(shuffled)
			if err != nil {
				t.Fatalf("commutativity violation on permutation #%d for object %s in %s: %v", i, objID, fix.Name, err)
			}

			shuffledJSON, err := canonicaljson.Marshal(mustJSON(t, shuffledSettings))
			if err != nil {
				t.Fatalf("canonicalizing shuffled settings state on permutation #%d for %s in %s: %v", i, objID, fix.Name, err)
			}

			if !bytes.Equal(shuffledJSON, expectedJSON) {
				t.Fatalf("commutativity violation on permutation #%d for object %s in fixture %s:\n got:  %s\n want: %s",
					i, objID, fix.Name, string(shuffledJSON), string(expectedJSON))
			}
		}

		golden.Objects = append(golden.Objects, SettingsObjectGolden{
			ObjectID: objID,
			Settings: settingsState,
		})
	}

	if len(golden.Objects) == 0 {
		return nil, fmt.Errorf("settings fixture %s yielded zero settings objects", fix.Name)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal settings golden: %w", err)
	}
	return append(b, '\n'), nil
}

func assertSettingsFoldAgreement(t *testing.T, s writ.Settings, state writ.ObjectState, fixtureName, objectID string) {
	t.Helper()
	def := writ.DefaultSettings()

	getInt := func(val any) (int, bool) {
		switch v := val.(type) {
		case int:
			return v, true
		case int64:
			return int(v), true
		case float64:
			return int(v), true
		default:
			return 0, false
		}
	}

	if name, ok := state.State["name"].(string); ok {
		if s.Name != name {
			t.Errorf("[%s/%s] agreement mismatch on name: FoldSettings=%q, Fold=%q", fixtureName, objectID, s.Name, name)
		}
	} else if s.Name != def.Name {
		t.Errorf("[%s/%s] name mismatch with default: FoldSettings=%q, want %q", fixtureName, objectID, s.Name, def.Name)
	}

	if id, ok := state.State["identifier"].(string); ok {
		if s.Identifier != id {
			t.Errorf("[%s/%s] agreement mismatch on identifier: FoldSettings=%q, Fold=%q", fixtureName, objectID, s.Identifier, id)
		}
	} else if s.Identifier != def.Identifier {
		t.Errorf("[%s/%s] identifier mismatch with default: FoldSettings=%q, want %q", fixtureName, objectID, s.Identifier, def.Identifier)
	}

	if tz, ok := state.State["timezone"].(string); ok {
		if s.Timezone != tz {
			t.Errorf("[%s/%s] agreement mismatch on timezone: FoldSettings=%q, Fold=%q", fixtureName, objectID, s.Timezone, tz)
		}
	} else if s.Timezone != def.Timezone {
		t.Errorf("[%s/%s] timezone mismatch with default: FoldSettings=%q, want %q", fixtureName, objectID, s.Timezone, def.Timezone)
	}

	if scale, ok := state.State["estimate_scale"].(string); ok {
		if s.EstimateScale != scale {
			t.Errorf("[%s/%s] agreement mismatch on estimate_scale: FoldSettings=%q, Fold=%q", fixtureName, objectID, s.EstimateScale, scale)
		}
	} else if s.EstimateScale != def.EstimateScale {
		t.Errorf("[%s/%s] estimate_scale mismatch with default: FoldSettings=%q, want %q", fixtureName, objectID, s.EstimateScale, def.EstimateScale)
	}

	if allowZero, ok := state.State["allow_zero_estimates"].(bool); ok {
		if s.AllowZeroEstimates != allowZero {
			t.Errorf("[%s/%s] agreement mismatch on allow_zero_estimates: FoldSettings=%v, Fold=%v", fixtureName, objectID, s.AllowZeroEstimates, allowZero)
		}
	} else if s.AllowZeroEstimates != def.AllowZeroEstimates {
		t.Errorf("[%s/%s] allow_zero_estimates mismatch with default: FoldSettings=%v, want %v", fixtureName, objectID, s.AllowZeroEstimates, def.AllowZeroEstimates)
	}

	if cycles, ok := state.State["cycles_enabled"].(bool); ok {
		if s.CyclesEnabled != cycles {
			t.Errorf("[%s/%s] agreement mismatch on cycles_enabled: FoldSettings=%v, Fold=%v", fixtureName, objectID, s.CyclesEnabled, cycles)
		}
	} else if s.CyclesEnabled != def.CyclesEnabled {
		t.Errorf("[%s/%s] cycles_enabled mismatch with default: FoldSettings=%v, want %v", fixtureName, objectID, s.CyclesEnabled, def.CyclesEnabled)
	}

	if durVal, ok := state.State["cycle_duration_weeks"]; ok {
		dur, ok := getInt(durVal)
		if !ok || s.CycleDurationWeeks != dur {
			t.Errorf("[%s/%s] agreement mismatch on cycle_duration_weeks: FoldSettings=%d, Fold=%v", fixtureName, objectID, s.CycleDurationWeeks, durVal)
		}
	} else if s.CycleDurationWeeks != def.CycleDurationWeeks {
		t.Errorf("[%s/%s] cycle_duration_weeks mismatch with default: FoldSettings=%d, want %d", fixtureName, objectID, s.CycleDurationWeeks, def.CycleDurationWeeks)
	}

	if startVal, ok := state.State["cycle_start_day"]; ok {
		start, ok := getInt(startVal)
		if !ok || s.CycleStartDay != start {
			t.Errorf("[%s/%s] agreement mismatch on cycle_start_day: FoldSettings=%d, Fold=%v", fixtureName, objectID, s.CycleStartDay, startVal)
		}
	} else if s.CycleStartDay != def.CycleStartDay {
		t.Errorf("[%s/%s] cycle_start_day mismatch with default: FoldSettings=%d, want %d", fixtureName, objectID, s.CycleStartDay, def.CycleStartDay)
	}

	if coolVal, ok := state.State["cycle_cooldown_weeks"]; ok {
		cool, ok := getInt(coolVal)
		if !ok || s.CycleCooldownWeeks != cool {
			t.Errorf("[%s/%s] agreement mismatch on cycle_cooldown_weeks: FoldSettings=%d, Fold=%v", fixtureName, objectID, s.CycleCooldownWeeks, coolVal)
		}
	} else if s.CycleCooldownWeeks != def.CycleCooldownWeeks {
		t.Errorf("[%s/%s] cycle_cooldown_weeks mismatch with default: FoldSettings=%d, want %d", fixtureName, objectID, s.CycleCooldownWeeks, def.CycleCooldownWeeks)
	}

	if triage, ok := state.State["triage_enabled"].(bool); ok {
		if s.TriageEnabled != triage {
			t.Errorf("[%s/%s] agreement mismatch on triage_enabled: FoldSettings=%v, Fold=%v", fixtureName, objectID, s.TriageEnabled, triage)
		}
	} else if s.TriageEnabled != def.TriageEnabled {
		t.Errorf("[%s/%s] triage_enabled mismatch with default: FoldSettings=%v, want %v", fixtureName, objectID, s.TriageEnabled, def.TriageEnabled)
	}
}
