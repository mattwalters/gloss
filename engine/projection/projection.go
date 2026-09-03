// Package projection provides a versioned SQLite cache of collaborative objects
// and ref tips for Writ.
package projection

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// OpenOption configures an Open invocation on the projection database.
type OpenOption func(*openConfig)

type openConfig struct {
	localPath string
}

// WithLocalPath configures an explicit custom filesystem path for the local SQLite database.
func WithLocalPath(path string) OpenOption {
	return func(c *openConfig) {
		c.localPath = path
	}
}

// DB represents a handle to the projection SQLite cache and accompanying local-only database.
type DB struct {
	db        *sql.DB
	path      string
	localDB   *sql.DB
	localPath string
}

// Open opens or creates a projection SQLite database at path and an accompanying local-only database,
// validating their respective schema versions. If the projection database is missing or the schema version
// does not match SchemaVersion(), folded tables are dropped and recreated cleanly without migration heroics.
// Local-only state is managed and preserved independently.
func Open(path string, opts ...OpenOption) (*DB, error) {
	cfg := &openConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	localPath := cfg.localPath
	if localPath == "" {
		if path == ":memory:" {
			localPath = ":memory:"
		} else {
			localPath = strings.TrimSuffix(path, ".db") + ".local.db"
		}
	}

	db, err := sql.Open("sqlite", formatDSN(path))
	if err != nil {
		return nil, fmt.Errorf("projection: open sqlite %q: %w", path, err)
	}
	if isMemory(path) {
		db.SetMaxOpenConns(1)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("projection: exec %q: %w", pragma, err)
		}
	}

	localDB, err := sql.Open("sqlite", formatDSN(localPath))
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("projection: open local sqlite %q: %w", localPath, err)
	}
	if isMemory(localPath) {
		localDB.SetMaxOpenConns(1)
	}

	for _, pragma := range pragmas {
		if _, err := localDB.Exec(pragma); err != nil {
			_ = db.Close()
			_ = localDB.Close()
			return nil, fmt.Errorf("projection: exec %q on local: %w", pragma, err)
		}
	}

	proj := &DB{
		db:        db,
		path:      path,
		localDB:   localDB,
		localPath: localPath,
	}

	if err := proj.ensureSchema(); err != nil {
		_ = proj.Close()
		return nil, err
	}

	if err := proj.ensureLocalSchema(); err != nil {
		_ = proj.Close()
		return nil, err
	}

	return proj, nil
}

// Close closes both the projection and local SQLite database connections.
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	var errs []error
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.localDB != nil {
		if err := d.localDB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// DB returns the underlying *sql.DB connection pool for custom queries.
func (d *DB) DB() *sql.DB {
	if d == nil {
		return nil
	}
	return d.db
}

// SchemaVersion returns the current expected schema version integer.
func SchemaVersion() int {
	return schemaVersion
}

func (d *DB) ensureSchema() error {
	var versionStr string
	err := d.db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&versionStr)
	if err != nil || versionStr != strconv.Itoa(schemaVersion) {
		// Version mismatch or missing: reset everything
		if err := d.resetSchema(); err != nil {
			return fmt.Errorf("projection: reset schema: %w", err)
		}
	}
	return nil
}

func (d *DB) resetSchema() error {
	// 1. Query and drop all existing user tables and views
	rows, err := d.db.Query("SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return fmt.Errorf("projection: query sqlite_master: %w", err)
	}

	var drops []string
	for rows.Next() {
		var name, objType string
		if err := rows.Scan(&name, &objType); err != nil {
			_ = rows.Close()
			return err
		}
		if objType == "view" {
			drops = append(drops, fmt.Sprintf("DROP VIEW IF EXISTS %s", name))
		} else {
			drops = append(drops, fmt.Sprintf("DROP TABLE IF EXISTS %s", name))
		}
	}
	_ = rows.Close()

	for _, dropStmt := range drops {
		if _, err := d.db.Exec(dropStmt); err != nil {
			return fmt.Errorf("projection: exec %s: %w", dropStmt, err)
		}
	}

	// 2. Execute schema SQL
	if _, err := d.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("projection: exec schemaSQL: %w", err)
	}

	// 3. Set schema_version in meta
	if _, err := d.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)", strconv.Itoa(schemaVersion)); err != nil {
		return fmt.Errorf("projection: record schema_version: %w", err)
	}

	return nil
}

// DumpTables returns a deterministic dump of all projection tables and their rows.
// Used primarily for asserting byte-for-byte equality across incremental vs cold builds.
func (d *DB) DumpTables() (map[string][]map[string]any, error) {
	dump := make(map[string][]map[string]any)

	for _, table := range projectionTables {
		query, ok := tableQueries[table]
		if !ok {
			return nil, fmt.Errorf("missing query for table %s", table)
		}
		rows, err := d.db.Query(query)
		if err != nil {
			return nil, fmt.Errorf("query table %s: %w", table, err)
		}

		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("get columns for %s: %w", table, err)
		}

		var tableRows []map[string]any
		for rows.Next() {
			values := make([]any, len(cols))
			valuePtrs := make([]any, len(cols))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan row in %s: %w", table, err)
			}

			rowMap := make(map[string]any, len(cols))
			for i, col := range cols {
				val := values[i]
				if b, ok := val.([]byte); ok {
					rowMap[col] = string(b)
				} else {
					rowMap[col] = val
				}
			}
			tableRows = append(tableRows, rowMap)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate %s rows: %w", table, err)
		}

		dump[table] = tableRows
	}

	return dump, nil
}

// String returns a brief representation of the DB connection path.
func (d *DB) String() string {
	if strings.Contains(d.path, ":memory:") {
		return "projection:memory"
	}
	return "projection:" + d.path
}

func formatDSN(path string) string {
	const params = "_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=NORMAL"
	if strings.Contains(path, "?") {
		if strings.HasSuffix(path, "?") || strings.HasSuffix(path, "&") {
			return path + params
		}
		return path + "&" + params
	}
	return path + "?" + params
}

func isMemory(path string) bool {
	return path == ":memory:" || strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
}

