// Package main is the entry point for the guestbook application server.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"guestbook-app/internal/config"
	"guestbook-app/internal/handler"
	"guestbook-app/internal/repository"
)

const (
	maxOpenDBConns    = 25
	maxIdleDBConns    = 10
	connMaxLifetimeDB = 15 * time.Minute
	connMaxIdleTimeDB = 5 * time.Minute
	shutdownTimeout   = 10 * time.Second
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to load .env file", "err", err)
	}

	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database connection", "err", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("failed to close database connection", "err", closeErr)
		}
	}()

	db.SetMaxOpenConns(maxOpenDBConns)
	db.SetMaxIdleConns(maxIdleDBConns)
	db.SetConnMaxLifetime(connMaxLifetimeDB)
	db.SetConnMaxIdleTime(connMaxIdleTimeDB)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()

	if err := db.PingContext(pingCtx); err != nil {
		slog.Error("failed to ping database", "err", err)
		os.Exit(1)
	}

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("failed to parse redis url", "err", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(redisOpts)
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			slog.Error("failed to close redis client", "err", closeErr)
		}
	}()

	redisPingCtx, redisPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisPingCancel()

	if err := redisClient.Ping(redisPingCtx).Err(); err != nil {
		slog.Error("failed to ping redis", "err", err)
		os.Exit(1)
	}

	repo := repository.NewPostgresRepository(db)
	sessionStore := repository.NewRedisSessionStore(redisClient)

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer migrateCancel()

	if err := repo.Migrate(migrateCtx); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}

	tmpl, err := handler.NewTemplate("web/template/index.html")
	if err != nil {
		slog.Error("failed to load template", "err", err)
		os.Exit(1)
	}

	srv := handler.New(cfg, repo, tmpl, sessionStore)

	httpServer := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      srv.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Listen for OS interrupt or termination signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.Addr())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		slog.Error("server encountered fatal error", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutdown signal received, commencing graceful shutdown")
		stop()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful server shutdown failed, forcing close", "err", err)
			if closeErr := httpServer.Close(); closeErr != nil {
				slog.Error("error forcing server close", "err", closeErr)
			}
			os.Exit(1)
		}
		slog.Info("server shutdown gracefully completed")
	}
}
