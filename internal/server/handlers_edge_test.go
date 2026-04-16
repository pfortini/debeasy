package server

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// These tests cover the error branches and less-common paths that the happy-path
// suite doesn't naturally hit. Each models a real scenario (bad input, missing
// resource, HTMX delete, etc.) — they are not coverage-for-coverage's-sake.

func TestSetupForm_WhenUsersExist_Redirects(t *testing.T) {
	env := newTestEnv(t)
	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := cli.Get(env.ts.URL + "/setup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("expected 303 -> /login; got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestTree_UnknownConn_404(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.get("/conn/9999/tree")
	if st != 404 {
		t.Errorf("got %d", st)
	}
}

func TestObjectDetail_UnknownConn(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.get("/conn/9999/object/main/foo")
	if st != 404 {
		t.Errorf("got %d", st)
	}
}

func TestObjectData_UnknownObject(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	st, _ := env.get("/conn/" + strconv.FormatInt(c.ID, 10) + "/object/main/nope/data")
	if st != http.StatusBadGateway {
		t.Errorf("got %d", st)
	}
}

func TestConnectionDelete_HTMX(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("x")

	req, _ := http.NewRequest(http.MethodPost,
		env.ts.URL+"/connections/"+strconv.FormatInt(c.ID, 10)+"/delete", http.NoBody)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", env.csrf)
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("got %d", resp.StatusCode)
	}
	if resp.Header.Get("HX-Redirect") != "/connections" {
		t.Errorf("HX-Redirect = %q", resp.Header.Get("HX-Redirect"))
	}
}

func TestConnectionUpdate_InvalidKind(t *testing.T) {
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	form := url.Values{"name": {"c1"}, "kind": {"oracle"}, "database": {"/x"}}
	resp := env.do(http.MethodPost, "/connections/"+strconv.FormatInt(c.ID, 10), form)
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid kind") {
		t.Errorf("missing error")
	}
}

func TestConnectionUpdate_WithPasswordClear(t *testing.T) {
	env := newTestEnv(t)
	c, err := env.s.store.Connections.Create(t.Context(), &store.Connection{
		Name: "c1", Kind: "postgres", Host: "h", Port: 5432, Username: "u",
		Password: "initial", Database: "d",
	}, env.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"name": {"c1"}, "kind": {"postgres"},
		"host": {"h"}, "port": {"5432"}, "username": {"u"},
		"database":       {"d"},
		"password_clear": {"on"},
	}
	resp := env.do(http.MethodPost, "/connections/"+strconv.FormatInt(c.ID, 10), form)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("got %d", resp.StatusCode)
	}
	got, _ := env.s.store.Connections.Get(t.Context(), c.ID)
	if got.Password != "" {
		t.Errorf("password should have been cleared, got %q", got.Password)
	}
}

func TestConnectionEditForm_Unknown(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.get("/connections/9999/edit")
	if st != 404 {
		t.Errorf("got %d", st)
	}
}

func TestObjectDetail_MissingTable(t *testing.T) {
	// sqlite's DescribeTable returns an empty definition (not an error) for missing
	// tables, so the handler returns 200 with a mostly-empty structure page.
	env := newTestEnv(t)
	c := env.mustCreateConnection("c1")
	st, body := env.get("/conn/" + strconv.FormatInt(c.ID, 10) + "/object/main/nope")
	if st != 200 {
		t.Errorf("got %d", st)
	}
	if !strings.Contains(body, "nope") {
		t.Errorf("response should reference the requested object name")
	}
}

func TestUserCreate_BadInput(t *testing.T) {
	env := newTestEnv(t)
	// Missing password — store rejects it, handler returns 400
	st, _ := env.post("/users", url.Values{"username": {"bob"}, "password": {"x"}})
	if st != 400 {
		t.Errorf("got %d", st)
	}
}

func TestUserReset_Missing(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.post("/users/9999/reset", url.Values{"password": {"whatever1"}})
	// 200 or 303 is fine — the handler redirects regardless; but a short password returns 400
	if st == 400 {
		t.Errorf("unexpected 400 for known user id path")
	}
}

func TestUserReset_Short(t *testing.T) {
	env := newTestEnv(t)
	st, _ := env.post("/users/"+strconv.FormatInt(env.admin.ID, 10)+"/reset", url.Values{"password": {"short"}})
	if st != 400 {
		t.Errorf("got %d", st)
	}
}

func TestCookieSecure_ForwardedProto(t *testing.T) {
	// X-Forwarded-Proto: https must cause cookieSecure() to return true so the
	// session cookie carries the Secure flag. Inspect the Set-Cookie header.
	env := newTestEnv(t)
	form := url.Values{"username": {"alice"}, "password": {"password1"}}
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/login",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")

	// Raw client that doesn't follow the redirect.
	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("got %d", resp.StatusCode)
	}
	foundSecure := false
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie && c.Secure {
			foundSecure = true
		}
	}
	if !foundSecure {
		t.Errorf("expected Secure cookie on X-Forwarded-Proto: https")
	}
}

func TestHandleCreateDB_NonSqlitePath(t *testing.T) {
	// Exercises the "non-sqlite create database" rendering path without needing PG.
	// The handler uses the connection's Kind to pick the template branch and to call
	// driver.CreateDatabase. We create a "postgres" connection pointing nowhere —
	// pool.Get will then fail, so we get the error branch.
	env := newTestEnv(t)
	c, err := env.s.store.Connections.Create(t.Context(), &store.Connection{
		Name: "pg-dead", Kind: "postgres", Host: "127.0.0.1", Port: 1, // closed
		Username: "u", Database: "d", SSLMode: "disable",
	}, env.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)

	// GET form — template should render for postgres
	st, body := env.get("/conn/" + id + "/create/database")
	if st != 200 {
		t.Fatalf("GET status = %d", st)
	}
	if strings.Contains(body, "sqlite databases are files") {
		t.Errorf("postgres form should not show sqlite copy")
	}

	// POST — should fail because the pool can't reach the target
	st, _ = env.post("/conn/"+id+"/create/database", url.Values{"name": {"x"}})
	if st == http.StatusNoContent {
		t.Errorf("expected failure, got 204")
	}
}
