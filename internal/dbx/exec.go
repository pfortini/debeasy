package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// runStatements executes a multi-statement SQL string by splitting on `;` (naive — quoted ; survive
// because we honour single quotes and double quotes; complex DDL with $$ blocks may get one statement
// chunk that the underlying driver can still handle if it allows multi).
func runStatements(ctx context.Context, db *sql.DB, sqlText string, maxRows int) ([]Result, error) {
	stmts := splitSQL(sqlText)
	out := make([]Result, 0, len(stmts))
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, execOne(ctx, db, s, maxRows))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no statement to execute")
	}
	return out, nil
}

func execOne(ctx context.Context, db *sql.DB, stmt string, maxRows int) Result {
	res := Result{SQL: stmt, IsQuery: looksLikeQuery(stmt)}
	start := time.Now()
	if res.IsQuery {
		rows, err := db.QueryContext(ctx, stmt)
		if err != nil {
			res.Err = err.Error()
			res.Stats.ElapsedMS = time.Since(start).Milliseconds()
			return res
		}
		defer rows.Close()
		rs, trunc, err := readRows(rows, maxRows)
		res.Stats.ElapsedMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Err = err.Error()
			return res
		}
		rs.Truncated = trunc
		res.Rows = rs
		res.Stats.RowsAffected = int64(len(rs.Rows))
		return res
	}
	r, err := db.ExecContext(ctx, stmt)
	res.Stats.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Err = err.Error()
		return res
	}
	if n, e := r.RowsAffected(); e == nil {
		res.Stats.RowsAffected = n
	}
	return res
}

func readRows(rows *sql.Rows, maxRows int) (*RowSet, bool, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, false, err
	}
	types, _ := rows.ColumnTypes()
	rs := &RowSet{Columns: cols}
	rs.Types = make([]string, len(cols))
	for i, t := range types {
		if t != nil {
			rs.Types[i] = t.DatabaseTypeName()
		}
	}
	count := 0
	for rows.Next() {
		if maxRows > 0 && count >= maxRows {
			return rs, true, nil
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, false, err
		}
		row := make([]any, len(cols))
		for i, v := range raw {
			row[i] = normaliseValue(v)
		}
		rs.Rows = append(rs.Rows, row)
		count++
	}
	return rs, false, rows.Err()
}

func normaliseValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	}
	return v
}

// looksLikeQuery returns true if the statement should be run with Query (returns rows).
func looksLikeQuery(s string) bool {
	t := strings.ToUpper(strings.TrimLeft(s, " \t\n\r("))
	for _, kw := range []string{"SELECT", "WITH", "SHOW", "EXPLAIN", "PRAGMA", "VALUES", "TABLE ", "DESCRIBE ", "DESC "} {
		if strings.HasPrefix(t, kw) {
			return true
		}
	}
	return false
}

// splitSQL splits on ';' respecting single/double quotes. Good enough for editor input.
func splitSQL(s string) []string {
	var out []string
	var cur strings.Builder
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
			cur.WriteByte(c)
		case c == '"' && !inS:
			inD = !inD
			cur.WriteByte(c)
		case c == ';' && !inS && !inD:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}
