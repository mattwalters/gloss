package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/sync"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping test")
	}
}

type testCLIEnv struct {
	repoDir       string
	globalCfgPath string
}

func setupTestCLIEnv(t *testing.T) testCLIEnv {
	t.Helper()
	requireGit(t)

	tempDir := t.TempDir()
	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}

	repoDir := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, string(out))
	}

	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	return testCLIEnv{
		repoDir:       repoDir,
		globalCfgPath: globalCfgPath,
	}
}

func setGitConfig(t *testing.T, dir, key, val string) {
	t.Helper()
	cmd := exec.Command("git", "config", key, val)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config %s %s failed: %v (%s)", key, val, err, string(out))
	}
}

func setFileConfig(t *testing.T, filePath, key, val string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--file", filePath, key, val)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config --file %s %s %s failed: %v (%s)", filePath, key, val, err, string(out))
	}
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add %s %s failed: %v (%s)", name, url, err, string(out))
	}
}

func getGitConfigAll(t *testing.T, dir, key string) []string {
	t.Helper()
	cmd := exec.Command("git", "config", "--get-all", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if err != nil && len(out) == 0 {
			return nil
		}
		_ = exitErr
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var res []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}

func TestInit_Idempotent(t *testing.T) {
	env := setupTestCLIEnv(t)
	addRemote(t, env.repoDir, "origin", "https://example.com/repo.git")

	var stdout1, stderr1 bytes.Buffer
	code1 := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout1, &stderr1)
	if code1 != 0 {
		t.Fatalf("run init (first) exited with %d; stderr: %s", code1, stderr1.String())
	}

	fetchEntries1 := getGitConfigAll(t, env.repoDir, "remote.origin.fetch")
	var writEntries1 []string
	for _, e := range fetchEntries1 {
		if strings.Contains(e, "writ") {
			writEntries1 = append(writEntries1, e)
		}
	}
	if len(writEntries1) != 1 {
		t.Fatalf("expected 1 writ fetch refspec after first init, got %d: %v", len(writEntries1), writEntries1)
	}
	if writEntries1[0] != "refs/writ/*:refs/remotes/origin/writ/*" {
		t.Errorf("refspec = %q, want refs/writ/*:refs/remotes/origin/writ/*", writEntries1[0])
	}

	writerID1 := getGitConfigAll(t, env.repoDir, "writ.writerId")
	if len(writerID1) != 1 || len(writerID1[0]) != 16 {
		t.Fatalf("expected valid writerId, got %v", writerID1)
	}

	// Run init second time
	var stdout2, stderr2 bytes.Buffer
	code2 := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout2, &stderr2)
	if code2 != 0 {
		t.Fatalf("run init (second) exited with %d; stderr: %s", code2, stderr2.String())
	}

	fetchEntries2 := getGitConfigAll(t, env.repoDir, "remote.origin.fetch")
	var writEntries2 []string
	for _, e := range fetchEntries2 {
		if strings.Contains(e, "writ") {
			writEntries2 = append(writEntries2, e)
		}
	}
	if len(writEntries2) != 1 {
		t.Fatalf("expected 1 writ fetch refspec after second init, got %d: %v", len(writEntries2), writEntries2)
	}

	writerID2 := getGitConfigAll(t, env.repoDir, "writ.writerId")
	if len(writerID2) != 1 || writerID2[0] != writerID1[0] {
		t.Errorf("writer ID changed on second run: %v vs %v", writerID2, writerID1)
	}

	if !strings.Contains(stdout2.String(), "already configured") {
		t.Errorf("stdout2 does not mention 'already configured': %s", stdout2.String())
	}
}

func TestInit_DriftRepair(t *testing.T) {
	env := setupTestCLIEnv(t)
	addRemote(t, env.repoDir, "origin", "https://example.com/repo.git")

	// Pre-seed forced writ refspec alongside head and custom refspecs
	setGitConfig(t, env.repoDir, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	cmd := exec.Command("git", "config", "--add", "remote.origin.fetch", "+refs/writ/*:refs/remotes/origin/writ/*")
	cmd.Dir = env.repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("seed forced refspec: %v", err)
	}
	cmd = exec.Command("git", "config", "--add", "remote.origin.fetch", "refs/custom/*:refs/remotes/origin/custom/*")
	cmd.Dir = env.repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("seed custom refspec: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run init exited with %d; stderr: %s", code, stderr.String())
	}

	fetchEntries := getGitConfigAll(t, env.repoDir, "remote.origin.fetch")
	expectedEntries := []string{
		"+refs/heads/*:refs/remotes/origin/*",
		"refs/custom/*:refs/remotes/origin/custom/*",
		"refs/writ/*:refs/remotes/origin/writ/*",
	}

	if len(fetchEntries) != len(expectedEntries) {
		t.Fatalf("fetch entries = %v, want %v", fetchEntries, expectedEntries)
	}
	for i, exp := range expectedEntries {
		if fetchEntries[i] != exp {
			t.Errorf("fetchEntries[%d] = %q, want %q", i, fetchEntries[i], exp)
		}
	}
}

func TestInit_WriterIDPrecedence(t *testing.T) {
	t.Run("local_preset", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.writerId", "1111111111111111")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exited with %d; stderr: %s", code, stderr.String())
		}
		got := getGitConfigAll(t, env.repoDir, "writ.writerId")
		if len(got) != 1 || got[0] != "1111111111111111" {
			t.Errorf("writerId = %v, want [\"1111111111111111\"]", got)
		}
		if !strings.Contains(stdout.String(), "1111111111111111") {
			t.Errorf("stdout does not contain local writer ID: %s", stdout.String())
		}
	})

	t.Run("unset_mints", func(t *testing.T) {
		env := setupTestCLIEnv(t)

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exited with %d; stderr: %s", code, stderr.String())
		}
		got := getGitConfigAll(t, env.repoDir, "writ.writerId")
		re := regexp.MustCompile(`^[0-9a-f]{16}$`)
		if len(got) != 1 || !re.MatchString(got[0]) {
			t.Errorf("minted writerId = %v, want valid 16 hex chars", got)
		}
		if !strings.Contains(stdout.String(), "minted") {
			t.Errorf("stdout does not indicate minted: %s", stdout.String())
		}
	})

	t.Run("global_only", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setFileConfig(t, env.globalCfgPath, "writ.writerId", "2222222222222222")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exited with %d; stderr: %s", code, stderr.String())
		}

		// Verify global ID was used
		if !strings.Contains(stdout.String(), "2222222222222222") {
			t.Errorf("stdout does not contain global writer ID: %s", stdout.String())
		}

		// Verify not written to local config
		cmd := exec.Command("git", "config", "--local", "--get", "writ.writerId")
		cmd.Dir = env.repoDir
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("git config --local writ.writerId should not be set, but got %q", string(out))
		}
	})
}

func TestInit_SigningKeyGuidance(t *testing.T) {
	env := setupTestCLIEnv(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exited with %d (want 0 for missing signing key); stderr: %s", code, stderr.String())
	}

	errStr := stderr.String()
	if !strings.Contains(errStr, "git config gpg.format ssh") {
		t.Errorf("stderr does not advise 'git config gpg.format ssh': %s", errStr)
	}
	if !strings.Contains(errStr, "git config user.signingKey") {
		t.Errorf("stderr does not advise 'git config user.signingKey': %s", errStr)
	}
	if !strings.Contains(errStr, "git config gpg.ssh.allowedSignersFile") {
		t.Errorf("stderr does not mention allowedSignersFile: %s", errStr)
	}
}

func TestInit_E2E_PlainGitFetch(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	// 1. Bare remote repo
	bareDir := filepath.Join(tempDir, "bare.git")
	_, err := git.PlainInit(bareDir, true)
	if err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	// 2. Clone A
	cloneADir := filepath.Join(tempDir, "cloneA")
	cmd := exec.Command("git", "clone", bareDir, cloneADir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone A failed: %v (%s)", err, string(out))
	}
	const writerIDA = "aaaaaaaaaaaaaaaa"
	setGitConfig(t, cloneADir, "writ.writerId", writerIDA)
	setGitConfig(t, cloneADir, "user.name", "Alice")
	setGitConfig(t, cloneADir, "user.email", "alice@example.com")
	setGitConfig(t, cloneADir, "gpg.format", "ssh")
	setGitConfig(t, cloneADir, "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyBlob")

	identA := identity.Identity{
		WriterID: identity.WriterID(writerIDA),
		Author:   identity.Author{Name: "Alice", Email: "alice@example.com"},
		Key:      identity.SigningKey{Format: "ssh", Value: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyBlob", Literal: true},
	}

	storeA, err := dag.Open(cloneADir, identA, dag.WithNow(func() time.Time {
		return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatalf("dag.Open clone A: %v", err)
	}

	bodyBytes, _ := json.Marshal(map[string]any{"action": "create"})
	envOp := codec.Envelope{
		ObjectID:   "rev-1234567890abcdef",
		ObjectType: "review",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}
	opA, err := storeA.Append(context.Background(), envOp, nil)
	if err != nil {
		t.Fatalf("storeA.Append: %v", err)
	}

	syncA, err := sync.Open(cloneADir, identA)
	if err != nil {
		t.Fatalf("sync.Open clone A: %v", err)
	}
	pushResult, err := syncA.Push(context.Background(), "origin")
	if err != nil {
		t.Fatalf("syncA.Push: %v", err)
	}
	if len(pushResult.PushedRefs) == 0 {
		t.Fatalf("syncA.Push did not push any refs")
	}

	// 3. Clone B
	cloneBDir := filepath.Join(tempDir, "cloneB")
	cmd = exec.Command("git", "clone", bareDir, cloneBDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone B failed: %v (%s)", err, string(out))
	}
	setGitConfig(t, cloneBDir, "user.name", "Bob")
	setGitConfig(t, cloneBDir, "user.email", "bob@example.com")

	// 4. In Clone B, run writ init
	var stdoutB, stderrB bytes.Buffer
	codeB := run(context.Background(), []string{"init", "-C", cloneBDir}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("writ init in clone B exited with %d; stderr: %s", codeB, stderrB.String())
	}

	// 5. In Clone B, run plain git fetch origin
	cmdFetch := exec.Command("git", "fetch", "origin")
	cmdFetch.Dir = cloneBDir
	if out, err := cmdFetch.CombinedOutput(); err != nil {
		t.Fatalf("plain git fetch origin in clone B failed: %v (%s)", err, string(out))
	}

	// 6. Assert remote tracking ref exists in Clone B and points to opA.ID
	repoB, err := git.PlainOpen(cloneBDir)
	if err != nil {
		t.Fatalf("open clone B repo: %v", err)
	}
	remoteRefName := plumbing.ReferenceName("refs/remotes/origin/writ/" + writerIDA + "/review")
	refB, err := repoB.Reference(remoteRefName, true)
	if err != nil {
		t.Fatalf("Clone B missing remote tracking ref %s: %v", remoteRefName, err)
	}
	if refB.Hash().String() != opA.ID {
		t.Errorf("Clone B ref %s hash = %s, want %s", remoteRefName, refB.Hash(), opA.ID)
	}

	// 7. Assert Clone B's own refs/writ/* namespace is completely untouched
	iterB, err := repoB.References()
	if err != nil {
		t.Fatalf("iter references clone B: %v", err)
	}
	defer iterB.Close()
	_ = iterB.ForEach(func(ref *plumbing.Reference) error {
		if strings.HasPrefix(ref.Name().String(), "refs/writ/") {
			t.Errorf("Clone B has unexpected local writ ref: %s", ref.Name())
		}
		return nil
	})
}

func TestInit_MultiRemoteAndPositional(t *testing.T) {
	t.Run("all_remotes_by_default", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		addRemote(t, env.repoDir, "origin", "https://example.com/origin.git")
		addRemote(t, env.repoDir, "upstream", "https://example.com/upstream.git")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exited with %d; stderr: %s", code, stderr.String())
		}

		originFetch := getGitConfigAll(t, env.repoDir, "remote.origin.fetch")
		upstreamFetch := getGitConfigAll(t, env.repoDir, "remote.upstream.fetch")

		if len(originFetch) == 0 || !strings.Contains(originFetch[len(originFetch)-1], "refs/writ/*:refs/remotes/origin/writ/*") {
			t.Errorf("origin fetch refspec missing: %v", originFetch)
		}
		if len(upstreamFetch) == 0 || !strings.Contains(upstreamFetch[len(upstreamFetch)-1], "refs/writ/*:refs/remotes/upstream/writ/*") {
			t.Errorf("upstream fetch refspec missing: %v", upstreamFetch)
		}
	})

	t.Run("positional_narrowing", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		addRemote(t, env.repoDir, "origin", "https://example.com/origin.git")
		addRemote(t, env.repoDir, "upstream", "https://example.com/upstream.git")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-C", env.repoDir, "origin"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("init exited with %d; stderr: %s", code, stderr.String())
		}

		originFetch := getGitConfigAll(t, env.repoDir, "remote.origin.fetch")
		upstreamFetch := getGitConfigAll(t, env.repoDir, "remote.upstream.fetch")

		if len(originFetch) == 0 || !strings.Contains(originFetch[len(originFetch)-1], "refs/writ/*:refs/remotes/origin/writ/*") {
			t.Errorf("origin fetch refspec missing: %v", originFetch)
		}
		for _, e := range upstreamFetch {
			if strings.Contains(e, "writ") {
				t.Errorf("upstream unexpectedly configured when narrowing to origin: %v", upstreamFetch)
			}
		}
	})
}

func TestInit_NoRemotes(t *testing.T) {
	env := setupTestCLIEnv(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init with no remotes exited with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No git remotes configured") {
		t.Errorf("stdout does not note missing remotes: %s", stdout.String())
	}
	writerID := getGitConfigAll(t, env.repoDir, "writ.writerId")
	if len(writerID) != 1 || len(writerID[0]) != 16 {
		t.Errorf("writerId not minted: %v", writerID)
	}
}

func TestInit_BareRepository(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "bare.git")
	_, err := git.PlainInit(bareDir, true)
	if err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", bareDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init on bare repo failed with %d; stderr: %s", code, stderr.String())
	}

	writerID := getGitConfigAll(t, bareDir, "writ.writerId")
	if len(writerID) != 1 || len(writerID[0]) != 16 {
		t.Errorf("bare repo writerId not minted: %v", writerID)
	}
}

func TestInit_NonRepo(t *testing.T) {
	requireGit(t)
	nonRepoDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", nonRepoDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("init on non-repo exited with %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not a git repository") {
		t.Errorf("stderr does not mention not a git repo: %s", stderr.String())
	}
}

func TestRoot_Dispatch(t *testing.T) {
	t.Run("no_args", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), nil, &stdout, &stderr)
		if code != 2 {
			t.Errorf("no args exit code = %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "Usage:") {
			t.Errorf("stderr does not contain usage: %s", stderr.String())
		}
	})

	t.Run("unknown_command", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"foobar"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("unknown command exit code = %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("stderr does not mention unknown command: %s", stderr.String())
		}
	})

	t.Run("help_flags", func(t *testing.T) {
		flags := []string{"-h", "-help", "--help", "help"}
		for _, f := range flags {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{f}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("%s exit code = %d, want 0", f, code)
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Errorf("%s stdout does not contain usage: %s", f, stdout.String())
			}
		}
	})

	t.Run("init_help", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"init", "-h"}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("init -h exit code = %d, want 0", code)
		}
		if !strings.Contains(stderr.String(), "Usage: writ init") {
			t.Errorf("init -h output does not contain usage: %s", stderr.String())
		}
	})

	t.Run("root_C_flag", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		addRemote(t, env.repoDir, "origin", "https://example.com/repo.git")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"-C", env.repoDir, "init"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("root -C flag exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
	})

	t.Run("root_C_flag_with_help", func(t *testing.T) {
		env := setupTestCLIEnv(t)

		for _, flag := range []string{"-h", "--help", "help"} {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"-C", env.repoDir, flag}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("root -C flag with %s exit code = %d, want 0; stderr: %s", flag, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Errorf("stdout does not contain usage for %s: %s", flag, stdout.String())
			}
		}
	})

	t.Run("root_C_flag_missing_arg", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"-C"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("root -C without arg exit code = %d, want 2", code)
		}
	})
}
