//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/dbx"
	"github.com/pfortini/debeasy/internal/store"
)

// newPGConnection returns a test-scoped PG connection wired to the dev container,
// or skips the test if PG isn't reachable.
func newPGConnection(t *testing.T, env *testEnv) (*store.Connection, dbx.Driver) {
	t.Helper()
	c, err := env.s.store.Connections.Create(t.Context(), &store.Connection{
		Name:     "pg-live",
		Kind:     "postgres",
		Host:     envDefault("DEBEASY_TEST_PG_HOST", "127.0.0.1"),
		Port:     55432,
		Username: envDefault("DEBEASY_TEST_PG_USER", "debeasy"),
		Password: envDefault("DEBEASY_TEST_PG_PASSWORD", "debeasy"),
		Database: envDefault("DEBEASY_TEST_PG_DB", "postgres"),
		SSLMode:  "disable",
	}, env.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := env.s.pool.Get(t.Context(), c.ID)
	if err != nil {
		t.Skipf("PG not reachable: %v", err)
	}
	return c, d
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestCreateDB_Postgres_HappyPath(t *testing.T) {
	env := newTestEnv(t)
	c, d := newPGConnection(t, env)
	id := strconv.FormatInt(c.ID, 10)

	name := "debeasy_createdb_e2e"
	_, _ = d.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name, 10)
	t.Cleanup(func() {
		_, _ = d.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name, 10)
	})

	st, _ := env.post("/conn/"+id+"/create/database",
		url.Values{"name": {name}, "encoding": {"UTF8"}})
	if st != http.StatusNoContent {
		t.Fatalf("status = %d; want 204", st)
	}
}

func TestCreateTable_Postgres_HappyPath(t *testing.T) {
	env := newTestEnv(t)
	c, d := newPGConnection(t, env)
	id := strconv.FormatInt(c.ID, 10)

	schema := "debeasy_e2e_table"
	mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	mustExec(t, d, "CREATE SCHEMA "+schema)
	t.Cleanup(func() { mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })

	form := url.Values{
		"schema":       {schema},
		"name":         {"widgets"},
		"col_name":     {"id", "label"},
		"col_type":     {"INT", "TEXT"},
		"col_nullable": {"0", "1"},
		"col_pk":       {"1", ""},
		"col_default":  {"", ""},
	}
	st, body := env.post("/conn/"+id+"/create/table", form)
	if st != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", st, truncate(body))
	}
}

func TestCreateView_Postgres_HappyPath(t *testing.T) {
	env := newTestEnv(t)
	c, d := newPGConnection(t, env)
	id := strconv.FormatInt(c.ID, 10)

	schema := "debeasy_e2e_view"
	mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	mustExec(t, d, "CREATE SCHEMA "+schema)
	mustExec(t, d, "CREATE TABLE "+schema+".t(id INT)")
	t.Cleanup(func() { mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })

	st, body := env.post("/conn/"+id+"/create/view", url.Values{
		"schema": {schema},
		"name":   {"v"},
		"sql":    {"SELECT id FROM " + schema + ".t"},
	})
	if st != http.StatusNoContent {
		t.Errorf("status = %d body=%s", st, truncate(body))
	}
}

func TestCreateIndex_Postgres_HappyPath(t *testing.T) {
	env := newTestEnv(t)
	c, d := newPGConnection(t, env)
	id := strconv.FormatInt(c.ID, 10)

	schema := "debeasy_e2e_idx"
	mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	mustExec(t, d, "CREATE SCHEMA "+schema)
	mustExec(t, d, "CREATE TABLE "+schema+".t(id INT, label TEXT)")
	t.Cleanup(func() { mustExec(t, d, "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })

	st, _ := env.post("/conn/"+id+"/create/index", url.Values{
		"name":    {"label_idx"},
		"schema":  {schema},
		"table":   {"t"},
		"columns": {"label"},
		"method":  {"btree"},
	})
	if st != http.StatusNoContent {
		t.Errorf("status = %d", st)
	}
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return strings.TrimSpace(s)
}

// mustExec runs a single SQL statement against the target and fails the test
// immediately if any of the resulting Result blocks carries an error. Needed
// because d.Exec never returns a Go error — failures live inside Result.Err.
func mustExec(t *testing.T, d dbx.Driver, sql string) {
	t.Helper()
	results, err := d.Exec(context.Background(), sql, 10)
	if err != nil {
		t.Fatalf("Exec(%q): %v", sql, err)
	}
	for _, r := range results {
		if r.Err != "" {
			t.Fatalf("Exec(%q): %s", sql, r.Err)
		}
	}
}
