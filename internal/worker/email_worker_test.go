// Package worker implements asynchronous background message consumers and processors.
package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"meme-hub/internal/broker"
	"meme-hub/internal/mailer"
)

// mockSender records sent email messages for worker unit testing.
type mockSender struct {
	sentMessages []mailer.Message
	err          error
}

func (m *mockSender) Send(ctx context.Context, msg mailer.Message) error {
	if m.err != nil {
		return m.err
	}
	m.sentMessages = append(m.sentMessages, msg)
	return nil
}

// TestEmailWorker_ProcessTask tests email worker task processing and sender error propagation.
func TestEmailWorker_ProcessTask(t *testing.T) {
	tests := []struct {
		name      string
		task      broker.EmailTask
		senderErr error
		wantErr   bool
	}{
		{
			name: "successful email delivery",
			task: broker.EmailTask{
				To:        []string{"admin@example.com"},
				Subject:   "New Entry Alert",
				TextBody:  "Someone posted a new message",
				HTMLBody:  "<p>Someone posted a new message</p>",
				CreatedAt: time.Now(),
			},
			senderErr: nil,
			wantErr:   false,
		},
		{
			name: "sender failure returns error",
			task: broker.EmailTask{
				To:        []string{"fail@example.com"},
				Subject:   "Failure Test",
				TextBody:  "Body",
				CreatedAt: time.Now(),
			},
			senderErr: errors.New("smtp connection lost"),
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sender := &mockSender{err: tc.senderErr}
			worker := NewEmailWorker(nil, sender, "test-queue")

			err := worker.ProcessTask(context.Background(), tc.task)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ProcessTask() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				if len(sender.sentMessages) != 1 {
					t.Fatalf("expected 1 sent message, got %d", len(sender.sentMessages))
				}
				sent := sender.sentMessages[0]
				if len(sent.To) != len(tc.task.To) || sent.To[0] != tc.task.To[0] {
					t.Errorf("sent.To = %v, want %v", sent.To, tc.task.To)
				}
				if sent.Subject != tc.task.Subject {
					t.Errorf("sent.Subject = %q, want %q", sent.Subject, tc.task.Subject)
				}
				if sent.TextBody != tc.task.TextBody {
					t.Errorf("sent.TextBody = %q, want %q", sent.TextBody, tc.task.TextBody)
				}
				if sent.HTMLBody != tc.task.HTMLBody {
					t.Errorf("sent.HTMLBody = %q, want %q", sent.HTMLBody, tc.task.HTMLBody)
				}
			}
		})
	}
}

// TestEmailWorker_Start_NilConn tests that Start returns an error when connection is nil.
func TestEmailWorker_Start_NilConn(t *testing.T) {
	worker := NewEmailWorker(nil, mailer.NewNopSender(), "test-queue")
	err := worker.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting worker with nil connection, got nil")
	}
}
