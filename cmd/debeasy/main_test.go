package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests exercise `main` and `runServer` by re-executing the compiled test
// binary as a subprocess. Standard Go trick: check for a sentinel env var, and if
// it's set, run the real code path instead of the test code.

const subprocessEnv = "DEBEASY_TEST_MAIN_SUBPROCESS"

func TestMain_RerunsAsServer(t *testing.T) {
	// When the sentinel is set, this test body calls runServer directly. The parent
	// test process below invokes this same test binary with the sentinel set.
	switch os.Getenv(subprocessEnv) {
	case "server":
		dir := os.Getenv("DEBEASY_DATA_DIR")
		os.Args = []string{"debeasy", "--addr", "127.0.0.1:0", "--data-dir", dir}
		os.Exit(runServer())
	case "admin-bad":
		os.Args = []string{"debeasy", "admin", "create", "--username", "x"} // no password
		main()
		return
	}
	// Parent path: fork a subprocess that actually starts the server briefly, then kill it.
	if testing.Short() {
		t.Skip("subprocess smoke test")
	}

	dir := t.TempDir()
	// Child: run the server; we'll SIGTERM it after a moment.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestMain_RerunsAsServer")
	cmd.Env = append(os.Environ(), subprocessEnv+"=server", "DEBEASY_DATA_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Let it bind + log "listening", then tell it to stop cleanly.
	time.Sleep(300 * time.Millisecond)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	// The data dir should now contain the sqlite store + secret file.
	if _, err := os.Stat(filepath.Join(dir, "debeasy.sqlite")); err != nil {
		t.Errorf("expected debeasy.sqlite to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secret")); err != nil {
		t.Errorf("expected secret to exist: %v", err)
	}
}

func TestMain_AdminBadArgsExitsNonzero(t *testing.T) {
	if os.Getenv(subprocessEnv) == "admin-bad" {
		os.Args = []string{"debeasy", "admin", "create", "--username", "x"} // missing password
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_AdminBadArgsExitsNonzero")
	cmd.Env = append(os.Environ(), subprocessEnv+"=admin-bad")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("subprocess should have exited non-zero, output:\n%s", out)
	}
	if !strings.Contains(string(out), "error:") {
		t.Errorf("missing error prefix in output: %s", out)
	}
}
