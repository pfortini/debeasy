package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pfortini/debeasy/internal/store"
)

// runAdmin handles `debeasy admin <subcommand> ...`.
//
// Subcommands:
//
//	create         create a user (defaults role=admin) — used by the installer
//	reset-password reset an existing user's password
//
// Both honour --data-dir / DEBEASY_DATA_DIR so they touch the same SQLite store
// the server uses. Passwords come from --password (insecure: visible in argv) or
// --password-stdin (read from stdin, no echo).
func runAdmin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: debeasy admin <create|reset-password> [flags]")
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "create":
		return runAdminCreate(rest)
	case "reset-password":
		return runAdminResetPassword(rest)
	case "-h", "--help":
		fmt.Println("subcommands: create, reset-password")
		return nil
	default:
		return fmt.Errorf("unknown admin subcommand %q", sub)
	}
}

func runAdminCreate(args []string) error {
	fs := flag.NewFlagSet("admin create", flag.ExitOnError)
	dataDir := fs.String("data-dir", adminDefaultDataDir(), "app data dir (overrides DEBEASY_DATA_DIR)")
	username := fs.String("username", "", "username")
	password := fs.String("password", "", "password (insecure — prefer --password-stdin)")
	passwordStdin := fs.Bool("password-stdin", false, "read password from stdin")
	role := fs.String("role", "admin", "role: admin | user")
	ifNotExists := fs.Bool("if-not-exists", false, "exit 0 silently if any admin already exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("--username is required")
	}
	if *role != "admin" && *role != "user" {
		return errors.New("--role must be 'admin' or 'user'")
	}

	pwd, err := readPassword(*password, *passwordStdin)
	if err != nil {
		return err
	}
	if len(pwd) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	st, err := openStoreForCLI(*dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	if *ifNotExists {
		users, err := st.Users.List(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Role == "admin" {
				fmt.Printf("admin already exists (%s) — skipping\n", u.Username)
				return nil
			}
		}
	}

	u, err := st.Users.Create(ctx, *username, pwd, *role)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			return fmt.Errorf("user %q already exists — use `debeasy admin reset-password` to change their password", *username)
		}
		return err
	}
	fmt.Printf("created user id=%d username=%s role=%s\n", u.ID, u.Username, u.Role)
	return nil
}

func runAdminResetPassword(args []string) error {
	fs := flag.NewFlagSet("admin reset-password", flag.ExitOnError)
	dataDir := fs.String("data-dir", adminDefaultDataDir(), "app data dir")
	username := fs.String("username", "", "username")
	password := fs.String("password", "", "new password (insecure — prefer --password-stdin)")
	passwordStdin := fs.Bool("password-stdin", false, "read password from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("--username is required")
	}
	pwd, err := readPassword(*password, *passwordStdin)
	if err != nil {
		return err
	}
	if len(pwd) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	st, err := openStoreForCLI(*dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	u, err := st.Users.FindByUsername(ctx, *username)
	if err != nil {
		return fmt.Errorf("user %q not found: %w", *username, err)
	}
	if err := st.Users.ResetPassword(ctx, u.ID, pwd); err != nil {
		return err
	}
	fmt.Printf("password reset for user %s (id=%d)\n", u.Username, u.ID)
	return nil
}

// openStoreForCLI mirrors what config.Load does for the store path, without invoking
// the global flag.Parse (which would conflict with subcommand FlagSets).
func openStoreForCLI(dataDir string) (*store.Store, error) {
	if dataDir == "" {
		return nil, errors.New("data dir not set (use --data-dir or DEBEASY_DATA_DIR)")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir data-dir: %w", err)
	}
	// Touch the secret file to keep parity with normal startup — keeps perms tight
	// and avoids a confusing missing-file state if the server is started later.
	secretPath := filepath.Join(dataDir, "secret")
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		if err := os.WriteFile(secretPath, []byte(hex.EncodeToString(buf)), 0o600); err != nil {
			return nil, err
		}
	}
	return store.Open(filepath.Join(dataDir, "debeasy.sqlite"))
}

func readPassword(literal string, fromStdin bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return literal, nil
}

func adminDefaultDataDir() string {
	if v := os.Getenv("DEBEASY_DATA_DIR"); v != "" {
		return v
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".debeasy")
	}
	return ".debeasy"
}
