package store

import (
	"errors"
	"testing"
)

func TestUsers_CreateAndVerify(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()

	u, err := s.Users.Create(ctx, "alice", "hunter2x", "admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 || u.Username != "alice" || u.Role != "admin" {
		t.Fatalf("unexpected user: %+v", u)
	}

	got, err := s.Users.Verify(ctx, "alice", "hunter2x")
	if err != nil || got.ID != u.ID {
		t.Fatalf("Verify: %v user=%+v", err, got)
	}

	if _, err := s.Users.Verify(ctx, "alice", "wrong"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("wrong password should return ErrBadPassword, got %v", err)
	}
	if _, err := s.Users.Verify(ctx, "ghost", "x"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("unknown user should return ErrBadPassword, got %v", err)
	}
}

func TestUsers_Create_RejectsInvalid(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	if _, err := s.Users.Create(ctx, "", "longenough", "user"); err == nil {
		t.Errorf("empty username should error")
	}
	if _, err := s.Users.Create(ctx, "bob", "short", "user"); err == nil {
		t.Errorf("short password should error")
	}
}

func TestUsers_Create_DuplicateUsername(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	if _, err := s.Users.Create(ctx, "alice", "password1", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Create(ctx, "ALICE", "password2", "user"); !errors.Is(err, ErrUserExists) {
		t.Errorf("case-insensitive duplicate should return ErrUserExists, got %v", err)
	}
}

func TestUsers_Create_NormalisesRole(t *testing.T) {
	s := newStore(t)
	u, err := s.Users.Create(t.Context(), "x", "password1", "superhero")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != "user" {
		t.Errorf("unknown role should fall back to 'user'; got %q", u.Role)
	}
}

func TestUsers_Count_List_Disable_Reset(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()

	if n, _ := s.Users.Count(ctx); n != 0 {
		t.Fatalf("fresh store should have 0 users, got %d", n)
	}

	alice, _ := s.Users.Create(ctx, "alice", "password1", "admin")
	_, _ = s.Users.Create(ctx, "bob", "password2", "user")

	n, err := s.Users.Count(ctx)
	if err != nil || n != 2 {
		t.Fatalf("Count = (%d,%v)", n, err)
	}
	list, err := s.Users.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("List len = %d err=%v", len(list), err)
	}

	if err := s.Users.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Verify(ctx, "alice", "password1"); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled user should return ErrDisabled, got %v", err)
	}
	if err := s.Users.SetDisabled(ctx, alice.ID, false); err != nil {
		t.Fatal(err)
	}

	if err := s.Users.ResetPassword(ctx, alice.ID, "short"); err == nil {
		t.Errorf("short password should be rejected")
	}
	if err := s.Users.ResetPassword(ctx, alice.ID, "newpassword"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Verify(ctx, "alice", "newpassword"); err != nil {
		t.Errorf("Verify with new password failed: %v", err)
	}
}

func TestUsers_FindByUsername_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Users.FindByUsername(t.Context(), "ghost"); err == nil {
		t.Error("want error for missing user")
	}
}

func TestUser_IsAdmin(t *testing.T) {
	if (&User{Role: "admin"}).IsAdmin() != true {
		t.Error()
	}
	if (&User{Role: "user"}).IsAdmin() != false {
		t.Error()
	}
}
