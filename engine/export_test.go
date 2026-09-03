package writ

import (
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/projection"
)

// StoreDAGStore returns the underlying dag.Store for testing.
func StoreDAGStore(s *Store) *dag.Store {
	return s.dagStore
}

// StoreProjection returns the underlying projection.DB for testing.
func StoreProjection(s *Store) *projection.DB {
	return s.projection
}
