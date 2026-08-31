package projection

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
)

// ObjectChange describes the modifications made to a collaborative object in an incremental refresh batch.
type ObjectChange struct {
	// ObjectID is the unique identifier of the collaborative object.
	ObjectID string `json:"object_id"`

	// ObjectType is the type of collaborative object (e.g. "review", "issue", "comment", "project", "cycle", "repo").
	ObjectType string `json:"object_type"`

	// OpTypes lists the distinct operation types applied to the object in this refresh batch.
	OpTypes []string `json:"op_types"`

	// Created reports whether the batch contained the creation operation for this object.
	Created bool `json:"created"`
}

// Stats reports the work performed during a Refresh or Rebuild pass.
type Stats struct {
	// OpsDecoded is the number of commits decoded from git during this pass.
	OpsDecoded int `json:"ops_decoded"`

	// ObjectsTouched is the number of collaborative objects refolded during this pass.
	ObjectsTouched int `json:"objects_touched"`

	// AnchorsResolved is the number of anchor resolution evaluations performed during this pass.
	AnchorsResolved int `json:"anchors_resolved"`

	// Rebuilt reports whether a full from-scratch rebuild was executed (e.g. on rollback, ref deletion, or explicit rebuild).
	Rebuilt bool `json:"rebuilt"`

	// Changed lists the objects modified during an incremental refresh pass.
	// Left empty on a full rebuild, where Rebuilt: true indicates all objects may have changed.
	Changed []ObjectChange `json:"changed,omitempty"`
}

type refreshConfig struct {
	targetRefs []string
}

// Option configures a Refresh or Rebuild pass.
type Option func(*refreshConfig)

// WithTargetRefs specifies explicit code ref names to resolve comment anchors against.
// If omitted, Refresh defaults to resolving against HEAD's ref.
func WithTargetRefs(refs ...string) Option {
	return func(c *refreshConfig) {
		c.targetRefs = append(c.targetRefs, refs...)
	}
}

// Refresh incrementally brings the projection SQLite cache up to date with the underlying DAG store.
// If a chain rollback or deleted chain ref is detected, Refresh falls through to a full rebuild.
func (d *DB) Refresh(store *dag.Store, opts ...Option) (Stats, error) {
	if d == nil || d.db == nil {
		return Stats{}, fmt.Errorf("projection: database is closed")
	}
	if store == nil {
		return Stats{}, fmt.Errorf("projection: nil dag.Store")
	}

	cfg := &refreshConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	targetTips, err := resolveTargetTips(store.Repo(), cfg.targetRefs)
	if err != nil {
		return Stats{}, fmt.Errorf("projection: resolve target tips: %w", err)
	}

	// 1. Read stored chain tips
	storedCursors, err := d.loadChainTips()
	if err != nil {
		return Stats{}, fmt.Errorf("projection: load chain tips: %w", err)
	}

	// 2. Discover current chains
	currentChains, err := dag.Chains(store.Repo().Storer)
	if err != nil {
		return Stats{}, fmt.Errorf("projection: discover chains: %w", err)
	}

	// Check if any previously stored chain has disappeared
	disappeared := false
	for refName := range storedCursors {
		if _, ok := currentChains[refName]; !ok {
			disappeared = true
			break
		}
	}

	if disappeared {
		// Fall through to full rebuild
		return d.rebuildWithConfig(store, cfg, targetTips)
	}

	// 3. Enumerate delta since stored cursors
	enumRes, err := store.EnumerateSince(storedCursors)
	if err != nil {
		return Stats{}, fmt.Errorf("projection: enumerate since cursors: %w", err)
	}

	if len(enumRes.Rewound) > 0 {
		// Rollback detected: fall through to full rebuild
		return d.rebuildWithConfig(store, cfg, targetTips)
	}

	// 4. Fast-forward incremental path in a single transaction
	tx, err := d.db.Begin()
	if err != nil {
		return Stats{}, fmt.Errorf("projection: begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Update chain_tips
	if _, err := tx.Exec("DELETE FROM chain_tips"); err != nil {
		return Stats{}, fmt.Errorf("projection: clear chain_tips: %w", err)
	}
	for refName, tip := range enumRes.Cursors {
		if _, err := tx.Exec("INSERT INTO chain_tips (ref_name, tip) VALUES (?, ?)", refName, tip); err != nil {
			return Stats{}, fmt.Errorf("projection: insert chain tip %s: %w", refName, err)
		}
	}

	// Update code_tips
	if _, err := tx.Exec("DELETE FROM code_tips"); err != nil {
		return Stats{}, fmt.Errorf("projection: clear code_tips: %w", err)
	}
	for refName, tip := range targetTips {
		if _, err := tx.Exec("INSERT INTO code_tips (ref_name, tip) VALUES (?, ?)", refName, tip); err != nil {
			return Stats{}, fmt.Errorf("projection: insert code tip %s: %w", refName, err)
		}
	}

	// Insert newly enumerated ops into ops table
	for _, ops := range enumRes.Ops {
		for _, op := range ops {
			if err := insertOp(tx, op); err != nil {
				return Stats{}, err
			}
		}
	}

	// Refold only touched objects
	for objID := range enumRes.Ops {
		opsForObj, err := readOpsForObject(tx, objID)
		if err != nil {
			return Stats{}, fmt.Errorf("projection: read ops for object %s: %w", objID, err)
		}
		if err := materializeObject(tx, objID, opsForObj); err != nil {
			return Stats{}, fmt.Errorf("projection: materialize object %s: %w", objID, err)
		}
	}

	// Materialize / re-resolve anchors against current code_tips
	anchorsResolved, err := materializeAnchors(tx, store.Repo())
	if err != nil {
		return Stats{}, fmt.Errorf("projection: materialize anchors: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("projection: commit refresh: %w", err)
	}

	var changed []ObjectChange
	objIDs := make([]string, 0, len(enumRes.Ops))
	for objID := range enumRes.Ops {
		objIDs = append(objIDs, objID)
	}
	sort.Strings(objIDs)

	for _, objID := range objIDs {
		ops := enumRes.Ops[objID]
		if len(ops) == 0 {
			continue
		}
		objType := ops[0].ObjectType
		var opTypes []string
		seenOpTypes := make(map[string]bool)
		created := false
		for _, op := range ops {
			if !seenOpTypes[op.OpType] {
				seenOpTypes[op.OpType] = true
				opTypes = append(opTypes, op.OpType)
			}
			if op.OpType == "create" {
				created = true
			}
		}
		sort.Strings(opTypes)
		changed = append(changed, ObjectChange{
			ObjectID:   objID,
			ObjectType: objType,
			OpTypes:    opTypes,
			Created:    created,
		})
	}

	return Stats{
		OpsDecoded:      enumRes.DecodedCommits,
		ObjectsTouched:  len(enumRes.Ops),
		AnchorsResolved: anchorsResolved,
		Rebuilt:         false,
		Changed:         changed,
	}, nil
}

// Rebuild completely discards and recreates the projection cache from a cold walk of all writ chains.
func (d *DB) Rebuild(store *dag.Store, opts ...Option) (Stats, error) {
	if d == nil || d.db == nil {
		return Stats{}, fmt.Errorf("projection: database is closed")
	}
	if store == nil {
		return Stats{}, fmt.Errorf("projection: nil dag.Store")
	}

	cfg := &refreshConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	targetTips, err := resolveTargetTips(store.Repo(), cfg.targetRefs)
	if err != nil {
		return Stats{}, fmt.Errorf("projection: resolve target tips: %w", err)
	}

	return d.rebuildWithConfig(store, cfg, targetTips)
}

func (d *DB) rebuildWithConfig(store *dag.Store, cfg *refreshConfig, targetTips map[string]string) (Stats, error) {
	enumRes, err := store.EnumerateSince(nil)
	if err != nil {
		return Stats{}, fmt.Errorf("projection: cold enumerate: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return Stats{}, fmt.Errorf("projection: begin rebuild transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Clear all projection tables
	for _, t := range projectionTables {
		if t == "meta" {
			continue
		}
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return Stats{}, fmt.Errorf("projection: truncate table %s: %w", t, err)
		}
	}

	// Insert chain_tips
	for refName, tip := range enumRes.Cursors {
		if _, err := tx.Exec("INSERT INTO chain_tips (ref_name, tip) VALUES (?, ?)", refName, tip); err != nil {
			return Stats{}, fmt.Errorf("projection: insert chain tip %s: %w", refName, err)
		}
	}

	// Insert code_tips
	for refName, tip := range targetTips {
		if _, err := tx.Exec("INSERT INTO code_tips (ref_name, tip) VALUES (?, ?)", refName, tip); err != nil {
			return Stats{}, fmt.Errorf("projection: insert code tip %s: %w", refName, err)
		}
	}

	// Insert all ops
	for _, ops := range enumRes.Ops {
		for _, op := range ops {
			if err := insertOp(tx, op); err != nil {
				return Stats{}, err
			}
		}
	}

	// Fold all objects
	for objID := range enumRes.Ops {
		opsForObj, err := readOpsForObject(tx, objID)
		if err != nil {
			return Stats{}, fmt.Errorf("projection: read ops for object %s: %w", objID, err)
		}
		if err := materializeObject(tx, objID, opsForObj); err != nil {
			return Stats{}, fmt.Errorf("projection: materialize object %s: %w", objID, err)
		}
	}

	// Materialize anchors
	anchorsResolved, err := materializeAnchors(tx, store.Repo())
	if err != nil {
		return Stats{}, fmt.Errorf("projection: materialize anchors: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("projection: commit rebuild: %w", err)
	}

	return Stats{
		OpsDecoded:      enumRes.DecodedCommits,
		ObjectsTouched:  len(enumRes.Ops),
		AnchorsResolved: anchorsResolved,
		Rebuilt:         true,
	}, nil
}

func (d *DB) loadChainTips() (dag.CursorSet, error) {
	rows, err := d.db.Query("SELECT ref_name, tip FROM chain_tips")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cursors := make(dag.CursorSet)
	for rows.Next() {
		var refName, tip string
		if err := rows.Scan(&refName, &tip); err != nil {
			return nil, err
		}
		cursors[refName] = tip
	}
	return cursors, rows.Err()
}

func insertOp(tx *sql.Tx, op codec.Op) error {
	parentsJSON, err := json.Marshal(op.Parents)
	if err != nil {
		return fmt.Errorf("projection: marshal parents for op %s: %w", op.ID, err)
	}

	payload := op.Raw
	if len(payload) == 0 {
		encoded, err := codec.EncodePayload(op.Envelope)
		if err != nil {
			return fmt.Errorf("projection: encode payload for op %s: %w", op.ID, err)
		}
		payload = encoded
	}

	authorTime := op.Author.When.UTC().Unix()
	authorTZ := op.Author.When.Format("-0700")
	committerTime := op.Committer.When.UTC().Unix()
	committerTZ := op.Committer.When.Format("-0700")

	var sig sql.NullString
	if op.Signature != "" {
		sig = sql.NullString{String: op.Signature, Valid: true}
	}

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO ops (
			op_id, object_id, object_type, op_type, op_version,
			parents, author_name, author_email, author_time, author_tz,
			committer_name, committer_email, committer_time, committer_tz,
			message, signature, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		op.ID, op.ObjectID, op.ObjectType, op.OpType, op.OpVersion,
		string(parentsJSON), op.Author.Name, op.Author.Email, authorTime, authorTZ,
		op.Committer.Name, op.Committer.Email, committerTime, committerTZ,
		op.Message, sig, payload,
	)
	if err != nil {
		return fmt.Errorf("projection: insert op %s: %w", op.ID, err)
	}
	return nil
}

func readOpsForObject(tx *sql.Tx, objectID string) ([]codec.Op, error) {
	rows, err := tx.Query(`
		SELECT op_id, parents, author_name, author_email, author_time, author_tz,
		       committer_name, committer_email, committer_time, committer_tz,
		       message, signature, payload
		FROM ops
		WHERE object_id = ?
		ORDER BY op_id ASC
	`, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []codec.Op
	for rows.Next() {
		var (
			opID, parentsJSON, authorName, authorEmail, authorTZ       string
			authorTime, committerTime                                  int64
			committerName, committerEmail, committerTZ, message        string
			sig                                                        sql.NullString
			payload                                                    []byte
		)

		if err := rows.Scan(
			&opID, &parentsJSON, &authorName, &authorEmail, &authorTime, &authorTZ,
			&committerName, &committerEmail, &committerTime, &committerTZ,
			&message, &sig, &payload,
		); err != nil {
			return nil, err
		}

		env, err := codec.DecodePayload(payload)
		if err != nil {
			return nil, fmt.Errorf("projection: decode payload for op %s: %w", opID, err)
		}

		var parents []string
		if len(parentsJSON) > 0 {
			if err := json.Unmarshal([]byte(parentsJSON), &parents); err != nil {
				return nil, fmt.Errorf("projection: unmarshal parents for op %s: %w", opID, err)
			}
		}

		authorWhen := parseTimeWithZone(authorTime, authorTZ)
		committerWhen := parseTimeWithZone(committerTime, committerTZ)

		var signature string
		if sig.Valid {
			signature = sig.String
		}

		ops = append(ops, codec.Op{
			Envelope:  env,
			ID:        opID,
			Parents:   parents,
			Author:    codec.Identity{Name: authorName, Email: authorEmail, When: authorWhen},
			Committer: codec.Identity{Name: committerName, Email: committerEmail, When: committerWhen},
			Message:   message,
			Signature: signature,
		})
	}

	return ops, rows.Err()
}

func parseTimeWithZone(sec int64, tz string) time.Time {
	t, err := time.Parse("-0700", tz)
	if err == nil {
		return time.Unix(sec, 0).In(t.Location())
	}
	return time.Unix(sec, 0).UTC()
}

func resolveTargetTips(repo *git.Repository, explicitRefs []string) (map[string]string, error) {
	targetTips := make(map[string]string)
	if repo == nil {
		return targetTips, nil
	}

	if len(explicitRefs) == 0 {
		// Default to HEAD
		headRef, err := repo.Head()
		if err == nil && headRef != nil {
			targetTips[headRef.Name().String()] = headRef.Hash().String()
		}
		return targetTips, nil
	}

	for _, refName := range explicitRefs {
		if refName == "HEAD" {
			headRef, err := repo.Head()
			if err == nil && headRef != nil {
				targetTips["HEAD"] = headRef.Hash().String()
			}
			continue
		}

		ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
		if err == nil && ref != nil {
			targetTips[refName] = ref.Hash().String()
			continue
		}

		// Try resolving as revision if ReferenceName lookup fails
		hash, err := repo.ResolveRevision(plumbing.Revision(refName))
		if err == nil && hash != nil {
			targetTips[refName] = hash.String()
		}
	}

	return targetTips, nil
}
