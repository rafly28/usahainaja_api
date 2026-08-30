package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("COOKIE_NAME", "")
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("BCRYPT_COST", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.CookieName != "usahainaja_session" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.SessionTTL != 7*24*time.Hour || cfg.BcryptCost != 12 {
		t.Fatalf("unexpected security defaults: %#v", cfg)
	}
}

func TestLoadRejectsInvalidSecurityConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("COOKIE_SECURE", "sometimes")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject an invalid COOKIE_SECURE")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should require DATABASE_URL")
	}
}
