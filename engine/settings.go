package writ

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/state"
)

// SettingsEdit specifies fields for updating workspace settings.
// Any non-nil field will be updated; nil fields are omitted from the operation body,
// preserving untouched fields and unknown keys.
type SettingsEdit struct {
	Name               *string        `json:"name,omitempty"`
	Identifier         *string        `json:"identifier,omitempty"`
	Timezone           *string        `json:"timezone,omitempty"`
	EstimateScale      *string        `json:"estimate_scale,omitempty"`
	AllowZeroEstimates *bool          `json:"allow_zero_estimates,omitempty"`
	CyclesEnabled      *bool          `json:"cycles_enabled,omitempty"`
	CycleDurationWeeks *int           `json:"cycle_duration_weeks,omitempty"`
	CycleStartDay      *int           `json:"cycle_start_day,omitempty"`
	CycleCooldownWeeks *int           `json:"cycle_cooldown_weeks,omitempty"`
	TriageEnabled      *bool          `json:"triage_enabled,omitempty"`
	CustomFields       map[string]any `json:"-"`
}

// SettingsService provides workspace settings retrieval and updates.
type SettingsService struct {
	store *Store
}

func (s *SettingsService) targetStore(ctx context.Context) (*Store, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, fmt.Errorf("writ: store is nil")
	}
	if s.store.Workspace != nil && s.store.Workspace.IsConfigured() {
		wsStore, err := s.store.Workspace.getStore(ctx)
		if err != nil {
			return nil, false, err
		}
		return wsStore, true, nil
	}
	return s.store, false, nil
}

// Get fetches the current folded workspace settings, returning defaults if no settings ops exist.
func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	target, _, err := s.targetStore(ctx)
	if err != nil {
		return Settings{}, err
	}
	res, err := target.Query.Settings()
	if err != nil {
		return Settings{}, err
	}
	return res.Settings, nil
}

// Set modifies workspace settings by appending a 'set' op to refs/writ/<writer-id>/settings.
func (s *SettingsService) Set(ctx context.Context, edit SettingsEdit) error {
	target, _, err := s.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	body := make(map[string]any)
	if edit.Name != nil {
		body["name"] = *edit.Name
	}
	if edit.Identifier != nil {
		body["identifier"] = *edit.Identifier
	}
	if edit.Timezone != nil {
		tz := *edit.Timezone
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("writ: invalid timezone %q: %w", tz, err)
		}
		body["timezone"] = tz
	}
	if edit.EstimateScale != nil {
		scale := *edit.EstimateScale
		switch scale {
		case "none", "fibonacci", "exponential", "linear", "t-shirt":
			// valid
		default:
			return fmt.Errorf("writ: invalid estimate scale %q (must be none, fibonacci, exponential, linear, or t-shirt)", scale)
		}
		body["estimate_scale"] = scale
	}
	if edit.AllowZeroEstimates != nil {
		body["allow_zero_estimates"] = *edit.AllowZeroEstimates
	}
	if edit.CyclesEnabled != nil {
		body["cycles_enabled"] = *edit.CyclesEnabled
	}
	if edit.CycleDurationWeeks != nil {
		dur := *edit.CycleDurationWeeks
		if dur < 1 {
			return fmt.Errorf("writ: cycle duration must be at least 1 week, got %d", dur)
		}
		body["cycle_duration_weeks"] = dur
	}
	if edit.CycleStartDay != nil {
		day := *edit.CycleStartDay
		if day < 1 || day > 7 {
			return fmt.Errorf("writ: cycle start day must be 1-7 (1=Monday, 7=Sunday), got %d", day)
		}
		body["cycle_start_day"] = day
	}
	if edit.CycleCooldownWeeks != nil {
		cd := *edit.CycleCooldownWeeks
		if cd < 0 {
			return fmt.Errorf("writ: cycle cooldown must be >= 0 weeks, got %d", cd)
		}
		body["cycle_cooldown_weeks"] = cd
	}
	if edit.TriageEnabled != nil {
		body["triage_enabled"] = *edit.TriageEnabled
	}
	for k, v := range edit.CustomFields {
		body[k] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("writ: no settings fields specified to update")
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal settings body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   state.DefaultSettingsObjectID,
		ObjectType: "settings",
		OpType:     "set",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, nil); err != nil {
		return fmt.Errorf("writ: append settings: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}
