// Package worker implements asynchronous background message consumers and processors.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"meme-hub/internal/broker"
	"meme-hub/internal/moderation"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
)

// ModerationWorker consumes guestbook messages from RabbitMQ and applies content moderation.
type ModerationWorker struct {
	conn       *amqp.Connection
	repo       repository.Repository
	moderator  moderation.Service
	publisher  broker.Publisher
	searcher   search.Service
	adminEmail string
	queueName  string
}

// NewModerationWorker creates a new ModerationWorker instance.
func NewModerationWorker(
	conn *amqp.Connection,
	repo repository.Repository,
	moderator moderation.Service,
	publisher broker.Publisher,
	searcher search.Service,
	adminEmail string,
	queueName string,
) *ModerationWorker {
	return &ModerationWorker{
		conn:       conn,
		repo:       repo,
		moderator:  moderator,
		publisher:  publisher,
		searcher:   searcher,
		adminEmail: adminEmail,
		queueName:  queueName,
	}
}

// Start opens a consumer channel, sets prefetch limits, and listens for moderation tasks until context cancellation.
func (w *ModerationWorker) Start(ctx context.Context) error {
	if w.conn == nil {
		return errors.New("cannot start moderation worker: rabbitmq connection is nil")
	}

	ch, err := w.conn.Channel()
	if err != nil {
		return fmt.Errorf("opening worker consumer channel: %w", err)
	}
	defer func() {
		if closeErr := ch.Close(); closeErr != nil && !errors.Is(closeErr, amqp.ErrClosed) {
			slog.Error("error closing worker channel", "err", closeErr)
		}
	}()

	if err := ch.Qos(10, 0, false); err != nil {
		return fmt.Errorf("setting worker qos: %w", err)
	}

	deliveries, err := ch.Consume(
		w.queueName,
		"guestbook-moderation-worker",
		false, // auto-ack disabled: manual ack/nack for reliability
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("starting rabbitmq consumer: %w", err)
	}

	slog.Info("moderation worker started listening for tasks", "queue", w.queueName)

	for {
		select {
		case <-ctx.Done():
			slog.Info("moderation worker stopping due to context cancellation")
			return nil
		case d, ok := <-deliveries:
			if !ok {
				slog.Info("moderation worker deliveries channel closed")
				return nil
			}
			w.handleDelivery(ctx, d)
		}
	}
}

// handleDelivery handles a single AMQP delivery, performs moderation, updates the database, and acknowledges or rejects the message.
func (w *ModerationWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	var task broker.ModerationTask
	if err := json.Unmarshal(d.Body, &task); err != nil {
		slog.Error("malformed moderation task json, rejecting to dead letter queue", "err", err)
		_ = d.Nack(false, false)
		return
	}

	approved, err := w.ProcessTask(ctx, task)
	if err != nil {
		slog.Error("error updating database for moderation task, requeuing for retry", "err", err, "messageID", task.MessageID)
		_ = d.Nack(false, true)
		return
	}

	if approved {
		if err := d.Ack(false); err != nil {
			slog.Error("error acknowledging approved message", "err", err, "messageID", task.MessageID)
		}
		slog.Info("message approved and acknowledged", "messageID", task.MessageID)
	} else {
		// Reject without requeue: RabbitMQ routes it to the Dead Letter Queue (DLQ) via DLX.
		if err := d.Nack(false, false); err != nil {
			slog.Error("error rejecting message to dead letter queue", "err", err, "messageID", task.MessageID)
		}
		slog.Warn("message rejected and dispatched to dead letter queue", "messageID", task.MessageID)
	}
}

// ProcessTask runs moderation checks on a task and persists the updated status in the repository.
func (w *ModerationWorker) ProcessTask(ctx context.Context, task broker.ModerationTask) (bool, error) {
	result := w.moderator.Moderate(task.Text)
	if result.Approved {
		if err := w.repo.UpdateMessageStatus(ctx, task.MessageID, repository.MessageStatusApproved, ""); err != nil {
			return false, fmt.Errorf("marking message approved: %w", err)
		}
		if w.searcher != nil {
			doc := search.Document{
				ID:        task.MessageID,
				UserID:    task.UserID,
				Author:    task.AuthorName,
				Avatar:    task.Avatar,
				Text:      task.Text,
				MediaURL:  task.MediaURL,
				CreatedAt: task.CreatedAt,
			}
			if idxErr := w.searcher.IndexMessage(ctx, doc); idxErr != nil {
				slog.ErrorContext(ctx, "failed to index approved message in elasticsearch", "err", idxErr, "messageID", task.MessageID)
			}
		}
		if w.publisher != nil && w.adminEmail != "" {
			emailTask := broker.EmailTask{
				To:        []string{w.adminEmail},
				Subject:   fmt.Sprintf("New Guestbook Message Approved (ID: %d)", task.MessageID),
				TextBody:  fmt.Sprintf("A new guestbook entry was approved.\n\nAuthor User ID: %d\nMessage: %s", task.UserID, task.Text),
				HTMLBody:  fmt.Sprintf("<h2>New Guestbook Entry Approved</h2><p><strong>Author User ID:</strong> %d</p><p><strong>Message:</strong></p><blockquote>%s</blockquote>", task.UserID, template.HTMLEscapeString(task.Text)),
				CreatedAt: time.Now().UTC(),
			}
			if pubErr := w.publisher.PublishEmailTask(ctx, emailTask); pubErr != nil {
				slog.ErrorContext(ctx, "failed to publish admin email for approved message", "err", pubErr, "messageID", task.MessageID)
			}
		}
		return true, nil
	}

	if err := w.repo.UpdateMessageStatus(ctx, task.MessageID, repository.MessageStatusRejected, result.Reason); err != nil {
		return false, fmt.Errorf("marking message rejected: %w", err)
	}

	if w.publisher != nil && w.adminEmail != "" {
		emailTask := broker.EmailTask{
			To:        []string{w.adminEmail},
			Subject:   fmt.Sprintf("[Spam Alert] Guestbook Message Rejected (ID: %d)", task.MessageID),
			TextBody:  fmt.Sprintf("A guestbook entry was rejected during moderation.\n\nAuthor User ID: %d\nReason: %s\nMessage: %s", task.UserID, result.Reason, task.Text),
			HTMLBody:  fmt.Sprintf("<h2 style=\"color: red;\">Spam Message Rejected</h2><p><strong>Author User ID:</strong> %d</p><p><strong>Reason:</strong> %s</p><p><strong>Message Content:</strong></p><blockquote>%s</blockquote>", task.UserID, template.HTMLEscapeString(result.Reason), template.HTMLEscapeString(task.Text)),
			CreatedAt: time.Now().UTC(),
		}
		if pubErr := w.publisher.PublishEmailTask(ctx, emailTask); pubErr != nil {
			slog.ErrorContext(ctx, "failed to publish admin spam alert email", "err", pubErr, "messageID", task.MessageID)
		}
	}

	return false, nil
}
