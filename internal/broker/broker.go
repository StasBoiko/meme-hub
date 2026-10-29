// Package broker defines messaging abstractions, payloads, and RabbitMQ integration for the application.
package broker

import (
	"context"
	"time"
)

// ModerationTask represents the payload dispatched to the broker for asynchronous moderation.
type ModerationTask struct {
	MessageID  int       `json:"message_id"`
	UserID     int       `json:"user_id"`
	AuthorName string    `json:"author_name"`
	Avatar     string    `json:"avatar"`
	Text       string    `json:"text"`
	MediaURL   string    `json:"media_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// EmailTask represents an email notification task payload dispatched to the message broker.
type EmailTask struct {
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	TextBody  string    `json:"text_body"`
	HTMLBody  string    `json:"html_body"`
	CreatedAt time.Time `json:"created_at"`
}

// Publisher defines message publishing operations.
type Publisher interface {
	// PublishModerationTask publishes a moderation task to the message broker.
	PublishModerationTask(ctx context.Context, task ModerationTask) error
	// PublishEmailTask publishes an email notification task to the message broker.
	PublishEmailTask(ctx context.Context, task EmailTask) error
	// Close gracefully closes the publisher connection or channel.
	Close() error
}

// NopPublisher implements Publisher with a no-op behavior, useful in tests and offline environments.
type NopPublisher struct{}

// NewNopPublisher creates a new NopPublisher instance.
func NewNopPublisher() *NopPublisher {
	return &NopPublisher{}
}

// PublishModerationTask performs a no-op message dispatch.
func (p *NopPublisher) PublishModerationTask(ctx context.Context, task ModerationTask) error {
	return nil
}

// PublishEmailTask performs a no-op email message dispatch.
func (p *NopPublisher) PublishEmailTask(ctx context.Context, task EmailTask) error {
	return nil
}

// Close performs a no-op connection cleanup.
func (p *NopPublisher) Close() error {
	return nil
}
