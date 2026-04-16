package store

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/pfortini/debeasy/internal/crypto"
)

func newKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return crypto.NewKeyring(secret)
}

func TestConnections_Create_Requires(t *testing.T) {
	s := newStore(t)
	s.Connections.WithKeyring(newKeyring(t))
	ctx := t.Context()

	if _, err := s.Connections.Create(ctx, &Connection{Kind: "postgres"}, 0); err == nil {
		t.Errorf("empty name should be rejected")
	}
	if _, err := s.Connections.Create(ctx, &Connection{Name: "c", Kind: "oracle"}, 0); err == nil {
		t.Errorf("unknown kind should be rejected")
	}
}

func TestConnections_PasswordEncrypted(t *testing.T) {
	s := newStore(t)
	kr := newKeyring(t)
	s.Connections.WithKeyring(kr)
	ctx := t.Context()

	in := &Connection{
		Name: "pg-prod", Kind: "postgres", Host: "h", Port: 5432,
		Username: "u", Password: "plaintext-pass", Database: "d",
	}
	c, err := s.Connections.Create(ctx, in, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The password must NOT be stored in plaintext.
	var encBlob []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT password_enc FROM connections WHERE id=?`, c.ID).Scan(&encBlob); err != nil {
		t.Fatal(err)
	}
	if string(encBlob) == "plaintext-pass" {
		t.Fatalf("password should be encrypted at rest")
	}

	// Fetching via Get should round-trip the plaintext back.
	got, err := s.Connections.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "plaintext-pass" {
		t.Errorf("round-tripped password = %q", got.Password)
	}
}

func TestConnections_DuplicateName(t *testing.T) {
	s := newStore(t)
	s.Connections.WithKeyring(newKeyring(t))
	ctx := t.Context()
	base := &Connection{Name: "x", Kind: "sqlite", Database: "/tmp/a"}
	if _, err := s.Connections.Create(ctx, base, 0); err != nil {
		t.Fatal(err)
	}
	dup := &Connection{Name: "x", Kind: "sqlite", Database: "/tmp/b"}
	if _, err := s.Connections.Create(ctx, dup, 0); err == nil {
		t.Errorf("duplicate name should be rejected")
	}
}

func TestConnections_Update_PasswordSemantics(t *testing.T) {
	s := newStore(t)
	kr := newKeyring(t)
	s.Connections.WithKeyring(kr)
	ctx := t.Context()
	c, _ := s.Connections.Create(ctx, &Connection{
		Name: "c", Kind: "postgres", Password: "initial",
	}, 0)

	// update=false: leaves password untouched even when Password field is changed
	c.Password = "ignored-when-updatePassword-is-false"
	c.Host = "newhost"
	if err := s.Connections.Update(ctx, c, false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Connections.Get(ctx, c.ID)
	if got.Password != "initial" {
		t.Errorf("password should remain %q, got %q", "initial", got.Password)
	}
	if got.Host != "newhost" {
		t.Errorf("host should be updated to newhost, got %q", got.Host)
	}

	// update=true with a new password rotates it
	c.Password = "rotated"
	if err := s.Connections.Update(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Connections.Get(ctx, c.ID)
	if got.Password != "rotated" {
		t.Errorf("rotation failed, got %q", got.Password)
	}

	// update=true with empty Password clears it
	c.Password = ""
	if err := s.Connections.Update(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Connections.Get(ctx, c.ID)
	if got.Password != "" {
		t.Errorf("password should have been cleared, got %q", got.Password)
	}
}

func TestConnections_Update_InvalidKind(t *testing.T) {
	s := newStore(t)
	s.Connections.WithKeyring(newKeyring(t))
	c, _ := s.Connections.Create(t.Context(), &Connection{Name: "c", Kind: "sqlite", Database: "/x"}, 0)
	c.Kind = "oracle"
	if err := s.Connections.Update(t.Context(), c, false); err == nil {
		t.Error("invalid kind should be rejected")
	}
}

func TestConnections_DeleteAndList(t *testing.T) {
	s := newStore(t)
	s.Connections.WithKeyring(newKeyring(t))
	ctx := t.Context()
	c1, _ := s.Connections.Create(ctx, &Connection{Name: "a", Kind: "sqlite", Database: "/a"}, 0)
	c2, _ := s.Connections.Create(ctx, &Connection{Name: "b", Kind: "sqlite", Database: "/b"}, 0)

	list, err := s.Connections.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}

	if err := s.Connections.Delete(ctx, c1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connections.Get(ctx, c1.ID); !errors.Is(err, ErrConnNotFound) {
		t.Errorf("deleted conn should 404, got %v", err)
	}
	if _, err := s.Connections.Get(ctx, c2.ID); err != nil {
		t.Errorf("other conn should survive: %v", err)
	}
}
