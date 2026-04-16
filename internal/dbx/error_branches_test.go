package dbx

import (
	"context"
	"testing"
)

// Exercises the identifier-validation error branches on the pg and mysql drivers.
// These checks run before any *sql.DB access, so zero-value drivers are fine.

func TestPG_IdentifierErrors(t *testing.T) {
	p := &pg{}
	ctx := context.Background()
	bad := `a"b` // double-quote is rejected for pg/sqlite
	ref := ObjectRef{Schema: bad, Name: "t"}

	if _, err := p.SampleRows(ctx, ref, Page{}); err == nil {
		t.Error("SampleRows bad schema: want err")
	}
	if _, err := p.CountRows(ctx, ref); err == nil {
		t.Error("CountRows bad schema: want err")
	}
	if err := p.CreateDatabase(ctx, bad, DBOpts{}); err == nil {
		t.Error("CreateDatabase bad name: want err")
	}
	if err := p.CreateDatabase(ctx, "ok", DBOpts{Owner: bad}); err == nil {
		t.Error("CreateDatabase bad owner: want err")
	}
	if err := p.CreateDatabase(ctx, "ok", DBOpts{Template: bad}); err == nil {
		t.Error("CreateDatabase bad template: want err")
	}
	if err := p.CreateTable(ctx, CreateTableDef{Schema: bad, Name: "t", Columns: []ColumnDef{{Name: "id", DataType: "INT"}}}); err == nil {
		t.Error("CreateTable bad schema: want err")
	}
	if err := p.CreateView(ctx, bad, "v", "SELECT 1"); err == nil {
		t.Error("CreateView bad schema: want err")
	}
	if err := p.CreateIndex(ctx, IndexDef{Name: bad, Table: "t", Columns: []string{"c"}}); err == nil {
		t.Error("CreateIndex bad name: want err")
	}
	if err := p.InsertRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "v"}); err == nil {
		t.Error("InsertRow bad column: want err")
	}
	if err := p.InsertRow(ctx, ref, map[string]string{"c": "v"}); err == nil {
		t.Error("InsertRow bad schema: want err")
	}
	if err := p.UpdateRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": "1"}, map[string]string{bad: "v"}); err == nil {
		t.Error("UpdateRow bad set col: want err")
	}
	if err := p.UpdateRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "1"}, map[string]string{"c": "v"}); err == nil {
		t.Error("UpdateRow bad pk col: want err")
	}
	if err := p.UpdateRow(ctx, ref, map[string]string{"id": "1"}, map[string]string{"c": "v"}); err == nil {
		t.Error("UpdateRow bad schema: want err")
	}
	if err := p.DeleteRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "1"}); err == nil {
		t.Error("DeleteRow bad pk col: want err")
	}
	if err := p.DeleteRow(ctx, ref, map[string]string{"id": "1"}); err == nil {
		t.Error("DeleteRow bad schema: want err")
	}
}

func TestMySQL_IdentifierErrors(t *testing.T) {
	m := &mysqlDriver{}
	ctx := context.Background()
	bad := "a`b" // backtick is rejected for mysql
	ref := ObjectRef{Schema: bad, Name: "t"}

	if _, err := m.SampleRows(ctx, ref, Page{}); err == nil {
		t.Error("SampleRows bad schema: want err")
	}
	if _, err := m.CountRows(ctx, ref); err == nil {
		t.Error("CountRows bad schema: want err")
	}
	if err := m.CreateDatabase(ctx, bad, DBOpts{}); err == nil {
		t.Error("CreateDatabase bad name: want err")
	}
	if err := m.CreateTable(ctx, CreateTableDef{Schema: bad, Name: "t", Columns: []ColumnDef{{Name: "id", DataType: "INT"}}}); err == nil {
		t.Error("CreateTable bad schema: want err")
	}
	if err := m.CreateView(ctx, bad, "v", "SELECT 1"); err == nil {
		t.Error("CreateView bad schema: want err")
	}
	if err := m.CreateIndex(ctx, IndexDef{Name: bad, Table: "t", Columns: []string{"c"}}); err == nil {
		t.Error("CreateIndex bad name: want err")
	}
	if err := m.InsertRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "v"}); err == nil {
		t.Error("InsertRow bad column: want err")
	}
	if err := m.InsertRow(ctx, ref, map[string]string{"c": "v"}); err == nil {
		t.Error("InsertRow bad schema: want err")
	}
	if err := m.UpdateRow(ctx, ObjectRef{Name: "t"}, map[string]string{"id": "1"}, map[string]string{bad: "v"}); err == nil {
		t.Error("UpdateRow bad set col: want err")
	}
	if err := m.UpdateRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "1"}, map[string]string{"c": "v"}); err == nil {
		t.Error("UpdateRow bad pk col: want err")
	}
	if err := m.UpdateRow(ctx, ref, map[string]string{"id": "1"}, map[string]string{"c": "v"}); err == nil {
		t.Error("UpdateRow bad schema: want err")
	}
	if err := m.DeleteRow(ctx, ObjectRef{Name: "t"}, map[string]string{bad: "1"}); err == nil {
		t.Error("DeleteRow bad pk col: want err")
	}
	if err := m.DeleteRow(ctx, ref, map[string]string{"id": "1"}); err == nil {
		t.Error("DeleteRow bad schema: want err")
	}
}

func TestOpenDriver_UnknownKind(t *testing.T) {
	if _, err := ParseKind("notadb"); err == nil {
		t.Error("ParseKind(notadb) should error")
	}
}
