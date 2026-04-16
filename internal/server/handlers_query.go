package server

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

type QueryResultData struct {
	Conn    *store.Connection
	Results []dbx.Result
	Err     string
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	c, d := s.resolveConn(w, r)
	if c == nil {
		return
	}
	sqlText := strings.TrimSpace(r.FormValue("sql"))
	if sqlText == "" {
		s.rend.Render(w, http.StatusOK, "partials/query_results.html",
			&QueryResultData{Conn: c, Err: "empty query"})
		return
	}
	maxRows := formInt(r, "max_rows", 1000)
	userID := CurrentUser(r).ID

	results, err := d.Exec(r.Context(), sqlText, maxRows)
	if err != nil {
		_ = s.store.History.Append(r.Context(), store.HistoryEntry{
			UserID:       userID,
			ConnectionID: sql.NullInt64{Int64: c.ID, Valid: true},
			SQL:          sqlText,
			Error:        sql.NullString{String: err.Error(), Valid: true},
		})
		s.rend.Render(w, http.StatusOK, "partials/query_results.html",
			&QueryResultData{Conn: c, Err: err.Error()})
		return
	}
	for _, res := range results {
		_ = s.store.History.Append(r.Context(), store.HistoryEntry{
			UserID:       userID,
			ConnectionID: sql.NullInt64{Int64: c.ID, Valid: true},
			SQL:          res.SQL,
			ElapsedMS:    res.Stats.ElapsedMS,
			RowsAffected: res.Stats.RowsAffected,
			Error:        nullableErr(res.Err),
		})
	}
	s.rend.Render(w, http.StatusOK, "partials/query_results.html",
		&QueryResultData{Conn: c, Results: results})
}

func nullableErr(msg string) sql.NullString {
	if msg == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: msg, Valid: true}
}
