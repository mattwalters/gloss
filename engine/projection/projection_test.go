package projection_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/writtendev/writ/engine/projection"
)

func TestOpenCloseMemory(t *testing.T) {
	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer db.Close()

	// The literal is deliberate: bumping the projection schema has to be a
	// conscious edit, because it is what makes an existing checkout rebuild
	// its cache. WRIT-117 took it to 7 so projections holding person
	// identifiers folded under the old rule are dropped rather than queried
	// with the new one.
	if v := projection.SchemaVersion(); v != 7 {
		t.Fatalf("expected schema version 7, got %d", v)
	}

	var version string
	err = db.DB().QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version failed: %v", err)
	}
	if want := strconv.Itoa(projection.SchemaVersion()); version != want {
		t.Fatalf("stored schema_version %q, want %q", version, want)
	}
}

func TestSchemaVersionMismatchRecreates(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "projection.db")

	// 1. Initial open
	db, err := projection.Open(dbPath)
	if err != nil {
		t.Fatalf("initial Open failed: %v", err)
	}

	// Insert dummy object row
	_, err = db.DB().Exec("INSERT INTO objects (object_id, object_type, op_count, last_op_id, author_name, author_email, created_at, updated_at) VALUES ('obj-1', 'review', 1, 'sha-1', 'alice', 'alice@example.com', 100, 100)")
	if err != nil {
		t.Fatalf("insert dummy object failed: %v", err)
	}

	// Deliberately corrupt schema_version
	_, err = db.DB().Exec("UPDATE meta SET value = '99' WHERE key = 'schema_version'")
	if err != nil {
		t.Fatalf("corrupt schema_version failed: %v", err)
	}
	_ = db.Close()

	// 2. Re-open: should detect mismatch, drop tables, and recreate at the current version
	db2, err := projection.Open(dbPath)
	if err != nil {
		t.Fatalf("re-open after mismatch failed: %v", err)
	}
	defer db2.Close()

	var version string
	err = db2.DB().QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version failed: %v", err)
	}
	if want := strconv.Itoa(projection.SchemaVersion()); version != want {
		t.Fatalf("recreated schema_version %q, want %q", version, want)
	}

	// The dummy object must no longer exist (tables recreated)
	var count int
	err = db2.DB().QueryRow("SELECT COUNT(*) FROM objects WHERE object_id = 'obj-1'").Scan(&count)
	if err != nil {
		t.Fatalf("query objects failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected objects to be wiped on schema mismatch, found %d rows", count)
	}
}
