package writ_test

import (
	"context"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/state"
)

func TestSettingsGetDefaults(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	sett, err := s.Settings.Get(ctx)
	if err != nil {
		t.Fatalf("Settings.Get failed: %v", err)
	}

	if sett.Timezone != "UTC" {
		t.Errorf("expected default Timezone 'UTC', got %q", sett.Timezone)
	}
	if sett.EstimateScale != "fibonacci" {
		t.Errorf("expected default EstimateScale 'fibonacci', got %q", sett.EstimateScale)
	}
	if sett.CycleDurationWeeks != 2 {
		t.Errorf("expected default CycleDurationWeeks 2, got %d", sett.CycleDurationWeeks)
	}
	if sett.CycleStartDay != 1 {
		t.Errorf("expected default CycleStartDay 1, got %d", sett.CycleStartDay)
	}
	if sett.CyclesEnabled != false {
		t.Errorf("expected default CyclesEnabled false, got true")
	}
}

func TestSettingsSetAndGet(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// 1. Validation tests
	if err := s.Settings.Set(ctx, writ.SettingsEdit{}); err == nil {
		t.Errorf("expected error with empty SettingsEdit, got nil")
	}
	badTZ := "Mars/Olympus"
	if err := s.Settings.Set(ctx, writ.SettingsEdit{Timezone: &badTZ}); err == nil {
		t.Errorf("expected error with invalid timezone, got nil")
	}
	badScale := "fib"
	if err := s.Settings.Set(ctx, writ.SettingsEdit{EstimateScale: &badScale}); err == nil {
		t.Errorf("expected error with invalid estimate scale, got nil")
	}
	badDuration := 0
	if err := s.Settings.Set(ctx, writ.SettingsEdit{CycleDurationWeeks: &badDuration}); err == nil {
		t.Errorf("expected error with cycle duration 0, got nil")
	}
	badDay := 8
	if err := s.Settings.Set(ctx, writ.SettingsEdit{CycleStartDay: &badDay}); err == nil {
		t.Errorf("expected error with cycle start day 8, got nil")
	}
	badCooldown := -1
	if err := s.Settings.Set(ctx, writ.SettingsEdit{CycleCooldownWeeks: &badCooldown}); err == nil {
		t.Errorf("expected error with cycle cooldown -1, got nil")
	}

	// 2. Set valid settings
	name := "Acme Corp"
	id := "ACME"
	tz := "America/New_York"
	scale := "t-shirt"
	allowZero := true
	cyclesEnabled := true
	duration := 3
	startDay := 1
	cooldown := 1
	triage := true

	err = s.Settings.Set(ctx, writ.SettingsEdit{
		Name:               &name,
		Identifier:         &id,
		Timezone:           &tz,
		EstimateScale:      &scale,
		AllowZeroEstimates: &allowZero,
		CyclesEnabled:      &cyclesEnabled,
		CycleDurationWeeks: &duration,
		CycleStartDay:      &startDay,
		CycleCooldownWeeks: &cooldown,
		TriageEnabled:      &triage,
	})
	if err != nil {
		t.Fatalf("Settings.Set failed: %v", err)
	}

	sett, err := s.Settings.Get(ctx)
	if err != nil {
		t.Fatalf("Settings.Get failed: %v", err)
	}

	if sett.Name != "Acme Corp" {
		t.Errorf("Name = %q, want 'Acme Corp'", sett.Name)
	}
	if sett.Identifier != "ACME" {
		t.Errorf("Identifier = %q, want 'ACME'", sett.Identifier)
	}
	if sett.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want 'America/New_York'", sett.Timezone)
	}
	if sett.EstimateScale != "t-shirt" {
		t.Errorf("EstimateScale = %q, want 't-shirt'", sett.EstimateScale)
	}
	if !sett.AllowZeroEstimates {
		t.Errorf("AllowZeroEstimates = false, want true")
	}
	if !sett.CyclesEnabled {
		t.Errorf("CyclesEnabled = false, want true")
	}
	if sett.CycleDurationWeeks != 3 {
		t.Errorf("CycleDurationWeeks = %d, want 3", sett.CycleDurationWeeks)
	}
	if sett.CycleStartDay != 1 {
		t.Errorf("CycleStartDay = %d, want 1", sett.CycleStartDay)
	}
	if sett.CycleCooldownWeeks != 1 {
		t.Errorf("CycleCooldownWeeks = %d, want 1", sett.CycleCooldownWeeks)
	}
	if !sett.TriageEnabled {
		t.Errorf("TriageEnabled = false, want true")
	}

	// 3. Partial update preserves untouched fields
	newName := "Acme Global"
	err = s.Settings.Set(ctx, writ.SettingsEdit{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("partial Settings.Set failed: %v", err)
	}

	sett2, err := s.Settings.Get(ctx)
	if err != nil {
		t.Fatalf("Settings.Get failed: %v", err)
	}
	if sett2.Name != "Acme Global" {
		t.Errorf("Name = %q, want 'Acme Global'", sett2.Name)
	}
	if sett2.Identifier != "ACME" {
		t.Errorf("Identifier = %q, want 'ACME'", sett2.Identifier)
	}
	if sett2.EstimateScale != "t-shirt" {
		t.Errorf("EstimateScale = %q, want 't-shirt'", sett2.EstimateScale)
	}
}

func TestSettingsSetCausalParentsFrontier(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupTestRepoWithID(t, "alice", "alice@writ.dev")

	sA, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open Alice failed: %v", err)
	}
	defer sA.Close()

	name1 := "Alice Settings"
	if err := sA.Settings.Set(ctx, writ.SettingsEdit{Name: &name1}); err != nil {
		t.Fatalf("Alice Settings.Set failed: %v", err)
	}

	// Switch writer to Bob in the same repository
	runGitCmd(t, repoDir, "config", "writ.writerId", "fedcba9876543210")
	runGitCmd(t, repoDir, "config", "user.name", "bob")
	runGitCmd(t, repoDir, "config", "user.email", "bob@writ.dev")
	runGitCmd(t, repoDir, "config", "gpg.format", "ssh")
	runGitCmd(t, repoDir, "config", "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGbob")

	sB, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open Bob failed: %v", err)
	}
	defer sB.Close()

	name2 := "Bob Settings"
	if err := sB.Settings.Set(ctx, writ.SettingsEdit{Name: &name2}); err != nil {
		t.Fatalf("Bob Settings.Set failed: %v", err)
	}

	enumRes, err := writ.StoreDAGStore(sB).Enumerate()
	if err != nil {
		t.Fatalf("Enumerate failed: %v", err)
	}

	ops := enumRes.Ops[state.DefaultSettingsObjectID]
	if len(ops) < 2 {
		t.Fatalf("expected at least 2 settings ops, got %d", len(ops))
	}

	var aliceOp, bobOp codec.Op
	for _, op := range ops {
		if op.Author.Name == "alice" {
			aliceOp = op
		} else if op.Author.Name == "bob" {
			bobOp = op
		}
	}

	if aliceOp.ID == "" {
		t.Fatalf("aliceOp not found")
	}
	if bobOp.ID == "" {
		t.Fatalf("bobOp not found")
	}

	found := false
	for _, p := range bobOp.Parents {
		if p == aliceOp.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("bobOp.Parents = %v, want to contain aliceOp.ID %s", bobOp.Parents, aliceOp.ID)
	}
}
