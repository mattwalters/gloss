package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHelp_Command(t *testing.T) {
	t.Run("root_help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"help"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("writ help exited with %d; stderr: %s", code, stderr.String())
		}
		out := stdout.String()
		if !strings.Contains(out, "Usage: writ [-C <dir>] <command> [arguments]") {
			t.Errorf("missing root usage: %s", out)
		}
		if !strings.Contains(out, "Commands:") {
			t.Errorf("missing Commands section: %s", out)
		}
		if !strings.Contains(out, "Plumbing:") {
			t.Errorf("missing Plumbing section: %s", out)
		}
	})

	t.Run("command_help", func(t *testing.T) {
		for _, cmd := range []string{"init", "issue", "review", "sync", "version", "completion", "help"} {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"help", cmd}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("writ help %s exited with %d; stderr: %s", cmd, code, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, "Usage: writ "+cmd) {
				t.Errorf("writ help %s missing usage line: %s", cmd, out)
			}
		}
	})

	t.Run("subcommand_help", func(t *testing.T) {
		subcmds := []struct {
			cmd    string
			subcmd string
		}{
			{"review", "open"},
			{"review", "comment"},
			{"review", "approve"},
			{"review", "status"},
			{"review", "list"},
			{"issue", "create"},
			{"issue", "status"},
			{"issue", "assign"},
			{"issue", "list"},
			{"issue", "link"},
		}

		for _, sc := range subcmds {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"help", sc.cmd, sc.subcmd}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("writ help %s %s exited with %d; stderr: %s", sc.cmd, sc.subcmd, code, stderr.String())
			}
			out := stdout.String()
			expectedUsage := "Usage: writ " + sc.cmd + " " + sc.subcmd
			if !strings.Contains(out, expectedUsage) {
				t.Errorf("writ help %s %s missing usage line: %s", sc.cmd, sc.subcmd, out)
			}
			if !strings.Contains(out, "Examples:") {
				t.Errorf("writ help %s %s missing Examples section: %s", sc.cmd, sc.subcmd, out)
			}
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"help", "nonexistent"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("writ help nonexistent exited with %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("stderr missing 'unknown command': %s", stderr.String())
		}
	})

	t.Run("unknown_subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"help", "review", "nonexistent"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("writ help review nonexistent exited with %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "unknown subcommand") {
			t.Errorf("stderr missing 'unknown subcommand': %s", stderr.String())
		}
	})
}

func TestHelp_FlagsRouting(t *testing.T) {
	cmds := [][]string{
		{"-h"},
		{"--help"},
		{"init", "-h"},
		{"init", "--help"},
		{"sync", "-h"},
		{"sync", "--help"},
		{"completion", "-h"},
		{"completion", "--help"},
		{"version", "-h"},
		{"version", "--help"},
		{"issue", "-h"},
		{"issue", "--help"},
		{"issue", "create", "-h"},
		{"issue", "create", "--help"},
		{"issue", "status", "-h"},
		{"issue", "status", "--help"},
		{"issue", "assign", "-h"},
		{"issue", "assign", "--help"},
		{"issue", "list", "-h"},
		{"issue", "list", "--help"},
		{"issue", "link", "-h"},
		{"issue", "link", "--help"},
		{"review", "-h"},
		{"review", "--help"},
		{"review", "open", "-h"},
		{"review", "open", "--help"},
		{"review", "comment", "-h"},
		{"review", "comment", "--help"},
		{"review", "approve", "-h"},
		{"review", "approve", "--help"},
		{"review", "status", "-h"},
		{"review", "status", "--help"},
		{"review", "list", "-h"},
		{"review", "list", "--help"},
	}

	for _, args := range cmds {
		name := strings.Join(args, "_")
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run %v exited with %d; stderr: %s", args, code, stderr.String())
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, "Usage: writ") {
				t.Errorf("run %v missing 'Usage: writ': stdout=%q stderr=%q", args, stdout.String(), stderr.String())
			}
		})
	}
}

