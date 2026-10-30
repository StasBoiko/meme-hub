// Package main provides the command-line interface entry point for seeding and resetting test data.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"meme-hub/internal/config"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
	"meme-hub/internal/seed"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not load .env file", "err", err)
	}

	cleanFlag := flag.Bool("clean", false, "Wipe all test data from database and search index")
	resetFlag := flag.Bool("reset", false, "Wipe existing data and re-seed from scratch")
	flag.Parse()

	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if pingErr := db.PingContext(ctx); pingErr != nil {
		slog.Error("failed to ping database", "err", pingErr)
		os.Exit(1)
	}

	var searchService search.Service = search.NewNopService()
	zincClient, err := search.NewClientWithAuth(cfg.SearchURL, cfg.SearchUser, cfg.SearchPassword, search.DefaultIndex, nil)
	if err != nil {
		slog.Warn("could not initialize zincsearch client, seeding will skip search indexing", "err", err)
	} else {
		pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
		if pingErr := zincClient.Ping(pingCtx); pingErr != nil {
			slog.Warn("zincsearch is unreachable, seeding will skip search indexing", "err", pingErr)
		} else {
			searchService = zincClient
		}
		pingCancel()
	}

	var sessionStore repository.SessionStore
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Warn("could not parse redis url for seeder", "err", err)
	} else {
		redisClient := redis.NewClient(redisOpts)
		defer func() {
			_ = redisClient.Close()
		}()
		sessionStore = repository.NewRedisSessionStore(redisClient)
	}

	seeder := seed.NewSeeder(db, searchService, sessionStore)

	if *cleanFlag {
		slog.Info("cleaning all seed data from database and search index...")
		if cleanErr := seeder.Clean(ctx); cleanErr != nil {
			slog.Error("clean operation failed", "err", cleanErr)
			os.Exit(1)
		}
		slog.Info("clean operation successfully completed")
		return
	}

	if *resetFlag {
		slog.Info("resetting and re-seeding test data...")
		summary, resetErr := seeder.Reset(ctx)
		if resetErr != nil {
			slog.Error("reset operation failed", "err", resetErr)
			os.Exit(1)
		}
		printSummary("Reset & Seed", summary)
		return
	}

	slog.Info("seeding test data into database and search index...")
	summary, seedErr := seeder.Seed(ctx)
	if seedErr != nil {
		slog.Error("seed operation failed", "err", seedErr)
		os.Exit(1)
	}
	printSummary("Seed", summary)
}

func printSummary(action string, s *seed.Summary) {
	fmt.Printf("\n✨ %s Completed Successfully!\n", action)
	fmt.Printf("   ├─ Users created:        %d\n", s.UsersCount)
	fmt.Printf("   ├─ Memes created:        %d\n", s.MemesCount)
	fmt.Printf("   ├─ Comments created:     %d\n", s.CommentsCount)
	fmt.Printf("   ├─ Votes created:        %d\n", s.VotesCount)
	fmt.Printf("   └─ Search docs indexed:  %d\n\n", s.SearchIndexed)
}
