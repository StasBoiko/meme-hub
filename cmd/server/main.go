// Package main is the entry point for the guestbook application server.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"guestbook-app/internal/config"
	"guestbook-app/internal/handler"
	"guestbook-app/internal/repository"
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
	defer db.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()

	if err := db.PingContext(pingCtx); err != nil {
		slog.Error("failed to ping database", "err", err)
		os.Exit(1)
	}

	repo := repository.NewPostgresRepository(db)

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

	srv := handler.New(cfg, repo, tmpl)

	httpServer := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      srv.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("server listening", "addr", cfg.Addr())
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server encountered fatal error", "err", err)
		os.Exit(1)
	}
}
