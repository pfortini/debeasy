package dbx

import "testing"

func TestDefaultSchema(t *testing.T) {
	schemas := func(names ...string) []Schema {
		out := make([]Schema, 0, len(names))
		for _, n := range names {
			out = append(out, Schema{Name: n})
		}
		return out
	}

	tests := []struct {
		name   string
		kind   Kind
		connDB string
		in     []Schema
		want   string
	}{
		{"pg prefers public over alphabetical first", KindPostgres, "app", schemas("drizzle", "public", "zeta"), "public"},
		{"pg falls back to first when no public", KindPostgres, "app", schemas("drizzle", "zeta"), "drizzle"},
		{"pg ignores connDB", KindPostgres, "app", schemas("app", "public"), "public"},
		{"mysql prefers connDB over alphabetical first", KindMySQL, "app", schemas("aardvark", "app", "zzz"), "app"},
		{"mysql falls back when connDB missing", KindMySQL, "app", schemas("aardvark", "zzz"), "aardvark"},
		{"mysql empty connDB falls back", KindMySQL, "", schemas("aardvark", "zzz"), "aardvark"},
		{"sqlite returns main", KindSQLite, "", schemas("main"), "main"},
		{"empty schemas returns empty", KindPostgres, "app", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultSchema(tc.kind, tc.connDB, tc.in)
			if got != tc.want {
				t.Errorf("DefaultSchema(%s, %q, %v) = %q, want %q", tc.kind, tc.connDB, tc.in, got, tc.want)
			}
		})
	}
}
