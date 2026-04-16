package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/pfortini/debeasy/internal/crypto"
)

type Connection struct {
	ID        int64
	Name      string
	Kind      string // postgres | mysql | sqlite
	Host      string
	Port      int
	Username  string
	Password  string // plaintext, only set transiently
	Database  string
	SSLMode   string
	Params    string
	CreatedBy sql.NullInt64
}

type Connections struct {
	db      *sql.DB
	Keyring *crypto.Keyring
}

var ErrConnNotFound = errors.New("connection not found")

func (s *Connections) WithKeyring(k *crypto.Keyring) *Connections {
	s.Keyring = k
	return s
}

func (s *Connections) Create(ctx context.Context, c *Connection, createdBy int64) (*Connection, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return nil, errors.New("name required")
	}
	if !validKind(c.Kind) {
		return nil, errors.New("invalid kind")
	}
	var encPwd []byte
	if c.Password != "" && s.Keyring != nil {
		var err error
		encPwd, err = s.Keyring.Seal([]byte(c.Password))
		if err != nil {
			return nil, err
		}
	}
	// A zero createdBy is treated as "unknown user" — store NULL so the FK holds.
	var createdByVal any
	if createdBy > 0 {
		createdByVal = createdBy
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO connections(name, kind, host, port, username, password_enc, database, sslmode, params, created_by)
         VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Kind, c.Host, c.Port, c.Username, encPwd, c.Database, c.SSLMode, c.Params, createdByVal)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("a connection with that name already exists")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

func (s *Connections) Update(ctx context.Context, c *Connection, updatePassword bool) error {
	c.Name = strings.TrimSpace(c.Name)
	if !validKind(c.Kind) {
		return errors.New("invalid kind")
	}
	if updatePassword {
		var encPwd []byte
		if c.Password != "" && s.Keyring != nil {
			var err error
			encPwd, err = s.Keyring.Seal([]byte(c.Password))
			if err != nil {
				return err
			}
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE connections SET name=?, kind=?, host=?, port=?, username=?, password_enc=?, database=?, sslmode=?, params=? WHERE id=?`,
			c.Name, c.Kind, c.Host, c.Port, c.Username, encPwd, c.Database, c.SSLMode, c.Params, c.ID)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE connections SET name=?, kind=?, host=?, port=?, username=?, database=?, sslmode=?, params=? WHERE id=?`,
		c.Name, c.Kind, c.Host, c.Port, c.Username, c.Database, c.SSLMode, c.Params, c.ID)
	return err
}

func (s *Connections) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM connections WHERE id=?`, id)
	return err
}

func (s *Connections) Get(ctx context.Context, id int64) (*Connection, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, host, port, username, password_enc, database, sslmode, params, created_by
         FROM connections WHERE id=?`, id)
	return s.scan(row)
}

func (s *Connections) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, host, port, username, password_enc, database, sslmode, params, created_by
         FROM connections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Connections) scan(r rowScanner) (*Connection, error) {
	var c Connection
	var encPwd []byte
	if err := r.Scan(&c.ID, &c.Name, &c.Kind, &c.Host, &c.Port, &c.Username, &encPwd, &c.Database, &c.SSLMode, &c.Params, &c.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConnNotFound
		}
		return nil, err
	}
	if len(encPwd) > 0 && s.Keyring != nil {
		pt, err := s.Keyring.Open(encPwd)
		if err == nil {
			c.Password = string(pt)
		}
	}
	return &c, nil
}

func validKind(k string) bool {
	switch k {
	case "postgres", "mysql", "sqlite":
		return true
	}
	return false
}
