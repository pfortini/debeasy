package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver with database/sql

	"github.com/pfortini/debeasy/internal/store"
)

type sqliteDriver struct {
	db   *sql.DB
	path string
}

func openSQLite(c *store.Connection, _ int) (Driver, error) {
	if c.Database == "" {
		return nil, errors.New("sqlite: database (file path) is required")
	}
	dsn := "file:" + c.Database + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &sqliteDriver{db: db, path: c.Database}, nil
}

func (s *sqliteDriver) Kind() Kind                     { return KindSQLite }
func (s *sqliteDriver) Close() error                   { return s.db.Close() }
func (s *sqliteDriver) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *sqliteDriver) ListDatabases(_ context.Context) ([]DB, error) {
	return []DB{{Name: "main"}}, nil
}

func (s *sqliteDriver) ListSchemas(_ context.Context, _ string) ([]Schema, error) {
	return []Schema{{Name: "main"}}, nil
}

func (s *sqliteDriver) ListObjects(ctx context.Context, _, _ string) (ObjectTree, error) {
	tree := ObjectTree{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, type FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		return tree, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Object
		var k string
		if err := rows.Scan(&o.Name, &k); err != nil {
			return tree, err
		}
		o.Schema = "main"
		o.Kind = ObjectKind(k)
		switch o.Kind {
		case ObjTable:
			tree.Tables = append(tree.Tables, o)
		case ObjView:
			tree.Views = append(tree.Views, o)
		case ObjIndex:
			tree.Indexes = append(tree.Indexes, o)
		default:
			tree.Other = append(tree.Other, o)
		}
	}
	return tree, rows.Err()
}

func (s *sqliteDriver) DescribeTable(ctx context.Context, ref ObjectRef) (TableDef, error) {
	def := TableDef{Ref: ref, Kind: ObjTable}
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return def, err
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+tbl+")")
	if err != nil {
		return def, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var c Column
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &c.Name, &c.DataType, &notnull, &dflt, &pk); err != nil {
			return def, err
		}
		c.Nullable = notnull == 0
		c.IsPK = pk > 0
		c.OrdinalPos = cid + 1
		if dflt.Valid {
			c.Default = dflt.String
		}
		def.Columns = append(def.Columns, c)
	}

	// Indexes — collect rows first, then close, THEN resolve columns per index.
	// The driver is configured with SetMaxOpenConns(1), so issuing a nested query
	// while idxRows is still open deadlocks on the single connection.
	type idxRow struct {
		ix      IndexInfo
		collect bool
	}
	var pending []idxRow
	if idxRows, err := s.db.QueryContext(ctx, "PRAGMA index_list("+tbl+")"); err == nil {
		for idxRows.Next() {
			var seq, unique int
			var origin, partial string
			var ix IndexInfo
			if err := idxRows.Scan(&seq, &ix.Name, &unique, &origin, &partial); err != nil {
				continue
			}
			ix.Unique = unique == 1
			ix.Primary = origin == "pk"
			pending = append(pending, idxRow{ix: ix, collect: true})
		}
		_ = idxRows.Close() //nolint:sqlclosecheck // must close before the nested indexColumns query; SetMaxOpenConns(1) would otherwise deadlock
	}
	for _, p := range pending {
		ix := p.ix
		if cols, err := s.indexColumns(ctx, ix.Name); err == nil {
			ix.Columns = cols
		}
		def.Indexes = append(def.Indexes, ix)
	}

	// FKs
	fkRows, err := s.db.QueryContext(ctx, "PRAGMA foreign_key_list("+tbl+")")
	if err == nil {
		defer fkRows.Close()
		grouped := map[int]*ForeignKey{}
		var order []int
		for fkRows.Next() {
			var id, seq int
			var refTable, from, to, onUpdate, onDelete, match string
			if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				continue
			}
			fk, ok := grouped[id]
			if !ok {
				fk = &ForeignKey{Name: fmt.Sprintf("fk_%d", id), RefTable: refTable, OnUpdate: onUpdate, OnDelete: onDelete}
				grouped[id] = fk
				order = append(order, id)
			}
			fk.Columns = append(fk.Columns, from)
			fk.RefColumns = append(fk.RefColumns, to)
		}
		for _, id := range order {
			def.FKs = append(def.FKs, *grouped[id])
		}
	}

	// DDL: pull from sqlite_schema
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE name=?`, ref.Name).Scan(&def.DDL); err != nil {
		def.DDL = synthesisedCreateTable(KindSQLite, ref, def.Columns, def.Indexes, def.FKs)
	}
	_ = s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+tbl).Scan(&def.RowCount)
	return def, nil
}

func (s *sqliteDriver) indexColumns(ctx context.Context, name string) ([]string, error) {
	q, err := quoteIdent(KindSQLite, name)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA index_info("+q+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seqno, cid int
		var col string
		if err := rows.Scan(&seqno, &cid, &col); err != nil {
			return nil, err
		}
		out = append(out, col)
	}
	return out, rows.Err()
}

func (s *sqliteDriver) SampleRows(ctx context.Context, ref ObjectRef, page Page) (RowSet, error) {
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return RowSet{}, err
	}
	order := ""
	if page.OrderBy != "" {
		col, err := quoteIdent(KindSQLite, page.OrderBy)
		if err != nil {
			return RowSet{}, err
		}
		dir := "ASC"
		if page.Desc {
			dir = "DESC"
		}
		order = " ORDER BY " + col + " " + dir
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	stmt := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", tbl, order, limit, page.Offset)
	rows, err := s.db.QueryContext(ctx, stmt)
	if err != nil {
		return RowSet{}, err
	}
	defer rows.Close()
	rs, _, err := readRows(rows, limit+1)
	if err != nil {
		return RowSet{}, err
	}
	return *rs, nil
}

func (s *sqliteDriver) CountRows(ctx context.Context, ref ObjectRef) (int64, error) {
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+tbl).Scan(&n)
	return n, err
}

func (s *sqliteDriver) CreateDatabase(_ context.Context, _ string, _ DBOpts) error {
	return errors.New("sqlite: a database is a file — create the connection with the desired file path instead")
}

func (s *sqliteDriver) CreateTable(ctx context.Context, def CreateTableDef) error {
	def.Schema = "" // sqlite doesn't use schema namespace
	stmt, err := buildCreateTable(KindSQLite, def)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, stmt)
	return err
}

func (s *sqliteDriver) CreateView(ctx context.Context, _, name, sqlText string) error {
	q, err := quoteIdent(KindSQLite, name)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "CREATE VIEW "+q+" AS "+sqlText)
	return err
}

func (s *sqliteDriver) CreateIndex(ctx context.Context, def IndexDef) error {
	def.Schema = ""
	def.Method = ""
	stmt, err := buildCreateIndex(KindSQLite, def)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, stmt)
	return err
}

func (s *sqliteDriver) InsertRow(ctx context.Context, ref ObjectRef, values map[string]string) error {
	cols, args, placeholders, err := prepareSQLiteInsert(values)
	if err != nil {
		return err
	}
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return err
	}
	stmt := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	_, err = s.db.ExecContext(ctx, stmt, args...)
	return err
}

func (s *sqliteDriver) UpdateRow(ctx context.Context, ref ObjectRef, pk, values map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	setCols := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(pk))
	for k, v := range values {
		col, err := quoteIdent(KindSQLite, k)
		if err != nil {
			return err
		}
		setCols = append(setCols, col+"=?")
		args = append(args, v)
	}
	whereCols := make([]string, 0, len(pk))
	for k, v := range pk {
		col, err := quoteIdent(KindSQLite, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, col+"=?")
		args = append(args, v)
	}
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return err
	}
	stmt := "UPDATE " + tbl + " SET " + strings.Join(setCols, ", ") + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = s.db.ExecContext(ctx, stmt, args...)
	return err
}

func (s *sqliteDriver) DeleteRow(ctx context.Context, ref ObjectRef, pk map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	whereCols := make([]string, 0, len(pk))
	args := make([]any, 0, len(pk))
	for k, v := range pk {
		col, err := quoteIdent(KindSQLite, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, col+"=?")
		args = append(args, v)
	}
	tbl, err := quoteIdent(KindSQLite, ref.Name)
	if err != nil {
		return err
	}
	stmt := "DELETE FROM " + tbl + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = s.db.ExecContext(ctx, stmt, args...)
	return err
}

func (s *sqliteDriver) Exec(ctx context.Context, sqlText string, maxRows int) ([]Result, error) {
	return runStatements(ctx, s.db, sqlText, maxRows)
}

func prepareSQLiteInsert(values map[string]string) (cols []string, args []any, placeholders []string, err error) {
	cols = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	placeholders = make([]string, 0, len(values))
	for k, v := range values {
		col, err := quoteIdent(KindSQLite, k)
		if err != nil {
			return nil, nil, nil, err
		}
		cols = append(cols, col)
		args = append(args, v)
		placeholders = append(placeholders, "?")
	}
	return cols, args, placeholders, nil
}
