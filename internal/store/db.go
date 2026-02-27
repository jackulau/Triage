package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const currentVersion = 1

// DB wraps a SQLite database connection for triage storage.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at the given path and runs migrations.
// Use ":memory:" for an in-memory database (useful for testing).
func Open(path string) (*DB, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	} else {
		dsn = ":memory:?_pragma=foreign_keys(ON)"
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Set connection pool to 1 for SQLite
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	store := &DB{db: sqlDB}
	if err := store.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Conn returns the underlying *sql.DB for advanced use cases.
func (d *DB) Conn() *sql.DB {
	return d.db
}

func (d *DB) migrate() error {
	var version int
	err := d.db.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		return fmt.Errorf("reading user_version: %w", err)
	}

	if version >= currentVersion {
		return nil
	}

	if version < 1 {
		if err := d.migrateV1(); err != nil {
			return err
		}
	}

	_, err = d.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentVersion))
	if err != nil {
		return fmt.Errorf("setting user_version: %w", err)
	}

	return nil
}

func (d *DB) migrateV1() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS repos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner TEXT NOT NULL,
			repo TEXT NOT NULL,
			last_polled_at TEXT,
			etag TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(owner, repo)
		)`,
		`CREATE TABLE IF NOT EXISTS issues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL REFERENCES repos(id),
			number INTEGER NOT NULL,
			title TEXT NOT NULL,
			body TEXT,
			body_hash TEXT,
			state TEXT NOT NULL,
			author TEXT,
			labels TEXT,
			embedding BLOB,
			embedding_model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			embedded_at TEXT,
			UNIQUE(repo_id, number)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_repo_state ON issues(repo_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_issues_repo_embedded ON issues(repo_id, embedded_at)`,
		`CREATE TABLE IF NOT EXISTS triage_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL REFERENCES repos(id),
			issue_number INTEGER NOT NULL,
			action TEXT NOT NULL,
			duplicate_of TEXT,
			suggested_labels TEXT,
			reasoning TEXT,
			notified_via TEXT,
			human_decision TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_triage_repo_issue ON triage_log(repo_id, issue_number)`,
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration statement: %w", err)
		}
	}

	return tx.Commit()
}
