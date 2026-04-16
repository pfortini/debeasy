package dbx

import (
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		in      string
		want    string
		wantErr bool
	}{
		{"pg plain", KindPostgres, "users", `"users"`, false},
		{"pg mixed case kept", KindPostgres, "Users", `"Users"`, false},
		{"pg rejects embedded dquote", KindPostgres, `u"ers`, "", true},
		{"pg empty", KindPostgres, "", "", true},
		{"mysql backticks", KindMySQL, "users", "`users`", false},
		{"mysql rejects embedded backtick", KindMySQL, "u`ers", "", true},
		{"sqlite uses dquote like pg", KindSQLite, "t", `"t"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := quoteIdent(tc.kind, tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestMustQuoteIdent_PanicsOnBadInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	mustQuoteIdent(KindPostgres, `bad"name`)
}

func TestMustQuoteIdent_HappyPath(t *testing.T) {
	if got := mustQuoteIdent(KindMySQL, "ok"); got != "`ok`" {
		t.Fatalf("got %q", got)
	}
}

func TestQualified(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		schema  string
		obj     string
		want    string
		wantErr bool
	}{
		{"pg with schema", KindPostgres, "public", "users", `"public"."users"`, false},
		{"pg no schema", KindPostgres, "", "users", `"users"`, false},
		{"mysql with schema", KindMySQL, "app", "t", "`app`.`t`", false},
		{"bad schema", KindPostgres, `bad"sch`, "t", "", true},
		{"bad name", KindPostgres, "public", `bad"n`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := qualified(tc.kind, tc.schema, tc.obj)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	cases := map[string]string{
		"":         "''",
		"simple":   "'simple'",
		"with '":   "'with '''",
		"O'Neil":   "'O''Neil'",
		"it's\nOK": "'it''s\nOK'",
	}
	for in, want := range cases {
		if got := quoteLiteral(in); got != want {
			t.Errorf("quoteLiteral(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestParseKind(t *testing.T) {
	aliases := map[string]Kind{
		"postgres":   KindPostgres,
		"pg":         KindPostgres,
		"postgresql": KindPostgres,
		"mysql":      KindMySQL,
		"mariadb":    KindMySQL,
		"sqlite":     KindSQLite,
		"sqlite3":    KindSQLite,
	}
	for alias, want := range aliases {
		got, err := ParseKind(alias)
		if err != nil || got != want {
			t.Errorf("ParseKind(%q) = (%q,%v); want (%q,nil)", alias, got, err, want)
		}
	}
	if _, err := ParseKind("oracle"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected unknown-kind error; got %v", err)
	}
}
