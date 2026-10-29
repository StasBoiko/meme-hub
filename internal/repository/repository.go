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

// MessageStatus defines the moderation state of a guestbook entry.
type MessageStatus string

const (
	// MessageStatusPending indicates the message is awaiting moderation.
	MessageStatusPending MessageStatus = "pending"
	// MessageStatusApproved indicates the message was approved by the moderator.
	MessageStatusApproved MessageStatus = "approved"
	// MessageStatusRejected indicates the message was rejected by the moderator.
	MessageStatusRejected MessageStatus = "rejected"
)

// CommentDTO represents a user comment on a meme or guestbook message.
type CommentDTO struct {
	ID        int         `json:"id"`
	MessageID int         `json:"message_id"`
	Text      string      `json:"text"`
	CreatedAt time.Time   `json:"created_at"`
	User      SessionUser `json:"user"`
}

// MessageDTO represents a guestbook entry returned to clients or templates.
type MessageDTO struct {
	ID              int           `json:"id"`
	Text            string        `json:"text"`
	MediaURL        string        `json:"media_url,omitempty"`
	Upvotes         int           `json:"upvotes"`
	Downvotes       int           `json:"downvotes"`
	Score           int           `json:"score"`
	UserVote        int           `json:"user_vote"`
	CommentsCount   int           `json:"comments_count"`
	Comments        []CommentDTO  `json:"comments,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	Status          MessageStatus `json:"status"`
	RejectionReason string        `json:"rejection_reason,omitempty"`
	User            SessionUser   `json:"user"`
}

// VoteResult represents updated vote statistics after a user vote operation.
type VoteResult struct {
	MessageID int `json:"message_id"`
	Upvotes   int `json:"upvotes"`
	Downvotes int `json:"downvotes"`
	Score     int `json:"score"`
	UserVote  int `json:"user_vote"`
}

// Repository defines data access methods for the guestbook application.
type Repository interface {
	// SaveUser creates or updates a user by email and returns their session data.
	SaveUser(ctx context.Context, email, name, avatar string) (SessionUser, error)
	// CreateMessage inserts a new guest message into the database and returns its ID.
	CreateMessage(ctx context.Context, userID int, text string, mediaURL string) (int, error)
	// UpdateMessageStatus updates the moderation status and optional rejection reason for a message.
	UpdateMessageStatus(ctx context.Context, messageID int, status MessageStatus, reason string) error
	// VoteMessage applies, toggles, or updates a user vote (+1 or -1) on a message.
	VoteMessage(ctx context.Context, messageID int, userID int, value int) (VoteResult, error)
	// GetMessages retrieves messages matching the sorting criterion (e.g. "hot", "fresh") with user vote status.
	GetMessages(ctx context.Context, currentUserID int, sortBy string, limit int) ([]MessageDTO, error)
	// CreateComment inserts a new user comment under a message and returns the comment DTO.
	CreateComment(ctx context.Context, messageID int, userID int, text string) (CommentDTO, error)
	// GetCommentsByMessageIDs retrieves all comments for the specified message IDs grouped by message ID.
	GetCommentsByMessageIDs(ctx context.Context, messageIDs []int) (map[int][]CommentDTO, error)
	// GetCommentsByMessageID retrieves all comments for a specific message ordered chronologically.
	GetCommentsByMessageID(ctx context.Context, messageID int) ([]CommentDTO, error)
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
