package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultCookieName      = "usahainaja_session"
	defaultSessionTTL      = 7 * 24 * time.Hour
	defaultShutdownTimeout = 10 * time.Second
	defaultBcryptCost      = 12
)

type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	CookieName      string
	CookieSecure    bool
	SessionTTL      time.Duration
	ShutdownTimeout time.Duration
	BcryptCost      int
}

func Load() (Config, error) {
	_ = godotenv.Load(".env", "../.env", "../../.env")
	cfg := Config{
		Env:             envOrDefault("ENV", "development"),
		HTTPAddr:        envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		CookieName:      envOrDefault("COOKIE_NAME", defaultCookieName),
		SessionTTL:      defaultSessionTTL,
		ShutdownTimeout: defaultShutdownTimeout,
		BcryptCost:      defaultBcryptCost,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	var err error
	if raw := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); raw != "" {
		cfg.CookieSecure, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false: %w", err)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SESSION_TTL")); raw != "" {
		cfg.SessionTTL, err = time.ParseDuration(raw)
		if err != nil || cfg.SessionTTL <= 0 {
			return Config{}, errors.New("SESSION_TTL must be a positive Go duration")
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT")); raw != "" {
		cfg.ShutdownTimeout, err = time.ParseDuration(raw)
		if err != nil || cfg.ShutdownTimeout <= 0 {
			return Config{}, errors.New("SHUTDOWN_TIMEOUT must be a positive Go duration")
		}
	}
	if raw := strings.TrimSpace(os.Getenv("BCRYPT_COST")); raw != "" {
		cfg.BcryptCost, err = strconv.Atoi(raw)
		if err != nil || cfg.BcryptCost < 4 || cfg.BcryptCost > 31 {
			return Config{}, errors.New("BCRYPT_COST must be between 4 and 31")
		}
	}
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return Config{}, errors.New("HTTP_ADDR cannot be empty")
	}
	if strings.TrimSpace(cfg.CookieName) == "" {
		return Config{}, errors.New("COOKIE_NAME cannot be empty")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
