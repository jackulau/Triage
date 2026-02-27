package store

// Store defines the storage operations used by the pipeline and dedup engine.
// It is satisfied by *DB and can be replaced with a mock for testing.
type Store interface {
	// GetRepoByOwnerRepo retrieves a repo by owner and name.
	GetRepoByOwnerRepo(owner, repo string) (*Repo, error)

	// CreateRepo inserts a new repo record.
	CreateRepo(owner, repo string) (*Repo, error)

	// UpsertIssue inserts or updates an issue.
	UpsertIssue(issue *Issue) error

	// LogTriageAction inserts a new triage log entry.
	LogTriageAction(log *TriageLog) error

	// GetEmbeddingsForRepo returns all issue embeddings for a repo that have been embedded.
	GetEmbeddingsForRepo(repoID int64) ([]IssueEmbedding, error)

	// UpdateEmbedding sets the embedding vector for an issue.
	UpdateEmbedding(repoID int64, number int, embedding []byte, model string) error
}

// Compile-time check that *DB satisfies the Store interface.
var _ Store = (*DB)(nil)
