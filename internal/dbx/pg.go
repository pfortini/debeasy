package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql

	"github.com/pfortini/debeasy/internal/store"
)

type pg struct{ db *sql.DB }

func openPostgres(c *store.Connection, maxOpen int) (Driver, error) {
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	port := c.Port
	if port == 0 {
		port = 5432
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + c.Database,
	}
	if c.Username != "" {
		if c.Password != "" {
			u.User = url.UserPassword(c.Username, c.Password)
		} else {
			u.User = url.User(c.Username)
		}
	}
	q := u.Query()
	if c.SSLMode != "" {
		q.Set("sslmode", c.SSLMode)
	}
	if c.Params != "" {
		extra, err := url.ParseQuery(c.Params)
		if err == nil {
			for k, vs := range extra {
				for _, v := range vs {
					q.Add(k, v)
				}
			}
		}
	}
	u.RawQuery = q.Encode()

	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &pg{db: db}, nil
}

func (p *pg) Kind() Kind                     { return KindPostgres }
func (p *pg) Close() error                   { return p.db.Close() }
func (p *pg) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func (p *pg) ListDatabases(ctx context.Context) ([]DB, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DB
	for rows.Next() {
		var d DB
		if err := rows.Scan(&d.Name); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *pg) ListSchemas(ctx context.Context, _ string) ([]Schema, error) {
	// connection's current database governs visible schemas; switching DB requires reopen.
	rows, err := p.db.QueryContext(ctx,
		`SELECT nspname FROM pg_namespace
         WHERE nspname NOT IN ('pg_catalog','information_schema','pg_toast')
           AND nspname NOT LIKE 'pg_temp_%' AND nspname NOT LIKE 'pg_toast_temp_%'
         ORDER BY nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schema
	for rows.Next() {
		var s Schema
		if err := rows.Scan(&s.Name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *pg) ListObjects(ctx context.Context, _, schema string) (ObjectTree, error) {
	tree := ObjectTree{}
	rows, err := p.db.QueryContext(ctx, `
        SELECT c.relname,
               CASE c.relkind
                   WHEN 'r' THEN 'table'
                   WHEN 'p' THEN 'table'
                   WHEN 'v' THEN 'view'
                   WHEN 'm' THEN 'matview'
                   WHEN 'i' THEN 'index'
                   WHEN 'S' THEN 'sequence'
                   ELSE c.relkind::text
               END AS kind,
               COALESCE(obj_description(c.oid, 'pg_class'), '') AS comment
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','i','S')
        ORDER BY kind, c.relname`, schema)
	if err != nil {
		return tree, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Object
		var k string
		o.Schema = schema
		if err := rows.Scan(&o.Name, &k, &o.Comment); err != nil {
			return tree, err
		}
		o.Kind = ObjectKind(k)
		switch o.Kind {
		case ObjTable:
			tree.Tables = append(tree.Tables, o)
		case ObjView, ObjMatView:
			tree.Views = append(tree.Views, o)
		case ObjIndex:
			tree.Indexes = append(tree.Indexes, o)
		default:
			tree.Other = append(tree.Other, o)
		}
	}
	return tree, rows.Err()
}

func (p *pg) DescribeTable(ctx context.Context, ref ObjectRef) (TableDef, error) {
	def := TableDef{Ref: ref, Kind: ObjTable}
	rows, err := p.db.QueryContext(ctx, `
        SELECT a.attname, format_type(a.atttypid, a.atttypmod) AS data_type,
               NOT a.attnotnull AS nullable,
               COALESCE(pg_get_expr(d.adbin, d.adrelid), '') AS dflt,
               EXISTS (SELECT 1 FROM pg_constraint c
                       WHERE c.conrelid = a.attrelid AND c.contype='p' AND a.attnum = ANY(c.conkey)) AS is_pk,
               a.attnum
        FROM pg_attribute a
        JOIN pg_class t ON t.oid = a.attrelid
        JOIN pg_namespace n ON n.oid = t.relnamespace
        LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
        WHERE n.nspname=$1 AND t.relname=$2 AND a.attnum>0 AND NOT a.attisdropped
        ORDER BY a.attnum`, ref.Schema, ref.Name)
	if err != nil {
		return def, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.DataType, &c.Nullable, &c.Default, &c.IsPK, &c.OrdinalPos); err != nil {
			return def, err
		}
		def.Columns = append(def.Columns, c)
	}
	if err := rows.Err(); err != nil {
		return def, err
	}

	// Indexes
	idxRows, err := p.db.QueryContext(ctx, `
        SELECT i.relname, ix.indisunique, ix.indisprimary,
               array_to_string(ARRAY(
                   SELECT a.attname FROM pg_attribute a
                   WHERE a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
                   ORDER BY array_position(ix.indkey, a.attnum)
               ), ',')
        FROM pg_index ix
        JOIN pg_class t ON t.oid = ix.indrelid
        JOIN pg_namespace n ON n.oid = t.relnamespace
        JOIN pg_class i ON i.oid = ix.indexrelid
        WHERE n.nspname=$1 AND t.relname=$2
        ORDER BY i.relname`, ref.Schema, ref.Name)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var ix IndexInfo
			var cols string
			if err := idxRows.Scan(&ix.Name, &ix.Unique, &ix.Primary, &cols); err == nil {
				ix.Columns = strings.Split(cols, ",")
				def.Indexes = append(def.Indexes, ix)
			}
		}
	}

	// Foreign keys
	fkRows, err := p.db.QueryContext(ctx, `
        SELECT con.conname,
               array_to_string(ARRAY(
                   SELECT a.attname FROM pg_attribute a
                   WHERE a.attrelid = con.conrelid AND a.attnum = ANY(con.conkey)
                   ORDER BY array_position(con.conkey, a.attnum)), ','),
               nf.nspname, cf.relname,
               array_to_string(ARRAY(
                   SELECT a.attname FROM pg_attribute a
                   WHERE a.attrelid = con.confrelid AND a.attnum = ANY(con.confkey)
                   ORDER BY array_position(con.confkey, a.attnum)), ',')
        FROM pg_constraint con
        JOIN pg_class c ON c.oid = con.conrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_class cf ON cf.oid = con.confrelid
        JOIN pg_namespace nf ON nf.oid = cf.relnamespace
        WHERE con.contype='f' AND n.nspname=$1 AND c.relname=$2`, ref.Schema, ref.Name)
	if err == nil {
		defer fkRows.Close()
		for fkRows.Next() {
			var fk ForeignKey
			var cols, refCols string
			if err := fkRows.Scan(&fk.Name, &cols, &fk.RefSchema, &fk.RefTable, &refCols); err == nil {
				fk.Columns = strings.Split(cols, ",")
				fk.RefColumns = strings.Split(refCols, ",")
				def.FKs = append(def.FKs, fk)
			}
		}
	}

	// DDL (best-effort: pg has no `pg_get_tabledef`; we synthesise a minimal CREATE TABLE)
	def.DDL = synthesisedCreateTable(p.Kind(), ref, def.Columns, def.Indexes, def.FKs)

	// rowcount approx
	_ = p.db.QueryRowContext(ctx,
		`SELECT COALESCE(reltuples,0)::bigint FROM pg_class c
         JOIN pg_namespace n ON n.oid=c.relnamespace
         WHERE n.nspname=$1 AND c.relname=$2`, ref.Schema, ref.Name).Scan(&def.RowCount)

	return def, nil
}

func (p *pg) SampleRows(ctx context.Context, ref ObjectRef, page Page) (RowSet, error) {
	q, err := qualified(KindPostgres, ref.Schema, ref.Name)
	if err != nil {
		return RowSet{}, err
	}
	order := ""
	if page.OrderBy != "" {
		col, err := quoteIdent(KindPostgres, page.OrderBy)
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
	sqlText := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", q, order, limit, page.Offset)
	rows, err := p.db.QueryContext(ctx, sqlText)
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

func (p *pg) CountRows(ctx context.Context, ref ObjectRef) (int64, error) {
	q, err := qualified(KindPostgres, ref.Schema, ref.Name)
	if err != nil {
		return 0, err
	}
	var n int64
	err = p.db.QueryRowContext(ctx, "SELECT count(*) FROM "+q).Scan(&n)
	return n, err
}

func (p *pg) CreateDatabase(ctx context.Context, name string, opts DBOpts) error {
	q, err := quoteIdent(KindPostgres, name)
	if err != nil {
		return err
	}
	stmt := "CREATE DATABASE " + q
	if opts.Owner != "" {
		o, err := quoteIdent(KindPostgres, opts.Owner)
		if err != nil {
			return err
		}
		stmt += " OWNER " + o
	}
	if opts.Encoding != "" {
		stmt += " ENCODING " + quoteLiteral(opts.Encoding)
	}
	if opts.Template != "" {
		t, err := quoteIdent(KindPostgres, opts.Template)
		if err != nil {
			return err
		}
		stmt += " TEMPLATE " + t
	}
	_, err = p.db.ExecContext(ctx, stmt)
	return err
}

func (p *pg) CreateTable(ctx context.Context, def CreateTableDef) error {
	stmt, err := buildCreateTable(KindPostgres, def)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, stmt)
	return err
}

func (p *pg) CreateView(ctx context.Context, schema, name, sqlText string) error {
	q, err := qualified(KindPostgres, schema, name)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, "CREATE VIEW "+q+" AS "+sqlText)
	return err
}

func (p *pg) CreateIndex(ctx context.Context, def IndexDef) error {
	stmt, err := buildCreateIndex(KindPostgres, def)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, stmt)
	return err
}

func (p *pg) InsertRow(ctx context.Context, ref ObjectRef, values map[string]string) error {
	cols, args, placeholders, err := preparePGInsert(values)
	if err != nil {
		return err
	}
	tbl, err := qualified(KindPostgres, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	_, err = p.db.ExecContext(ctx, stmt, args...)
	return err
}

func (p *pg) UpdateRow(ctx context.Context, ref ObjectRef, pk, values map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	setCols := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(pk))
	i := 1
	for k, v := range values {
		col, err := quoteIdent(KindPostgres, k)
		if err != nil {
			return err
		}
		setCols = append(setCols, fmt.Sprintf("%s=$%d", col, i))
		args = append(args, v)
		i++
	}
	whereCols := make([]string, 0, len(pk))
	for k, v := range pk {
		col, err := quoteIdent(KindPostgres, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, fmt.Sprintf("%s=$%d", col, i))
		args = append(args, v)
		i++
	}
	tbl, err := qualified(KindPostgres, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "UPDATE " + tbl + " SET " + strings.Join(setCols, ", ") + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = p.db.ExecContext(ctx, stmt, args...)
	return err
}

func (p *pg) DeleteRow(ctx context.Context, ref ObjectRef, pk map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	whereCols := make([]string, 0, len(pk))
	args := make([]any, 0, len(pk))
	i := 1
	for k, v := range pk {
		col, err := quoteIdent(KindPostgres, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, fmt.Sprintf("%s=$%d", col, i))
		args = append(args, v)
		i++
	}
	tbl, err := qualified(KindPostgres, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "DELETE FROM " + tbl + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = p.db.ExecContext(ctx, stmt, args...)
	return err
}

func (p *pg) Exec(ctx context.Context, sqlText string, maxRows int) ([]Result, error) {
	return runStatements(ctx, p.db, sqlText, maxRows)
}

func preparePGInsert(values map[string]string) (cols []string, args []any, placeholders []string, err error) {
	cols = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	placeholders = make([]string, 0, len(values))
	i := 1
	for k, v := range values {
		col, err := quoteIdent(KindPostgres, k)
		if err != nil {
			return nil, nil, nil, err
		}
		cols = append(cols, col)
		args = append(args, v)
		placeholders = append(placeholders, "$"+strconv.Itoa(i))
		i++
	}
	return cols, args, placeholders, nil
}
