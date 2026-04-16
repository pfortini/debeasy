package store

import (
	"database/sql"
	"testing"
)

func TestHistory_AppendRecent(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")

	for i := 0; i < 3; i++ {
		err := s.History.Append(ctx, HistoryEntry{
			UserID: u.ID, SQL: "SELECT 1", ElapsedMS: int64(10 + i), RowsAffected: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	entries, err := s.History.Recent(ctx, u.ID, 10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("Recent: got %d err=%v", len(entries), err)
	}
	// ordered newest first
	if entries[0].ID < entries[1].ID {
		t.Errorf("history should be newest-first")
	}
}

func TestHistory_Recent_LimitClamping(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")
	for i := 0; i < 5; i++ {
		_ = s.History.Append(ctx, HistoryEntry{UserID: u.ID, SQL: "x"})
	}
	// limit=0 should fall back to default 50 (we only inserted 5)
	entries, err := s.History.Recent(ctx, u.ID, 0)
	if err != nil || len(entries) != 5 {
		t.Errorf("got %d entries, want 5", len(entries))
	}
	// limit=1 honoured
	entries, _ = s.History.Recent(ctx, u.ID, 1)
	if len(entries) != 1 {
		t.Errorf("limit not honoured")
	}
	// limit >200 is clamped — we have only 5, so can't distinguish, just exercise the branch
	entries, _ = s.History.Recent(ctx, u.ID, 99999)
	if len(entries) != 5 {
		t.Errorf("clamped-limit result = %d", len(entries))
	}
}

func TestHistory_ErrorStored(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	u, _ := s.Users.Create(ctx, "alice", "password1", "user")
	err := s.History.Append(ctx, HistoryEntry{
		UserID: u.ID,
		SQL:    "BAD SQL",
		Error:  sql.NullString{String: "boom", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.History.Recent(ctx, u.ID, 1)
	if !got[0].Error.Valid || got[0].Error.String != "boom" {
		t.Errorf("error not persisted: %+v", got[0].Error)
	}
}
