// Package main is the entry point for the meme-hub application server.
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

	"meme-hub/internal/broker"
	"meme-hub/internal/config"
	"meme-hub/internal/handler"
	"meme-hub/internal/mailer"
	"meme-hub/internal/moderation"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
	"meme-hub/internal/worker"
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

	var mailSender mailer.Sender = mailer.NewNopSender()
	if cfg.SMTP.Host != "" {
		mailSender = mailer.NewSMTPSender(
			cfg.SMTP.Host,
			cfg.SMTP.Port,
			cfg.SMTP.Username,
			cfg.SMTP.Password,
			cfg.SMTP.From,
		)
		slog.Info("initialized smtp mailer", "host", cfg.SMTP.Host, "port", cfg.SMTP.Port, "from", cfg.SMTP.From)
	}

	var searchService search.Service = search.NewNopService()
	zincClient, err := search.NewClientWithAuth(cfg.SearchURL, cfg.SearchUser, cfg.SearchPassword, search.DefaultIndex, nil)
	if err != nil {
		slog.Warn("could not initialize zincsearch client, using nop searcher", "err", err)
	} else {
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if pingErr := zincClient.Ping(pingCtx); pingErr != nil {
			slog.Warn("zincsearch ping failed, search will operate with fallback", "err", pingErr)
		} else {
			slog.Info("connected to zincsearch successfully", "url", cfg.SearchURL)
			searchService = zincClient
		}
		pingCancel()
	}

	// Asynchronously sync existing approved messages into search index on startup.
	go func() {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer syncCancel()
		messages, fetchErr := repo.GetMessages(syncCtx, 0, "fresh", 1000)
		if fetchErr == nil {
			for _, m := range messages {
				if m.Status == repository.MessageStatusApproved {
					_ = searchService.IndexMessage(syncCtx, search.Document{
						ID:        m.ID,
						UserID:    m.User.ID,
						Author:    m.User.Name,
						Avatar:    m.User.Avatar,
						Text:      m.Text,
						MediaURL:  m.MediaURL,
						Upvotes:   m.Upvotes,
						Downvotes: m.Downvotes,
						Score:     m.Score,
						CreatedAt: m.CreatedAt,
					})
				}
			}
		}
	}()

	var publisher broker.Publisher = broker.NewNopPublisher()
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	rabbitClient, err := broker.NewRabbitMQClient(cfg.RabbitMQURL)
	if err != nil {
		slog.Warn("could not connect to rabbitmq, using nop publisher", "err", err)
	} else {
		slog.Info("connected to rabbitmq successfully", "url", cfg.RabbitMQURL)
		publisher = rabbitClient
		defer func() {
			if closeErr := rabbitClient.Close(); closeErr != nil {
				slog.Error("failed to close rabbitmq connection", "err", closeErr)
			}
		}()

		moderator := moderation.NewRuleBasedModerator()
		moderationWorker := worker.NewModerationWorker(
			rabbitClient.Connection(),
			repo,
			moderator,
			rabbitClient,
			searchService,
			cfg.SMTP.AdminEmail,
			broker.QueueModeration,
		)

		go func() {
			if workerErr := moderationWorker.Start(workerCtx); workerErr != nil && !errors.Is(workerErr, context.Canceled) {
				slog.Error("moderation worker stopped unexpectedly", "err", workerErr)
			}
		}()

		emailWorker := worker.NewEmailWorker(
			rabbitClient.Connection(),
			mailSender,
			broker.QueueEmail,
		)

		go func() {
			if workerErr := emailWorker.Start(workerCtx); workerErr != nil && !errors.Is(workerErr, context.Canceled) {
				slog.Error("email worker stopped unexpectedly", "err", workerErr)
			}
		}()
	}

	tmpl, err := handler.NewTemplate("web/template/index.html")
	if err != nil {
		slog.Error("failed to load template", "err", err)
		os.Exit(1)
	}

	srv := handler.New(cfg, repo, tmpl, sessionStore, publisher, searchService)

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
		workerCancel()

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
