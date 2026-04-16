package dbx

import (
	"reflect"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"SELECT 1", []string{"SELECT 1"}},
		{"SELECT 1; SELECT 2;", []string{"SELECT 1", " SELECT 2"}},
		{"SELECT ';'", []string{"SELECT ';'"}},
		{`SELECT ";"; SELECT 2`, []string{`SELECT ";"`, " SELECT 2"}},
		// splitSQL preserves whitespace-only segments between semicolons; the caller
		// (runStatements) is the one that trims and drops empties.
		{"  ;  ; ", []string{"  ", "  "}},
	}
	for _, tc := range tests {
		got := splitSQL(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitSQL(%q) = %#v; want %#v", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeQuery(t *testing.T) {
	queries := []string{
		"SELECT 1",
		"  select 1",
		"with q as (select 1) select * from q",
		"SHOW TABLES",
		"EXPLAIN SELECT 1",
		"PRAGMA table_info(t)",
		"VALUES (1)",
		"(SELECT 1)",
	}
	for _, q := range queries {
		if !looksLikeQuery(q) {
			t.Errorf("looksLikeQuery(%q) = false; want true", q)
		}
	}
	notQueries := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a=1",
		"CREATE TABLE t(id int)",
		"DELETE FROM t",
		"",
	}
	for _, q := range notQueries {
		if looksLikeQuery(q) {
			t.Errorf("looksLikeQuery(%q) = true; want false", q)
		}
	}
}

func TestNormaliseValue(t *testing.T) {
	if got := normaliseValue([]byte("hi")); got != "hi" {
		t.Errorf("got %v", got)
	}
	if got := normaliseValue(int64(42)); got != int64(42) {
		t.Errorf("got %v", got)
	}
	if got := normaliseValue(nil); got != nil {
		t.Errorf("got %v", got)
	}
}
