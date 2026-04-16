//go:build integration

package dbx

import (
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// Integration tests hit the mysql container from docker-compose.dev.yml.
// Run: `go test -tags=integration ./internal/dbx/...`

func newMySQL(t *testing.T) Driver {
	t.Helper()
	c := &store.Connection{
		Kind:     "mysql",
		Host:     envDefault("DEBEASY_TEST_MYSQL_HOST", "127.0.0.1"),
		Port:     53306,
		Username: envDefault("DEBEASY_TEST_MYSQL_USER", "debeasy"),
		Password: envDefault("DEBEASY_TEST_MYSQL_PASSWORD", "debeasy"),
		Database: envDefault("DEBEASY_TEST_MYSQL_DB", "debeasy"),
	}
	d, err := openMySQL(c, 4)
	if err != nil {
		t.Fatalf("openMySQL: %v", err)
	}
	if err := d.Ping(t.Context()); err != nil {
		t.Skipf("mysql not reachable — skipping (run docker-compose up first): %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestMySQL_RowCRUD(t *testing.T) {
	d := newMySQL(t)
	ctx := t.Context()
	dbName := "debeasy_rowcrud"
	_, _ = d.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName, 10)
	if err := d.CreateDatabase(ctx, dbName, DBOpts{Encoding: "utf8mb4"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP DATABASE IF EXISTS "+dbName, 10) })

	err := d.CreateTable(ctx, CreateTableDef{
		Schema: dbName, Name: "t",
		Columns: []ColumnDef{
			{Name: "id", DataType: "INT", IsPK: true},
			{Name: "label", DataType: "VARCHAR(64)"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := ObjectRef{Schema: dbName, Name: "t"}
	_ = d.InsertRow(ctx, ref, map[string]string{"id": "1", "label": "alpha"})
	_ = d.InsertRow(ctx, ref, map[string]string{"id": "2", "label": "beta"})
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

func TestMySQL_DescribeTable_WithFKs(t *testing.T) {
	d := newMySQL(t)
	ctx := t.Context()
	dbName := "debeasy_fks"
	_, _ = d.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName, 10)
	if err := d.CreateDatabase(ctx, dbName, DBOpts{Encoding: "utf8mb4"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP DATABASE IF EXISTS "+dbName, 10) })

	_, err := d.Exec(ctx, `
        CREATE TABLE `+dbName+`.parents(id INT PRIMARY KEY);
        CREATE TABLE `+dbName+`.kids(
            id INT PRIMARY KEY,
            parent_id INT NOT NULL,
            CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES `+dbName+`.parents(id)
        );
        CREATE INDEX kids_parent_idx ON `+dbName+`.kids(parent_id);
    `, 10)
	if err != nil {
		t.Fatal(err)
	}
	def, err := d.DescribeTable(ctx, ObjectRef{Schema: dbName, Name: "kids"})
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

func TestMySQL_ListDatabases(t *testing.T) {
	d := newMySQL(t)
	dbs, err := d.ListDatabases(t.Context())
	if err != nil || len(dbs) == 0 {
		t.Errorf("ListDatabases len=%d err=%v", len(dbs), err)
	}
}

func TestMySQL_Lifecycle(t *testing.T) {
	d := newMySQL(t)
	ctx := t.Context()
	dbName := "debeasy_test"

	// Clean slate
	_, _ = d.Exec(ctx, "DROP DATABASE IF EXISTS "+dbName, 10)
	if err := d.CreateDatabase(ctx, dbName, DBOpts{Encoding: "utf8mb4", Collation: "utf8mb4_unicode_ci"}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	t.Cleanup(func() { _, _ = d.Exec(t.Context(), "DROP DATABASE IF EXISTS "+dbName, 10) })

	// CreateTable + CreateIndex + CreateView
	def := CreateTableDef{
		Schema: dbName, Name: "widgets",
		Columns: []ColumnDef{
			{Name: "id", DataType: "INT AUTO_INCREMENT", IsPK: true},
			{Name: "label", DataType: "VARCHAR(64)", Nullable: true},
		},
	}
	if err := d.CreateTable(ctx, def); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := d.CreateIndex(ctx, IndexDef{
		Name: "widgets_label", Schema: dbName, Table: "widgets", Columns: []string{"label"},
	}); err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}
	if err := d.CreateView(ctx, dbName, "widgets_v", "SELECT id, label FROM "+dbName+".widgets"); err != nil {
		t.Fatalf("CreateView: %v", err)
	}

	// Navigator sees the new DB + its contents
	schemas, _ := d.ListSchemas(ctx, "")
	foundDB := false
	for _, s := range schemas {
		if s.Name == dbName {
			foundDB = true
		}
	}
	if !foundDB {
		t.Errorf("ListSchemas did not include new DB %q", dbName)
	}

	tree, err := d.ListObjects(ctx, "", dbName)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Tables) != 1 || tree.Tables[0].Name != "widgets" {
		t.Errorf("tables = %+v", tree.Tables)
	}
	if len(tree.Views) != 1 {
		t.Errorf("views = %+v", tree.Views)
	}

	// InsertRow / DescribeTable / CountRows
	if err := d.InsertRow(ctx, ObjectRef{Schema: dbName, Name: "widgets"}, map[string]string{"label": "alpha"}); err != nil {
		t.Fatal(err)
	}
	n, _ := d.CountRows(ctx, ObjectRef{Schema: dbName, Name: "widgets"})
	if n != 1 {
		t.Errorf("CountRows = %d", n)
	}
	tdef, err := d.DescribeTable(ctx, ObjectRef{Schema: dbName, Name: "widgets"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tdef.DDL, "CREATE TABLE") {
		t.Errorf("DDL missing CREATE TABLE: %s", tdef.DDL)
	}
}
