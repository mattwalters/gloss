package writ

import "github.com/writtendev/writ/internal/gitdir"

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
	info, err := gitdir.Resolve(path)
	if err != nil {
		return GitDirInfo{}, err
	}
	return GitDirInfo{
		WorkTree:  info.WorkTree,
		GitDir:    info.GitDir,
		CommonDir: info.CommonDir,
	}, nil
}
