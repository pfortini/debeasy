package config

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Addr      string
	DataDir   string
	AppSecret []byte

	// UpdateCheckEnabled controls the in-server background poll of GitHub
	// releases. Set DEBEASY_UPDATE_CHECK=0 to disable.
	UpdateCheckEnabled bool
	// UpdateCheckInterval is how often the server polls for a newer release.
	// Parsed from DEBEASY_UPDATE_INTERVAL (e.g. "24h", "1h"), default 24h.
	UpdateCheckInterval time.Duration
	// UpdateRepo is the GitHub "owner/repo" we poll. Override via
	// DEBEASY_UPDATE_REPO for forks.
	UpdateRepo string
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

	c.UpdateCheckEnabled = envOr("DEBEASY_UPDATE_CHECK", "1") != "0"
	c.UpdateCheckInterval = parseDurationOr(os.Getenv("DEBEASY_UPDATE_INTERVAL"), 24*time.Hour)
	c.UpdateRepo = envOr("DEBEASY_UPDATE_REPO", "pfortini/debeasy")
	return c, nil
}

// parseDurationOr returns time.ParseDuration(s) when it parses to something
// positive, otherwise def. An empty or malformed value silently falls back so
// a typo in DEBEASY_UPDATE_INTERVAL doesn't keep the server from booting.
func parseDurationOr(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
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
