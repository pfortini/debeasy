package store

import (
	"context"
	"database/sql"
)

type HistoryEntry struct {
	ID           int64
	UserID       int64
	ConnectionID sql.NullInt64
	SQL          string
	ElapsedMS    int64
	RowsAffected int64
	Error        sql.NullString
	CreatedAt    string
}

type History struct{ db *sql.DB }

func (h *History) Append(ctx context.Context, e HistoryEntry) error {
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO query_history(user_id, connection_id, sql, elapsed_ms, rows_affected, error)
         VALUES(?, ?, ?, ?, ?, ?)`,
		e.UserID, e.ConnectionID, e.SQL, e.ElapsedMS, e.RowsAffected, e.Error)
	return err
}

func (h *History) Recent(ctx context.Context, userID int64, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := h.db.QueryContext(ctx,
		`SELECT id, user_id, connection_id, sql, elapsed_ms, rows_affected, error, created_at
         FROM query_history WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.ConnectionID, &e.SQL, &e.ElapsedMS, &e.RowsAffected, &e.Error, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
