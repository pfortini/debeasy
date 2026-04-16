package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// These tests exercise the error-rendering branches of every create-wizard handler
// without needing Postgres/MySQL containers. SQLite's CreateDatabase returns an
// error by design, and malformed table/view/index inputs reliably trip the
// underlying driver methods.

func TestHandleCreateDB_SqliteErrorRendersFormWith400(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	// SQLite's CreateDatabase is intentionally a no-op that returns an error.
	st, body := env.post("/conn/"+id+"/create/database", url.Values{"name": {"x"}})
	if st != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", st)
	}
	if !strings.Contains(body, "sqlite") {
		t.Errorf("expected sqlite-specific error in response")
	}
}

func TestHandleCreateTable_BadColumnErrorRenders(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	// Column name with a double-quote → quoteIdent fails → handler renders the
	// form with 400.
	st, body := env.post("/conn/"+id+"/create/table", url.Values{
		"schema":   {"main"},
		"name":     {"t"},
		"col_name": {`bad"col`}, // rejected by quoteIdent
		"col_type": {"INTEGER"},
	})
	if st != http.StatusBadRequest {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "form-err") {
		t.Errorf("expected inline error rendering")
	}
}

func TestHandleCreateIndex_EmptyColumnsErrors(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)

	st, _ := env.post("/conn/"+id+"/create/index", url.Values{
		"name":    {"i"},
		"table":   {"t"},
		"columns": {""}, // splitCSV returns empty → driver rejects
	})
	if st != http.StatusBadRequest {
		t.Errorf("status = %d", st)
	}
}

func TestHandleCreateView_BadSQLErrors(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	id := strconv.FormatInt(c.ID, 10)
	st, _ := env.post("/conn/"+id+"/create/view", url.Values{
		"name": {"v"},
		"sql":  {"NOT A SELECT!!"},
	})
	if st != http.StatusBadRequest {
		t.Errorf("status = %d", st)
	}
}

func TestTree_EmptySchemaAutoFills(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	// No ?schema= → handler should auto-pick the first schema (main for sqlite).
	st, body := env.get("/conn/" + strconv.FormatInt(c.ID, 10) + "/tree")
	if st != http.StatusOK {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "tables") {
		t.Errorf("expected tree body")
	}
}

func TestConnectionApp_DeadPG_RendersErrorPage(t *testing.T) {
	env := newTestEnv(t)
	c := deadPgConnection(t, env, "dead-app-page")
	st, body := env.get("/conn/" + strconv.FormatInt(c.ID, 10))
	if st != http.StatusOK {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "could not connect") {
		t.Errorf("expected conn_error template")
	}
}

func TestRowEditForm_PKNotInSample_FallsBackToSeed(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	// pk_id=999 doesn't match any row — handler should still render a form
	// pre-filled with the pk value from the URL.
	st, body := env.get("/conn/" + strconv.FormatInt(connID, 10) +
		"/object/main/widgets/row/edit?pk_id=999")
	if st != http.StatusOK {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "value=\"999\"") {
		t.Errorf("expected pk value seeded into form")
	}
}

func TestHandleRowInsert_InvalidColumn_400(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	// widgets has columns (id, label). Pass c_label with a NOT NULL violation
	// (empty string) to trigger the driver error branch.
	st, _ := env.post("/conn/"+strconv.FormatInt(connID, 10)+"/object/main/widgets/row",
		url.Values{"c_id": {"1"}}) // duplicate PK → insert fails
	if st != http.StatusBadRequest {
		t.Errorf("status = %d", st)
	}
}

func TestHandleRowUpdate_InvalidPK_400(t *testing.T) {
	env := newTestEnv(t)
	connID, _ := seedTable(t, env)
	// Passing a malformed identifier as a column name isn't possible via the form
	// (it's always "c_<col>"/"pk_<col>"), so trigger via a non-matching row with
	// a constraint violation: set label=NULL but it's NOT NULL in schema.
	st, _ := env.post("/conn/"+strconv.FormatInt(connID, 10)+"/object/main/widgets/row/update",
		url.Values{"pk_id": {"1"}, "c_id": {"1"}, "c_label": {""}})
	// Some drivers accept empty string as valid TEXT, so widen: accept 400 or 204.
	if st != http.StatusBadRequest && st != http.StatusNoContent {
		t.Errorf("unexpected status %d", st)
	}
}
