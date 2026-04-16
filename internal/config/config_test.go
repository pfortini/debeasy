package config

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSecret_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	b, err := loadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("loadOrCreateSecret: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("secret length = %d; want 32", len(b))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("secret file perm = %o; want 0600", info.Mode().Perm())
	}
}

func TestLoadOrCreateSecret_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	first, _ := loadOrCreateSecret(path)
	second, err := loadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("secret should persist across loads")
	}
}

func TestLoadOrCreateSecret_RejectsCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("nothex"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateSecret(path); err == nil {
		t.Fatalf("expected corrupt-secret error")
	}
}

func TestLoadOrCreateSecret_FromEnv(t *testing.T) {
	want := make([]byte, 32)
	for i := range want {
		want[i] = byte(i)
	}
	t.Setenv("DEBEASY_APP_SECRET", hex.EncodeToString(want))
	got, err := loadOrCreateSecret(filepath.Join(t.TempDir(), "secret"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("env secret not honoured")
	}
}

func TestLoadOrCreateSecret_BadEnv(t *testing.T) {
	t.Setenv("DEBEASY_APP_SECRET", "tooshort")
	if _, err := loadOrCreateSecret(filepath.Join(t.TempDir(), "secret")); err == nil {
		t.Fatalf("expected error on short env secret")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("FOO_XYZ", "v")
	if got := envOr("FOO_XYZ", "def"); got != "v" {
		t.Errorf("got %q; want v", got)
	}
	if got := envOr("FOO_XYZ_MISSING", "def"); got != "def" {
		t.Errorf("got %q; want def", got)
	}
}

func TestDefaultDataDir_UsesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	got := defaultDataDir()
	if got != filepath.Join(home, ".debeasy") {
		t.Errorf("got %q; want %s/.debeasy", got, home)
	}
}

func TestConfig_StorePath(t *testing.T) {
	c := &Config{DataDir: "/tmp/x"}
	if got := c.StorePath(); got != "/tmp/x/debeasy.sqlite" {
		t.Errorf("got %q", got)
	}
}

func TestLoad_HappyPath(t *testing.T) {
	dir := t.TempDir()
	resetFlagsAndArgs(t, []string{"test", "--addr", ":9999", "--data-dir", dir})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":9999" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.DataDir != dir {
		t.Errorf("DataDir = %q", c.DataDir)
	}
	if len(c.AppSecret) != 32 {
		t.Errorf("AppSecret len = %d", len(c.AppSecret))
	}
	if got := c.StorePath(); got != dir+"/debeasy.sqlite" {
		t.Errorf("StorePath = %q", got)
	}
}

// resetFlagsAndArgs isolates a Load() call from ambient flag state.
func resetFlagsAndArgs(t *testing.T, args []string) {
	t.Helper()
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ExitOnError)
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})
}
