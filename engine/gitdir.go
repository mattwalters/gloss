package writ

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitDirInfo contains resolved git repository directory paths.
type GitDirInfo struct {
	// WorkTree is the root of the working directory, or empty for bare repositories.
	WorkTree string

	// GitDir is the repository metadata directory (.git or worktree gitdir).
	GitDir string

	// CommonDir is the common .git directory shared across all linked worktrees.
	CommonDir string
}

// ResolveGitDir locates the git directory and common directory for a given path
// purely in Go without invoking external git processes.
func ResolveGitDir(path string) (GitDirInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return GitDirInfo{}, fmt.Errorf("writ: resolve absolute path %q: %w", path, err)
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return GitDirInfo{}, fmt.Errorf("writ: stat path %q: %w", path, err)
	}

	startDir := absPath
	if !fi.IsDir() {
		startDir = filepath.Dir(absPath)
	}

	// 1. Walk up looking for .git
	curr := startDir
	for {
		gitEntry := filepath.Join(curr, ".git")
		entryInfo, err := os.Stat(gitEntry)
		if err == nil {
			if entryInfo.IsDir() {
				// Standard working tree
				gitDir := gitEntry
				commonDir := gitDir
				commondirFile := filepath.Join(gitDir, "commondir")
				if data, err := os.ReadFile(commondirFile); err == nil {
					relCommon := strings.TrimSpace(string(data))
					if relCommon != "" {
						if filepath.IsAbs(relCommon) {
							commonDir = filepath.Clean(relCommon)
						} else {
							commonDir = filepath.Clean(filepath.Join(gitDir, relCommon))
						}
					}
				}
				return GitDirInfo{
					WorkTree:  curr,
					GitDir:    gitDir,
					CommonDir: commonDir,
				}, nil
			}

			// Linked worktree (.git is a file containing "gitdir: <path>")
			data, err := os.ReadFile(gitEntry)
			if err != nil {
				return GitDirInfo{}, fmt.Errorf("writ: read .git file %q: %w", gitEntry, err)
			}

			content := strings.TrimSpace(string(data))
			const gitdirPrefix = "gitdir:"
			if strings.HasPrefix(content, gitdirPrefix) {
				rawTarget := strings.TrimSpace(content[len(gitdirPrefix):])
				var gitDir string
				if filepath.IsAbs(rawTarget) {
					gitDir = filepath.Clean(rawTarget)
				} else {
					gitDir = filepath.Clean(filepath.Join(curr, rawTarget))
				}

				commonDir := gitDir
				commondirFile := filepath.Join(gitDir, "commondir")
				if data, err := os.ReadFile(commondirFile); err == nil {
					relCommon := strings.TrimSpace(string(data))
					if relCommon != "" {
						if filepath.IsAbs(relCommon) {
							commonDir = filepath.Clean(relCommon)
						} else {
							commonDir = filepath.Clean(filepath.Join(gitDir, relCommon))
						}
					}
				}

				return GitDirInfo{
					WorkTree:  curr,
					GitDir:    gitDir,
					CommonDir: commonDir,
				}, nil
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	// 2. Check if startDir itself is a bare repository (contains HEAD, objects, refs)
	if isBareRepo(startDir) {
		gitDir := startDir
		commonDir := gitDir
		commondirFile := filepath.Join(gitDir, "commondir")
		if data, err := os.ReadFile(commondirFile); err == nil {
			relCommon := strings.TrimSpace(string(data))
			if relCommon != "" {
				if filepath.IsAbs(relCommon) {
					commonDir = filepath.Clean(relCommon)
				} else {
					commonDir = filepath.Clean(filepath.Join(gitDir, relCommon))
				}
			}
		}
		return GitDirInfo{
			WorkTree:  "",
			GitDir:    gitDir,
			CommonDir: commonDir,
		}, nil
	}

	return GitDirInfo{}, fmt.Errorf("writ: not a git repository (or any parent up to mount point): %s", path)
}

func isBareRepo(dir string) bool {
	headPath := filepath.Join(dir, "HEAD")
	objectsPath := filepath.Join(dir, "objects")
	refsPath := filepath.Join(dir, "refs")

	if _, err := os.Stat(headPath); err != nil {
		return false
	}
	if fi, err := os.Stat(objectsPath); err != nil || !fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(refsPath); err != nil || !fi.IsDir() {
		return false
	}
	return true
}
