package dbx

import (
	"context"
	"errors"
	"fmt"
)

type Kind string

const (
	KindPostgres Kind = "postgres"
	KindMySQL    Kind = "mysql"
	KindSQLite   Kind = "sqlite"
)

func ParseKind(s string) (Kind, error) {
	switch s {
	case "postgres", "pg", "postgresql":
		return KindPostgres, nil
	case "mysql", "mariadb":
		return KindMySQL, nil
	case "sqlite", "sqlite3":
		return KindSQLite, nil
	}
	return "", fmt.Errorf("unknown driver kind %q", s)
}

// ObjectKind describes a leaf navigator node.
type ObjectKind string

const (
	ObjTable    ObjectKind = "table"
	ObjView     ObjectKind = "view"
	ObjMatView  ObjectKind = "matview"
	ObjIndex    ObjectKind = "index"
	ObjSequence ObjectKind = "sequence"
)

// DB represents a top-level database (or "main" for sqlite).
type DB struct {
	Name    string
	Comment string
}

// Schema represents a namespace within a DB. For mysql/sqlite this equals the DB itself.
type Schema struct {
	Name string
}

// DefaultSchema picks the schema to present when the user has not chosen one.
// Alphabetical-first is a bad default: a PG database with Drizzle's `drizzle`
// schema sorts before `public`, so a fresh connection would open onto a near-
// empty tree. Prefer the driver-native default (`public` on PG, the connection's
// DB on MySQL, `main` on SQLite) and only fall back to the first schema.
func DefaultSchema(kind Kind, connDB string, schemas []Schema) string {
	if len(schemas) == 0 {
		return ""
	}
	has := func(name string) bool {
		for _, s := range schemas {
			if s.Name == name {
				return true
			}
		}
		return false
	}
	switch kind {
	case KindPostgres:
		if has("public") {
			return "public"
		}
	case KindMySQL:
		if connDB != "" && has(connDB) {
			return connDB
		}
	}
	return schemas[0].Name
}

// Object is a navigator node.
type Object struct {
	Kind    ObjectKind
	Schema  string
	Name    string
	Comment string
}

// ObjectTree is a flat list returned in render order.
type ObjectTree struct {
	Tables  []Object
	Views   []Object
	Indexes []Object
	Other   []Object
}

// ObjectRef refers to a single object in a (db, schema, name) tuple.
type ObjectRef struct {
	DB     string
	Schema string
	Name   string
}

// Column metadata.
type Column struct {
	Name       string
	DataType   string
	Nullable   bool
	Default    string
	IsPK       bool
	OrdinalPos int
}

// IndexInfo about a single index on a table.
type IndexInfo struct {
	Name    string
	Columns []string
	Unique  bool
	Primary bool
}

// ForeignKey on a table.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefSchema  string
	RefTable   string
	RefColumns []string
	OnDelete   string
	OnUpdate   string
}

// TableDef is the full definition of a table-like object.
type TableDef struct {
	Ref      ObjectRef
	Kind     ObjectKind
	Columns  []Column
	Indexes  []IndexInfo
	FKs      []ForeignKey
	RowCount int64
	DDL      string
}

// Page request for paginated reads.
type Page struct {
	Offset  int
	Limit   int
	OrderBy string // optional column name
	Desc    bool
}

// RowSet is a snapshotted result. Streaming is implemented internally by Exec.
type RowSet struct {
	Columns   []string
	Types     []string
	Rows      [][]any
	Truncated bool
}

// ExecStats describes a non-query execution.
type ExecStats struct {
	RowsAffected int64
	ElapsedMS    int64
}

// DBOpts for CreateDatabase.
type DBOpts struct {
	Encoding  string
	Collation string
	Owner     string
	Template  string // pg
}

// IndexDef for CreateIndex.
type IndexDef struct {
	Name    string
	Schema  string
	Table   string
	Columns []string
	Unique  bool
	Method  string // pg only: btree, hash, gin, gist
}

// ColumnDef for CreateTable.
type ColumnDef struct {
	Name     string
	DataType string
	Nullable bool
	Default  string
	IsPK     bool
}

// CreateTableDef for CreateTable input.
type CreateTableDef struct {
	Schema      string
	Name        string
	Columns     []ColumnDef
	IfNotExists bool
}

// Driver is the abstraction every backend implements.
type Driver interface {
	Kind() Kind
	Ping(ctx context.Context) error
	Close() error

	ListDatabases(ctx context.Context) ([]DB, error)
	ListSchemas(ctx context.Context, db string) ([]Schema, error)
	ListObjects(ctx context.Context, db, schema string) (ObjectTree, error)

	DescribeTable(ctx context.Context, ref ObjectRef) (TableDef, error)
	SampleRows(ctx context.Context, ref ObjectRef, page Page) (RowSet, error)
	CountRows(ctx context.Context, ref ObjectRef) (int64, error)

	CreateDatabase(ctx context.Context, name string, opts DBOpts) error
	CreateTable(ctx context.Context, def CreateTableDef) error
	CreateView(ctx context.Context, schema, name, sql string) error
	CreateIndex(ctx context.Context, def IndexDef) error

	InsertRow(ctx context.Context, ref ObjectRef, values map[string]string) error
	UpdateRow(ctx context.Context, ref ObjectRef, pk, values map[string]string) error
	DeleteRow(ctx context.Context, ref ObjectRef, pk map[string]string) error

	Exec(ctx context.Context, sql string, maxRows int) ([]Result, error)
}

// Result is one statement's outcome (Exec can run multi-statement).
type Result struct {
	SQL     string
	IsQuery bool
	Rows    *RowSet
	Stats   ExecStats
	Err     string
}

var ErrNoPrimaryKey = errors.New("table has no primary key — row edit unsupported")
