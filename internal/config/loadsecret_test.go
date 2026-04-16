package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_InvalidDataDir forces Load() down the mkdir-error branch by pointing
// --data-dir at a path that already exists as a regular file.
func TestLoad_InvalidDataDir(t *testing.T) {
	// Prepare a regular file in a temp dir so MkdirAll can't claim the path.
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(badPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetFlagsAndArgs(t, []string{"debeasy", "--data-dir", badPath})

	if _, err := Load(); err == nil {
		t.Fatal("expected mkdir error")
	}
}
