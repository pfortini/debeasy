package server

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// form is a tiny convenience for `url.Values` with a sequence of alternating k,v args.
func form(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Add(kv[i], kv[i+1])
	}
	return v
}

func TestQuery_MultiStatement(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	st, body := env.post("/conn/"+strconv.FormatInt(c.ID, 10)+"/query",
		form("sql", "CREATE TABLE t(id INTEGER); INSERT INTO t(id) VALUES (1),(2); SELECT * FROM t ORDER BY id;", "max_rows", "100"))
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	for _, want := range []string{"rows", "affected", "CREATE TABLE", "INSERT INTO", "SELECT"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestQuery_Empty(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	st, body := env.post("/conn/"+strconv.FormatInt(c.ID, 10)+"/query", form("sql", "   "))
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "empty query") {
		t.Errorf("missing error: %s", body)
	}
}

func TestQuery_ErrorRecorded(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	st, body := env.post("/conn/"+strconv.FormatInt(c.ID, 10)+"/query", form("sql", "SELECT * FROM nope"))
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "error") {
		t.Errorf("expected error in body")
	}

	// History got the failed statement
	entries, err := env.s.store.History.Recent(t.Context(), env.admin.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("no history entries recorded")
	}
	if !entries[0].Error.Valid {
		t.Errorf("top history entry should have Error set: %+v", entries[0])
	}
}
