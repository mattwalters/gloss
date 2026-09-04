package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
)

func TestSettingsCLI(t *testing.T) {
	ctx := context.Background()
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	// 1. settings get (default)
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "get"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("settings get failed with code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "UTC") {
		t.Errorf("expected UTC in default settings get output, got: %s", out)
	}
	if !strings.Contains(out, "fibonacci") {
		t.Errorf("expected fibonacci in default settings get output, got: %s", out)
	}

	// 2. settings get --json
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "get", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("settings get --json failed with code %d, stderr: %s", code, stderr.String())
	}
	var envWrap wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envWrap); err != nil {
		t.Fatalf("unmarshal json envelope failed: %v", err)
	}
	if envWrap.Kind != wire.KindSettings {
		t.Errorf("expected kind %q, got %q", wire.KindSettings, envWrap.Kind)
	}

	// 3. settings set with invalid args
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "set"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected code 2 for empty settings set, got %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "set", "--allow-zero-estimates", "notabool"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected code 2 for invalid bool, got %d", code)
	}

	// 4. settings set valid flags
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{
		"-C", env.repoDir, "settings", "set",
		"--name", "Acme Team",
		"--identifier", "ACME",
		"--timezone", "America/New_York",
		"--estimate-scale", "t-shirt",
		"--allow-zero-estimates=true",
		"--cycles-enabled=true",
		"--cycle-duration", "3",
		"--cycle-start-day", "2",
		"--cycle-cooldown", "1",
		"--triage-enabled=true",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("settings set failed with code %d, stderr: %s", code, stderr.String())
	}

	// 5. settings get reflects new values
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "get", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("settings get --json failed: %s", stderr.String())
	}
	var env2 struct {
		Kind string            `json:"kind"`
		Data wire.SettingsWire `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env2); err != nil {
		t.Fatalf("unmarshal json failed: %v", err)
	}
	if env2.Data.Name != "Acme Team" {
		t.Errorf("Name = %q, want 'Acme Team'", env2.Data.Name)
	}
	if env2.Data.Identifier != "ACME" {
		t.Errorf("Identifier = %q, want 'ACME'", env2.Data.Identifier)
	}
	if env2.Data.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want 'America/New_York'", env2.Data.Timezone)
	}
	if env2.Data.EstimateScale != "t-shirt" {
		t.Errorf("EstimateScale = %q, want 't-shirt'", env2.Data.EstimateScale)
	}
	if !env2.Data.AllowZeroEstimates {
		t.Errorf("AllowZeroEstimates = false, want true")
	}
	if !env2.Data.CyclesEnabled {
		t.Errorf("CyclesEnabled = false, want true")
	}
	if env2.Data.CycleDurationWeeks != 3 {
		t.Errorf("CycleDurationWeeks = %d, want 3", env2.Data.CycleDurationWeeks)
	}
	if env2.Data.CycleStartDay != 2 {
		t.Errorf("CycleStartDay = %d, want 2", env2.Data.CycleStartDay)
	}
	if env2.Data.CycleCooldownWeeks != 1 {
		t.Errorf("CycleCooldownWeeks = %d, want 1", env2.Data.CycleCooldownWeeks)
	}
	if !env2.Data.TriageEnabled {
		t.Errorf("TriageEnabled = false, want true")
	}

	// 6. settings set --json output
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"-C", env.repoDir, "settings", "set", "--name", "Acme Worldwide", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("settings set --json failed: %s", stderr.String())
	}
	var env3 struct {
		Kind string            `json:"kind"`
		Data wire.SettingsWire `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env3); err != nil {
		t.Fatalf("unmarshal json failed: %v", err)
	}
	if env3.Data.Name != "Acme Worldwide" {
		t.Errorf("Name = %q, want 'Acme Worldwide'", env3.Data.Name)
	}
	if env3.Data.Identifier != "ACME" {
		t.Errorf("Identifier = %q, want 'ACME' (preserved)", env3.Data.Identifier)
	}
}
