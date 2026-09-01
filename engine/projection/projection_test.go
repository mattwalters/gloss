package projection_test

import (
	"path/filepath"
	"testing"

	"github.com/writtendev/writ/engine/projection"
)

func TestOpenCloseMemory(t *testing.T) {
	db, err := projection.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer db.Close()

	if v := projection.SchemaVersion(); v != 6 {
		t.Fatalf("expected schema version 6, got %d", v)
	}

	var version string
	err = db.DB().QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version failed: %v", err)
	}
	if version != "6" {
		t.Fatalf("expected stored schema_version '6', got %q", version)
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

	// 2. Re-open: should detect mismatch, drop tables, and recreate with schema_version 6
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
	if version != "6" {
		t.Fatalf("expected recreated schema_version '6', got %q", version)
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

