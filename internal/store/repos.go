package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/zxela/claude-monitor/internal/repo"

	_ "modernc.org/sqlite"
)

// UpsertRepo inserts or updates a repo record.
func (d *DB) UpsertRepo(r *repo.Repo) error {
	_, err := d.db.Exec(`INSERT INTO repos (id, name, url, first_seen) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE repos.name END,
			url  = CASE WHEN excluded.url  <> '' THEN excluded.url  ELSE repos.url  END`,
		r.ID, r.Name, r.URL, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("UpsertRepo: %w", err)
	}
	return nil
}

const cwdRepoUpsertSQL = `INSERT INTO cwd_repos (cwd, repo_id, source_rank, repo_toplevel) VALUES (?, ?, ?, ?)
	ON CONFLICT(cwd) DO UPDATE SET repo_id=excluded.repo_id, source_rank=excluded.source_rank, repo_toplevel=excluded.repo_toplevel`

func cwdRepoUpsertArgs(e repo.CacheEntry) []any {
	return []any{e.CWD, e.RepoID, e.Rank, e.Toplevel}
}

// UpsertCwdRepo persists one cwd resolution.
func (d *DB) UpsertCwdRepo(e repo.CacheEntry) error {
	if _, err := d.db.Exec(cwdRepoUpsertSQL, cwdRepoUpsertArgs(e)...); err != nil {
		return fmt.Errorf("UpsertCwdRepo: %w", err)
	}
	return nil
}

// LoadCwdRepos returns all persisted cwd resolutions.
func (d *DB) LoadCwdRepos() ([]repo.CacheEntry, error) {
	rows, err := d.rdb.Query(`SELECT cwd, repo_id, source_rank, repo_toplevel FROM cwd_repos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []repo.CacheEntry
	for rows.Next() {
		var e repo.CacheEntry
		if err := rows.Scan(&e.CWD, &e.RepoID, &e.Rank, &e.Toplevel); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// ClearCwdRepos deletes all cwd → repo_id mappings.
func (d *DB) ClearCwdRepos() error {
	_, err := d.db.Exec(`DELETE FROM cwd_repos`)
	if err != nil {
		return fmt.Errorf("ClearCwdRepos: %w", err)
	}
	return nil
}

// ListRepos returns all repos with their names.
type RepoRow struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	URL       string  `json:"url,omitempty"`
	TotalCost float64 `json:"totalCost"`
}

func (d *DB) ListRepos() ([]RepoRow, error) {
	rows, err := d.rdb.Query(`SELECT r.id, r.name, COALESCE(r.url,''),
		COALESCE(SUM(s.total_cost), 0)
		FROM repos r LEFT JOIN sessions s ON s.repo_id = r.id
		GROUP BY r.id ORDER BY COALESCE(SUM(s.total_cost),0) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RepoRow
	for rows.Next() {
		var r RepoRow
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.TotalCost); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// RepoExists reports whether a repo row with the given id exists. Used by the
// per-repo endpoints to return 404 for unknown ids (matching the session-by-id
// endpoint), rather than serving a zeroed 200.
func (d *DB) RepoExists(id string) (bool, error) {
	var one int
	err := d.rdb.QueryRow(`SELECT 1 FROM repos WHERE id = ? LIMIT 1`, id).Scan(&one)
	switch err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}
