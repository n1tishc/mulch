package event

import (
	"context"
	"database/sql"
	"errors"
)

func (s *SQLiteStore) DeleteConversation(ctx context.Context, id string) error {
	r, err := s.write(ctx, writeRequest{kind: writeDeleteSession, sessionID: id})
	if err != nil {
		return err
	}
	return r.err
}

// Remove an entire branch subtree in one write transaction. Tombstones keep a
// racing resume from acquiring execution after the deletion commits.
func deleteConversation(conn *sql.Conn, ctx context.Context, id string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Take the database write lock before inspecting leases from other processes.
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET label=label WHERE id=?`, id); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	const subtree = `WITH RECURSIVE subtree(id) AS (SELECT id FROM sessions WHERE id=? UNION SELECT s.id FROM sessions s JOIN subtree t ON s.parent_id=t.id) `
	var active int
	err = tx.QueryRowContext(ctx, subtree+`SELECT count(*) FROM sessions WHERE id IN (SELECT id FROM subtree) AND (status='running' OR id IN (SELECT session_id FROM execution_leases))`, id).Scan(&active)
	if err != nil {
		return err
	}
	if active > 0 {
		return errors.New("stop all running tasks and candidate branches before deleting this conversation")
	}
	if _, err = tx.ExecContext(ctx, subtree+`INSERT OR IGNORE INTO deleted_sessions SELECT id FROM subtree`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, subtree+`DELETE FROM events WHERE session_id IN (SELECT id FROM subtree)`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, subtree+`DELETE FROM sessions WHERE id IN (SELECT id FROM subtree)`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) AddWorkspace(ctx context.Context, path string) error {
	r, err := s.write(ctx, writeRequest{kind: writeWorkspace, label: path})
	if err != nil {
		return err
	}
	return r.err
}

func (s *SQLiteStore) Workspaces(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM workspaces UNION SELECT workdir FROM sessions ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	paths := []string{}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}
