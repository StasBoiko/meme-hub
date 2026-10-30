// Package repository defines data access interfaces and models for the Meme Hub application.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestNewPostgresRepository tests constructor initialization of PostgresRepository.
func TestNewPostgresRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewPostgresRepository(db)
	if repo == nil {
		t.Fatal("expected non-nil PostgresRepository instance")
	}
	if repo.db != db {
		t.Error("expected repo.db to reference the provided database connection")
	}
}

// TestPostgresRepository_Ping tests connectivity checks and error propagation.
func TestPostgresRepository_Ping(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name: "successful ping",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()
			},
			wantErr: false,
		},
		{
			name: "ping failure returns error",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing().WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			err = repo.Ping(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.Ping() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_Migrate tests schema migration execution and error handling.
func TestPostgresRepository_Migrate(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name: "successful migration",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))

				migrationsList := []struct {
					version   string
					execRegex string
				}{
					{"000001_create_users_table", "CREATE TABLE IF NOT EXISTS users"},
					{"000002_create_messages_table", "CREATE TABLE IF NOT EXISTS messages"},
					{"000003_create_message_votes_table", "CREATE TABLE IF NOT EXISTS message_votes"},
					{"000004_create_comments_table", "CREATE TABLE IF NOT EXISTS comments"},
				}

				for _, m := range migrationsList {
					mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM schema_migrations WHERE version = $1")).
						WithArgs(m.version).
						WillReturnError(sql.ErrNoRows)
					mock.ExpectBegin()
					mock.ExpectExec(m.execRegex).
						WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec(regexp.QuoteMeta("INSERT INTO schema_migrations (version) VALUES ($1)")).
						WithArgs(m.version).
						WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
				}
			},
			wantErr: false,
		},
		{
			name: "already applied migrations skipped",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))

				migrationsList := []string{
					"000001_create_users_table",
					"000002_create_messages_table",
					"000003_create_message_votes_table",
					"000004_create_comments_table",
				}

				for _, ver := range migrationsList {
					mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM schema_migrations WHERE version = $1")).
						WithArgs(ver).
						WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
				}
			},
			wantErr: false,
		},
		{
			name: "error creating schema_migrations table",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnError(errors.New("permission denied"))
			},
			wantErr: true,
		},
		{
			name: "migration execution error triggers rollback",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM schema_migrations WHERE version = $1")).
					WithArgs("000001_create_users_table").
					WillReturnError(sql.ErrNoRows)
				mock.ExpectBegin()
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnError(errors.New("syntax error in sql"))
				mock.ExpectRollback()
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			err = repo.Migrate(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.Migrate() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_MigrateDown tests schema rollback execution and error handling.
func TestPostgresRepository_MigrateDown(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name: "successful rollback of latest migration",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")).
					WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("000004_create_comments_table"))
				mock.ExpectBegin()
				mock.ExpectExec("DROP TABLE IF EXISTS comments").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(regexp.QuoteMeta("DELETE FROM schema_migrations WHERE version = $1")).
					WithArgs("000004_create_comments_table").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name: "no migrations to revert returns nil",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")).
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: false,
		},
		{
			name: "down migration error triggers rollback",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")).
					WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("000004_create_comments_table"))
				mock.ExpectBegin()
				mock.ExpectExec("DROP TABLE IF EXISTS comments").
					WillReturnError(errors.New("foreign key constraint error"))
				mock.ExpectRollback()
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			err = repo.MigrateDown(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.MigrateDown() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_SaveUser tests user upsert and session user mapping.
func TestPostgresRepository_SaveUser(t *testing.T) {
	queryPattern := regexp.QuoteMeta(`
		INSERT INTO users (email, name, avatar)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET
			name = EXCLUDED.name,
			avatar = EXCLUDED.avatar
		RETURNING id, email, name, COALESCE(avatar, '')
	`)

	tests := []struct {
		name      string
		email     string
		userName  string
		avatar    string
		mockSetup func(mock sqlmock.Sqlmock)
		wantUser  SessionUser
		wantErr   bool
	}{
		{
			name:     "successful user save",
			email:    "alice@example.com",
			userName: "Alice Smith",
			avatar:   "https://example.com/avatar.png",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id", "email", "name", "avatar"}).
					AddRow(1, "alice@example.com", "Alice Smith", "https://example.com/avatar.png")
				mock.ExpectQuery(queryPattern).
					WithArgs("alice@example.com", "Alice Smith", "https://example.com/avatar.png").
					WillReturnRows(rows)
			},
			wantUser: SessionUser{
				ID:     1,
				Email:  "alice@example.com",
				Name:   "Alice Smith",
				Avatar: "https://example.com/avatar.png",
			},
			wantErr: false,
		},
		{
			name:     "database query error",
			email:    "bob@example.com",
			userName: "Bob",
			avatar:   "",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(queryPattern).
					WithArgs("bob@example.com", "Bob", "").
					WillReturnError(sql.ErrConnDone)
			},
			wantUser: SessionUser{},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			gotUser, err := repo.SaveUser(context.Background(), tc.email, tc.userName, tc.avatar)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.SaveUser() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && gotUser != tc.wantUser {
				t.Errorf("repo.SaveUser() = %+v, want %+v", gotUser, tc.wantUser)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_CreateMessage tests inserting new messages into the database.
func TestPostgresRepository_CreateMessage(t *testing.T) {
	queryPattern := regexp.QuoteMeta(`
		INSERT INTO messages (user_id, body, media_url, status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING id
	`)

	tests := []struct {
		name      string
		userID    int
		text      string
		mediaURL  string
		mockSetup func(mock sqlmock.Sqlmock)
		wantID    int
		wantErr   bool
	}{
		{
			name:     "successful message creation with media",
			userID:   1,
			text:     "Hello meme hub!",
			mediaURL: "https://example.com/cat.jpg",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(queryPattern).
					WithArgs(1, "Hello meme hub!", "https://example.com/cat.jpg").
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
			},
			wantID:  42,
			wantErr: false,
		},
		{
			name:     "database execution error",
			userID:   2,
			text:     "Failing message",
			mediaURL: "",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(queryPattern).
					WithArgs(2, "Failing message", "").
					WillReturnError(errors.New("foreign key violation"))
			},
			wantID:  0,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			id, err := repo.CreateMessage(context.Background(), tc.userID, tc.text, tc.mediaURL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.CreateMessage() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && id != tc.wantID {
				t.Errorf("repo.CreateMessage() id = %d, want %d", id, tc.wantID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_UpdateMessageStatus tests updating the moderation status and reason.
func TestPostgresRepository_UpdateMessageStatus(t *testing.T) {
	queryPattern := regexp.QuoteMeta(`
		UPDATE messages
		SET status = $1, rejection_reason = $2
		WHERE id = $3
	`)

	tests := []struct {
		name      string
		messageID int
		status    MessageStatus
		reason    string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:      "successful status update to approved",
			messageID: 10,
			status:    MessageStatusApproved,
			reason:    "",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(queryPattern).
					WithArgs(string(MessageStatusApproved), "", 10).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name:      "successful status update to rejected with reason",
			messageID: 11,
			status:    MessageStatusRejected,
			reason:    "contains spam",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(queryPattern).
					WithArgs(string(MessageStatusRejected), "contains spam", 11).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name:      "database execution failure",
			messageID: 12,
			status:    MessageStatusRejected,
			reason:    "spam",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(queryPattern).
					WithArgs(string(MessageStatusRejected), "spam", 12).
					WillReturnError(errors.New("database connection down"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			err = repo.UpdateMessageStatus(context.Background(), tc.messageID, tc.status, tc.reason)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.UpdateMessageStatus() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_GetMessages tests retrieving messages with limit, sort, and scanning.
func TestPostgresRepository_GetMessages(t *testing.T) {
	freshQueryPattern := regexp.QuoteMeta(`
		SELECT m.id, m.body, COALESCE(m.media_url, ''), m.upvotes, m.downvotes,
		       m.created_at, m.status, COALESCE(m.rejection_reason, ''),
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, ''),
		       COALESCE(v.value, 0)
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN message_votes v ON v.message_id = m.id AND v.user_id = $1
		WHERE m.status != 'rejected'
		ORDER BY m.created_at DESC
		LIMIT $2
	`)

	hotQueryPattern := regexp.QuoteMeta(`
		SELECT m.id, m.body, COALESCE(m.media_url, ''), m.upvotes, m.downvotes,
		       m.created_at, m.status, COALESCE(m.rejection_reason, ''),
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, ''),
		       COALESCE(v.value, 0)
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN message_votes v ON v.message_id = m.id AND v.user_id = $1
		WHERE m.status != 'rejected'
		ORDER BY (m.upvotes - m.downvotes) DESC, m.created_at DESC
		LIMIT $2
	`)

	now := time.Now().Truncate(time.Second)
	columns := []string{
		"id", "body", "media_url", "upvotes", "downvotes",
		"created_at", "status", "rejection_reason",
		"u_id", "u_email", "u_name", "u_avatar",
		"user_vote",
	}

	tests := []struct {
		name      string
		userID    int
		sortBy    string
		limit     int
		mockSetup func(mock sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name:   "retrieve fresh messages",
			userID: 1,
			sortBy: "fresh",
			limit:  10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(columns).
					AddRow(1, "First", "https://img.jpg", 5, 1, now, "approved", "", 10, "alice@example.com", "Alice", "a.png", 1).
					AddRow(2, "Second", "", 0, 0, now.Add(time.Minute), "pending", "", 20, "bob@example.com", "Bob", "", 0)
				mock.ExpectQuery(freshQueryPattern).
					WithArgs(1, 10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:   "retrieve hot messages",
			userID: 2,
			sortBy: "hot",
			limit:  10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(columns).
					AddRow(1, "Top meme", "https://video.mp4", 10, 2, now, "approved", "", 10, "alice@example.com", "Alice", "a.png", -1)
				mock.ExpectQuery(hotQueryPattern).
					WithArgs(2, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:   "limit <= 0 defaults to 100",
			userID: 0,
			sortBy: "fresh",
			limit:  0,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(columns)
				mock.ExpectQuery(freshQueryPattern).
					WithArgs(0, 100).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:   "query execution error",
			userID: 1,
			sortBy: "fresh",
			limit:  5,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(freshQueryPattern).
					WithArgs(1, 5).
					WillReturnError(errors.New("table not found"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			msgs, err := repo.GetMessages(context.Background(), tc.userID, tc.sortBy, tc.limit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.GetMessages() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && len(msgs) != tc.wantCount {
				t.Errorf("repo.GetMessages() returned %d messages, want %d", len(msgs), tc.wantCount)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_VoteMessage tests applying, toggling, and updating user votes.
func TestPostgresRepository_VoteMessage(t *testing.T) {
	t.Run("invalid vote value returns error", func(t *testing.T) {
		db, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer db.Close()

		repo := NewPostgresRepository(db)
		_, err = repo.VoteMessage(context.Background(), 1, 1, 0)
		if err == nil {
			t.Fatal("expected error for invalid vote value 0, got nil")
		}
	})

	t.Run("new upvote transaction commits successfully", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer db.Close()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM message_votes WHERE message_id = $1 AND user_id = $2 FOR UPDATE`)).
			WithArgs(10, 5).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO message_votes (message_id, user_id, value) VALUES ($1, $2, $3)`)).
			WithArgs(10, 5, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE messages SET upvotes = upvotes + 1 WHERE id = $1`)).
			WithArgs(10).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT upvotes, downvotes FROM messages WHERE id = $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows([]string{"upvotes", "downvotes"}).AddRow(1, 0))
		mock.ExpectCommit()

		repo := NewPostgresRepository(db)
		res, err := repo.VoteMessage(context.Background(), 10, 5, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.UserVote != 1 || res.Score != 1 || res.Upvotes != 1 {
			t.Errorf("unexpected vote result: %+v", res)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled sqlmock expectations: %v", err)
		}
	})
}

// TestPostgresRepository_CreateComment tests inserting comments and mapping author details.
func TestPostgresRepository_CreateComment(t *testing.T) {
	tests := []struct {
		name      string
		messageID int
		userID    int
		text      string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:      "successful comment creation",
			messageID: 10,
			userID:    5,
			text:      "Hilarious meme!",
			mockSetup: func(mock sqlmock.Sqlmock) {
				cols := []string{"id", "message_id", "body", "created_at", "user_id", "email", "name", "avatar"}
				mock.ExpectQuery("WITH inserted AS").
					WithArgs(10, 5, "Hilarious meme!").
					WillReturnRows(sqlmock.NewRows(cols).AddRow(1, 10, "Hilarious meme!", time.Now(), 5, "alice@example.com", "Alice", "https://avatar.com/a.png"))
			},
			wantErr: false,
		},
		{
			name:      "database query error",
			messageID: 10,
			userID:    5,
			text:      "Hilarious meme!",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("WITH inserted AS").
					WithArgs(10, 5, "Hilarious meme!").
					WillReturnError(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			comment, err := repo.CreateComment(context.Background(), tc.messageID, tc.userID, tc.text)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.CreateComment() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && comment.ID != 1 {
				t.Errorf("expected comment ID 1, got %d", comment.ID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_GetCommentsByMessageID tests retrieving comments for a single message.
func TestPostgresRepository_GetCommentsByMessageID(t *testing.T) {
	tests := []struct {
		name      string
		messageID int
		mockSetup func(mock sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name:      "successful retrieval of comments",
			messageID: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				cols := []string{"id", "message_id", "body", "created_at", "user_id", "email", "name", "avatar"}
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT c.id, c.message_id, c.body, c.created_at`)).
					WithArgs(10).
					WillReturnRows(sqlmock.NewRows(cols).
						AddRow(1, 10, "First", time.Now(), 5, "u5@ex.com", "U5", "").
						AddRow(2, 10, "Second", time.Now(), 6, "u6@ex.com", "U6", ""))
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "database query error",
			messageID: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT c.id, c.message_id, c.body, c.created_at`)).
					WithArgs(10).
					WillReturnError(errors.New("connection closed"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			tc.mockSetup(mock)
			repo := NewPostgresRepository(db)

			comments, err := repo.GetCommentsByMessageID(context.Background(), tc.messageID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.GetCommentsByMessageID() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if len(comments) != tc.wantCount {
				t.Errorf("expected %d comments, got %d", tc.wantCount, len(comments))
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_GetCommentsByMessageIDs tests batch comment retrieval and grouping.
func TestPostgresRepository_GetCommentsByMessageIDs(t *testing.T) {
	t.Run("empty message IDs returns empty map immediately", func(t *testing.T) {
		db, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer db.Close()

		repo := NewPostgresRepository(db)
		res, err := repo.GetCommentsByMessageIDs(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("expected empty map, got %v", res)
		}
	})

	t.Run("successful batch retrieval grouped by message id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer db.Close()

		cols := []string{"id", "message_id", "body", "created_at", "user_id", "email", "name", "avatar"}
		mock.ExpectQuery("SELECT c.id, c.message_id, c.body, c.created_at").
			WithArgs(10, 20).
			WillReturnRows(sqlmock.NewRows(cols).
				AddRow(1, 10, "Comment for 10", time.Now(), 5, "u5@ex.com", "U5", "").
				AddRow(2, 20, "Comment for 20", time.Now(), 6, "u6@ex.com", "U6", ""))

		repo := NewPostgresRepository(db)
		res, err := repo.GetCommentsByMessageIDs(context.Background(), []int{10, 20})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res[10]) != 1 || len(res[20]) != 1 {
			t.Errorf("unexpected grouping result: %+v", res)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled sqlmock expectations: %v", err)
		}
	})
}

// TestPostgresRepository_GetUserByID tests retrieving a user by ID including success, not found, and error cases.
func TestPostgresRepository_GetUserByID(t *testing.T) {
	t.Run("successful user retrieval", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer func() { _ = db.Close() }()

		cols := []string{"id", "email", "name", "avatar"}
		mock.ExpectQuery("SELECT id, email, name, COALESCE\\(avatar, ''\\) FROM users WHERE id = \\$1").
			WithArgs(42).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(42, "user@example.com", "Test User", "https://example.com/avatar.jpg"))

		repo := NewPostgresRepository(db)
		user, err := repo.GetUserByID(context.Background(), 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 42 || user.Email != "user@example.com" {
			t.Errorf("unexpected user returned: %+v", user)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled sqlmock expectations: %v", err)
		}
	})

	t.Run("user not found returns ErrNotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("SELECT id, email, name, COALESCE\\(avatar, ''\\) FROM users WHERE id = \\$1").
			WithArgs(999).
			WillReturnError(sql.ErrNoRows)

		repo := NewPostgresRepository(db)
		_, err = repo.GetUserByID(context.Background(), 999)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled sqlmock expectations: %v", err)
		}
	})
}
