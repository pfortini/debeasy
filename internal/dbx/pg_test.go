//go:build integration

package dbx

import (
	"os"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// Integration tests hit the postgres container from docker-compose.dev.yml.
// Override via DEBEASY_TEST_PG_DSN (host:port, or env DEBEASY_TEST_PG_HOST/PORT/USER/PASSWORD/DB).
// Run: `go test -tags=integration ./internal/dbx/...`

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func newPG(t *testing.T) Driver {
	t.Helper()
	c := &store.Connection{
		Kind:     "postgres",
		Host:     envDefault("DEBEASY_TEST_PG_HOST", "127.0.0.1"),
		Port:     55432,
		Username: envDefault("DEBEASY_TEST_PG_USER", "debeasy"),
		Password: envDefault("DEBEASY_TEST_PG_PASSWORD", "debeasy"),
		Database: envDefault("DEBEASY_TEST_PG_DB", "postgres"),
		SSLMode:  "disable",
	}
	d, err := openPostgres(c, 4)
	if err != nil {
		t.Fatalf("openPostgres: %v", err)
	}
	if err := d.Ping(t.Context()); err != nil {
		t.Skipf("postgres not reachable — skipping (run docker-compose up first): %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestPG_Lifecycle(t *testing.T) {
	d := newPG(t)
	ctx := t.Context()
	schema := "debeasy_test"

	// Clean slate
	_, _ = d.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10)
	_, _ = d.Exec(ctx, "CREATE SCHEMA "+schema, 10)
	t.Cleanup(func() {
		_, _ = d.Exec(t.Context(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10)
	})

	// CreateTable + CreateIndex + CreateView
	def := CreateTableDef{
		Schema: schema, Name: "widgets",
		Columns: []ColumnDef{
			{Name: "id", DataType: "SERIAL", IsPK: true},
			{Name: "label", DataType: "TEXT", Nullable: true},
		},
	}
	if err := d.CreateTable(ctx, def); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := d.CreateIndex(ctx, IndexDef{
		Name: "widgets_label_idx", Schema: schema, Table: "widgets", Columns: []string{"label"},
	}); err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}
	if err := d.CreateView(ctx, schema, "widgets_v", "SELECT id, label FROM "+schema+".widgets"); err != nil {
		t.Fatalf("CreateView: %v", err)
	}

	// ListObjects
	tree, err := d.ListObjects(ctx, "", schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Tables) != 1 || tree.Tables[0].Name != "widgets" {
		t.Errorf("tables = %+v", tree.Tables)
	}
	if len(tree.Views) != 1 {
		t.Errorf("views = %+v", tree.Views)
	}

	// InsertRow / CountRows / DescribeTable
	if err := d.InsertRow(ctx, ObjectRef{Schema: schema, Name: "widgets"}, map[string]string{"label": "alpha"}); err != nil {
		t.Fatal(err)
	}
	n, _ := d.CountRows(ctx, ObjectRef{Schema: schema, Name: "widgets"})
	if n != 1 {
		t.Errorf("CountRows = %d", n)
	}
	tdef, err := d.DescribeTable(ctx, ObjectRef{Schema: schema, Name: "widgets"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tdef.Columns) != 2 || !tdef.Columns[0].IsPK {
		t.Errorf("columns = %+v", tdef.Columns)
	}
	if !strings.Contains(tdef.DDL, "CREATE TABLE") {
		t.Errorf("DDL missing: %s", tdef.DDL)
	}
}

func TestPG_RowCRUD(t *testing.T) {
	d := newPG(t)
	ctx := t.Context()
	schema := "debeasy_rowcrud"
	_, _ = d.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10)
	_, _ = d.Exec(ctx, "CREATE SCHEMA "+schema, 10)
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10) })

	err := d.CreateTable(ctx, CreateTableDef{
		Schema: schema, Name: "t",
		Columns: []ColumnDef{
			{Name: "id", DataType: "INT", IsPK: true},
			{Name: "label", DataType: "TEXT"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ref := ObjectRef{Schema: schema, Name: "t"}
	if err := d.InsertRow(ctx, ref, map[string]string{"id": "1", "label": "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertRow(ctx, ref, map[string]string{"id": "2", "label": "beta"}); err != nil {
		t.Fatal(err)
	}
	rs, err := d.SampleRows(ctx, ref, Page{Limit: 10, OrderBy: "id"})
	if err != nil || len(rs.Rows) != 2 {
		t.Fatalf("sample len=%d err=%v", len(rs.Rows), err)
	}
	if err := d.UpdateRow(ctx, ref, map[string]string{"id": "1"}, map[string]string{"label": "ALPHA"}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteRow(ctx, ref, map[string]string{"id": "2"}); err != nil {
		t.Fatal(err)
	}
	n, _ := d.CountRows(ctx, ref)
	if n != 1 {
		t.Errorf("after delete, count = %d", n)
	}
	if err := d.UpdateRow(ctx, ref, nil, map[string]string{"label": "x"}); err != ErrNoPrimaryKey {
		t.Errorf("UpdateRow(nil PK) = %v", err)
	}
	if err := d.DeleteRow(ctx, ref, nil); err != ErrNoPrimaryKey {
		t.Errorf("DeleteRow(nil PK) = %v", err)
	}
}

func TestPG_DescribeTable_WithFKs(t *testing.T) {
	d := newPG(t)
	ctx := t.Context()
	schema := "debeasy_fks"
	_, _ = d.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10)
	_, _ = d.Exec(ctx, "CREATE SCHEMA "+schema, 10)
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE", 10) })

	_, err := d.Exec(ctx, `
        CREATE TABLE `+schema+`.parents(id INT PRIMARY KEY);
        CREATE TABLE `+schema+`.kids(
            id INT PRIMARY KEY,
            parent_id INT NOT NULL REFERENCES `+schema+`.parents(id)
        );
        CREATE INDEX kids_parent_idx ON `+schema+`.kids(parent_id);
    `, 10)
	if err != nil {
		t.Fatal(err)
	}
	def, err := d.DescribeTable(ctx, ObjectRef{Schema: schema, Name: "kids"})
	if err != nil {
		t.Fatal(err)
	}
	if len(def.FKs) != 1 || def.FKs[0].RefTable != "parents" {
		t.Errorf("fks = %+v", def.FKs)
	}
	if len(def.Indexes) == 0 {
		t.Errorf("expected indexes in describe; got %+v", def.Indexes)
	}
}

func TestPG_CreateDatabase(t *testing.T) {
	d := newPG(t)
	ctx := t.Context()

	// Happy path + exercise every optional DBOpts field.
	name := "debeasy_createdb_test"
	_, _ = d.Exec(ctx, "DROP DATABASE IF EXISTS "+name, 10)
	if err := d.CreateDatabase(ctx, name, DBOpts{
		Encoding: "UTF8",
		Owner:    "debeasy",
		Template: "template0",
	}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP DATABASE IF EXISTS "+name, 10) })

	// Error branch — bad owner.
	if err := d.CreateDatabase(ctx, "xdbe2", DBOpts{Owner: "noexist"}); err == nil {
		_, _ = d.Exec(ctx, "DROP DATABASE IF EXISTS xdbe2", 10)
		t.Errorf("expected error on nonexistent owner")
	}
}

func TestPG_ListDatabasesAndSchemas(t *testing.T) {
	d := newPG(t)
	dbs, err := d.ListDatabases(t.Context())
	if err != nil || len(dbs) == 0 {
		t.Errorf("ListDatabases len=%d err=%v", len(dbs), err)
	}
	schemas, err := d.ListSchemas(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	foundPublic := false
	for _, s := range schemas {
		if s.Name == "public" {
			foundPublic = true
		}
	}
	if !foundPublic {
		t.Errorf("expected 'public' schema in list; got %+v", schemas)
	}
}
