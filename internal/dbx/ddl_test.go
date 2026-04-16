package dbx

import (
	"strings"
	"testing"
)

func TestBuildCreateTable_Postgres(t *testing.T) {
	def := CreateTableDef{
		Schema: "public",
		Name:   "users",
		Columns: []ColumnDef{
			{Name: "id", DataType: "SERIAL", IsPK: true},
			{Name: "email", DataType: "TEXT", Nullable: true},
			{Name: "created", DataType: "TIMESTAMPTZ", Default: "now()"},
		},
	}
	got, err := buildCreateTable(KindPostgres, def)
	if err != nil {
		t.Fatalf("buildCreateTable: %v", err)
	}
	for _, frag := range []string{
		`CREATE TABLE "public"."users"`,
		`"id" SERIAL NOT NULL`,
		`"email" TEXT`,
		`"created" TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`PRIMARY KEY ("id")`,
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing %q in:\n%s", frag, got)
		}
	}
}

func TestBuildCreateTable_MySQL(t *testing.T) {
	def := CreateTableDef{
		Schema:      "app",
		Name:        "t",
		IfNotExists: true,
		Columns: []ColumnDef{
			{Name: "id", DataType: "INT AUTO_INCREMENT", IsPK: true},
			{Name: "name", DataType: "VARCHAR(64)"},
		},
	}
	got, err := buildCreateTable(KindMySQL, def)
	if err != nil {
		t.Fatalf("buildCreateTable: %v", err)
	}
	if !strings.Contains(got, "CREATE TABLE IF NOT EXISTS `app`.`t`") {
		t.Errorf("missing IF NOT EXISTS + backticked schema:\n%s", got)
	}
	if !strings.Contains(got, "`id` INT AUTO_INCREMENT NOT NULL") {
		t.Errorf("id col missing:\n%s", got)
	}
	if !strings.Contains(got, "PRIMARY KEY (`id`)") {
		t.Errorf("PK missing:\n%s", got)
	}
}

func TestBuildCreateTable_Errors(t *testing.T) {
	_, err := buildCreateTable(KindPostgres, CreateTableDef{Schema: "s", Name: "t"})
	if err == nil || !strings.Contains(err.Error(), "at least one column") {
		t.Errorf("want empty-column error; got %v", err)
	}
	_, err = buildCreateTable(KindPostgres, CreateTableDef{
		Schema: "s", Name: "t",
		Columns: []ColumnDef{{Name: "bad", DataType: ""}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing data type") {
		t.Errorf("want missing-type error; got %v", err)
	}
	_, err = buildCreateTable(KindMySQL, CreateTableDef{
		Schema: "`bad", Name: "t",
		Columns: []ColumnDef{{Name: "c", DataType: "INT"}},
	})
	if err == nil {
		t.Errorf("want identifier-quoting error for bad schema")
	}
	// Bad column name
	_, err = buildCreateTable(KindPostgres, CreateTableDef{
		Schema: "s", Name: "t",
		Columns: []ColumnDef{{Name: `bad"col`, DataType: "INT"}},
	})
	if err == nil {
		t.Error("expected column-name quoting error")
	}
}

func TestBuildCreateIndex_Errors_Extended(t *testing.T) {
	// Bad index name
	_, err := buildCreateIndex(KindPostgres, IndexDef{
		Name: `bad"idx`, Table: "t", Columns: []string{"c"},
	})
	if err == nil {
		t.Error("expected index-name quoting error")
	}
	// Bad table name
	_, err = buildCreateIndex(KindPostgres, IndexDef{
		Name: "idx", Table: `bad"tbl`, Columns: []string{"c"},
	})
	if err == nil {
		t.Error("expected table-name quoting error")
	}
	// Bad column name
	_, err = buildCreateIndex(KindPostgres, IndexDef{
		Name: "idx", Table: "t", Columns: []string{`bad"col`},
	})
	if err == nil {
		t.Error("expected column-name quoting error")
	}
}

func TestSynthesisedCreateTable_HandlesBadIdentifier(t *testing.T) {
	// If the ref contains a bad identifier, the function returns a "-- <err>" comment
	// instead of crashing.
	got := synthesisedCreateTable(KindPostgres, ObjectRef{Schema: `bad"x`, Name: "t"}, nil, nil, nil)
	if !strings.HasPrefix(got, "-- ") {
		t.Errorf("expected error comment; got %q", got)
	}
}

func TestBuildCreateIndex(t *testing.T) {
	def := IndexDef{
		Name: "u_email", Schema: "public", Table: "users",
		Columns: []string{"email"}, Unique: true, Method: "btree",
	}
	got, err := buildCreateIndex(KindPostgres, def)
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE UNIQUE INDEX "u_email" ON "public"."users" USING btree ("email")`
	if got != want {
		t.Errorf("\ngot:  %s\nwant: %s", got, want)
	}

	// Non-PG drivers ignore Method.
	def.Method = "hash"
	got, err = buildCreateIndex(KindMySQL, def)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "USING") {
		t.Errorf("mysql shouldn't get USING clause: %s", got)
	}
}

func TestBuildCreateIndex_Errors(t *testing.T) {
	_, err := buildCreateIndex(KindPostgres, IndexDef{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "column") {
		t.Errorf("want columns-required error; got %v", err)
	}
}

func TestSynthesisedCreateTable_IncludesFKsAndIndexes(t *testing.T) {
	ref := ObjectRef{Schema: "public", Name: "orders"}
	cols := []Column{
		{Name: "id", DataType: "integer", IsPK: true},
		{Name: "user_id", DataType: "integer"},
	}
	fks := []ForeignKey{{
		Name: "fk_u", Columns: []string{"user_id"},
		RefSchema: "public", RefTable: "users", RefColumns: []string{"id"},
	}}
	idx := []IndexInfo{
		{Name: "orders_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "orders_user_idx", Columns: []string{"user_id"}},
	}
	got := synthesisedCreateTable(KindPostgres, ref, cols, idx, fks)
	for _, frag := range []string{
		`CREATE TABLE "public"."orders"`,
		`"id" integer NOT NULL`,
		`FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id")`,
		`CREATE INDEX "orders_user_idx"`,
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing fragment %q in:\n%s", frag, got)
		}
	}
	if strings.Contains(got, `CREATE INDEX "orders_pkey"`) {
		t.Errorf("primary-key index should be skipped")
	}
}
