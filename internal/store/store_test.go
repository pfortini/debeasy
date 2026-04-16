package store

import (
	"path/filepath"
	"testing"
)

// newStore spins up a fresh store in a temp directory — tests should call this rather
// than reusing a single instance so each test has an isolated schema + data.
func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpen_AppliesSchema(t *testing.T) {
	s := newStore(t)
	// Every expected table must exist.
	tables := []string{"users", "sessions", "connections", "query_history"}
	for _, name := range tables {
		var got string
		err := s.DB.QueryRow(`SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, name).Scan(&got)
		if err != nil {
			t.Errorf("table %s missing: %v", name, err)
		}
	}
}

func TestOpen_IdempotentMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()
	s2, err := Open(path) // should not fail on re-apply
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestOpen_BadPath(t *testing.T) {
	// Directory that doesn't exist and no parent means sqlite can't create the file.
	// modernc.org/sqlite defers errors to first use, so exercise by attempting a query.
	s, err := Open("/nonexistent-path-xyz/abc.sqlite")
	if err != nil {
		return // expected path
	}
	_, err = s.Users.Count(t.Context())
	if err == nil {
		t.Fatal("expected error opening/using invalid sqlite path")
	}
	_ = s.Close()
}
