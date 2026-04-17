package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		os, arch string
		want     string
		wantErr  bool
	}{
		{"linux", "amd64", "debeasy-linux-amd64", false},
		{"linux", "arm64", "debeasy-linux-arm64", false},
		{"darwin", "amd64", "debeasy-darwin-amd64", false},
		{"darwin", "arm64", "debeasy-darwin-arm64", false},
		{"windows", "amd64", "", true},
		{"linux", "386", "", true},
	}
	for _, tc := range cases {
		got, err := assetName(tc.os, tc.arch)
		if tc.wantErr {
			if err == nil {
				t.Errorf("assetName(%s,%s) want err, got %q", tc.os, tc.arch, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("assetName(%s,%s)=%q,%v want %q", tc.os, tc.arch, got, err, tc.want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	t.Parallel()
	body := `
abc123  debeasy-linux-amd64
def456  debeasy-linux-arm64
cafebabe *debeasy-darwin-amd64
`
	got, err := parseChecksum(body, "debeasy-linux-arm64")
	if err != nil || got != "def456" {
		t.Fatalf("linux-arm64: got %q err=%v", got, err)
	}
	// sha256sum "binary mode" — leading asterisk on filename
	got, err = parseChecksum(body, "debeasy-darwin-amd64")
	if err != nil || got != "cafebabe" {
		t.Fatalf("darwin-amd64 with asterisk: got %q err=%v", got, err)
	}
	if _, err := parseChecksum(body, "debeasy-nowhere-x86"); err == nil {
		t.Fatal("expected missing-asset error")
	}
}

func TestReadYes(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"y", "Y", "yes", "YES\n", " yes \n"} {
		if !readYes(strings.NewReader(in)) {
			t.Errorf("readYes(%q) want true", in)
		}
	}
	for _, in := range []string{"", "n", "no", "maybe", "\n"} {
		if readYes(strings.NewReader(in)) {
			t.Errorf("readYes(%q) want false", in)
		}
	}
}

// fakeReleaseServer serves the three endpoints our updater hits: the API's
// /releases/latest, the asset binary, and the checksums file. We point both
// APIBaseURL and DownloadBaseURL at the same server.
func fakeReleaseServer(t *testing.T, tag, asset string, assetBytes []byte) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	})
	mux.HandleFunc(fmt.Sprintf("/owner/repo/releases/download/%s/%s", tag, asset), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(assetBytes)
	})
	mux.HandleFunc(fmt.Sprintf("/owner/repo/releases/download/%s/checksums.txt", tag), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	return httptest.NewServer(mux)
}

func TestRunUpdate_HappyPath(t *testing.T) {
	t.Parallel()
	// Force a known "current" so the updater has something to upgrade from.
	asset, err := assetName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	// Use a build-time-stable fake asset so runtime.GOOS/GOARCH in tests
	// still lands on something the fake server will serve. This path only
	// works where runtime matches the fake's "linux/amd64" choice; for CI
	// we gate the test on those values.
	realAsset, rerr := assetName("linux", "amd64")
	if rerr != nil {
		t.Skipf("asset builder rejected linux/amd64: %v", rerr)
	}
	if realAsset != asset {
		t.Fatalf("asset mismatch")
	}

	payload := []byte("this is a fake debeasy binary")
	srv := fakeReleaseServer(t, "v1.0.1", asset, payload)
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "debeasy")
	if err := os.WriteFile(binPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var restarted string
	opts := defaultUpdateOpts()
	opts.Repo = "owner/repo"
	opts.Tag = "latest"
	opts.Yes = true
	opts.Service = "debeasy.service"
	opts.CurrentVersion = "v1.0.0"
	opts.InstallPath = binPath
	opts.APIBaseURL = srv.URL
	opts.DownloadBaseURL = srv.URL
	opts.HTTPClient = srv.Client()
	opts.RestartFn = func(svc string) error { restarted = svc; return nil }

	var out bytes.Buffer
	if err := runUpdateWithOpts(context.Background(), &out, strings.NewReader(""), opts); err != nil {
		// Only linux/amd64 runs will find the built asset name matching
		// runtime. Skip elsewhere rather than false-failing.
		if strings.Contains(err.Error(), "unsupported") {
			t.Skipf("runtime not supported on this platform: %v", err)
		}
		t.Fatalf("update: %v\nout: %s", err, out.String())
	}

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary not swapped; got %q", got)
	}
	if restarted != "debeasy.service" {
		t.Fatalf("restart not called, restarted=%q", restarted)
	}
	if !strings.Contains(out.String(), "sha256 verified") {
		t.Fatalf("expected sha256 verification in output; got:\n%s", out.String())
	}
}

func TestRunUpdate_CheckOnly(t *testing.T) {
	t.Parallel()
	asset, err := assetName("linux", "amd64")
	if err != nil {
		t.Skipf("asset builder: %v", err)
	}

	srv := fakeReleaseServer(t, "v2.0.0", asset, []byte("fake"))
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "debeasy")
	if err := os.WriteFile(binPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := defaultUpdateOpts()
	opts.Repo = "owner/repo"
	opts.Check = true
	opts.CurrentVersion = "v1.0.0"
	opts.InstallPath = binPath
	opts.APIBaseURL = srv.URL
	opts.DownloadBaseURL = srv.URL
	opts.HTTPClient = srv.Client()
	opts.RestartFn = func(string) error { t.Fatal("restart should not run in --check"); return nil }

	var out bytes.Buffer
	if err := runUpdateWithOpts(context.Background(), &out, nil, opts); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out.String(), "v2.0.0") {
		t.Fatalf("expected latest tag in output; got:\n%s", out.String())
	}
	// Binary must be untouched.
	got, _ := os.ReadFile(binPath)
	if string(got) != "old" {
		t.Fatalf("binary was modified during --check; got %q", got)
	}
}

func TestRunUpdate_AlreadyUpToDate(t *testing.T) {
	t.Parallel()
	asset, err := assetName("linux", "amd64")
	if err != nil {
		t.Skipf("asset builder: %v", err)
	}

	srv := fakeReleaseServer(t, "v1.2.3", asset, []byte("unused"))
	defer srv.Close()

	opts := defaultUpdateOpts()
	opts.Repo = "owner/repo"
	opts.Yes = true
	opts.CurrentVersion = "v1.2.3"
	opts.InstallPath = filepath.Join(t.TempDir(), "debeasy")
	_ = os.WriteFile(opts.InstallPath, []byte("old"), 0o755)
	opts.APIBaseURL = srv.URL
	opts.DownloadBaseURL = srv.URL
	opts.HTTPClient = srv.Client()
	opts.RestartFn = func(string) error { t.Fatal("should not restart"); return nil }

	var out bytes.Buffer
	if err := runUpdateWithOpts(context.Background(), &out, nil, opts); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("expected up-to-date message; got:\n%s", out.String())
	}
}

func TestRunUpdate_ChecksumMismatch(t *testing.T) {
	t.Parallel()
	asset, err := assetName("linux", "amd64")
	if err != nil {
		t.Skipf("asset builder: %v", err)
	}

	tag := "v1.0.1"
	// Serve a valid asset but a *mismatched* checksum.
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	})
	mux.HandleFunc(fmt.Sprintf("/owner/repo/releases/download/%s/%s", tag, asset), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("real-content"))
	})
	mux.HandleFunc(fmt.Sprintf("/owner/repo/releases/download/%s/checksums.txt", tag), func(w http.ResponseWriter, _ *http.Request) {
		// Wrong hash on purpose.
		_, _ = fmt.Fprintf(w, "deadbeef  %s\n", asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "debeasy")
	if err := os.WriteFile(binPath, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := defaultUpdateOpts()
	opts.Repo = "owner/repo"
	opts.Yes = true
	opts.Service = ""
	opts.CurrentVersion = "v1.0.0"
	opts.InstallPath = binPath
	opts.APIBaseURL = srv.URL
	opts.DownloadBaseURL = srv.URL
	opts.HTTPClient = srv.Client()

	err = runUpdateWithOpts(context.Background(), new(bytes.Buffer), nil, opts)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch error, got %v", err)
	}
	got, _ := os.ReadFile(binPath)
	if string(got) != "original" {
		t.Fatalf("binary should not have been swapped; got %q", got)
	}
}

func TestRunUpdate_Cancelled(t *testing.T) {
	t.Parallel()
	asset, err := assetName("linux", "amd64")
	if err != nil {
		t.Skipf("asset builder: %v", err)
	}

	srv := fakeReleaseServer(t, "v1.0.1", asset, []byte("new"))
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "debeasy")
	_ = os.WriteFile(binPath, []byte("old"), 0o755)

	opts := defaultUpdateOpts()
	opts.Repo = "owner/repo"
	opts.CurrentVersion = "v1.0.0"
	opts.InstallPath = binPath
	opts.APIBaseURL = srv.URL
	opts.DownloadBaseURL = srv.URL
	opts.HTTPClient = srv.Client()

	err = runUpdateWithOpts(context.Background(), new(bytes.Buffer), strings.NewReader("n\n"), opts)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("want cancelled error, got %v", err)
	}
	got, _ := os.ReadFile(binPath)
	if string(got) != "old" {
		t.Fatalf("binary should be untouched; got %q", got)
	}
}
