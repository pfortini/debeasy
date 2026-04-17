package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		latest, cur string
		want        bool
	}{
		{"strictly newer patch", "v1.0.1", "v1.0.0", true},
		{"strictly newer minor", "v1.1.0", "v1.0.9", true},
		{"strictly newer major", "v2.0.0", "v1.99.99", true},
		{"equal", "v1.0.0", "v1.0.0", false},
		{"older", "v1.0.0", "v1.0.1", false},
		{"no v prefix still works", "1.2.3", "1.2.2", true},
		{"current dev → never upgrade", "v1.0.0", "dev", false},
		{"latest malformed", "v1", "v1.0.0", false},
		{"current malformed", "v1.0.0", "nonsense", false},
		{"pre-release suffix stripped", "v1.2.3-rc1", "v1.2.2", true},
		{"empty strings", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Newer(tc.latest, tc.cur); got != tc.want {
				t.Fatalf("Newer(%q,%q)=%v want %v", tc.latest, tc.cur, got, tc.want)
			}
		})
	}
}

func TestCheck_NewerRelease(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v1.2.3",
			"html_url":     "https://example.com/releases/v1.2.3",
			"published_at": "2026-04-17T00:00:00Z",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := New("owner/repo", "v1.0.0", dir, srv.Client()).WithAPIBaseURL(srv.URL)

	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.Tag != "v1.2.3" {
		t.Fatalf("got %+v, want tag v1.2.3", rel)
	}
	if got := c.Latest(); got == nil || got.Tag != "v1.2.3" {
		t.Fatalf("Latest()=%+v", got)
	}
	// Cache file should exist and round-trip.
	b, err := os.ReadFile(filepath.Join(dir, "update_check.json"))
	if err != nil {
		t.Fatalf("cache read: %v", err)
	}
	var cached Release
	if err := json.Unmarshal(b, &cached); err != nil {
		t.Fatalf("cache decode: %v", err)
	}
	if cached.Tag != "v1.2.3" || cached.URL == "" {
		t.Fatalf("bad cache: %+v", cached)
	}
}

func TestCheck_UpToDateClearsCache(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.0.0"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Pre-seed a stale cache file claiming v1.0.0 is available.
	staleCache := Release{Tag: "v1.0.0", URL: "https://example.com/old", CheckedAt: time.Now().UTC()}
	b, _ := json.Marshal(staleCache)
	if err := os.WriteFile(filepath.Join(dir, "update_check.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	c := New("owner/repo", "v1.0.0", dir, srv.Client()).WithAPIBaseURL(srv.URL)
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel != nil {
		t.Fatalf("expected no upgrade, got %+v", rel)
	}
	if c.Latest() != nil {
		t.Fatalf("Latest should be nil after up-to-date check")
	}
	if _, err := os.Stat(filepath.Join(dir, "update_check.json")); !os.IsNotExist(err) {
		t.Fatalf("cache file should be removed, got err=%v", err)
	}
}

func TestCheck_IgnoresDraftsAndPrereleases(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0", "prerelease": true})
	}))
	defer srv.Close()

	c := New("owner/repo", "v1.0.0", "", srv.Client()).WithAPIBaseURL(srv.URL)
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel != nil {
		t.Fatalf("prerelease should be ignored, got %+v", rel)
	}
}

func TestCheck_ReturnsCacheWriteError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3",
			"html_url": "https://example.com/v1.2.3",
		})
	}))
	defer srv.Close()

	// Point the cache at a path under a directory that does not exist —
	// os.WriteFile will fail with ENOENT, which Check should surface.
	bogusDataDir := filepath.Join(t.TempDir(), "does", "not", "exist")
	c := New("owner/repo", "v1.0.0", bogusDataDir, srv.Client()).WithAPIBaseURL(srv.URL)

	rel, err := c.Check(context.Background())
	if err == nil {
		t.Fatalf("expected cache write error, got nil")
	}
	// In-memory state must still reflect the new release so the banner works.
	if rel == nil || rel.Tag != "v1.2.3" {
		t.Fatalf("release should be returned even when cache write fails; got %+v", rel)
	}
	if got := c.Latest(); got == nil || got.Tag != "v1.2.3" {
		t.Fatalf("Latest should be primed; got %+v", got)
	}
}

func TestCheck_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New("owner/repo", "v1.0.0", "", srv.Client()).WithAPIBaseURL(srv.URL)
	if _, err := c.Check(context.Background()); err == nil {
		t.Fatalf("expected error on 429")
	}
}

func TestLoad_PrimesFromCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cached := Release{Tag: "v1.5.0", URL: "https://example.com/v1.5.0", CheckedAt: time.Now().UTC()}
	b, _ := json.Marshal(cached)
	if err := os.WriteFile(filepath.Join(dir, "update_check.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := New("owner/repo", "v1.0.0", dir, nil)
	if err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := c.Latest(); got == nil || got.Tag != "v1.5.0" {
		t.Fatalf("Latest after Load = %+v", got)
	}
}

func TestLoad_IgnoresStaleCacheWhenCurrentIsNewer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cached := Release{Tag: "v0.9.0", CheckedAt: time.Now().UTC()}
	b, _ := json.Marshal(cached)
	if err := os.WriteFile(filepath.Join(dir, "update_check.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := New("owner/repo", "v1.0.0", dir, nil)
	if err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Latest() != nil {
		t.Fatalf("Latest should be nil when cache < current")
	}
}

func TestLoad_MissingCacheIsNoOp(t *testing.T) {
	t.Parallel()
	c := New("owner/repo", "v1.0.0", t.TempDir(), nil)
	if err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Latest() != nil {
		t.Fatalf("Latest should be nil with no cache file")
	}
}
