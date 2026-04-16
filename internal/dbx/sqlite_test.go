package dbx

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// newSQLiteDriver opens a fresh sqlite file in a temp dir and returns the open Driver.
func newSQLiteDriver(t *testing.T) Driver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.sqlite")
	d, err := openSQLite(&store.Connection{Database: path}, 1)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	return d
}

func TestOpenSQLite_RequiresDatabasePath(t *testing.T) {
	if _, err := openSQLite(&store.Connection{}, 1); err == nil {
		t.Fatal("openSQLite with empty Database should error")
	}
}

func TestSQLite_KindAndLists(t *testing.T) {
	d := newSQLiteDriver(t)
	if d.Kind() != KindSQLite {
		t.Errorf("Kind = %s", d.Kind())
	}
	dbs, _ := d.ListDatabases(t.Context())
	if len(dbs) != 1 || dbs[0].Name != "main" {
		t.Errorf("ListDatabases = %+v", dbs)
	}
	schemas, _ := d.ListSchemas(t.Context(), "")
	if len(schemas) != 1 || schemas[0].Name != "main" {
		t.Errorf("ListSchemas = %+v", schemas)
	}
}

func TestSQLite_FullLifecycle(t *testing.T) {
	d := newSQLiteDriver(t)
	ctx := t.Context()

	// CreateTable
	err := d.CreateTable(ctx, CreateTableDef{
		Name: "t",
		Columns: []ColumnDef{
			{Name: "id", DataType: "INTEGER", IsPK: true},
			{Name: "label", DataType: "TEXT", Nullable: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	// ListObjects picks it up
	tree, err := d.ListObjects(ctx, "main", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Tables) != 1 || tree.Tables[0].Name != "t" {
		t.Fatalf("ListObjects.Tables = %+v", tree.Tables)
	}

	// DescribeTable
	def, err := d.DescribeTable(ctx, ObjectRef{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Columns) != 2 || !def.Columns[0].IsPK {
		t.Fatalf("columns = %+v", def.Columns)
	}
	if !strings.Contains(def.DDL, "CREATE TABLE") {
		t.Errorf("DDL missing CREATE TABLE: %s", def.DDL)
	}

	// InsertRow + SampleRows + CountRows
	if err := d.InsertRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": "1", "label": "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": "2", "label": "beta"}); err != nil {
		t.Fatal(err)
	}
	n, err := d.CountRows(ctx, ObjectRef{Name: "t"})
	if err != nil || n != 2 {
		t.Errorf("CountRows = %d err=%v", n, err)
	}
	rs, err := d.SampleRows(ctx, ObjectRef{Name: "t"}, Page{Limit: 10, OrderBy: "id"})
	if err != nil || len(rs.Rows) != 2 {
		t.Fatalf("SampleRows len=%d err=%v", len(rs.Rows), err)
	}

	// UpdateRow
	if err := d.UpdateRow(ctx, ObjectRef{Name: "t"},
		map[string]string{"id": "1"},
		map[string]string{"label": "alpha-renamed"},
	); err != nil {
		t.Fatal(err)
	}

	// DeleteRow
	if err := d.DeleteRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": "2"}); err != nil {
		t.Fatal(err)
	}
	n, _ = d.CountRows(ctx, ObjectRef{Name: "t"})
	if n != 1 {
		t.Errorf("after delete, count = %d", n)
	}

	// No-PK guard
	if err := d.UpdateRow(ctx, ObjectRef{Name: "t"}, nil, map[string]string{"label": "x"}); !errors.Is(err, ErrNoPrimaryKey) {
		t.Errorf("UpdateRow(nil PK) = %v; want ErrNoPrimaryKey", err)
	}
	if err := d.DeleteRow(ctx, ObjectRef{Name: "t"}, nil); !errors.Is(err, ErrNoPrimaryKey) {
		t.Errorf("DeleteRow(nil PK) = %v; want ErrNoPrimaryKey", err)
	}
}

func TestSQLite_CreateIndexAndView(t *testing.T) {
	d := newSQLiteDriver(t)
	ctx := t.Context()
	_ = d.CreateTable(ctx, CreateTableDef{
		Name: "widgets",
		Columns: []ColumnDef{
			{Name: "id", DataType: "INTEGER", IsPK: true},
			{Name: "label", DataType: "TEXT"},
		},
	})
	if err := d.CreateIndex(ctx, IndexDef{
		Name: "widgets_label", Table: "widgets", Columns: []string{"label"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateView(ctx, "main", "widgets_v",
		"SELECT id, label FROM widgets"); err != nil {
		t.Fatal(err)
	}
	tree, _ := d.ListObjects(ctx, "main", "main")
	if len(tree.Views) != 1 || tree.Views[0].Name != "widgets_v" {
		t.Errorf("view not listed: %+v", tree.Views)
	}
	if len(tree.Indexes) == 0 {
		t.Errorf("index not listed: %+v", tree.Indexes)
	}
}

func TestSQLite_DescribeTable_WithIndexes(t *testing.T) {
	// Exercises indexColumns(): describe a table that has a non-PK index so
	// PRAGMA index_list returns entries, forcing DescribeTable into the
	// index-resolution branch.
	d := newSQLiteDriver(t)
	ctx := t.Context()
	_, err := d.Exec(ctx, `
        CREATE TABLE widgets (id INTEGER PRIMARY KEY, label TEXT, tag TEXT);
        CREATE INDEX widgets_label_idx ON widgets(label);
        CREATE UNIQUE INDEX widgets_tag_uniq ON widgets(tag);
    `, 100)
	if err != nil {
		t.Fatal(err)
	}
	def, err := d.DescribeTable(ctx, ObjectRef{Name: "widgets"})
	if err != nil {
		t.Fatal(err)
	}
	gotNames := map[string]IndexInfo{}
	for _, ix := range def.Indexes {
		gotNames[ix.Name] = ix
	}
	labelIdx, ok := gotNames["widgets_label_idx"]
	if !ok || len(labelIdx.Columns) != 1 || labelIdx.Columns[0] != "label" || labelIdx.Unique {
		t.Errorf("label index not parsed correctly: %+v", gotNames)
	}
	tagIdx, ok := gotNames["widgets_tag_uniq"]
	if !ok || !tagIdx.Unique {
		t.Errorf("unique index not parsed correctly: %+v", tagIdx)
	}
}

func TestSQLite_DescribeTable_WithFKs(t *testing.T) {
	// Exercises the FK branch of DescribeTable — one table referencing another.
	d := newSQLiteDriver(t)
	ctx := t.Context()
	_, err := d.Exec(ctx, `
        CREATE TABLE parents (id INTEGER PRIMARY KEY);
        CREATE TABLE kids (
            id INTEGER PRIMARY KEY,
            parent_id INTEGER REFERENCES parents(id) ON DELETE CASCADE
        );
    `, 100)
	if err != nil {
		t.Fatal(err)
	}
	def, err := d.DescribeTable(ctx, ObjectRef{Name: "kids"})
	if err != nil {
		t.Fatal(err)
	}
	if len(def.FKs) != 1 {
		t.Fatalf("expected 1 FK, got %+v", def.FKs)
	}
	fk := def.FKs[0]
	if fk.RefTable != "parents" || fk.OnDelete != "CASCADE" {
		t.Errorf("FK = %+v", fk)
	}
}

func TestSQLite_CreateDatabase_Rejected(t *testing.T) {
	d := newSQLiteDriver(t)
	err := d.CreateDatabase(t.Context(), "x", DBOpts{})
	if err == nil || !strings.Contains(err.Error(), "file") {
		t.Errorf("sqlite CreateDatabase should tell the caller to make a new connection; got %v", err)
	}
}

func TestSQLite_Exec_MultiStatement(t *testing.T) {
	d := newSQLiteDriver(t)
	ctx := t.Context()
	results, err := d.Exec(ctx,
		"CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT); "+
			"INSERT INTO t(id,name) VALUES(1,'a'),(2,'b'); "+
			"SELECT * FROM t ORDER BY id;",
		100)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 result blocks, got %d", len(results))
	}
	if results[0].IsQuery || results[1].IsQuery || !results[2].IsQuery {
		t.Errorf("IsQuery flags wrong: %+v / %+v / %+v",
			results[0].IsQuery, results[1].IsQuery, results[2].IsQuery)
	}
	if results[2].Rows == nil || len(results[2].Rows.Rows) != 2 {
		t.Errorf("last block should return 2 rows")
	}
}

func TestSQLite_Exec_Error(t *testing.T) {
	d := newSQLiteDriver(t)
	results, err := d.Exec(t.Context(), "SELECT * FROM nosuch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err == "" {
		t.Errorf("expected result with Err set, got %+v", results)
	}
}

func TestSQLite_Exec_Empty(t *testing.T) {
	d := newSQLiteDriver(t)
	if _, err := d.Exec(t.Context(), "   \n  ", 10); err == nil {
		t.Error("want error for empty input")
	}
}

func TestSQLite_SampleRows_MaxRowsCap(t *testing.T) {
	d := newSQLiteDriver(t)
	ctx := t.Context()
	_ = d.CreateTable(ctx, CreateTableDef{Name: "t", Columns: []ColumnDef{{Name: "id", DataType: "INTEGER", IsPK: true}}})
	for i := 1; i <= 5; i++ {
		_ = d.InsertRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": intStr(i)})
	}
	rs, _ := d.SampleRows(ctx, ObjectRef{Name: "t"}, Page{Limit: 2, OrderBy: "id", Desc: true})
	if len(rs.Rows) != 2 {
		t.Errorf("limit not honoured: %d", len(rs.Rows))
	}
}

func intStr(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	b := []byte{}
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}
