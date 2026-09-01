package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestCompletion_Bash(t *testing.T) {
	var buf bytes.Buffer
	emitBashCompletion(&buf)
	script := buf.String()

	if script == "" {
		t.Fatal("emitBashCompletion produced empty output")
	}

	if !strings.Contains(script, "_writ()") || !strings.Contains(script, "complete -F _writ writ") {
		t.Errorf("bash completion missing entry points")
	}

	// Verify all subcommands mentioned
	expectedWords := []string{
		"init", "issue", "review", "sync", "completion", "help",
		"open", "comment", "approve", "status", "list", "create", "assign", "link",
		"approve", "request-changes", "draft", "merged", "closed",
	}
	for _, word := range expectedWords {
		if !strings.Contains(script, word) {
			t.Errorf("bash completion missing expected word %q", word)
		}
	}

	if _, err := exec.LookPath("bash"); err == nil {
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("bash syntax check failed: %v\nOutput: %s", err, string(out))
		}
	}
}

func TestCompletion_Zsh(t *testing.T) {
	var buf bytes.Buffer
	emitZshCompletion(&buf)
	script := buf.String()

	if script == "" {
		t.Fatal("emitZshCompletion produced empty output")
	}

	if !strings.HasPrefix(script, "#compdef writ") {
		t.Errorf("zsh completion missing #compdef header")
	}

	expectedWords := []string{
		"init", "issue", "review", "sync", "completion", "help",
		"open", "comment", "approve", "status", "list", "create", "assign", "link",
		"approve", "request-changes", "draft", "merged", "closed",
	}
	for _, word := range expectedWords {
		if !strings.Contains(script, word) {
			t.Errorf("zsh completion missing expected word %q", word)
		}
	}

	if _, err := exec.LookPath("zsh"); err == nil {
		cmd := exec.Command("zsh", "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("zsh syntax check failed: %v\nOutput: %s", err, string(out))
		}
	}
}

func TestCompletion_Fish(t *testing.T) {
	var buf bytes.Buffer
	emitFishCompletion(&buf)
	script := buf.String()

	if script == "" {
		t.Fatal("emitFishCompletion produced empty output")
	}

	if !strings.Contains(script, "complete -c writ") {
		t.Errorf("fish completion missing complete -c writ")
	}

	expectedWords := []string{
		"init", "issue", "review", "sync", "completion", "help",
		"open", "comment", "approve", "status", "list", "create", "assign", "link",
		"approve", "request-changes", "draft", "merged", "closed",
	}
	for _, word := range expectedWords {
		if !strings.Contains(script, word) {
			t.Errorf("fish completion missing expected word %q", word)
		}
	}

	if _, err := exec.LookPath("fish"); err == nil {
		cmd := exec.Command("fish", "--no-execute")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("fish syntax check failed: %v\nOutput: %s", err, string(out))
		}
	}
}

func TestCompletion_CLI(t *testing.T) {
	t.Run("missing_shell", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"completion"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("completion no args code = %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "shell required") {
			t.Errorf("stderr missing 'shell required': %s", stderr.String())
		}
	})

	t.Run("unsupported_shell", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"completion", "powershell"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("completion unsupported shell code = %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "unsupported shell") {
			t.Errorf("stderr missing 'unsupported shell': %s", stderr.String())
		}
	})

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run("valid_"+shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"completion", shell}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("completion %s failed with %d: %s", shell, code, stderr.String())
			}
			if stdout.Len() == 0 {
				t.Errorf("completion %s stdout is empty", shell)
			}
		})
	}
}
