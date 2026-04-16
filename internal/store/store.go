package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" driver with database/sql
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	DB          *sql.DB
	Users       *Users
	Sessions    *Sessions
	Connections *Connections
	History     *History
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open app store: %w", err)
	}
	db.SetMaxOpenConns(1) // sqlite + WAL: single writer is simplest & safe
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	s := &Store{DB: db}
	s.Users = &Users{db: db}
	s.Sessions = &Sessions{db: db}
	s.Connections = &Connections{db: db}
	s.History = &History{db: db}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }
