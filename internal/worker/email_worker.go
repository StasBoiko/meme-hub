// Package worker implements asynchronous background message consumers and processors.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"

	"meme-hub/internal/broker"
	"meme-hub/internal/mailer"
)

// EmailWorker consumes email tasks from RabbitMQ and delivers them via the configured mailer.Sender.
type EmailWorker struct {
	conn      *amqp.Connection
	sender    mailer.Sender
	queueName string
}

// NewEmailWorker creates a new EmailWorker instance.
func NewEmailWorker(
	conn *amqp.Connection,
	sender mailer.Sender,
	queueName string,
) *EmailWorker {
	return &EmailWorker{
		conn:      conn,
		sender:    sender,
		queueName: queueName,
	}
}

// Start opens an AMQP channel, starts consuming email notification tasks, and processes them until context cancellation.
func (w *EmailWorker) Start(ctx context.Context) error {
	if w.conn == nil {
		return errors.New("cannot start email worker: rabbitmq connection is nil")
	}

	ch, err := w.conn.Channel()
	if err != nil {
		return fmt.Errorf("opening email worker channel: %w", err)
	}
	defer func() {
		if closeErr := ch.Close(); closeErr != nil && !errors.Is(closeErr, amqp.ErrClosed) {
			slog.Error("error closing email worker channel", "err", closeErr)
		}
	}()

	if err := ch.Qos(10, 0, false); err != nil {
		return fmt.Errorf("setting email worker qos: %w", err)
	}

	deliveries, err := ch.Consume(
		w.queueName,
		"guestbook-email-worker",
		false, // manual acknowledgment for delivery guarantees
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("starting email rabbitmq consumer: %w", err)
	}

	slog.Info("email worker started listening for tasks", "queue", w.queueName)

	for {
		select {
		case <-ctx.Done():
			slog.Info("email worker stopping due to context cancellation")
			return nil
		case d, ok := <-deliveries:
			if !ok {
				slog.Info("email worker deliveries channel closed")
				return nil
			}
			w.handleDelivery(ctx, d)
		}
	}
}

// handleDelivery parses an email task, executes the delivery, and acknowledges or requeues the message.
func (w *EmailWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	var task broker.EmailTask
	if err := json.Unmarshal(d.Body, &task); err != nil {
		slog.Error("malformed email task payload, rejecting without requeue", "err", err)
		_ = d.Nack(false, false)
		return
	}

	if err := w.ProcessTask(ctx, task); err != nil {
		slog.Error("failed to deliver email, requeuing for retry", "err", err, "to", task.To, "subject", task.Subject)
		_ = d.Nack(false, true)
		return
	}

	if err := d.Ack(false); err != nil {
		slog.Error("error acknowledging email delivery", "err", err)
	}
	slog.Info("email delivered and acknowledged successfully", "to", task.To, "subject", task.Subject)
}

// ProcessTask maps an EmailTask to a mailer Message and dispatches it via the Sender.
func (w *EmailWorker) ProcessTask(ctx context.Context, task broker.EmailTask) error {
	msg := mailer.Message{
		To:       task.To,
		Subject:  task.Subject,
		TextBody: task.TextBody,
		HTMLBody: task.HTMLBody,
	}

	if err := w.sender.Send(ctx, msg); err != nil {
		return fmt.Errorf("sending email message: %w", err)
	}

	return nil
}
