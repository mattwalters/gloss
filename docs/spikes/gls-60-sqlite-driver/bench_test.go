package sqlitedriver

import (
	"database/sql"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	_ "modernc.org/sqlite"
)

// openDB opens a fresh on-disk database (projection is file-backed in
// practice, not :memory:) under driverName ("sqlite3" for mattn,
// "sqlite" for modernc), with the schema applied.
func openDB(b *testing.B, driverName string) *sql.DB {
	b.Helper()
	path := filepath.Join(b.TempDir(), "projection.db")
	db, err := sql.Open(driverName, path)
	if err != nil {
		b.Fatalf("open %s: %v", driverName, err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		b.Fatalf("%s: set WAL: %v", driverName, err)
	}
	if _, err := db.Exec(schema); err != nil {
		b.Fatalf("%s: apply schema: %v", driverName, err)
	}
	return db
}

// bulkInsert loads numReviews reviews and numComments comments in one
// transaction per table, the shape a from-scratch refold takes: everything
// derived from the op log, committed once, not row by row.
func bulkInsert(b *testing.B, db *sql.DB, driverName string) {
	b.Helper()

	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("%s: begin: %v", driverName, err)
	}
	reviewStmt, err := tx.Prepare("INSERT INTO reviews (id, status, base, head) VALUES (?, ?, ?, ?)")
	if err != nil {
		b.Fatalf("%s: prepare reviews: %v", driverName, err)
	}
	for i := 0; i < numReviews; i++ {
		id := fmt.Sprintf("review-%d", i)
		if _, err := reviewStmt.Exec(id, "open", "deadbeef", "cafebabe"); err != nil {
			b.Fatalf("%s: insert review: %v", driverName, err)
		}
	}
	reviewStmt.Close()

	commentStmt, err := tx.Prepare("INSERT INTO comments (id, review_id, author, body, blob_hash, created_at, resolved) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		b.Fatalf("%s: prepare comments: %v", driverName, err)
	}
	for i := 0; i < numComments; i++ {
		reviewID := fmt.Sprintf("review-%d", i%numReviews)
		id := fmt.Sprintf("comment-%d", i)
		if _, err := commentStmt.Exec(id, reviewID, "alice", "looks good to me, one nit below", "abc123", int64(i), 0); err != nil {
			b.Fatalf("%s: insert comment: %v", driverName, err)
		}
	}
	commentStmt.Close()

	if err := tx.Commit(); err != nil {
		b.Fatalf("%s: commit: %v", driverName, err)
	}
}

func BenchmarkBulkInsert_Mattn(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openDB(b, "sqlite3")
		bulkInsert(b, db, "sqlite3")
		db.Close()
	}
}

func BenchmarkBulkInsert_Modernc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		db := openDB(b, "sqlite")
		bulkInsert(b, db, "sqlite")
		db.Close()
	}
}

// indexedRead exercises the query shape a review view uses: every comment
// on one review, via the review_id index.
func indexedRead(b *testing.B, db *sql.DB, driverName string, rng *rand.Rand) {
	b.Helper()
	reviewID := fmt.Sprintf("review-%d", rng.Intn(numReviews))
	rows, err := db.Query("SELECT id, author, body FROM comments WHERE review_id = ?", reviewID)
	if err != nil {
		b.Fatalf("%s: query: %v", driverName, err)
	}
	count := 0
	for rows.Next() {
		var id, author, body string
		if err := rows.Scan(&id, &author, &body); err != nil {
			b.Fatalf("%s: scan: %v", driverName, err)
		}
		count++
	}
	rows.Close()
	if count != commentsPerReview {
		b.Fatalf("%s: expected %d comments, got %d", driverName, commentsPerReview, count)
	}
}

func BenchmarkIndexedRead_Mattn(b *testing.B) {
	db := openDB(b, "sqlite3")
	defer db.Close()
	bulkInsert(b, db, "sqlite3")
	rng := rand.New(rand.NewSource(1))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		indexedRead(b, db, "sqlite3", rng)
	}
}

func BenchmarkIndexedRead_Modernc(b *testing.B) {
	db := openDB(b, "sqlite")
	defer db.Close()
	bulkInsert(b, db, "sqlite")
	rng := rand.New(rand.NewSource(1))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		indexedRead(b, db, "sqlite", rng)
	}
}
