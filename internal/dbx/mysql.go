package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/pfortini/debeasy/internal/store"
)

type mysqlDriver struct{ db *sql.DB }

func openMySQL(c *store.Connection, maxOpen int) (Driver, error) {
	cfg := mysqldrv.NewConfig()
	cfg.User = c.Username
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	port := c.Port
	if port == 0 {
		port = 3306
	}
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = c.Database
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.AllowNativePasswords = true
	if c.Params != "" {
		// "k1=v1&k2=v2" style — map onto cfg.Params
		for _, p := range strings.Split(c.Params, "&") {
			kv := strings.SplitN(p, "=", 2)
			if len(kv) == 2 {
				if cfg.Params == nil {
					cfg.Params = map[string]string{}
				}
				cfg.Params[kv[0]] = kv[1]
			}
		}
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &mysqlDriver{db: db}, nil
}

func (m *mysqlDriver) Kind() Kind                     { return KindMySQL }
func (m *mysqlDriver) Close() error                   { return m.db.Close() }
func (m *mysqlDriver) Ping(ctx context.Context) error { return m.db.PingContext(ctx) }

func (m *mysqlDriver) ListDatabases(ctx context.Context) ([]DB, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT schema_name FROM information_schema.schemata
         WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys')
         ORDER BY schema_name`)
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

// MySQL: a "schema" == a database. Return every user-visible database so the
// navigator's schema dropdown lets the user browse across them (and pick up DBs
// created from the wizard / SQL editor).
func (m *mysqlDriver) ListSchemas(ctx context.Context, _ string) ([]Schema, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT schema_name FROM information_schema.schemata
         WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys')
         ORDER BY schema_name`)
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

func (m *mysqlDriver) ListObjects(ctx context.Context, _, schema string) (ObjectTree, error) {
	tree := ObjectTree{}
	rows, err := m.db.QueryContext(ctx, `
        SELECT table_name,
               CASE table_type WHEN 'BASE TABLE' THEN 'table' WHEN 'VIEW' THEN 'view' ELSE LOWER(table_type) END,
               COALESCE(table_comment,'')
        FROM information_schema.tables WHERE table_schema = ?
        ORDER BY 2, 1`, schema)
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
		case ObjView:
			tree.Views = append(tree.Views, o)
		default:
			tree.Other = append(tree.Other, o)
		}
	}
	idxRows, err := m.db.QueryContext(ctx,
		`SELECT DISTINCT index_name, table_name FROM information_schema.statistics
         WHERE table_schema=? AND index_name <> 'PRIMARY' ORDER BY table_name, index_name`, schema)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var o Object
			o.Schema = schema
			o.Kind = ObjIndex
			var tbl string
			if err := idxRows.Scan(&o.Name, &tbl); err == nil {
				o.Comment = "on " + tbl
				tree.Indexes = append(tree.Indexes, o)
			}
		}
	}
	return tree, nil
}

func (m *mysqlDriver) DescribeTable(ctx context.Context, ref ObjectRef) (TableDef, error) {
	def := TableDef{Ref: ref, Kind: ObjTable}
	rows, err := m.db.QueryContext(ctx, `
        SELECT column_name, column_type,
               CASE WHEN is_nullable = 'YES' THEN 1 ELSE 0 END,
               COALESCE(column_default,''),
               CASE WHEN column_key='PRI' THEN 1 ELSE 0 END,
               ordinal_position
        FROM information_schema.columns
        WHERE table_schema=? AND table_name=? ORDER BY ordinal_position`, ref.Schema, ref.Name)
	if err != nil {
		return def, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Column
		var nullable, isPK int
		if err := rows.Scan(&c.Name, &c.DataType, &nullable, &c.Default, &isPK, &c.OrdinalPos); err != nil {
			return def, err
		}
		c.Nullable = nullable == 1
		c.IsPK = isPK == 1
		def.Columns = append(def.Columns, c)
	}

	idxRows, err := m.db.QueryContext(ctx, `
        SELECT index_name,
               CASE WHEN non_unique=0 THEN 1 ELSE 0 END,
               CASE WHEN index_name='PRIMARY' THEN 1 ELSE 0 END,
               GROUP_CONCAT(column_name ORDER BY seq_in_index)
        FROM information_schema.statistics
        WHERE table_schema=? AND table_name=? GROUP BY index_name, non_unique`,
		ref.Schema, ref.Name)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var ix IndexInfo
			var unique, primary int
			var cols string
			if err := idxRows.Scan(&ix.Name, &unique, &primary, &cols); err == nil {
				ix.Unique = unique == 1
				ix.Primary = primary == 1
				ix.Columns = strings.Split(cols, ",")
				def.Indexes = append(def.Indexes, ix)
			}
		}
	}

	fkRows, err := m.db.QueryContext(ctx, `
        SELECT k.constraint_name,
               GROUP_CONCAT(k.column_name ORDER BY k.ordinal_position),
               k.referenced_table_schema, k.referenced_table_name,
               GROUP_CONCAT(k.referenced_column_name ORDER BY k.ordinal_position)
        FROM information_schema.key_column_usage k
        WHERE k.table_schema=? AND k.table_name=? AND k.referenced_table_name IS NOT NULL
        GROUP BY k.constraint_name, k.referenced_table_schema, k.referenced_table_name`,
		ref.Schema, ref.Name)
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

	// SHOW CREATE TABLE for accurate DDL
	var ddlName, ddl string
	if err := m.db.QueryRowContext(ctx, "SHOW CREATE TABLE "+mustQuoteIdent(KindMySQL, ref.Schema)+"."+mustQuoteIdent(KindMySQL, ref.Name)).Scan(&ddlName, &ddl); err == nil {
		def.DDL = ddl
	} else {
		def.DDL = synthesisedCreateTable(KindMySQL, ref, def.Columns, def.Indexes, def.FKs)
	}

	_ = m.db.QueryRowContext(ctx,
		`SELECT COALESCE(table_rows,0) FROM information_schema.tables WHERE table_schema=? AND table_name=?`,
		ref.Schema, ref.Name).Scan(&def.RowCount)
	return def, nil
}

func (m *mysqlDriver) SampleRows(ctx context.Context, ref ObjectRef, page Page) (RowSet, error) {
	q, err := qualified(KindMySQL, ref.Schema, ref.Name)
	if err != nil {
		return RowSet{}, err
	}
	order := ""
	if page.OrderBy != "" {
		col, err := quoteIdent(KindMySQL, page.OrderBy)
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
	stmt := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", q, order, limit, page.Offset)
	rows, err := m.db.QueryContext(ctx, stmt)
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

func (m *mysqlDriver) CountRows(ctx context.Context, ref ObjectRef) (int64, error) {
	q, err := qualified(KindMySQL, ref.Schema, ref.Name)
	if err != nil {
		return 0, err
	}
	var n int64
	err = m.db.QueryRowContext(ctx, "SELECT count(*) FROM "+q).Scan(&n)
	return n, err
}

func (m *mysqlDriver) CreateDatabase(ctx context.Context, name string, opts DBOpts) error {
	q, err := quoteIdent(KindMySQL, name)
	if err != nil {
		return err
	}
	stmt := "CREATE DATABASE " + q
	if opts.Encoding != "" {
		stmt += " CHARACTER SET " + opts.Encoding
	}
	if opts.Collation != "" {
		stmt += " COLLATE " + opts.Collation
	}
	_, err = m.db.ExecContext(ctx, stmt)
	return err
}

func (m *mysqlDriver) CreateTable(ctx context.Context, def CreateTableDef) error {
	stmt, err := buildCreateTable(KindMySQL, def)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, stmt)
	return err
}

func (m *mysqlDriver) CreateView(ctx context.Context, schema, name, sqlText string) error {
	q, err := qualified(KindMySQL, schema, name)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, "CREATE VIEW "+q+" AS "+sqlText)
	return err
}

func (m *mysqlDriver) CreateIndex(ctx context.Context, def IndexDef) error {
	stmt, err := buildCreateIndex(KindMySQL, def)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, stmt)
	return err
}

func (m *mysqlDriver) InsertRow(ctx context.Context, ref ObjectRef, values map[string]string) error {
	cols, args, placeholders, err := prepareMySQLInsert(values)
	if err != nil {
		return err
	}
	tbl, err := qualified(KindMySQL, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	_, err = m.db.ExecContext(ctx, stmt, args...)
	return err
}

func (m *mysqlDriver) UpdateRow(ctx context.Context, ref ObjectRef, pk, values map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	setCols := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(pk))
	for k, v := range values {
		col, err := quoteIdent(KindMySQL, k)
		if err != nil {
			return err
		}
		setCols = append(setCols, col+"=?")
		args = append(args, v)
	}
	whereCols := make([]string, 0, len(pk))
	for k, v := range pk {
		col, err := quoteIdent(KindMySQL, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, col+"=?")
		args = append(args, v)
	}
	tbl, err := qualified(KindMySQL, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "UPDATE " + tbl + " SET " + strings.Join(setCols, ", ") + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = m.db.ExecContext(ctx, stmt, args...)
	return err
}

func (m *mysqlDriver) DeleteRow(ctx context.Context, ref ObjectRef, pk map[string]string) error {
	if len(pk) == 0 {
		return ErrNoPrimaryKey
	}
	whereCols := make([]string, 0, len(pk))
	args := make([]any, 0, len(pk))
	for k, v := range pk {
		col, err := quoteIdent(KindMySQL, k)
		if err != nil {
			return err
		}
		whereCols = append(whereCols, col+"=?")
		args = append(args, v)
	}
	tbl, err := qualified(KindMySQL, ref.Schema, ref.Name)
	if err != nil {
		return err
	}
	stmt := "DELETE FROM " + tbl + " WHERE " + strings.Join(whereCols, " AND ")
	_, err = m.db.ExecContext(ctx, stmt, args...)
	return err
}

func (m *mysqlDriver) Exec(ctx context.Context, sqlText string, maxRows int) ([]Result, error) {
	return runStatements(ctx, m.db, sqlText, maxRows)
}

func prepareMySQLInsert(values map[string]string) (cols []string, args []any, placeholders []string, err error) {
	cols = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	placeholders = make([]string, 0, len(values))
	for k, v := range values {
		col, err := quoteIdent(KindMySQL, k)
		if err != nil {
			return nil, nil, nil, err
		}
		cols = append(cols, col)
		args = append(args, v)
		placeholders = append(placeholders, "?")
	}
	return cols, args, placeholders, nil
}
