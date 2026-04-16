package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pfortini/debeasy/internal/store"
)

// withStdin swaps os.Stdin with a reader for the duration of f.
func withStdin(t *testing.T, content string, f func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })
	go func() {
		_, _ = w.WriteString(content)
		_ = w.Close()
	}()
	f()
}

// captureStdout redirects os.Stdout while f runs and returns what was written.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	f()
	_ = w.Close()
	<-done
	return buf.String()
}

func TestAdmin_NoArgs(t *testing.T) {
	if err := runAdmin(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got %v", err)
	}
}

func TestAdmin_UnknownSub(t *testing.T) {
	if err := runAdmin([]string{"nope"}); err == nil {
		t.Errorf("expected error for unknown subcommand")
	}
}

func TestAdmin_Help(t *testing.T) {
	out := captureStdout(t, func() {
		_ = runAdmin([]string{"--help"})
	})
	if !strings.Contains(out, "subcommands") {
		t.Errorf("help output missing: %q", out)
	}
}

func TestAdmin_Create_RequiresUsername(t *testing.T) {
	dir := t.TempDir()
	err := runAdmin([]string{"create", "--data-dir", dir, "--password", "password1"})
	if err == nil || !strings.Contains(err.Error(), "username") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_Create_RejectsShortPassword(t *testing.T) {
	dir := t.TempDir()
	err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "short"})
	if err == nil || !strings.Contains(err.Error(), "8 characters") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_Create_RejectsBadRole(t *testing.T) {
	dir := t.TempDir()
	err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password1", "--role", "superhero"})
	if err == nil || !strings.Contains(err.Error(), "role") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_Create_Success_AndStdin(t *testing.T) {
	dir := t.TempDir()

	// Stdin-mode: password never appears in argv
	var err error
	out := captureStdout(t, func() {
		withStdin(t, "supersecret", func() {
			err = runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password-stdin"})
		})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "created user") {
		t.Errorf("expected creation message, got %q", out)
	}

	// Verify stored and is admin
	s, err := store.Open(filepath.Join(dir, "debeasy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.Users.Verify(t.Context(), "alice", "supersecret")
	if err != nil || !u.IsAdmin() {
		t.Errorf("verify: %v user=%+v", err, u)
	}

	// Secret file is 0600
	info, _ := os.Stat(filepath.Join(dir, "secret"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("secret perm = %o", info.Mode().Perm())
	}
}

func TestAdmin_Create_IfNotExists(t *testing.T) {
	dir := t.TempDir()

	// First call creates admin
	if err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password1"}); err != nil {
		t.Fatal(err)
	}

	// Second call with --if-not-exists should succeed silently
	var err error
	out := captureStdout(t, func() {
		err = runAdmin([]string{"create", "--data-dir", dir, "--username", "bob", "--password", "password2", "--if-not-exists"})
	})
	if err != nil {
		t.Fatalf("if-not-exists create: %v", err)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected skip message; got %q", out)
	}
	// Bob must NOT have been created
	s, _ := store.Open(filepath.Join(dir, "debeasy.sqlite"))
	defer s.Close()
	if _, err := s.Users.FindByUsername(t.Context(), "bob"); err == nil {
		t.Errorf("bob should not have been created")
	}
}

func TestAdmin_Create_Duplicate(t *testing.T) {
	dir := t.TempDir()
	if err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password1"}); err != nil {
		t.Fatal(err)
	}
	err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password2"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_ResetPassword(t *testing.T) {
	dir := t.TempDir()
	if err := runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password1"}); err != nil {
		t.Fatal(err)
	}
	if err := runAdmin([]string{"reset-password", "--data-dir", dir, "--username", "alice", "--password", "newpassword"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	s, _ := store.Open(filepath.Join(dir, "debeasy.sqlite"))
	defer s.Close()
	if _, err := s.Users.Verify(t.Context(), "alice", "newpassword"); err != nil {
		t.Errorf("new password not in effect: %v", err)
	}
}

func TestAdmin_ResetPassword_UnknownUser(t *testing.T) {
	dir := t.TempDir()
	err := runAdmin([]string{"reset-password", "--data-dir", dir, "--username", "ghost", "--password", "password1"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_ResetPassword_Short(t *testing.T) {
	dir := t.TempDir()
	_ = runAdmin([]string{"create", "--data-dir", dir, "--username", "alice", "--password", "password1"})
	err := runAdmin([]string{"reset-password", "--data-dir", dir, "--username", "alice", "--password", "short"})
	if err == nil || !strings.Contains(err.Error(), "8 characters") {
		t.Errorf("got %v", err)
	}
}

func TestAdmin_OpenStoreForCLI_EmptyDir(t *testing.T) {
	if _, err := openStoreForCLI(""); err == nil {
		t.Error("empty dir should error")
	}
}

func TestAdminDefaultDataDir_EnvWins(t *testing.T) {
	t.Setenv("DEBEASY_DATA_DIR", "/tmp/explicit")
	if got := adminDefaultDataDir(); got != "/tmp/explicit" {
		t.Errorf("got %q; want /tmp/explicit", got)
	}
}

func TestAdminDefaultDataDir_FallbackToHome(t *testing.T) {
	t.Setenv("DEBEASY_DATA_DIR", "")
	got := adminDefaultDataDir()
	if got == "" {
		t.Error("should return a path")
	}
}
