package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lmittmann/tint"

	"usahainaja/backend/db/migrations"
	"usahainaja/backend/internal/app"
	"usahainaja/backend/internal/config"
	"usahainaja/backend/internal/httpapi"
	"usahainaja/backend/internal/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	var logHandler slog.Handler
	if cfg.Env == "production" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		logHandler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.TimeOnly,
		})
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Error("parse database configuration", "error", err)
		os.Exit(1)
	}
	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(rootCtx, poolConfig)
	if err != nil {
		logger.Error("create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	startupCtx, cancelStartup := context.WithTimeout(rootCtx, 30*time.Second)
	defer cancelStartup()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("connect to database", "error", err)
		os.Exit(1)
	}
	if err := migrations.Up(startupCtx, pool); err != nil {
		logger.Error("apply database migrations", "error", err)
		os.Exit(1)
	}

	repository := postgres.New(pool)
	service := app.NewService(repository, cfg.SessionTTL, cfg.BcryptCost)
	handler := httpapi.New(service, cfg.CookieName, cfg.CookieSecure)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("API listening", "address", cfg.HTTPAddr, "cookie_secure", cfg.CookieSecure)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve API", "error", err)
			os.Exit(1)
		}
	case <-rootCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}
}
