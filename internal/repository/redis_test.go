// Package repository defines data access interfaces and models for the meme-hub application.
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestRedisSessionStore tests CRUD operations and TTL expiration using an in-memory Redis instance.
func TestRedisSessionStore(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	store := NewRedisSessionStore(client)
	ctx := context.Background()

	// Test Ping
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("store.Ping() error = %v", err)
	}

	user := SessionUser{
		ID:     42,
		Email:  "redis_user@example.com",
		Name:   "Redis User",
		Avatar: "https://example.com/avatar.png",
	}

	sessionID := "test-session-token"
	ttl := 2 * time.Hour

	// Test Get on nonexistent key
	_, found, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("unexpected error getting nonexistent session: %v", err)
	}
	if found {
		t.Fatalf("expected found = false for nonexistent session")
	}

	// Test Set
	if err := store.Set(ctx, sessionID, user, ttl); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}

	// Test Get on existing key
	gotUser, found, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !found {
		t.Fatal("expected session to be found")
	}
	if gotUser != user {
		t.Errorf("store.Get() = %+v, want %+v", gotUser, user)
	}

	// Test Delete
	if err := store.Delete(ctx, sessionID); err != nil {
		t.Fatalf("store.Delete() error = %v", err)
	}

	// Test Get after Delete
	_, found, err = store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("store.Get() after delete error = %v", err)
	}
	if found {
		t.Fatal("expected session to be deleted")
	}

	// Test Expiration with FastForward
	expiringID := "expiring-token"
	if err := store.Set(ctx, expiringID, user, 10*time.Minute); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}
	mr.FastForward(11 * time.Minute)

	_, found, err = store.Get(ctx, expiringID)
	if err != nil {
		t.Fatalf("store.Get() after expiry error = %v", err)
	}
	if found {
		t.Fatal("expected expired session not to be found")
	}
}
