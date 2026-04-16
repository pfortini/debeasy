package store

import (
	"errors"
	"testing"
	"time"
)

func TestSessions_CreateGetDelete(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")

	sess, err := s.Sessions.Create(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(sess.ID) != 64 { // 32 bytes hex
		t.Errorf("session id len = %d; want 64", len(sess.ID))
	}
	if len(sess.CSRFToken) != 48 {
		t.Errorf("csrf len = %d; want 48", len(sess.CSRFToken))
	}

	got, err := s.Sessions.Get(ctx, sess.ID)
	if err != nil || got.UserID != u.ID {
		t.Fatalf("Get: %v", err)
	}

	if err := s.Sessions.Delete(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get(ctx, sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("deleted session should 404, got %v", err)
	}
}

func TestSessions_Expired(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "bob", "password1", "user")

	sess, err := s.Sessions.Create(ctx, u.ID, -time.Second) // already expired
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get(ctx, sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session should 404, got %v", err)
	}
}

func TestSessions_Get_Unknown(t *testing.T) {
	s := newStore(t)
	if _, err := s.Sessions.Get(t.Context(), "not-a-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestSessions_PurgeExpired(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")
	_, _ = s.Sessions.Create(ctx, u.ID, -time.Second)
	active, _ := s.Sessions.Create(ctx, u.ID, time.Hour)

	if err := s.Sessions.PurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get(ctx, active.ID); err != nil {
		t.Errorf("active session should survive purge: %v", err)
	}
}

func TestSessions_CascadeOnUserDelete(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")
	sess, _ := s.Sessions.Create(ctx, u.ID, time.Hour)

	if _, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get(ctx, sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("session should cascade-delete with user, got %v", err)
	}
}
