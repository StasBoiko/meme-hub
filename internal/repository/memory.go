// Package repository defines data access interfaces and models for the meme-hub application.
package repository

import (
	"context"
	"sync"
	"time"
)

// memorySessionEntry holds a cached session user and expiration timestamp.
type memorySessionEntry struct {
	user      SessionUser
	expiresAt time.Time
}

// MemorySessionStore implements SessionStore in memory with thread-safe locking.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]memorySessionEntry
}

// NewMemorySessionStore creates a new MemorySessionStore instance.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]memorySessionEntry),
	}
}

// Set stores a user session for the given session ID with an expiration duration.
func (m *MemorySessionStore) Set(ctx context.Context, sessionID string, user SessionUser, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = memorySessionEntry{
		user:      user,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// Get retrieves a user session by its session ID. Returns false if not found or expired.
func (m *MemorySessionStore) Get(ctx context.Context, sessionID string) (SessionUser, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.sessions[sessionID]
	if !ok || time.Now().After(entry.expiresAt) {
		return SessionUser{}, false, nil
	}
	return entry.user, true, nil
}

// Delete removes a user session by its session ID.
func (m *MemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	return nil
}

// Ping verifies connectivity to the in-memory session store.
func (m *MemorySessionStore) Ping(ctx context.Context) error {
	return nil
}

// ClearAll removes all active user sessions from memory.
func (m *MemorySessionStore) ClearAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]memorySessionEntry)
	return nil
}
