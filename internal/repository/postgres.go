// Package repository defines data access interfaces and models for the Meme Hub application.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"meme-hub/migrations"
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

// Migrate applies database schema migrations for PostgreSQL using embedded SQL files.
func (r *PostgresRepository) Migrate(ctx context.Context) error {
	schemaQuery := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`
	if _, err := r.db.ExecContext(ctx, schemaQuery); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	for _, file := range upFiles {
		version := strings.TrimSuffix(file, ".up.sql")

		var exists int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = $1`, version).Scan(&exists)
		if err == nil {
			// Migration has already been applied.
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking migration status for %s: %w", version, err)
		}

		content, err := fs.ReadFile(migrations.FS, file)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", file, err)
		}

		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing transaction for %s: %w", version, err)
		}
	}

	return nil
}

// MigrateDown rolls back the most recently applied database schema migration.
func (r *PostgresRepository) MigrateDown(ctx context.Context) error {
	schemaQuery := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`
	if _, err := r.db.ExecContext(ctx, schemaQuery); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	var latestVersion string
	err := r.db.QueryRowContext(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&latestVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No migrations to revert.
			return nil
		}
		return fmt.Errorf("fetching latest applied migration: %w", err)
	}

	downFile := latestVersion + ".down.sql"
	content, err := fs.ReadFile(migrations.FS, downFile)
	if err != nil {
		return fmt.Errorf("reading down migration file %s: %w", downFile, err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction for %s down: %w", latestVersion, err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("executing down migration %s: %w", latestVersion, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1`, latestVersion); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("deleting schema migration record %s: %w", latestVersion, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction for %s down: %w", latestVersion, err)
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

// CreateMessage inserts a new guest message into the database and returns its ID.
func (r *PostgresRepository) CreateMessage(ctx context.Context, userID int, text string, mediaURL string) (int, error) {
	var id int
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO messages (user_id, body, media_url, status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING id
	`, userID, text, mediaURL).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("creating message: %w", err)
	}
	return id, nil
}

// UpdateMessageStatus updates the moderation status and optional rejection reason for a message.
func (r *PostgresRepository) UpdateMessageStatus(ctx context.Context, messageID int, status MessageStatus, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE messages
		SET status = $1, rejection_reason = $2
		WHERE id = $3
	`, string(status), reason, messageID)
	if err != nil {
		return fmt.Errorf("updating message status: %w", err)
	}
	return nil
}

// VoteMessage applies, toggles, or updates a user vote (+1 or -1) on a message.
func (r *PostgresRepository) VoteMessage(ctx context.Context, messageID int, userID int, value int) (VoteResult, error) {
	if value != 1 && value != -1 {
		return VoteResult{}, errors.New("vote value must be either 1 or -1")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return VoteResult{}, fmt.Errorf("beginning vote transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var existingValue int
	checkErr := tx.QueryRowContext(ctx, `
		SELECT value FROM message_votes
		WHERE message_id = $1 AND user_id = $2
		FOR UPDATE
	`, messageID, userID).Scan(&existingValue)

	newUserVote := value

	if errors.Is(checkErr, sql.ErrNoRows) {
		if _, insErr := tx.ExecContext(ctx, `
			INSERT INTO message_votes (message_id, user_id, value)
			VALUES ($1, $2, $3)
		`, messageID, userID, value); insErr != nil {
			return VoteResult{}, fmt.Errorf("inserting message vote: %w", insErr)
		}
		if value == 1 {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET upvotes = upvotes + 1 WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("incrementing upvotes: %w", updErr)
			}
		} else {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET downvotes = downvotes + 1 WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("incrementing downvotes: %w", updErr)
			}
		}
	} else if checkErr != nil {
		return VoteResult{}, fmt.Errorf("checking existing vote: %w", checkErr)
	} else if existingValue == value {
		newUserVote = 0
		if _, delErr := tx.ExecContext(ctx, `
			DELETE FROM message_votes
			WHERE message_id = $1 AND user_id = $2
		`, messageID, userID); delErr != nil {
			return VoteResult{}, fmt.Errorf("deleting message vote: %w", delErr)
		}
		if value == 1 {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET upvotes = GREATEST(0, upvotes - 1) WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("decrementing upvotes: %w", updErr)
			}
		} else {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET downvotes = GREATEST(0, downvotes - 1) WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("decrementing downvotes: %w", updErr)
			}
		}
	} else {
		if _, updErr := tx.ExecContext(ctx, `
			UPDATE message_votes SET value = $3
			WHERE message_id = $1 AND user_id = $2
		`, messageID, userID, value); updErr != nil {
			return VoteResult{}, fmt.Errorf("updating message vote: %w", updErr)
		}
		if value == 1 {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET upvotes = upvotes + 1, downvotes = GREATEST(0, downvotes - 1) WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("switching to upvote: %w", updErr)
			}
		} else {
			if _, updErr := tx.ExecContext(ctx, `UPDATE messages SET downvotes = downvotes + 1, upvotes = GREATEST(0, upvotes - 1) WHERE id = $1`, messageID); updErr != nil {
				return VoteResult{}, fmt.Errorf("switching to downvote: %w", updErr)
			}
		}
	}

	var upvotes, downvotes int
	if err := tx.QueryRowContext(ctx, `
		SELECT upvotes, downvotes FROM messages WHERE id = $1
	`, messageID).Scan(&upvotes, &downvotes); err != nil {
		return VoteResult{}, fmt.Errorf("reading updated votes: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return VoteResult{}, fmt.Errorf("committing vote transaction: %w", err)
	}

	return VoteResult{
		MessageID: messageID,
		Upvotes:   upvotes,
		Downvotes: downvotes,
		Score:     upvotes - downvotes,
		UserVote:  newUserVote,
	}, nil
}

// GetMessages retrieves messages matching the sorting criterion ("hot", "fresh") with user vote status.
func (r *PostgresRepository) GetMessages(ctx context.Context, currentUserID int, sortBy string, limit int) ([]MessageDTO, error) {
	if limit <= 0 {
		limit = 100
	}

	orderClause := "ORDER BY m.created_at DESC"
	if sortBy == "hot" {
		orderClause = "ORDER BY (m.upvotes - m.downvotes) DESC, m.created_at DESC"
	}

	query := fmt.Sprintf(`
		SELECT m.id, m.body, COALESCE(m.media_url, ''), m.upvotes, m.downvotes,
		       m.created_at, m.status, COALESCE(m.rejection_reason, ''),
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, ''),
		       COALESCE(v.value, 0)
		FROM messages m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN message_votes v ON v.message_id = m.id AND v.user_id = $1
		WHERE m.status != 'rejected'
		%s
		LIMIT $2
	`, orderClause)

	rows, err := r.db.QueryContext(ctx, query, currentUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	messages := make([]MessageDTO, 0)
	for rows.Next() {
		var m MessageDTO
		var statusStr string
		if err := rows.Scan(
			&m.ID, &m.Text, &m.MediaURL, &m.Upvotes, &m.Downvotes,
			&m.CreatedAt, &statusStr, &m.RejectionReason,
			&m.User.ID, &m.User.Email, &m.User.Name, &m.User.Avatar,
			&m.UserVote,
		); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.Score = m.Upvotes - m.Downvotes
		m.Status = MessageStatus(statusStr)
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating messages rows: %w", err)
	}
	return messages, nil
}

// CreateComment inserts a new user comment under a message and returns the comment DTO.
func (r *PostgresRepository) CreateComment(ctx context.Context, messageID int, userID int, text string) (CommentDTO, error) {
	var c CommentDTO
	query := `
		WITH inserted AS (
			INSERT INTO comments (message_id, user_id, body)
			VALUES ($1, $2, $3)
			RETURNING id, message_id, body, created_at, user_id
		)
		SELECT i.id, i.message_id, i.body, i.created_at,
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, '')
		FROM inserted i
		JOIN users u ON u.id = i.user_id
	`
	err := r.db.QueryRowContext(ctx, query, messageID, userID, text).Scan(
		&c.ID, &c.MessageID, &c.Text, &c.CreatedAt,
		&c.User.ID, &c.User.Email, &c.User.Name, &c.User.Avatar,
	)
	if err != nil {
		return CommentDTO{}, fmt.Errorf("inserting comment: %w", err)
	}
	return c, nil
}

// GetCommentsByMessageID retrieves all comments for a specific message ordered chronologically.
func (r *PostgresRepository) GetCommentsByMessageID(ctx context.Context, messageID int) ([]CommentDTO, error) {
	query := `
		SELECT c.id, c.message_id, c.body, c.created_at,
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, '')
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.message_id = $1
		ORDER BY c.created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, messageID)
	if err != nil {
		return nil, fmt.Errorf("querying comments for message: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	comments := make([]CommentDTO, 0)
	for rows.Next() {
		var c CommentDTO
		if err := rows.Scan(
			&c.ID, &c.MessageID, &c.Text, &c.CreatedAt,
			&c.User.ID, &c.User.Email, &c.User.Name, &c.User.Avatar,
		); err != nil {
			return nil, fmt.Errorf("scanning comment: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating comments rows: %w", err)
	}
	return comments, nil
}

// GetCommentsByMessageIDs retrieves all comments for the specified message IDs grouped by message ID.
func (r *PostgresRepository) GetCommentsByMessageIDs(ctx context.Context, messageIDs []int) (map[int][]CommentDTO, error) {
	result := make(map[int][]CommentDTO)
	if len(messageIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.message_id, c.body, c.created_at,
		       u.id, COALESCE(u.email, ''), COALESCE(u.name, ''), COALESCE(u.avatar, '')
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.message_id IN (%s)
		ORDER BY c.created_at ASC
	`, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying comments by message ids: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var c CommentDTO
		if err := rows.Scan(
			&c.ID, &c.MessageID, &c.Text, &c.CreatedAt,
			&c.User.ID, &c.User.Email, &c.User.Name, &c.User.Avatar,
		); err != nil {
			return nil, fmt.Errorf("scanning grouped comment: %w", err)
		}
		result[c.MessageID] = append(result[c.MessageID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating grouped comments rows: %w", err)
	}
	return result, nil
}
