package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Repo represents a tracked GitHub repository.
type Repo struct {
	ID           int64
	Owner        string
	RepoName     string
	LastPolledAt *time.Time
	ETag         string
	CreatedAt    time.Time
}

// CreateRepo inserts a new repo record.
func (d *DB) CreateRepo(owner, repo string) (*Repo, error) {
	result, err := d.db.Exec(
		`INSERT INTO repos (owner, repo) VALUES (?, ?)`,
		owner, repo,
	)
	if err != nil {
		return nil, fmt.Errorf("creating repo: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting repo id: %w", err)
	}

	return d.GetRepo(id)
}

// GetRepo retrieves a repo by its ID.
func (d *DB) GetRepo(id int64) (*Repo, error) {
	row := d.db.QueryRow(
		`SELECT id, owner, repo, last_polled_at, etag, created_at FROM repos WHERE id = ?`,
		id,
	)
	return scanRepo(row)
}

// GetRepoByOwnerRepo retrieves a repo by owner and name.
func (d *DB) GetRepoByOwnerRepo(owner, repo string) (*Repo, error) {
	row := d.db.QueryRow(
		`SELECT id, owner, repo, last_polled_at, etag, created_at FROM repos WHERE owner = ? AND repo = ?`,
		owner, repo,
	)
	return scanRepo(row)
}

// UpdatePollState updates the last_polled_at and etag for a repo.
func (d *DB) UpdatePollState(id int64, polledAt time.Time, etag string) error {
	_, err := d.db.Exec(
		`UPDATE repos SET last_polled_at = ?, etag = ? WHERE id = ?`,
		polledAt.UTC().Format(time.RFC3339), etag, id,
	)
	if err != nil {
		return fmt.Errorf("updating poll state: %w", err)
	}
	return nil
}

// ListRepos returns all tracked repos.
func (d *DB) ListRepos() ([]Repo, error) {
	rows, err := d.db.Query(
		`SELECT id, owner, repo, last_polled_at, etag, created_at FROM repos ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		r, err := scanRepoRows(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, *r)
	}
	return repos, rows.Err()
}

func scanRepo(row *sql.Row) (*Repo, error) {
	var r Repo
	var lastPolled, etag sql.NullString
	var createdAt string

	err := row.Scan(&r.ID, &r.Owner, &r.RepoName, &lastPolled, &etag, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("scanning repo: %w", err)
	}

	if lastPolled.Valid {
		t, _ := time.Parse(time.RFC3339, lastPolled.String)
		r.LastPolledAt = &t
	}
	r.ETag = etag.String
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &r, nil
}

func scanRepoRows(rows *sql.Rows) (*Repo, error) {
	var r Repo
	var lastPolled, etag sql.NullString
	var createdAt string

	err := rows.Scan(&r.ID, &r.Owner, &r.RepoName, &lastPolled, &etag, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("scanning repo: %w", err)
	}

	if lastPolled.Valid {
		t, _ := time.Parse(time.RFC3339, lastPolled.String)
		r.LastPolledAt = &t
	}
	r.ETag = etag.String
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &r, nil
}
