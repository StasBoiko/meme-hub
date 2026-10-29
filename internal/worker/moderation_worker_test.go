// Package worker implements asynchronous background message consumers and processors.
package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"meme-hub/internal/broker"
	"meme-hub/internal/moderation"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
)

// mockRepo tracks message status updates for worker unit tests.
type mockRepo struct {
	updateErr     error
	updatedID     int
	updatedStatus repository.MessageStatus
	updatedReason string
}

func (m *mockRepo) SaveUser(ctx context.Context, email, name, avatar string) (repository.SessionUser, error) {
	return repository.SessionUser{}, nil
}

func (m *mockRepo) CreateMessage(ctx context.Context, userID int, text string, mediaURL string) (int, error) {
	return 1, nil
}

func (m *mockRepo) UpdateMessageStatus(ctx context.Context, messageID int, status repository.MessageStatus, reason string) error {
	m.updatedID = messageID
	m.updatedStatus = status
	m.updatedReason = reason
	return m.updateErr
}

func (m *mockRepo) VoteMessage(ctx context.Context, messageID int, userID int, value int) (repository.VoteResult, error) {
	return repository.VoteResult{MessageID: messageID, UserVote: value}, nil
}

func (m *mockRepo) GetMessages(ctx context.Context, currentUserID int, sortBy string, limit int) ([]repository.MessageDTO, error) {
	return nil, nil
}

func (m *mockRepo) CreateComment(ctx context.Context, messageID int, userID int, text string) (repository.CommentDTO, error) {
	return repository.CommentDTO{ID: 1, MessageID: messageID, Text: text}, nil
}

func (m *mockRepo) GetCommentsByMessageIDs(ctx context.Context, messageIDs []int) (map[int][]repository.CommentDTO, error) {
	return make(map[int][]repository.CommentDTO), nil
}

func (m *mockRepo) GetCommentsByMessageID(ctx context.Context, messageID int) ([]repository.CommentDTO, error) {
	return nil, nil
}

func (m *mockRepo) Ping(ctx context.Context) error {
	return nil
}

// mockPub records published email and moderation tasks for worker unit testing.
type mockPub struct {
	emailTasks []broker.EmailTask
}

func (m *mockPub) PublishModerationTask(ctx context.Context, task broker.ModerationTask) error {
	return nil
}

func (m *mockPub) PublishEmailTask(ctx context.Context, task broker.EmailTask) error {
	m.emailTasks = append(m.emailTasks, task)
	return nil
}

func (m *mockPub) Close() error {
	return nil
}

// mockSearch records indexed documents for worker unit testing.
type mockSearch struct {
	indexedDocs []search.Document
}

func (m *mockSearch) IndexMessage(ctx context.Context, doc search.Document) error {
	m.indexedDocs = append(m.indexedDocs, doc)
	return nil
}

func (m *mockSearch) SearchMessages(ctx context.Context, query string, limit int) ([]search.Document, error) {
	return nil, nil
}

func (m *mockSearch) Ping(ctx context.Context) error {
	return nil
}

// TestModerationWorker_ProcessTask tests worker processing for approved, rejected, and database failure conditions.
func TestModerationWorker_ProcessTask(t *testing.T) {
	moderator := moderation.NewRuleBasedModerator()

	tests := []struct {
		name         string
		task         broker.ModerationTask
		updateErr    error
		wantApproved bool
		wantStatus   repository.MessageStatus
		wantEmails   int
		wantIndexed  int
		wantErr      bool
	}{
		{
			name: "clean message approved and updated in database",
			task: broker.ModerationTask{
				MessageID:  101,
				UserID:     1,
				AuthorName: "Alice",
				Avatar:     "https://example.com/avatar.png",
				Text:       "Great guestbook service, thank you!",
				CreatedAt:  time.Now(),
			},
			updateErr:    nil,
			wantApproved: true,
			wantStatus:   repository.MessageStatusApproved,
			wantEmails:   1,
			wantIndexed:  1,
			wantErr:      false,
		},
		{
			name: "spam message rejected and updated in database",
			task: broker.ModerationTask{
				MessageID:  102,
				UserID:     2,
				AuthorName: "Spammer",
				Text:       "Free crypto giveaways! Click here now!",
				CreatedAt:  time.Now(),
			},
			updateErr:    nil,
			wantApproved: false,
			wantStatus:   repository.MessageStatusRejected,
			wantEmails:   1,
			wantIndexed:  0,
			wantErr:      false,
		},
		{
			name: "database failure returns error",
			task: broker.ModerationTask{
				MessageID:  103,
				UserID:     3,
				AuthorName: "Bob",
				Text:       "Legitimate message",
				CreatedAt:  time.Now(),
			},
			updateErr:    errors.New("db write failed"),
			wantApproved: false,
			wantStatus:   repository.MessageStatusApproved,
			wantEmails:   0,
			wantIndexed:  0,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{updateErr: tc.updateErr}
			pub := &mockPub{}
			srch := &mockSearch{}
			worker := NewModerationWorker(nil, repo, moderator, pub, srch, "admin@example.com", "test-queue")

			approved, err := worker.ProcessTask(context.Background(), tc.task)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ProcessTask() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				if approved != tc.wantApproved {
					t.Errorf("ProcessTask() approved = %v, want %v", approved, tc.wantApproved)
				}
				if repo.updatedStatus != tc.wantStatus {
					t.Errorf("repo.updatedStatus = %v, want %v", repo.updatedStatus, tc.wantStatus)
				}
				if repo.updatedID != tc.task.MessageID {
					t.Errorf("repo.updatedID = %d, want %d", repo.updatedID, tc.task.MessageID)
				}
				if len(pub.emailTasks) != tc.wantEmails {
					t.Errorf("len(pub.emailTasks) = %d, want %d", len(pub.emailTasks), tc.wantEmails)
				}
				if len(srch.indexedDocs) != tc.wantIndexed {
					t.Errorf("len(srch.indexedDocs) = %d, want %d", len(srch.indexedDocs), tc.wantIndexed)
				}
			}
		})
	}
}

// TestModerationWorker_Start_NilConn tests that Start returns an error when connection is nil.
func TestModerationWorker_Start_NilConn(t *testing.T) {
	worker := NewModerationWorker(nil, &mockRepo{}, moderation.NewRuleBasedModerator(), &mockPub{}, &mockSearch{}, "admin@example.com", "test-queue")
	err := worker.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting worker with nil connection, got nil")
	}
}
