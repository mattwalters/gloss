// Package dag implements the DAG store for Writ operations, handling append
// operations onto local writer chains and enumeration across all writer chains.
package dag

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/storage"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/internal/gitdir"
)

// Signer is an alias for codec.Signer.
type Signer = codec.Signer

// SignerFunc is an alias for codec.SignerFunc.
type SignerFunc = codec.SignerFunc

// Option configures a Store instance during Open.
type Option func(*Store)

// WithSigner sets the cryptographic signer used for op commits.
func WithSigner(signer Signer) Option {
	return func(s *Store) {
		s.signer = signer
	}
}

// WithNow sets the time function used for commit timestamps (used in tests).
func WithNow(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = now
		}
	}
}

// Store provides atomic op appends onto local writer chains and multi-writer
// chain enumeration over a git repository.
type Store struct {
	repoDir  string
	storer   storage.Storer
	identity identity.Identity
	signer   Signer
	now      func() time.Time
	mu       sync.Mutex
}

// Open opens a git repository at repoDir and initializes a Store with the given identity.
func Open(repoDir string, ident identity.Identity, opts ...Option) (*Store, error) {
	info, err := gitdir.Resolve(repoDir)
	if err != nil {
		return nil, fmt.Errorf("dag: open repo %s: %w", repoDir, err)
	}
	storer, err := gitdir.OpenStorage(info)
	if err != nil {
		return nil, fmt.Errorf("dag: open repo %s: %w", repoDir, err)
	}
	s := &Store{
		repoDir:  repoDir,
		storer:   storer,
		identity: ident,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// OpenStorage initializes a Store with a storage.Storer.
func OpenStorage(s storage.Storer, ident identity.Identity, opts ...Option) (*Store, error) {
	if s == nil {
		return nil, fmt.Errorf("dag: nil storer")
	}
	store := &Store{
		storer:   s,
		identity: ident,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

// OpenRepo initializes a Store with an existing go-git repository instance.
func OpenRepo(repo *git.Repository, ident identity.Identity, opts ...Option) (*Store, error) {
	if repo == nil {
		return nil, fmt.Errorf("dag: nil repo")
	}
	return OpenStorage(repo.Storer, ident, opts...)
}

// Storer returns the underlying storage.Storer.
func (s *Store) Storer() storage.Storer {
	return s.storer
}

// Identity returns the configured writer identity.
func (s *Store) Identity() identity.Identity {
	return s.identity
}

// WriterID returns the writer ID for the local store.
func (s *Store) WriterID() identity.WriterID {
	return s.identity.WriterID
}
