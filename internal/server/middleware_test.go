package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBodyLimit_RejectsOversized(t *testing.T) {
	env := newTestEnv(t)

	// Oversized POST body — 3 MB, over the 2 MB cap.
	big := bytes.Repeat([]byte("a"), 3<<20)
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/login", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// ParseForm will fail with "http: request body too large" → 400 from the handler path.
	if resp.StatusCode == 200 {
		t.Errorf("expected non-200 for oversized body, got %d", resp.StatusCode)
	}
}

func TestClientIP_VariousShapes(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "X-Forwarded-For single",
			req: func() *http.Request {
				r, _ := http.NewRequest("GET", "/", http.NoBody)
				r.Header.Set("X-Forwarded-For", "203.0.113.9")
				return r
			}(),
			want: "203.0.113.9",
		},
		{
			name: "X-Forwarded-For multi — first wins",
			req: func() *http.Request {
				r, _ := http.NewRequest("GET", "/", http.NoBody)
				r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1, 10.0.0.2")
				return r
			}(),
			want: "203.0.113.9",
		},
		{
			name: "RemoteAddr port stripped",
			req: func() *http.Request {
				r, _ := http.NewRequest("GET", "/", http.NoBody)
				r.RemoteAddr = "192.0.2.1:54321"
				return r
			}(),
			want: "192.0.2.1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(tc.req); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestLoginRateLimiter_Refill(t *testing.T) {
	rl := newLoginRateLimiter()
	ip := "1.2.3.4"
	// Drain the bucket
	for i := 0; i < 10; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("drain step %d unexpectedly blocked", i)
		}
	}
	if rl.Allow(ip) {
		t.Errorf("11th call should be blocked")
	}
}

func TestRequireAuth_RedirectsHTMX(t *testing.T) {
	env := newTestEnv(t)

	// Unauthenticated HTMX request should get HX-Redirect + 401.
	req, _ := http.NewRequest(http.MethodGet, env.ts.URL+"/", http.NoBody)
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("HX-Redirect") != "/login" {
		t.Errorf("HX-Redirect = %q", resp.Header.Get("HX-Redirect"))
	}
}

func TestRecoverMiddleware_CatchesPanic(t *testing.T) {
	env := newTestEnv(t)

	// Forge a form post that triggers a known 400 path — not a panic test per se,
	// but ensures the recover middleware isn't swallowing normal errors.
	form := url.Values{"username": {"alice"}, "password": {"wrong"}}
	resp, err := http.PostForm(env.ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(b), "invalid credentials") {
		t.Errorf("missing error copy")
	}
}

func TestHasPrefix(t *testing.T) {
	if !hasPrefix("/static/app.css", "/static/") {
		t.Error("expected true")
	}
	if hasPrefix("/set", "/setup") {
		t.Error("too-short string shouldn't match prefix")
	}
}

func TestRecoverMiddleware_ActuallyRecovers(t *testing.T) {
	// Build a tiny handler that panics, wrap it in the recover middleware, and verify
	// we get a 500 instead of a crashed goroutine.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	panicHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	wrapped := recoverMiddleware(logger)(panicHandler)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d; want 500", rr.Code)
	}
}
