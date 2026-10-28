// Package repository defines data access interfaces and models for the guestbook application.
package repository

import (
	"context"
	"time"
)

// SessionUser represents an authenticated user session.
type SessionUser struct {
	ID     int    `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// MessageDTO represents a guestbook entry returned to clients or templates.
type MessageDTO struct {
	ID        int         `json:"id"`
	Text      string      `json:"text"`
	CreatedAt time.Time   `json:"created_at"`
	User      SessionUser `json:"user"`
}

// Repository defines data access methods for the guestbook application.
type Repository interface {
	// SaveUser creates or updates a user by email and returns their session data.
	SaveUser(ctx context.Context, email, name, avatar string) (SessionUser, error)
	// CreateMessage inserts a new guest message into the database.
	CreateMessage(ctx context.Context, userID int, text string) error
	// GetMessages retrieves recent messages up to the specified limit.
	GetMessages(ctx context.Context, limit int) ([]MessageDTO, error)
	// Ping verifies connectivity to the underlying data store.
	Ping(ctx context.Context) error
}

// SessionStore defines data persistence operations for user sessions.
type SessionStore interface {
	// Set stores a user session for the given session ID with an expiration duration.
	Set(ctx context.Context, sessionID string, user SessionUser, ttl time.Duration) error
	// Get retrieves a user session by its session ID. Returns false if the session is not found or expired.
	Get(ctx context.Context, sessionID string) (SessionUser, bool, error)
	// Delete removes a user session by its session ID.
	Delete(ctx context.Context, sessionID string) error
	// Ping verifies connectivity to the underlying session store.
	Ping(ctx context.Context) error
}
