// Package updates polls the GitHub releases API to detect when a newer debeasy
// release is available and surfaces that signal to the web UI via a small
// thread-safe cache.
//
// The server boots a Checker, primes it from an on-disk cache file (so the
// banner survives restarts without an immediate network round-trip), and runs
// Check on a ticker in the background. Handlers read Latest() without ever
// blocking on the network.
//
// This package deliberately keeps zero external dependencies — stdlib
// net/http + encoding/json are enough to hit the public GitHub API.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release is the subset of GitHub's releases payload we care about, plus our
// own CheckedAt timestamp so the on-disk cache records when we last observed
// the version.
type Release struct {
	Tag         string    `json:"tag"`
	URL         string    `json:"url"`
	PublishedAt string    `json:"published_at,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Checker queries GitHub releases and caches the result (both in memory and on
// disk). A zero-value Checker is not valid; use New.
type Checker struct {
	repo           string
	currentVersion string
	httpClient     *http.Client
	cachePath      string
	apiBaseURL     string // override for tests; defaults to https://api.github.com

	mu     sync.RWMutex
	latest *Release
}

// New builds a Checker for the given "owner/repo" and current version. The
// cache file lives at <dataDir>/update_check.json; passing an empty dataDir
// disables persistence (useful for tests and for operators who set
// DEBEASY_UPDATE_CHECK=0 but still want Check() to work manually).
func New(repo, currentVersion, dataDir string, httpClient *http.Client) *Checker {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	c := &Checker{
		repo:           repo,
		currentVersion: currentVersion,
		httpClient:     httpClient,
		apiBaseURL:     "https://api.github.com",
	}
	if dataDir != "" {
		c.cachePath = filepath.Join(dataDir, "update_check.json")
	}
	return c
}

// WithAPIBaseURL overrides the GitHub API base (e.g. for tests pointing at an
// httptest.Server). Returns the Checker for chaining.
func (c *Checker) WithAPIBaseURL(u string) *Checker {
	c.apiBaseURL = u
	return c
}

// Current returns the version the running binary was built at.
func (c *Checker) Current() string { return c.currentVersion }

// Latest returns the cached newer release, or nil when no upgrade is known.
// Safe for concurrent use.
func (c *Checker) Latest() *Release {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// Load reads the on-disk cache and, if it refers to a version newer than the
// running one, populates the in-memory state. Missing-file is not an error.
func (c *Checker) Load() error {
	if c.cachePath == "" {
		return nil
	}
	b, err := os.ReadFile(c.cachePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if !Newer(r.Tag, c.currentVersion) {
		return nil
	}
	c.mu.Lock()
	c.latest = &r
	c.mu.Unlock()
	return nil
}

// githubRelease matches the fields we need off GitHub's /releases/latest
// payload. Unknown fields are ignored.
type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// Check fetches the repo's latest release and, if its tag is strictly newer
// than currentVersion, updates the in-memory cache and writes the on-disk
// cache. Returns the newer Release (or nil if already up to date).
func (c *Checker) Check(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBaseURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "debeasy/"+c.currentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var gr githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, err
	}
	if gr.Draft || gr.Prerelease || gr.TagName == "" {
		return nil, nil
	}
	if !Newer(gr.TagName, c.currentVersion) {
		c.clear()
		return nil, nil
	}
	r := &Release{
		Tag:         gr.TagName,
		URL:         gr.HTMLURL,
		PublishedAt: gr.PublishedAt,
		CheckedAt:   time.Now().UTC(),
	}
	c.mu.Lock()
	c.latest = r
	c.mu.Unlock()
	_ = c.writeCache(r)
	return r, nil
}

func (c *Checker) clear() {
	c.mu.Lock()
	c.latest = nil
	c.mu.Unlock()
	if c.cachePath != "" {
		_ = os.Remove(c.cachePath)
	}
}

// writeCache persists the release to cachePath via temp-file + atomic rename,
// matching the perms pattern used elsewhere (0600).
func (c *Checker) writeCache(r *Release) error {
	if c.cachePath == "" {
		return nil
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.cachePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.cachePath)
}

// Newer reports whether the semver-like tag `latest` is strictly greater than
// `current`. Both are normalised by stripping a leading "v" and discarding any
// "-pre" / "+meta" suffix. Unparsable input (including "dev") returns false —
// we'd rather miss a legitimate update than nag developers about their own
// local builds.
func Newer(latest, current string) bool {
	lParts, lOK := parseSemver(latest)
	cParts, cOK := parseSemver(current)
	if !lOK || !cOK {
		return false
	}
	for i := range lParts {
		if lParts[i] != cParts[i] {
			return lParts[i] > cParts[i]
		}
	}
	return false
}

// parseSemver returns the [major, minor, patch] triple for tags of the form
// "vX.Y.Z" or "X.Y.Z", ignoring any "-pre"/"+meta" suffix. Ok is false when
// the input is anything else (empty, "dev", non-numeric components, …).
func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return out, false
	}
	// Drop pre-release / build suffixes for comparison purposes.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
