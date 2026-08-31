package writ

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/projection"
	writsync "github.com/writtendev/writ/engine/sync"
)

type openConfig struct {
	cacheDir    string
	signer      codec.Signer
	autoRefresh bool
	gitBin      string
	targetRefs  []string
}

// Option configures an Open invocation.
type Option func(*openConfig)

// WithCacheDir configures an explicit custom directory for the SQLite projection cache.
func WithCacheDir(dir string) Option {
	return func(c *openConfig) {
		c.cacheDir = dir
	}
}

// WithSigner configures an explicit Signer for operation commits.
func WithSigner(signer Signer) Option {
	return func(c *openConfig) {
		c.signer = signer
	}
}

// WithoutAutoRefresh disables automatic projection refresh before and after operations,
// allowing hot loops to manage refresh explicitly via Store.Refresh.
func WithoutAutoRefresh() Option {
	return func(c *openConfig) {
		c.autoRefresh = false
	}
}

// WithGitBinary configures the git executable binary path used for network transport and git config.
func WithGitBinary(gitBin string) Option {
	return func(c *openConfig) {
		c.gitBin = gitBin
	}
}

// WithTargetRefs configures code branch or tag ref names to resolve comment anchors against.
func WithTargetRefs(refs ...string) Option {
	return func(c *openConfig) {
		c.targetRefs = append(c.targetRefs, refs...)
	}
}

// Open opens a git repository at the given path (which may be a repository root,
// subdirectory, linked worktree, or bare repository) and initializes a Store handle.
//
// Open is fully offline and performs no network I/O or automatic projection refresh.
func Open(path string, opts ...Option) (*Store, error) {
	gitInfo, err := ResolveGitDir(path)
	if err != nil {
		return nil, err
	}

	cfg := &openConfig{
		autoRefresh: true,
		gitBin:      "git",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	repoDir := gitInfo.WorkTree
	if repoDir == "" {
		repoDir = gitInfo.GitDir
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return nil, fmt.Errorf("writ: open git repo %s: %w", repoDir, err)
	}

	// Read writer identity (non-fatal if unconfigured)
	ident, identErr := identity.Load(context.Background(), repoDir)
	var hasIdentity bool
	var hasSigner bool
	var signer codec.Signer
	var signerErr error

	if identErr == nil {
		hasIdentity = true
		if cfg.signer != nil {
			signer = cfg.signer
			hasSigner = true
		} else if ident.Key.Value != "" && ident.Key.Format == "ssh" {
			s, err := codec.NewSigner(ident.Key)
			if err == nil {
				signer = s
				hasSigner = true
			} else {
				signerErr = err
			}
		} else {
			signerErr = ErrNoSigningKey
		}
	} else {
		// Check if error was solely due to signing key / gpg format
		var cfgErr *identity.ConfigError
		if errors.As(identErr, &cfgErr) && (cfgErr.Key == "user.signingKey" || cfgErr.Key == "gpg.format") {
			// Writer ID and user name/email are valid and retained in ident
			hasIdentity = true
			identErr = nil // Clear ident error so identity check passes
			if cfg.signer != nil {
				signer = cfg.signer
				hasSigner = true
			} else {
				signerErr = ErrNoSigningKey
			}
		} else {
			hasIdentity = false
			identErr = ErrNoIdentity
			signerErr = ErrNoSigningKey
		}
	}

	// Open DAG store
	var dagOpts []dag.Option
	if hasSigner {
		dagOpts = append(dagOpts, dag.WithSigner(signer))
	}
	dagStore, err := dag.OpenRepo(repo, ident, dagOpts...)
	if err != nil {
		return nil, fmt.Errorf("writ: open dag store: %w", err)
	}

	// Open projection SQLite cache
	cacheDir := cfg.cacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(gitInfo.CommonDir, "writ")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("writ: create projection cache dir %s: %w", cacheDir, err)
	}

	dbPath := filepath.Join(cacheDir, "projection.db")
	projDB, err := projection.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("writ: open projection db %s: %w", dbPath, err)
	}

	// Open sync client
	syncClient, err := writsync.OpenRepo(repo, repoDir, ident, writsync.WithGitBinary(cfg.gitBin))
	if err != nil {
		_ = projDB.Close()
		return nil, fmt.Errorf("writ: open sync client: %w", err)
	}

	s := &Store{
		gitInfo:     gitInfo,
		repo:        repo,
		dagStore:    dagStore,
		projection:  projDB,
		syncClient:  syncClient,
		identity:    ident,
		hasIdentity: hasIdentity,
		identErr:    identErr,
		signer:      signer,
		hasSigner:   hasSigner,
		signerErr:   signerErr,
		autoRefresh: cfg.autoRefresh,
		targetRefs:  cfg.targetRefs,
	}

	s.Reviews = &Reviews{store: s}
	s.Issues = &Issues{store: s}
	s.Comments = &Comments{store: s}
	s.Query = &Query{store: s}

	return s, nil
}
