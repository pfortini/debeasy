package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID        int64
	Username  string
	Role      string
	Disabled  bool
	CreatedAt string
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

type Users struct{ db *sql.DB }

var (
	ErrUserExists  = errors.New("user already exists")
	ErrBadPassword = errors.New("invalid credentials")
	ErrDisabled    = errors.New("user is disabled")
)

// BcryptCost is the cost factor used when hashing passwords. Tests override it
// to bcrypt.MinCost via init() in test-only files — hashing at cost 12 otherwise
// dominates test runtime under -race.
var BcryptCost = 12

func (s *Users) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Users) Create(ctx context.Context, username, password, role string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 8 {
		return nil, errors.New("username required and password >= 8 chars")
	}
	if role != "admin" && role != "user" {
		role = "user"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role) VALUES(?, ?, ?)`,
		username, string(hash), role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrUserExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

func (s *Users) Get(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, role, disabled, created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Users) FindByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, role, disabled, created_at FROM users WHERE username=? COLLATE NOCASE`, username)
	return scanUser(row)
}

func (s *Users) Verify(ctx context.Context, username, password string) (*User, error) {
	var hash string
	var u User
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, disabled, created_at FROM users WHERE username=? COLLATE NOCASE`,
		username)
	if err := row.Scan(&u.ID, &u.Username, &hash, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// run a fake compare to keep timing similar
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$00000000000000000000000000000000000000000000000000000"), []byte(password))
			return nil, ErrBadPassword
		}
		return nil, err
	}
	if u.Disabled {
		return nil, ErrDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrBadPassword
	}
	return &u, nil
}

func (s *Users) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, disabled, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Users) SetDisabled(ctx context.Context, id int64, disabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled=? WHERE id=?`, disabled, id)
	return err
}

func (s *Users) ResetPassword(ctx context.Context, id int64, password string) error {
	if len(password) < 8 {
		return errors.New("password must be >= 8 chars")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, string(hash), id)
	return err
}

type rowScanner interface {
	Scan(...any) error
}

func scanUser(r rowScanner) (*User, error) {
	var u User
	if err := r.Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}
