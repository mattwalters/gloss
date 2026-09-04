package projection_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/projection"
	"github.com/writtendev/writ/engine/state"
)

func TestProjectionSettingsLifecycle(t *testing.T) {
	ctx := context.Background()
	_, store := createTestStore(t, "0123456789abcdef")

	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer db.Close()

	// 1. Initially returns defaults
	res, err := db.Settings()
	if err != nil {
		t.Fatalf("db.Settings failed: %v", err)
	}
	if res.Settings.Timezone != "UTC" {
		t.Errorf("expected default Timezone 'UTC', got %q", res.Settings.Timezone)
	}
	if res.Settings.EstimateScale != "fibonacci" {
		t.Errorf("expected default EstimateScale 'fibonacci', got %q", res.Settings.EstimateScale)
	}

	// 2. Append settings op
	env := codec.Envelope{
		ObjectID:   state.DefaultSettingsObjectID,
		ObjectType: "settings",
		OpType:     "set",
		OpVersion:  1,
		Body: json.RawMessage(`{
			"name": "Custom Workspace",
			"identifier": "CUST",
			"timezone": "Europe/London",
			"estimate_scale": "exponential",
			"allow_zero_estimates": true,
			"cycles_enabled": true,
			"cycle_duration_weeks": 4,
			"cycle_start_day": 2,
			"cycle_cooldown_weeks": 1,
			"triage_enabled": true,
			"custom_plugin_field": "hello"
		}`),
	}
	raw, _ := codec.EncodePayload(env)
	env.Raw = raw
	if _, err := store.Append(ctx, env, nil); err != nil {
		t.Fatalf("store.Append failed: %v", err)
	}

	// 3. Refresh projection
	if _, err := db.Refresh(store); err != nil {
		t.Fatalf("db.Refresh failed: %v", err)
	}

	res, err = db.Settings()
	if err != nil {
		t.Fatalf("db.Settings failed: %v", err)
	}
	if res.Settings.Name != "Custom Workspace" {
		t.Errorf("Name = %q, want 'Custom Workspace'", res.Settings.Name)
	}
	if res.Settings.Identifier != "CUST" {
		t.Errorf("Identifier = %q, want 'CUST'", res.Settings.Identifier)
	}
	if res.Settings.Timezone != "Europe/London" {
		t.Errorf("Timezone = %q, want 'Europe/London'", res.Settings.Timezone)
	}
	if res.Settings.EstimateScale != "exponential" {
		t.Errorf("EstimateScale = %q, want 'exponential'", res.Settings.EstimateScale)
	}
	if !res.Settings.AllowZeroEstimates {
		t.Errorf("AllowZeroEstimates = false, want true")
	}
	if !res.Settings.CyclesEnabled {
		t.Errorf("CyclesEnabled = false, want true")
	}
	if res.Settings.CycleDurationWeeks != 4 {
		t.Errorf("CycleDurationWeeks = %d, want 4", res.Settings.CycleDurationWeeks)
	}
	if res.Settings.CycleStartDay != 2 {
		t.Errorf("CycleStartDay = %d, want 2", res.Settings.CycleStartDay)
	}
	if res.Settings.CycleCooldownWeeks != 1 {
		t.Errorf("CycleCooldownWeeks = %d, want 1", res.Settings.CycleCooldownWeeks)
	}
	if !res.Settings.TriageEnabled {
		t.Errorf("TriageEnabled = false, want true")
	}
	if res.Settings.UnknownKeys["custom_plugin_field"] != "hello" {
		t.Errorf("UnknownKeys['custom_plugin_field'] = %v, want 'hello'", res.Settings.UnknownKeys["custom_plugin_field"])
	}
}
