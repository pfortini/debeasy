package server

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/config"
	"github.com/pfortini/debeasy/internal/store"
)

// testEnv is a one-stop harness for handler tests.
//
// Every test gets an isolated temp data dir, a fresh Server, and an httptest.Server
// fronting it. By default the harness creates an admin user and logs it in so tests
// can hit authenticated endpoints directly.
type testEnv struct {
	t      *testing.T
	s      *Server
	ts     *httptest.Server
	admin  *store.User
	client *http.Client
	csrf   string
}

// newTestEnv builds a fully-wired server + logged-in admin client.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, true)
}

// newTestEnvWith gives callers a choice of whether to auto-create + log in the admin.
// Use `auth=false` when you're testing the first-run setup or the login flow itself.
func newTestEnvWith(t *testing.T, auth bool) *testEnv {
	t.Helper()

	dir := t.TempDir()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Addr:      ":0",
		DataDir:   dir,
		AppSecret: secret,
	}
	// Ensure we never clash with ambient env/flag state.
	_ = filepath.Join // silence static checker

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	s, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() {
		_ = s.store.Close()
		s.pool.Stop()
	})

	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	env := &testEnv{t: t, s: s, ts: ts}

	jar, _ := cookiejar.New(nil)
	env.client = &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // don't auto-follow; tests assert on redirects
		},
	}

	if !auth {
		return env
	}

	admin, err := s.store.Users.Create(context.Background(), "alice", "password1", "admin")
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	env.admin = admin
	env.login("alice", "password1")
	return env
}

// login posts to /login and captures the session cookie on env.client.
func (e *testEnv) login(username, password string) {
	e.t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	resp, err := e.client.PostForm(e.ts.URL+"/login", form)
	if err != nil {
		e.t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("login expected 303, got %d", resp.StatusCode)
	}
	// Fetch a page to grab the per-session CSRF token rendered into the meta tag.
	e.refreshCSRF()
}

var csrfRe = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

func (e *testEnv) refreshCSRF() {
	e.t.Helper()
	resp, err := e.client.Get(e.ts.URL + "/")
	if err != nil {
		e.t.Fatalf("get home: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := csrfRe.FindSubmatch(body)
	if len(m) < 2 {
		e.t.Fatalf("CSRF token not found in home page")
	}
	e.csrf = string(m[1])
}

// do executes a request with the authenticated client, auto-adding the CSRF header.
func (e *testEnv) do(method, path string, form url.Values) *http.Response {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, e.ts.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if e.csrf != "" && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", e.csrf)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// get is a convenience that returns (status, body).
func (e *testEnv) get(path string) (status int, body string) {
	e.t.Helper()
	resp := e.do(http.MethodGet, path, nil)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// post is a convenience for form posts returning (status, body).
func (e *testEnv) post(path string, form url.Values) (status int, body string) {
	e.t.Helper()
	resp := e.do(http.MethodPost, path, form)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// mustCreateConnection inserts a sqlite connection pointing at a throwaway file so
// connection-scoped endpoints have something to use.
func (e *testEnv) mustCreateConnection(name string) *store.Connection {
	e.t.Helper()
	path := filepath.Join(e.t.TempDir(), name+".sqlite")
	c, err := e.s.store.Connections.Create(context.Background(), &store.Connection{
		Name:     name,
		Kind:     "sqlite",
		Database: path,
	}, e.admin.ID)
	if err != nil {
		e.t.Fatalf("create conn: %v", err)
	}
	return c
}
