package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Session struct {
	ID        string
	UserID    int64
	CSRFToken string
	ExpiresAt time.Time
}

type Sessions struct{ db *sql.DB }

var ErrSessionNotFound = errors.New("session not found")

func (s *Sessions) Create(ctx context.Context, userID int64, ttl time.Duration) (*Session, error) {
	id, err := randHex(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randHex(24)
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(ttl).UTC()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions(id, user_id, csrf_token, expires_at) VALUES(?, ?, ?, ?)`,
		id, userID, csrf, exp.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &Session{ID: id, UserID: userID, CSRFToken: csrf, ExpiresAt: exp}, nil
}

func (s *Sessions) Get(ctx context.Context, id string) (*Session, error) {
	var sess Session
	var exp string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, csrf_token, expires_at FROM sessions WHERE id=?`, id).
		Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &exp)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		return nil, err
	}
	if time.Now().After(t) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
		return nil, ErrSessionNotFound
	}
	sess.ExpiresAt = t
	return &sess, nil
}

func (s *Sessions) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	return err
}

func (s *Sessions) PurgeExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
