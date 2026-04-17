package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pfortini/debeasy/internal/version"
)

// runUpdate implements `debeasy update [flags]`.
//
// The flow mirrors scripts/install.sh intentionally — same asset URLs, same
// checksum file layout — so a fresh install and an in-place upgrade land on
// identical bytes.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	opts := defaultUpdateOpts()
	fs.StringVar(&opts.Repo, "repo", opts.Repo, "GitHub owner/repo to check for releases")
	fs.StringVar(&opts.Tag, "version", opts.Tag, "release tag (e.g. v1.2.3) or \"latest\"")
	fs.BoolVar(&opts.Check, "check", false, "only print current/latest, do not download or swap")
	fs.BoolVar(&opts.Yes, "yes", false, "skip confirmation prompt")
	fs.StringVar(&opts.Service, "service", opts.Service, "systemd unit to restart after swap (empty to skip)")
	noRestart := fs.Bool("no-restart", false, "alias for --service=\"\"")
	fs.StringVar(&opts.InstallPath, "install-path", "", "binary path to replace (default: own executable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *noRestart {
		opts.Service = ""
	}
	return runUpdateWithOpts(context.Background(), os.Stdout, os.Stdin, opts)
}

// updateOpts is the parsed form of the CLI flags. Split out so tests can drive
// runUpdateWithOpts directly without round-tripping through flag.
type updateOpts struct {
	Repo        string
	Tag         string
	Check       bool
	Yes         bool
	Service     string
	InstallPath string

	// CurrentVersion is what we compare against the resolved release tag.
	// Defaults to version.Version; tests override it to avoid mutating the
	// package global under t.Parallel.
	CurrentVersion string

	// Endpoints — overridden in tests so we don't hit the real GitHub.
	APIBaseURL      string // e.g. https://api.github.com
	DownloadBaseURL string // e.g. https://github.com

	// HTTPClient lets tests inject an httptest.Server's client.
	HTTPClient *http.Client

	// RestartFn runs the systemctl restart step. Tests replace it with a noop.
	RestartFn func(service string) error
}

func defaultUpdateOpts() updateOpts {
	return updateOpts{
		Repo:            "pfortini/debeasy",
		Tag:             "latest",
		Service:         "debeasy.service",
		CurrentVersion:  version.Version,
		APIBaseURL:      "https://api.github.com",
		DownloadBaseURL: "https://github.com",
		HTTPClient:      &http.Client{Timeout: 60 * time.Second},
		RestartFn:       systemctlRestart,
	}
}

func runUpdateWithOpts(ctx context.Context, out io.Writer, in io.Reader, opts updateOpts) error {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.RestartFn == nil {
		opts.RestartFn = systemctlRestart
	}
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = "https://api.github.com"
	}
	if opts.DownloadBaseURL == "" {
		opts.DownloadBaseURL = "https://github.com"
	}

	asset, err := assetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	current := opts.CurrentVersion
	if current == "" {
		current = version.Version
	}
	targetTag := opts.Tag
	if targetTag == "" || targetTag == "latest" {
		tag, err := fetchLatestTag(ctx, opts)
		if err != nil {
			return fmt.Errorf("resolve latest: %w", err)
		}
		targetTag = tag
	}

	_, _ = fmt.Fprintf(out, "current: %s\nlatest:  %s\n", current, targetTag)

	if opts.Check {
		return nil
	}
	if targetTag == current {
		_, _ = fmt.Fprintln(out, "already up to date.")
		return nil
	}

	binaryPath := opts.InstallPath
	if binaryPath == "" {
		p, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate own binary: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			p = resolved
		}
		binaryPath = p
	}

	if !opts.Yes {
		_, _ = fmt.Fprintf(out, "replace %s with %s? [y/N] ", binaryPath, targetTag)
		if !readYes(in) {
			return errors.New("cancelled")
		}
	}

	// Download into the *same directory* as the target so os.Rename is atomic
	// (rename(2) requires the same filesystem).
	dir := filepath.Dir(binaryPath)
	tmp := filepath.Join(dir, fmt.Sprintf(".debeasy.new.%d", os.Getpid()))
	defer os.Remove(tmp) //nolint:errcheck // best-effort cleanup of temp download

	assetURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", opts.DownloadBaseURL, opts.Repo, targetTag, asset)
	_, _ = fmt.Fprintf(out, "downloading %s\n", assetURL)
	got, err := downloadWithHash(ctx, opts.HTTPClient, assetURL, tmp)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	checksumURL := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", opts.DownloadBaseURL, opts.Repo, targetTag)
	want, err := fetchChecksum(ctx, opts.HTTPClient, checksumURL, asset)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if got != want {
		return fmt.Errorf("sha256 mismatch: want %s, got %s", want, got)
	}
	_, _ = fmt.Fprintf(out, "sha256 verified: %s\n", got)

	// 0o755 is correct for an executable we're about to put on $PATH; gosec's
	// generic G302 warning doesn't apply here.
	if err := os.Chmod(tmp, 0o755); err != nil { //nolint:gosec // executable binary
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmp, binaryPath); err != nil {
		return fmt.Errorf("swap binary (need write access to %s — re-run with sudo?): %w", binaryPath, err)
	}
	_, _ = fmt.Fprintf(out, "installed: %s\n", binaryPath)

	if opts.Service != "" {
		if err := opts.RestartFn(opts.Service); err != nil {
			_, _ = fmt.Fprintf(out, "warning: restart %s failed: %v\n", opts.Service, err)
			_, _ = fmt.Fprintf(out, "binary is in place — restart manually with: sudo systemctl restart %s\n", opts.Service)
		} else {
			_, _ = fmt.Fprintf(out, "restarted: %s\n", opts.Service)
		}
	} else {
		_, _ = fmt.Fprintln(out, "skipped service restart (--service=\"\")")
	}
	return nil
}

// assetName mirrors the release.yml matrix: debeasy-<os>-<arch>. Any other
// combination has no release artifact and we refuse instead of 404ing later.
func assetName(goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("unsupported OS %q (no release artifact published)", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported arch %q (no release artifact published)", goarch)
	}
	return fmt.Sprintf("debeasy-%s-%s", goos, goarch), nil
}

func fetchLatestTag(ctx context.Context, opts updateOpts) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", opts.APIBaseURL, opts.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "debeasy-update/"+version.Version)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("github: %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.TagName == "" {
		return "", errors.New("release has no tag_name")
	}
	return body.TagName, nil
}

// downloadWithHash streams the URL into dst, computing the sha256 hex digest
// along the way. We verify before swapping so a corrupted download never
// reaches $PREFIX/bin.
func downloadWithHash(ctx context.Context, hc *http.Client, url, dst string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "debeasy-update/"+version.Version)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchChecksum reads the release's aggregated checksums.txt (one
// "<sha256>  <filename>" line per asset, matching `sha256sum`'s format) and
// returns the hex digest for the requested asset.
func fetchChecksum(ctx context.Context, hc *http.Client, url, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "debeasy-update/"+version.Version)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseChecksum(string(body), asset)
}

// parseChecksum scans sha256sum-formatted text for the line matching `asset`
// and returns the hex digest. Accepts either "<hash>  <name>" (two spaces, as
// GNU sha256sum writes) or a single-space separator.
func parseChecksum(body, asset string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// Split on the first run of whitespace; tolerate the sha256sum
		// "binary mode" asterisk (e.g. "hash *filename").
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum entry for %q", asset)
}

// readYes returns true when the user types y / yes on the first non-blank
// line. Any other input (including EOF) counts as no.
func readYes(r io.Reader) bool {
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sc.Text())) {
	case "y", "yes":
		return true
	}
	return false
}

// systemctlRestart shells out to systemctl. Missing systemctl is reported as
// an error; the caller prints a manual-restart hint in that case.
func systemctlRestart(service string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found on PATH")
	}
	cmd := exec.Command("systemctl", "restart", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
