// Package gitdir provides helpers for discovering git repository paths
// and initializing filesystem-backed storage instances without relying on
// go-git's porcelain repository loader.
package gitdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

// Info contains resolved git repository directory paths.
type Info struct {
	// WorkTree is the root of the working directory, or empty for bare repositories.
	WorkTree string

	// GitDir is the repository metadata directory (.git or worktree gitdir).
	GitDir string

	// CommonDir is the common .git directory shared across all linked worktrees.
	CommonDir string
}

// Resolve locates the git directory and common directory for a given path
// purely in Go without invoking external git processes.
func Resolve(path string) (Info, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Info{}, fmt.Errorf("gitdir: resolve absolute path %q: %w", path, err)
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return Info{}, fmt.Errorf("gitdir: stat path %q: %w", path, err)
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
				return Info{
					WorkTree:  curr,
					GitDir:    gitDir,
					CommonDir: commonDir,
				}, nil
			}

			// Linked worktree (.git is a file containing "gitdir: <path>")
			data, err := os.ReadFile(gitEntry)
			if err != nil {
				return Info{}, fmt.Errorf("gitdir: read .git file %q: %w", gitEntry, err)
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

				return Info{
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
		return Info{
			WorkTree:  "",
			GitDir:    gitDir,
			CommonDir: commonDir,
		}, nil
	}

	return Info{}, fmt.Errorf("gitdir: not a git repository (or any parent up to mount point): %s", path)
}

// OpenStorage initializes a filesystem-backed Storage storer for the given git info.
func OpenStorage(info Info) *filesystem.Storage {
	dot := osfs.New(info.GitDir)
	var repositoryFs billy.Filesystem = dot
	if info.CommonDir != "" && info.CommonDir != info.GitDir {
		commonDot := osfs.New(info.CommonDir)
		repositoryFs = dotgit.NewRepositoryFilesystem(dot, commonDot)
	}
	return filesystem.NewStorage(repositoryFs, cache.NewObjectLRUDefault())
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
