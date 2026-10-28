// Package repository defines data access interfaces and models for the guestbook application.
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
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS messages").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr: false,
		},
		{
			name: "error creating users table",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnError(errors.New("syntax error"))
			},
			wantErr: true,
		},
		{
			name: "error altering avatar column",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar").
					WillReturnError(errors.New("permission denied"))
			},
			wantErr: true,
		},
		{
			name: "error creating email index",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx").
					WillReturnError(errors.New("index conflict"))
			},
			wantErr: true,
		},
		{
			name: "error creating messages table",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS users").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS messages").
					WillReturnError(errors.New("disk full"))
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
	queryPattern := regexp.QuoteMeta(`INSERT INTO messages (user_id, body) VALUES ($1, $2)`)

	tests := []struct {
		name      string
		userID    int
		text      string
		mockSetup func(mock sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:   "successful message creation",
			userID: 1,
			text:   "Hello guestbook!",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(queryPattern).
					WithArgs(1, "Hello guestbook!").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name:   "database execution error",
			userID: 2,
			text:   "Failing message",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(queryPattern).
					WithArgs(2, "Failing message").
					WillReturnError(errors.New("foreign key violation"))
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

			err = repo.CreateMessage(context.Background(), tc.userID, tc.text)
			if (err != nil) != tc.wantErr {
				t.Fatalf("repo.CreateMessage() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled sqlmock expectations: %v", err)
			}
		})
	}
}

// TestPostgresRepository_GetMessages tests retrieving messages with limit and scanning.
func TestPostgresRepository_GetMessages(t *testing.T) {
	queryPattern := regexp.QuoteMeta(`
		SELECT m.id, m.body, m.created_at,
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, '')
		FROM messages m
		JOIN users u ON u.id = m.user_id
		ORDER BY m.created_at DESC
		LIMIT $1
	`)

	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name      string
		limit     int
		mockSetup func(mock sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name:  "retrieve messages with positive limit",
			limit: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"m_id", "m_body", "m_created_at", "u_id", "u_email", "u_name", "u_avatar"}).
					AddRow(1, "First message", now, 10, "alice@example.com", "Alice", "avatar1.png").
					AddRow(2, "Second message", now.Add(time.Minute), 20, "bob@example.com", "Bob", "")
				mock.ExpectQuery(queryPattern).
					WithArgs(10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:  "limit <= 0 defaults to 100",
			limit: 0,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"m_id", "m_body", "m_created_at", "u_id", "u_email", "u_name", "u_avatar"})
				mock.ExpectQuery(queryPattern).
					WithArgs(100).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:  "query execution error",
			limit: 5,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(queryPattern).
					WithArgs(5).
					WillReturnError(errors.New("table not found"))
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:  "row scanning error",
			limit: 5,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"m_id", "m_body"}).
					AddRow(1, "Incomplete row")
				mock.ExpectQuery(queryPattern).
					WithArgs(5).
					WillReturnRows(rows)
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

			msgs, err := repo.GetMessages(context.Background(), tc.limit)
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
