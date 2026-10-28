// Package repository defines data access interfaces and models for the guestbook application.
package repository

import (
	"context"
	"testing"
	"time"
)

// TestMemorySessionStore tests in-memory session operations and expiration.
func TestMemorySessionStore(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	// Test Ping
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("store.Ping() error = %v", err)
	}

	user := SessionUser{
		ID:    1,
		Email: "mem@example.com",
		Name:  "Memory User",
	}

	sessionID := "mem-token"

	// Test Get nonexistent
	_, found, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("unexpected error on missing key: %v", err)
	}
	if found {
		t.Fatal("expected found = false for missing key")
	}

	// Test Set and Get
	if err := store.Set(ctx, sessionID, user, time.Hour); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}

	got, found, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !found {
		t.Fatal("expected session to be found")
	}
	if got != user {
		t.Errorf("store.Get() = %+v, want %+v", got, user)
	}

	// Test Delete
	if err := store.Delete(ctx, sessionID); err != nil {
		t.Fatalf("store.Delete() error = %v", err)
	}

	_, found, err = store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("store.Get() after delete error = %v", err)
	}
	if found {
		t.Fatal("expected session to be deleted")
	}

	// Test Expired
	expiredID := "expired-token"
	if err := store.Set(ctx, expiredID, user, -time.Second); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}
	_, found, err = store.Get(ctx, expiredID)
	if err != nil {
		t.Fatalf("store.Get() on expired session error = %v", err)
	}
	if found {
		t.Fatal("expected expired session not to be found")
	}
}
