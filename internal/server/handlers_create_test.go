package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestCreate_Forms(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	for _, path := range []string{
		"/conn/" + id + "/create/database",
		"/conn/" + id + "/create/table",
		"/conn/" + id + "/create/view",
		"/conn/" + id + "/create/index",
	} {
		st, body := env.get(path)
		if st != 200 || !strings.Contains(body, "modal-panel") {
			t.Errorf("%s → %d body=%q", path, st, body[:intMin(200, len(body))])
		}
	}
}

func TestCreate_Database_Sqlite_SuppressedByDriver(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)
	st, body := env.get("/conn/" + id + "/create/database")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	// Template explicitly rejects CREATE DATABASE for sqlite
	if !strings.Contains(body, "sqlite databases are files") {
		t.Errorf("expected sqlite-specific copy; got %s", body)
	}
}

func TestCreate_Table(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	resp := env.do(http.MethodPost, "/conn/"+id+"/create/table", form(
		"schema", "main",
		"name", "widgets",
		"col_name", "id",
		"col_type", "INTEGER",
		"col_nullable", "0",
		"col_pk", "1",
		"col_default", "",
		"col_name", "label",
		"col_type", "TEXT",
		"col_nullable", "1",
		"col_pk", "",
		"col_default", "",
	))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	d, _ := env.s.pool.Get(t.Context(), c.ID)
	tree, _ := d.ListObjects(t.Context(), "main", "main")
	if len(tree.Tables) != 1 || tree.Tables[0].Name != "widgets" {
		t.Errorf("widgets not created: %+v", tree.Tables)
	}
}

func TestCreate_View_Invalid(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)
	// Syntactically-broken SELECT so sqlite rejects CREATE VIEW immediately
	// (referencing a missing table alone succeeds — sqlite binds views lazily).
	resp := env.do(http.MethodPost, "/conn/"+id+"/create/view",
		form("schema", "main", "name", "v", "sql", "NOT A VALID SELECT!!"))
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestCreate_Index(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	d, _ := env.s.pool.Get(t.Context(), c.ID)
	_, _ = d.Exec(t.Context(), "CREATE TABLE t(id INTEGER PRIMARY KEY, label TEXT);", 10)

	resp := env.do(http.MethodPost, "/conn/"+id+"/create/index", form(
		"name", "t_label",
		"schema", "main",
		"table", "t",
		"columns", "label",
	))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
