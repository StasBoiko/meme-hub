// Package repository defines data access interfaces and models for the guestbook application.
package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// PostgresRepository implements Repository backed by a PostgreSQL database.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a new PostgresRepository instance.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// Ping verifies connectivity to the PostgreSQL database.
func (r *PostgresRepository) Ping(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging postgres: %w", err)
	}
	return nil
}

// Migrate applies database schema migrations for PostgreSQL.
func (r *PostgresRepository) Migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			email TEXT,
			name TEXT,
			avatar TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating users table: %w", err)
	}

	if _, err = r.db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar TEXT`); err != nil {
		return fmt.Errorf("altering users table avatar column: %w", err)
	}
	if _, err = r.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users (email)`); err != nil {
		return fmt.Errorf("creating users email index: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id),
			body TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("creating messages table: %w", err)
	}

	return nil
}

// SaveUser creates or updates a user by email and returns their session data.
func (r *PostgresRepository) SaveUser(ctx context.Context, email, name, avatar string) (SessionUser, error) {
	var user SessionUser
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (email, name, avatar)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET
			name = EXCLUDED.name,
			avatar = EXCLUDED.avatar
		RETURNING id, email, name, COALESCE(avatar, '')
	`, email, name, avatar).Scan(&user.ID, &user.Email, &user.Name, &user.Avatar)
	if err != nil {
		return SessionUser{}, fmt.Errorf("saving user: %w", err)
	}
	return user, nil
}

// CreateMessage inserts a new guest message into the database.
func (r *PostgresRepository) CreateMessage(ctx context.Context, userID int, text string) error {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO messages (user_id, body) VALUES ($1, $2)`, userID, text); err != nil {
		return fmt.Errorf("creating message: %w", err)
	}
	return nil
}

// GetMessages retrieves recent messages up to the specified limit.
func (r *PostgresRepository) GetMessages(ctx context.Context, limit int) ([]MessageDTO, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, m.body, m.created_at,
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, '')
		FROM messages m
		JOIN users u ON u.id = m.user_id
		ORDER BY m.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	messages := make([]MessageDTO, 0)
	for rows.Next() {
		var m MessageDTO
		if err := rows.Scan(&m.ID, &m.Text, &m.CreatedAt, &m.User.ID, &m.User.Email, &m.User.Name, &m.User.Avatar); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating messages rows: %w", err)
	}
	return messages, nil
}
