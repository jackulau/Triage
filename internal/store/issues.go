package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Issue represents a stored GitHub issue.
type Issue struct {
	ID             int64
	RepoID         int64
	Number         int
	Title          string
	Body           string
	BodyHash       string
	State          string
	Author         string
	Labels         []string
	Embedding      []byte
	EmbeddingModel string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	EmbeddedAt     *time.Time
}

// IssueEmbedding holds an issue number and its embedding vector.
type IssueEmbedding struct {
	Number    int
	Embedding []byte
	Model     string
}

// UpsertIssue inserts or updates an issue.
func (d *DB) UpsertIssue(issue *Issue) error {
	labelsJSON, err := json.Marshal(issue.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	_, err = d.db.Exec(`
		INSERT INTO issues (repo_id, number, title, body, body_hash, state, author, labels, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
			title = excluded.title,
			body = excluded.body,
			body_hash = excluded.body_hash,
			state = excluded.state,
			author = excluded.author,
			labels = excluded.labels,
			updated_at = excluded.updated_at`,
		issue.RepoID, issue.Number, issue.Title, issue.Body, issue.BodyHash,
		issue.State, issue.Author, string(labelsJSON),
		issue.CreatedAt.UTC().Format(time.RFC3339),
		issue.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting issue: %w", err)
	}
	return nil
}

// GetIssue retrieves an issue by repo ID and number.
func (d *DB) GetIssue(repoID int64, number int) (*Issue, error) {
	row := d.db.QueryRow(`
		SELECT id, repo_id, number, title, body, body_hash, state, author, labels,
		       embedding, embedding_model, created_at, updated_at, embedded_at
		FROM issues WHERE repo_id = ? AND number = ?`,
		repoID, number,
	)
	return scanIssue(row)
}

// GetIssuesByRepo returns all issues for a given repo.
func (d *DB) GetIssuesByRepo(repoID int64) ([]Issue, error) {
	rows, err := d.db.Query(`
		SELECT id, repo_id, number, title, body, body_hash, state, author, labels,
		       embedding, embedding_model, created_at, updated_at, embedded_at
		FROM issues WHERE repo_id = ? ORDER BY number`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying issues: %w", err)
	}
	defer rows.Close()

	var issues []Issue
	for rows.Next() {
		issue, err := scanIssueRows(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, *issue)
	}
	return issues, rows.Err()
}

// UpdateEmbedding sets the embedding vector for an issue.
func (d *DB) UpdateEmbedding(repoID int64, number int, embedding []byte, model string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		UPDATE issues SET embedding = ?, embedding_model = ?, embedded_at = ?
		WHERE repo_id = ? AND number = ?`,
		embedding, model, now, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("updating embedding: %w", err)
	}
	return nil
}

// UpdateEmbeddingWithHash sets the embedding vector and content hash for an issue.
func (d *DB) UpdateEmbeddingWithHash(repoID int64, number int, embedding []byte, model, bodyHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		UPDATE issues SET embedding = ?, embedding_model = ?, embedded_at = ?, body_hash = ?
		WHERE repo_id = ? AND number = ?`,
		embedding, model, now, bodyHash, repoID, number,
	)
	if err != nil {
		return fmt.Errorf("updating embedding with hash: %w", err)
	}
	return nil
}

// GetIssueEmbeddingHash returns the stored body_hash and whether an embedding exists
// for the given issue. This is used to check if re-embedding is needed.
func (d *DB) GetIssueEmbeddingHash(repoID int64, number int) (hash string, hasEmbedding bool, err error) {
	var bodyHash sql.NullString
	var embedding []byte
	err = d.db.QueryRow(`
		SELECT body_hash, embedding FROM issues WHERE repo_id = ? AND number = ?`,
		repoID, number,
	).Scan(&bodyHash, &embedding)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("getting issue embedding hash: %w", err)
	}
	return bodyHash.String, len(embedding) > 0, nil
}

// GetEmbeddingsForRepo returns all issue embeddings for a repo that have been embedded.
func (d *DB) GetEmbeddingsForRepo(repoID int64) ([]IssueEmbedding, error) {
	rows, err := d.db.Query(`
		SELECT number, embedding, embedding_model
		FROM issues WHERE repo_id = ? AND embedding IS NOT NULL`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying embeddings: %w", err)
	}
	defer rows.Close()

	var results []IssueEmbedding
	for rows.Next() {
		var ie IssueEmbedding
		if err := rows.Scan(&ie.Number, &ie.Embedding, &ie.Model); err != nil {
			return nil, fmt.Errorf("scanning embedding: %w", err)
		}
		results = append(results, ie)
	}
	return results, rows.Err()
}

func scanIssue(row *sql.Row) (*Issue, error) {
	var issue Issue
	var body, bodyHash, author, labels, embeddingModel, embeddedAt sql.NullString
	var embedding []byte
	var createdAt, updatedAt string

	err := row.Scan(
		&issue.ID, &issue.RepoID, &issue.Number, &issue.Title,
		&body, &bodyHash, &issue.State, &author, &labels,
		&embedding, &embeddingModel, &createdAt, &updatedAt, &embeddedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning issue: %w", err)
	}

	issue.Body = body.String
	issue.BodyHash = bodyHash.String
	issue.Author = author.String
	issue.Embedding = embedding
	issue.EmbeddingModel = embeddingModel.String
	issue.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	issue.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if embeddedAt.Valid {
		t, _ := time.Parse(time.RFC3339, embeddedAt.String)
		issue.EmbeddedAt = &t
	}

	if labels.Valid && labels.String != "" {
		_ = json.Unmarshal([]byte(labels.String), &issue.Labels)
	}

	return &issue, nil
}

func scanIssueRows(rows *sql.Rows) (*Issue, error) {
	var issue Issue
	var body, bodyHash, author, labels, embeddingModel, embeddedAt sql.NullString
	var embedding []byte
	var createdAt, updatedAt string

	err := rows.Scan(
		&issue.ID, &issue.RepoID, &issue.Number, &issue.Title,
		&body, &bodyHash, &issue.State, &author, &labels,
		&embedding, &embeddingModel, &createdAt, &updatedAt, &embeddedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning issue: %w", err)
	}

	issue.Body = body.String
	issue.BodyHash = bodyHash.String
	issue.Author = author.String
	issue.Embedding = embedding
	issue.EmbeddingModel = embeddingModel.String
	issue.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	issue.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if embeddedAt.Valid {
		t, _ := time.Parse(time.RFC3339, embeddedAt.String)
		issue.EmbeddedAt = &t
	}

	if labels.Valid && labels.String != "" {
		_ = json.Unmarshal([]byte(labels.String), &issue.Labels)
	}

	return &issue, nil
}
