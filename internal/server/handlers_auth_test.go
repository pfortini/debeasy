package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSetup_FirstRunRedirect(t *testing.T) {
	env := newTestEnvWith(t, false) // no admin yet
	resp, err := env.client.Get(env.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 to /setup, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/setup" {
		t.Errorf("Location = %q", got)
	}
}

func TestSetup_CreatesAdminAndLogsIn(t *testing.T) {
	env := newTestEnvWith(t, false)

	// GET /setup renders
	resp, _ := env.client.Get(env.ts.URL + "/setup")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /setup = %d", resp.StatusCode)
	}

	// POST /setup creates the admin and starts a session
	form := url.Values{
		"username": {"alice"},
		"password": {"password1"},
		"confirm":  {"password1"},
	}
	resp, _ = env.client.PostForm(env.ts.URL+"/setup", form)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /setup = %d", resp.StatusCode)
	}

	// GET / now renders the home page (not /setup)
	resp2, err := env.client.Get(env.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	resp = resp2
	if resp.StatusCode != 200 {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
}

func TestSetup_PasswordsMustMatch(t *testing.T) {
	env := newTestEnvWith(t, false)
	form := url.Values{
		"username": {"alice"},
		"password": {"password1"},
		"confirm":  {"mismatch2"},
	}
	resp, err := env.client.PostForm(env.ts.URL+"/setup", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestSetup_AlreadyCompleted(t *testing.T) {
	env := newTestEnv(t) // admin already exists

	// Raw client (no cookies) so we're not treated as logged in.
	form := url.Values{"username": {"bob"}, "password": {"password2"}, "confirm": {"password2"}}
	resp, err := http.PostForm(env.ts.URL+"/setup", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLogin_FormRenders(t *testing.T) {
	env := newTestEnv(t)

	// Fresh client with no session — GET /login should render the form.
	resp, err := http.Get(env.ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "sign in") {
		t.Errorf("login form not rendered")
	}

	// An already-logged-in client should be redirected off /login.
	resp, errX := env.client.Get(env.ts.URL + "/login")
	if errX != nil {
		t.Fatal(errX)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("authed GET /login should redirect, got %d", resp.StatusCode)
	}
}

func TestLogin_BadCreds(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{"username": {"alice"}, "password": {"wrong"}}
	resp, err := http.PostForm(env.ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d", resp.StatusCode)
	}
}

func TestLogin_DisabledUser(t *testing.T) {
	env := newTestEnv(t)
	_ = env.s.store.Users.SetDisabled(t.Context(), env.admin.ID, true)

	form := url.Values{"username": {"alice"}, "password": {"password1"}}
	resp, err := http.PostForm(env.ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "disabled") {
		t.Errorf("expected disabled hint in body")
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	env := newTestEnv(t)

	// Logged in → / returns 200
	if st, _ := env.get("/"); st != 200 {
		t.Fatalf("authed GET / = %d", st)
	}

	// Logout
	if st, _ := env.post("/logout", nil); st != http.StatusSeeOther {
		t.Fatalf("logout status = %d", st)
	}

	// / now redirects to /login
	resp, err := env.client.Get(env.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("unauth status=%d loc=%s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestRateLimit_BlocksAfterMax used to POST /login 12× rapidly to trigger a 429.
// Under `-race`, bcrypt is slow enough that the token bucket refills between
// attempts, making the test flaky. The pure-unit version lives in middleware_test.go
// (TestLoginRateLimiter_Refill) and covers the same logic deterministically.

func TestCSRF_RejectsMissingToken(t *testing.T) {
	env := newTestEnv(t)
	// build a POST without the CSRF header or body field; should be 403
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/logout", http.NoBody)
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 missing-CSRF, got %d", resp.StatusCode)
	}
}
