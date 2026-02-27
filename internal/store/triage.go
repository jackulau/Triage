package store

import (
	"database/sql"
	"fmt"
	"time"
)

// TriageLog represents a triage action log entry.
type TriageLog struct {
	ID              int64
	RepoID          int64
	IssueNumber     int
	Action          string
	DuplicateOf     string
	SuggestedLabels string
	Reasoning       string
	NotifiedVia     string
	HumanDecision   string
	CreatedAt       time.Time
}

// LogTriageAction inserts a new triage log entry.
func (d *DB) LogTriageAction(log *TriageLog) error {
	_, err := d.db.Exec(`
		INSERT INTO triage_log (repo_id, issue_number, action, duplicate_of, suggested_labels, reasoning, notified_via)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		log.RepoID, log.IssueNumber, log.Action,
		nullStr(log.DuplicateOf), nullStr(log.SuggestedLabels),
		nullStr(log.Reasoning), nullStr(log.NotifiedVia),
	)
	if err != nil {
		return fmt.Errorf("logging triage action: %w", err)
	}
	return nil
}

// GetTriageLog retrieves triage log entries for a repo and issue.
func (d *DB) GetTriageLog(repoID int64, issueNumber int) ([]TriageLog, error) {
	rows, err := d.db.Query(`
		SELECT id, repo_id, issue_number, action, duplicate_of, suggested_labels,
		       reasoning, notified_via, human_decision, created_at
		FROM triage_log WHERE repo_id = ? AND issue_number = ?
		ORDER BY created_at DESC`,
		repoID, issueNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("querying triage log: %w", err)
	}
	defer rows.Close()

	var logs []TriageLog
	for rows.Next() {
		log, err := scanTriageLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, *log)
	}
	return logs, rows.Err()
}

// UpdateHumanDecision updates the human_decision field for a triage log entry.
func (d *DB) UpdateHumanDecision(logID int64, decision string) error {
	_, err := d.db.Exec(
		`UPDATE triage_log SET human_decision = ? WHERE id = ?`,
		decision, logID,
	)
	if err != nil {
		return fmt.Errorf("updating human decision: %w", err)
	}
	return nil
}

func scanTriageLog(rows *sql.Rows) (*TriageLog, error) {
	var log TriageLog
	var dupOf, labels, reasoning, notified, decision sql.NullString
	var createdAt string

	err := rows.Scan(
		&log.ID, &log.RepoID, &log.IssueNumber, &log.Action,
		&dupOf, &labels, &reasoning, &notified, &decision, &createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning triage log: %w", err)
	}

	log.DuplicateOf = dupOf.String
	log.SuggestedLabels = labels.String
	log.Reasoning = reasoning.String
	log.NotifiedVia = notified.String
	log.HumanDecision = decision.String
	log.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &log, nil
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
