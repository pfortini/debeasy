package config

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Addr      string
	DataDir   string
	AppSecret []byte
}

func Load() (*Config, error) {
	c := &Config{}
	flag.StringVar(&c.Addr, "addr", envOr("DEBEASY_ADDR", ":8080"), "HTTP listen address")
	flag.StringVar(&c.DataDir, "data-dir", envOr("DEBEASY_DATA_DIR", defaultDataDir()), "directory for app SQLite store + secret")
	flag.Parse()

	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir data-dir: %w", err)
	}
	secret, err := loadOrCreateSecret(filepath.Join(c.DataDir, "secret"))
	if err != nil {
		return nil, err
	}
	c.AppSecret = secret
	return c, nil
}

func (c *Config) StorePath() string { return filepath.Join(c.DataDir, "debeasy.sqlite") }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".debeasy")
	}
	return ".debeasy"
}

func loadOrCreateSecret(path string) ([]byte, error) {
	if env := os.Getenv("DEBEASY_APP_SECRET"); env != "" {
		b, err := hex.DecodeString(env)
		if err != nil || len(b) != 32 {
			return nil, fmt.Errorf("DEBEASY_APP_SECRET must be 64 hex chars (32 bytes)")
		}
		return b, nil
	}
	if b, err := os.ReadFile(path); err == nil {
		out, err := hex.DecodeString(string(b))
		if err != nil || len(out) != 32 {
			return nil, fmt.Errorf("corrupt secret file %s", path)
		}
		return out, nil
	}
	out := make([]byte, 32)
	if _, err := rand.Read(out); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(out)), 0o600); err != nil {
		return nil, fmt.Errorf("write secret: %w", err)
	}
	return out, nil
}
