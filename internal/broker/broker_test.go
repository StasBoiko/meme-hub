// Package broker defines messaging abstractions, payloads, and RabbitMQ integration for the application.
package broker

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestNopPublisher tests that NopPublisher operations succeed without errors.
func TestNopPublisher(t *testing.T) {
	pub := NewNopPublisher()
	if pub == nil {
		t.Fatal("expected non-nil NopPublisher")
	}

	task := ModerationTask{
		MessageID: 1,
		UserID:    2,
		Text:      "Test message",
		CreatedAt: time.Now(),
	}

	if err := pub.PublishModerationTask(context.Background(), task); err != nil {
		t.Errorf("NopPublisher.PublishModerationTask() error = %v, want nil", err)
	}

	emailTask := EmailTask{
		To:       []string{"recipient@example.com"},
		Subject:  "Test Subject",
		TextBody: "Body text",
	}
	if err := pub.PublishEmailTask(context.Background(), emailTask); err != nil {
		t.Errorf("NopPublisher.PublishEmailTask() error = %v, want nil", err)
	}

	if err := pub.Close(); err != nil {
		t.Errorf("NopPublisher.Close() error = %v, want nil", err)
	}
}

// TestModerationTaskSerialization tests JSON marshaling and unmarshaling of moderation payloads.
func TestModerationTaskSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	task := ModerationTask{
		MessageID: 42,
		UserID:    100,
		Text:      "Hello from RabbitMQ",
		CreatedAt: now,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("json.Marshal(task) error = %v", err)
	}

	var decoded ModerationTask
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}

	if decoded.MessageID != task.MessageID {
		t.Errorf("decoded.MessageID = %d, want %d", decoded.MessageID, task.MessageID)
	}
	if decoded.UserID != task.UserID {
		t.Errorf("decoded.UserID = %d, want %d", decoded.UserID, task.UserID)
	}
	if decoded.Text != task.Text {
		t.Errorf("decoded.Text = %q, want %q", decoded.Text, task.Text)
	}
	if !decoded.CreatedAt.Equal(task.CreatedAt) {
		t.Errorf("decoded.CreatedAt = %v, want %v", decoded.CreatedAt, task.CreatedAt)
	}
}

// TestEmailTaskSerialization tests JSON marshaling and unmarshaling of email task payloads.
func TestEmailTaskSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	task := EmailTask{
		To:        []string{"admin@example.com", "moderator@example.com"},
		Subject:   "New Meme Submission",
		TextBody:  "Plain text body",
		HTMLBody:  "<p>HTML body</p>",
		CreatedAt: now,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("json.Marshal(task) error = %v", err)
	}

	var decoded EmailTask
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}

	if len(decoded.To) != len(task.To) || decoded.To[0] != task.To[0] {
		t.Errorf("decoded.To = %v, want %v", decoded.To, task.To)
	}
	if decoded.Subject != task.Subject {
		t.Errorf("decoded.Subject = %q, want %q", decoded.Subject, task.Subject)
	}
	if decoded.TextBody != task.TextBody {
		t.Errorf("decoded.TextBody = %q, want %q", decoded.TextBody, task.TextBody)
	}
	if decoded.HTMLBody != task.HTMLBody {
		t.Errorf("decoded.HTMLBody = %q, want %q", decoded.HTMLBody, task.HTMLBody)
	}
	if !decoded.CreatedAt.Equal(task.CreatedAt) {
		t.Errorf("decoded.CreatedAt = %v, want %v", decoded.CreatedAt, task.CreatedAt)
	}
}
