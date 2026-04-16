package server

import (
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestConnections_ListEmpty(t *testing.T) {
	env := newTestEnv(t)
	st, body := env.get("/connections")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "no connections yet") {
		t.Errorf("expected empty-state copy")
	}
}

func TestConnections_CreateEditDelete(t *testing.T) {
	env := newTestEnv(t)
	sqlitePath := filepath.Join(t.TempDir(), "target.sqlite")

	form := url.Values{
		"name":     {"my-db"},
		"kind":     {"sqlite"},
		"database": {sqlitePath},
	}
	resp := env.do(http.MethodPost, "/connections", form)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	m := regexp.MustCompile(`/conn/(\d+)`).FindStringSubmatch(loc)
	if len(m) != 2 {
		t.Fatalf("unexpected redirect %q", loc)
	}
	id, _ := strconv.Atoi(m[1])

	// Appears in /connections
	_, body := env.get("/connections")
	if !strings.Contains(body, "my-db") {
		t.Errorf("connection not listed")
	}

	// Edit form renders
	st, editBody := env.get("/connections/" + m[1] + "/edit")
	if st != 200 || !strings.Contains(editBody, "my-db") {
		t.Errorf("edit form missing; status=%d", st)
	}

	// Update: rename
	newForm := url.Values{
		"name":     {"my-db-renamed"},
		"kind":     {"sqlite"},
		"database": {sqlitePath},
	}
	resp = env.do(http.MethodPost, "/connections/"+m[1], newForm)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("update status = %d", resp.StatusCode)
	}
	_, body = env.get("/connections")
	if !strings.Contains(body, "my-db-renamed") {
		t.Errorf("rename not reflected")
	}

	// Delete
	st, _ = env.post("/connections/"+m[1]+"/delete", nil)
	if st != http.StatusSeeOther {
		t.Errorf("delete status = %d", st)
	}
	if _, err := env.s.store.Connections.Get(t.Context(), int64(id)); err == nil {
		t.Errorf("connection should be gone")
	}
}

func TestConnections_Test_Sqlite_Success(t *testing.T) {
	env := newTestEnv(t)
	form := url.Values{
		"name":     {"ephemeral"},
		"kind":     {"sqlite"},
		"database": {filepath.Join(t.TempDir(), "x.sqlite")},
	}
	st, body := env.post("/connections/test", form)
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "test-ok") {
		t.Errorf("expected success chip: %s", body)
	}
}

func TestConnections_Test_InvalidKind(t *testing.T) {
	env := newTestEnv(t)
	form := url.Values{
		"name": {"ephemeral"},
		"kind": {"oracle"},
	}
	st, body := env.post("/connections/test", form)
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "test-fail") {
		t.Errorf("expected fail chip: %s", body)
	}
}

func TestConnections_Create_RejectsInvalidKind(t *testing.T) {
	env := newTestEnv(t)
	form := url.Values{"name": {"x"}, "kind": {"oracle"}}
	st, body := env.post("/connections", form)
	if st != 400 {
		t.Errorf("status = %d", st)
	}
	if !strings.Contains(body, "invalid kind") {
		t.Errorf("missing error text: %s", body)
	}
}

func TestConnections_NewFormRenders(t *testing.T) {
	env := newTestEnv(t)
	st, body := env.get("/connections/new")
	if st != 200 {
		t.Fatalf("status = %d", st)
	}
	if !strings.Contains(body, "new connection") {
		t.Errorf("expected form header")
	}
}
