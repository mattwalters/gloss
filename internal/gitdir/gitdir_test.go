package gitdir_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/internal/gitdir"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, string(out))
	}
	return string(out)
}

func evalPath(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return real
}

func TestResolve_StandardRepo(t *testing.T) {
	dir := evalPath(t, t.TempDir())
	runGit(t, dir, "init", "-b", "main")
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "hello.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Test root
	info, err := gitdir.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve root failed: %v", err)
	}
	if evalPath(t, info.WorkTree) != dir {
		t.Errorf("WorkTree = %s, want %s", evalPath(t, info.WorkTree), dir)
	}
	expectedGitDir := filepath.Join(dir, ".git")
	if evalPath(t, info.GitDir) != expectedGitDir {
		t.Errorf("GitDir = %s, want %s", evalPath(t, info.GitDir), expectedGitDir)
	}
	if evalPath(t, info.CommonDir) != expectedGitDir {
		t.Errorf("CommonDir = %s, want %s", evalPath(t, info.CommonDir), expectedGitDir)
	}

	// Test subdirectory
	subDir := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	infoSub, err := gitdir.Resolve(subDir)
	if err != nil {
		t.Fatalf("Resolve subDir failed: %v", err)
	}
	if evalPath(t, infoSub.WorkTree) != dir {
		t.Errorf("SubDir WorkTree = %s, want %s", evalPath(t, infoSub.WorkTree), dir)
	}
	if evalPath(t, infoSub.GitDir) != expectedGitDir {
		t.Errorf("SubDir GitDir = %s, want %s", evalPath(t, infoSub.GitDir), expectedGitDir)
	}

	// Test OpenStorage
	storer := gitdir.OpenStorage(info)
	if storer == nil {
		t.Fatal("OpenStorage returned nil storer")
	}
	ref, err := storer.Reference(plumbing.ReferenceName("refs/heads/main"))
	if err != nil {
		t.Fatalf("storer.Reference failed: %v", err)
	}
	if ref == nil || ref.Hash().IsZero() {
		t.Fatalf("expected valid ref, got %v", ref)
	}
}

func TestResolve_LinkedWorktree(t *testing.T) {
	mainDir := evalPath(t, t.TempDir())
	runGit(t, mainDir, "init", "-b", "main")
	testFile := filepath.Join(mainDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "hello.txt")
	runGit(t, mainDir, "commit", "-m", "initial commit")

	wtDir := filepath.Join(evalPath(t, t.TempDir()), "wt")
	runGit(t, mainDir, "worktree", "add", "-b", "feat", wtDir)

	info, err := gitdir.Resolve(wtDir)
	if err != nil {
		t.Fatalf("Resolve linked worktree failed: %v", err)
	}
	if evalPath(t, info.WorkTree) != wtDir {
		t.Errorf("WorkTree = %s, want %s", evalPath(t, info.WorkTree), wtDir)
	}
	expectedCommonDir := filepath.Join(mainDir, ".git")
	if evalPath(t, info.CommonDir) != expectedCommonDir {
		t.Errorf("CommonDir = %s, want %s", evalPath(t, info.CommonDir), expectedCommonDir)
	}
	if info.GitDir == "" || evalPath(t, info.GitDir) == expectedCommonDir {
		t.Errorf("GitDir = %s, expected separate worktree gitdir", info.GitDir)
	}

	// Test OpenStorage on worktree
	storer := gitdir.OpenStorage(info)
	if storer == nil {
		t.Fatal("OpenStorage returned nil")
	}
	ref, err := storer.Reference(plumbing.ReferenceName("refs/heads/feat"))
	if err != nil {
		t.Fatalf("storer.Reference failed: %v", err)
	}
	if ref == nil || ref.Hash().IsZero() {
		t.Fatalf("expected valid ref, got %v", ref)
	}
}

func TestResolve_BareRepo(t *testing.T) {
	bareDir := evalPath(t, t.TempDir())
	runGit(t, bareDir, "init", "--bare", "-b", "main")

	// Root of bare repo
	info, err := gitdir.Resolve(bareDir)
	if err != nil {
		t.Fatalf("Resolve bare repo failed: %v", err)
	}
	if info.WorkTree != "" {
		t.Errorf("WorkTree = %s, want empty", info.WorkTree)
	}
	if evalPath(t, info.GitDir) != bareDir {
		t.Errorf("GitDir = %s, want %s", evalPath(t, info.GitDir), bareDir)
	}
	if evalPath(t, info.CommonDir) != bareDir {
		t.Errorf("CommonDir = %s, want %s", evalPath(t, info.CommonDir), bareDir)
	}

	// Subdirectory of bare repo
	subDir := filepath.Join(bareDir, "objects")
	infoSub, err := gitdir.Resolve(subDir)
	if err != nil {
		t.Fatalf("Resolve bare repo subDir failed: %v", err)
	}
	if infoSub.WorkTree != "" {
		t.Errorf("SubDir WorkTree = %s, want empty", infoSub.WorkTree)
	}
	if evalPath(t, infoSub.GitDir) != bareDir {
		t.Errorf("SubDir GitDir = %s, want %s", evalPath(t, infoSub.GitDir), bareDir)
	}
}

func TestResolve_NonGitDir(t *testing.T) {
	nonGitDir := evalPath(t, t.TempDir())
	_, err := gitdir.Resolve(nonGitDir)
	if err == nil {
		t.Fatalf("expected error for non-git directory, got nil")
	}
}
