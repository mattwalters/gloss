// Package sync implements refspec management and system-git transport
// for Writ append chains.
//
// Following the settled architecture decisions (ARCHITECTURE.md §Language),
// Writ takes a hybrid approach: go-git for local object I/O, and system git
// for all network transport. This ensures SSH agents, credential helpers,
// gitconfig, proxies, and enterprise auth setups work automatically with
// zero credential code in Writ.
//
// Callers who also use the standard library sync package should alias
// this package (e.g. `import writsync "github.com/writtendev/writ/engine/sync"`).
package sync

import (
	"fmt"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/writtendev/writ/engine/identity"
)

// Option configures a Client instance.
type Option func(*Client)

// WithGitBinary configures the git executable binary path (defaults to "git").
func WithGitBinary(gitBin string) Option {
	return func(c *Client) {
		if gitBin != "" {
			c.gitBin = gitBin
		}
	}
}

// WithEnv configures additional environment variables for git subprocess invocations.
func WithEnv(env []string) Option {
	return func(c *Client) {
		c.env = env
	}
}

// Client manages git refspecs and performs network transport (fetch and push)
// for Writ operations using system git.
type Client struct {
	repoDir  string
	repo     *git.Repository
	identity identity.Identity
	gitBin   string
	env      []string
	mu       sync.Mutex
}

// Open opens a git repository at repoDir and returns an initialized Client.
func Open(repoDir string, ident identity.Identity, opts ...Option) (*Client, error) {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return nil, fmt.Errorf("sync: open repo %s: %w", repoDir, err)
	}
	c := &Client{
		repoDir:  repoDir,
		repo:     repo,
		identity: ident,
		gitBin:   "git",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// OpenRepo initializes a Client with an existing go-git repository instance.
func OpenRepo(repo *git.Repository, repoDir string, ident identity.Identity, opts ...Option) (*Client, error) {
	if repo == nil {
		return nil, fmt.Errorf("sync: nil repo")
	}
	c := &Client{
		repoDir:  repoDir,
		repo:     repo,
		identity: ident,
		gitBin:   "git",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// RepoDir returns the repository root directory.
func (c *Client) RepoDir() string {
	return c.repoDir
}

// Repo returns the underlying go-git repository instance.
func (c *Client) Repo() *git.Repository {
	return c.repo
}

// Identity returns the configured writer identity.
func (c *Client) Identity() identity.Identity {
	return c.identity
}

// WriterID returns the writer ID of the configured identity.
func (c *Client) WriterID() identity.WriterID {
	return c.identity.WriterID
}
