// Package broker defines messaging abstractions, payloads, and RabbitMQ integration for the application.
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// ExchangeName defines the primary topic exchange for meme-hub events.
	ExchangeName = "memehub.exchange"
	// RoutingKeySubmitted defines the routing key for newly submitted messages.
	RoutingKeySubmitted = "message.submitted"
	// RoutingKeyRejected defines the routing key for rejected messages destined for the DLQ.
	RoutingKeyRejected = "message.rejected"
	// RoutingKeyEmail defines the routing key for outgoing email notification tasks.
	RoutingKeyEmail = "notification.email"
	// QueueModeration defines the primary queue consumed by the moderation worker.
	QueueModeration = "memehub.messages.moderation"
	// QueueEmail defines the primary queue consumed by the email notification worker.
	QueueEmail = "memehub.notifications.email"
	// DLXExchangeName defines the Dead Letter Exchange for rejected messages.
	DLXExchangeName = "memehub.dlx"
	// DLQQueueName defines the Dead Letter Queue storing failed and rejected messages.
	DLQQueueName = "memehub.messages.rejected"
)

// RabbitMQClient provides message publishing and topology configuration for RabbitMQ.
type RabbitMQClient struct {
	conn *amqp.Connection
	ch   *amqp.Channel
	mu   sync.Mutex
}

// NewRabbitMQClient establishes a connection to RabbitMQ, initializes exchanges, queues, and DLQ bindings.
func NewRabbitMQClient(amqpURL string) (*RabbitMQClient, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("dialing rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("opening rabbitmq channel: %w", err)
	}

	client := &RabbitMQClient{
		conn: conn,
		ch:   ch,
	}

	if err := client.setupTopology(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("setting up rabbitmq topology: %w", err)
	}

	return client, nil
}

// setupTopology configures exchanges, queues, and Dead Letter Queue routing.
func (c *RabbitMQClient) setupTopology() error {
	// 1. Declare Dead Letter Exchange (DLX).
	if err := c.ch.ExchangeDeclare(
		DLXExchangeName,
		amqp.ExchangeDirect,
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("declaring dead letter exchange %s: %w", DLXExchangeName, err)
	}

	// 2. Declare Dead Letter Queue (DLQ).
	if _, err := c.ch.QueueDeclare(
		DLQQueueName,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("declaring dead letter queue %s: %w", DLQQueueName, err)
	}

	// 3. Bind Dead Letter Queue to DLX.
	if err := c.ch.QueueBind(
		DLQQueueName,
		RoutingKeyRejected,
		DLXExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("binding dead letter queue to dlx: %w", err)
	}

	// 4. Declare Primary Topic Exchange.
	if err := c.ch.ExchangeDeclare(
		ExchangeName,
		amqp.ExchangeTopic,
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("declaring main topic exchange %s: %w", ExchangeName, err)
	}

	// 5. Declare Main Moderation Queue with DLX configuration.
	queueArgs := amqp.Table{
		"x-dead-letter-exchange":    DLXExchangeName,
		"x-dead-letter-routing-key": RoutingKeyRejected,
	}
	if _, err := c.ch.QueueDeclare(
		QueueModeration,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		queueArgs,
	); err != nil {
		return fmt.Errorf("declaring moderation queue %s with dlx: %w", QueueModeration, err)
	}

	// 6. Bind Main Queue to Topic Exchange with message.submitted key.
	if err := c.ch.QueueBind(
		QueueModeration,
		RoutingKeySubmitted,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("binding moderation queue to main exchange: %w", err)
	}

	// 7. Declare Email Notification Queue with DLX configuration.
	emailQueueArgs := amqp.Table{
		"x-dead-letter-exchange":    DLXExchangeName,
		"x-dead-letter-routing-key": RoutingKeyRejected,
	}
	if _, err := c.ch.QueueDeclare(
		QueueEmail,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		emailQueueArgs,
	); err != nil {
		return fmt.Errorf("declaring email queue %s with dlx: %w", QueueEmail, err)
	}

	// 8. Bind Email Queue to Topic Exchange with notification.email key.
	if err := c.ch.QueueBind(
		QueueEmail,
		RoutingKeyEmail,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("binding email queue to main exchange: %w", err)
	}

	return nil
}

// PublishModerationTask dispatches a new moderation task payload to the main topic exchange.
func (c *RabbitMQClient) PublishModerationTask(ctx context.Context, task ModerationTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshaling moderation task: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         payload,
	}

	if err := c.ch.PublishWithContext(
		ctx,
		ExchangeName,
		RoutingKeySubmitted,
		false, // mandatory
		false, // immediate
		publishing,
	); err != nil {
		return fmt.Errorf("publishing message to exchange %s: %w", ExchangeName, err)
	}

	return nil
}

// PublishEmailTask dispatches a new email notification task to the main topic exchange.
func (c *RabbitMQClient) PublishEmailTask(ctx context.Context, task EmailTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshaling email task: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         payload,
	}

	if err := c.ch.PublishWithContext(
		ctx,
		ExchangeName,
		RoutingKeyEmail,
		false, // mandatory
		false, // immediate
		publishing,
	); err != nil {
		return fmt.Errorf("publishing email task to exchange %s: %w", ExchangeName, err)
	}

	return nil
}

// Connection returns the active RabbitMQ connection.
func (c *RabbitMQClient) Connection() *amqp.Connection {
	return c.conn
}

// Close terminates the RabbitMQ channel and connection.
func (c *RabbitMQClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	if c.ch != nil {
		if err := c.ch.Close(); err != nil {
			firstErr = err
		}
	}
	if c.conn != nil {
		if err := c.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
