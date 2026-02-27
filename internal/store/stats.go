package store

import "fmt"

// RepoStats holds aggregate statistics for a single repository.
type RepoStats struct {
	Repo            Repo
	IssueCount      int
	EmbeddingCount  int
	ClassifiedCount int
}

// GetRepoStats returns aggregate statistics for a single repo.
func (d *DB) GetRepoStats(repoID int64) (*RepoStats, error) {
	repo, err := d.GetRepo(repoID)
	if err != nil {
		return nil, fmt.Errorf("getting repo: %w", err)
	}

	stats := &RepoStats{Repo: *repo}

	// Total issues
	err = d.db.QueryRow(
		`SELECT COUNT(*) FROM issues WHERE repo_id = ?`, repoID,
	).Scan(&stats.IssueCount)
	if err != nil {
		return nil, fmt.Errorf("counting issues: %w", err)
	}

	// Issues with embeddings
	err = d.db.QueryRow(
		`SELECT COUNT(*) FROM issues WHERE repo_id = ? AND embedding IS NOT NULL`, repoID,
	).Scan(&stats.EmbeddingCount)
	if err != nil {
		return nil, fmt.Errorf("counting embeddings: %w", err)
	}

	// Classified issues (distinct issue numbers in triage_log)
	err = d.db.QueryRow(
		`SELECT COUNT(DISTINCT issue_number) FROM triage_log WHERE repo_id = ?`, repoID,
	).Scan(&stats.ClassifiedCount)
	if err != nil {
		return nil, fmt.Errorf("counting classified issues: %w", err)
	}

	return stats, nil
}

// GetAllRepoStats returns statistics for all tracked repos.
func (d *DB) GetAllRepoStats() ([]RepoStats, error) {
	repos, err := d.ListRepos()
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	var results []RepoStats
	for _, repo := range repos {
		stats, err := d.GetRepoStats(repo.ID)
		if err != nil {
			return nil, fmt.Errorf("getting stats for %s/%s: %w", repo.Owner, repo.RepoName, err)
		}
		results = append(results, *stats)
	}

	return results, nil
}
