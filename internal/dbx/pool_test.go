package dbx

import (
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/pfortini/debeasy/internal/crypto"
	"github.com/pfortini/debeasy/internal/store"
)

// newPoolEnv returns a freshly-opened store + a pool wired to it, plus a ready-to-use
// saved sqlite Connection row.
func newPoolEnv(t *testing.T) (*store.Store, *Pool, *store.Connection) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	s.Connections.WithKeyring(crypto.NewKeyring(secret))

	target := filepath.Join(t.TempDir(), "target.sqlite")
	conn, err := s.Connections.Create(t.Context(), &store.Connection{
		Name:     "target",
		Kind:     "sqlite",
		Database: target,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	pool := NewPool(s.Connections)
	t.Cleanup(pool.Stop)
	return s, pool, conn
}

func TestPool_GetCachesAndEvicts(t *testing.T) {
	_, pool, conn := newPoolEnv(t)
	ctx := t.Context()

	a, err := pool.Get(ctx, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pool.Get(ctx, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("Pool.Get should return same driver instance for same conn")
	}

	pool.Evict(conn.ID)
	c, err := pool.Get(ctx, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Errorf("Evict should have closed the prior driver")
	}
}

func TestPool_GetUnknownConn(t *testing.T) {
	_, pool, _ := newPoolEnv(t)
	if _, err := pool.Get(t.Context(), 9999); err == nil {
		t.Error("expected error for unknown conn id")
	}
}

func TestPool_Test_PingsWithoutCaching(t *testing.T) {
	_, pool, conn := newPoolEnv(t)

	if err := pool.Test(t.Context(), conn); err != nil {
		t.Fatalf("Test: %v", err)
	}
	// pool shouldn't have stored the test driver
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if _, ok := pool.drivers[conn.ID]; ok {
		t.Errorf("Test should not leave an entry in the pool")
	}
}

func TestPool_EvictIdle(t *testing.T) {
	_, pool, conn := newPoolEnv(t)
	pool.idleTTL = 1 * time.Millisecond

	if _, err := pool.Get(t.Context(), conn.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // cross the TTL

	evicted := pool.evictIdle()
	if evicted != 1 {
		t.Errorf("evicted %d; want 1", evicted)
	}

	pool.mu.Lock()
	_, stillThere := pool.drivers[conn.ID]
	pool.mu.Unlock()
	if stillThere {
		t.Errorf("idle driver should have been evicted")
	}

	// A fresh get then immediate evictIdle (long TTL) must not evict.
	pool.idleTTL = time.Hour
	_, _ = pool.Get(t.Context(), conn.ID)
	if n := pool.evictIdle(); n != 0 {
		t.Errorf("fresh driver shouldn't be evicted; got %d", n)
	}
}

func TestOpenDriver_Dispatch(t *testing.T) {
	// sqlite: happy path with a real file
	sqlite, err := openDriver(&store.Connection{Kind: "sqlite", Database: filepath.Join(t.TempDir(), "x.db")}, 1)
	if err != nil || sqlite.Kind() != KindSQLite {
		t.Errorf("sqlite dispatch failed: %v", err)
	}
	_ = sqlite.Close()

	// postgres / mysql: openDriver is lazy (doesn't ping), so dispatch succeeds
	// even without a reachable server — ensures the switch branches are hit.
	pg, err := openDriver(&store.Connection{Kind: "postgres"}, 1)
	if err != nil || pg.Kind() != KindPostgres {
		t.Errorf("postgres dispatch: %v", err)
	}
	_ = pg.Close()

	my, err := openDriver(&store.Connection{Kind: "mysql"}, 1)
	if err != nil || my.Kind() != KindMySQL {
		t.Errorf("mysql dispatch: %v", err)
	}
	_ = my.Close()

	// sqlite without Database → openSQLite returns an error.
	if _, err := openDriver(&store.Connection{Kind: "sqlite"}, 1); err == nil {
		t.Error("sqlite without Database should error")
	}

	// unknown kind
	if _, err := openDriver(&store.Connection{Kind: "oracle"}, 1); err == nil {
		t.Error("unknown kind should error")
	}
}
