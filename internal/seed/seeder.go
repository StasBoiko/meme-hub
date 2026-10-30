// Package seed provides test data generation, database seeding, and fixture cleanup utilities.
package seed

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"meme-hub/internal/repository"
	"meme-hub/internal/search"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

// seedCommentTemplates contains authentic community comments used to dynamically expand comment threads.
var seedCommentTemplates = []string{
	"Take my upvote, good sir! 😂",
	"Can confirm, happened to me in production last Friday.",
	"Senior devs in absolute shambles right now.",
	"My last two brain cells during the Monday morning standup.",
	"Is it possible to learn this supreme power?",
	"I feel personally attacked by this post 💀",
	"Git blame never lies, but it sure hurts.",
	"Have you tried turning production off and on again?",
	"This meme speaks directly to my exhausted developer soul.",
	"Adding this to my team's sprint retro presentation right now.",
	"LGTM! Merging without review as is tradition.",
	"I choked on my coffee laughing at this ☕",
	"Wait, you guys are actually testing before deploying?",
	"Documentation? What documentation? The code IS the documentation!",
	"Docker saved my marriage, but Kubernetes took the kids.",
	"Top tier engineering humor right here.",
	"Works on my localhost, 10/10 no notes.",
	"The accuracy of this is genuinely frightening.",
	"Send help, our pipeline has been running for 47 minutes.",
	"Legendary post! Made my whole week.",
	"I'm in this picture and I don't like it.",
	"100% bug-free code is code that has never been written.",
	"Sudo make me unsee this deployment log.",
	"My manager literally said this exact quote yesterday.",
	"Nothing a quick hotfix commit 'fix stuff' can't solve.",
	"Deploying to prod on Friday at 5:00 PM: a dramatic thriller.",
	"Who authorized you to leak our company Slack chats?",
	"This deserves to be framed in every computer science classroom.",
	"Finally, some relatable content on this platform!",
	"Underrated meme, needs 10x more upvotes.",
	"Rubber duck debugging at its finest.",
	"Tabs vs spaces war has entered the chat.",
	"Never laughed so hard while crying inside.",
	"Pure gold! Sharing this to our dev channel.",
	"The compiler knows your sins.",
	"Stack Overflow is down, guess my shift is officially over.",
	"It's not a bug, it's an undocumented surprise feature.",
	"Classic! Reminds me of our legacy codebase.",
	"I'm printing this and taping it next to the server rack.",
	"Every junior dev needs to see this on day one.",
	"My terminal history agrees with this 100%.",
	"Brilliant! Bookmarking this masterpiece.",
	"Nothing beats the panic of typing rm -rf and realizing the path was wrong.",
	"Vim users trying to exit will never see this post.",
	"Post of the year! Take all the upvotes.",
	"This hits way too close to home.",
	"Why is this so painfully true?",
	"Bookmarked for when I need motivation during on-call shift.",
	"Absolute cinema 🍿",
	"10/10 quality meme, bless the author.",
	"My CPU fans started spinning faster just reading this.",
	"Big mood for this entire release cycle.",
	"Spitting straight facts here.",
	"Someone promote this person to VP of Memes immediately.",
}

// UserFixture represents a fixture record for a user.
type UserFixture struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar"`
}

// CommentFixture represents a comment under a meme fixture.
type CommentFixture struct {
	UserEmail string `json:"user_email"`
	Text      string `json:"text"`
}

// MemeFixture represents a meme fixture with media, votes, and comments.
type MemeFixture struct {
	UserEmail      string           `json:"user_email"`
	Text           string           `json:"text"`
	MediaURL       string           `json:"media_url"`
	Upvotes        int              `json:"upvotes"`
	Downvotes      int              `json:"downvotes"`
	DaysAgo        int              `json:"days_ago"`
	TargetComments int              `json:"target_comments,omitempty"`
	Comments       []CommentFixture `json:"comments"`
}

// Summary provides a summary of entities created during a seed operation.
type Summary struct {
	UsersCount    int
	MemesCount    int
	CommentsCount int
	VotesCount    int
	SearchIndexed int
}

// Seeder manages loading fixtures, writing to PostgreSQL, and indexing search documents.
type Seeder struct {
	db       *sql.DB
	search   search.Service
	sessions repository.SessionStore
}

// NewSeeder creates a new Seeder instance with database, search, and optional session dependencies.
func NewSeeder(db *sql.DB, searchService search.Service, sessionStores ...repository.SessionStore) *Seeder {
	var ss repository.SessionStore
	if len(sessionStores) > 0 {
		ss = sessionStores[0]
	}
	return &Seeder{
		db:       db,
		search:   searchService,
		sessions: ss,
	}
}

// LoadFixtures loads fixture files from embedded assets.
func (s *Seeder) LoadFixtures() ([]UserFixture, []MemeFixture, error) {
	usersData, err := fixturesFS.ReadFile("fixtures/users.json")
	if err != nil {
		return nil, nil, fmt.Errorf("reading users fixture file: %w", err)
	}

	var users []UserFixture
	if decErr := json.Unmarshal(usersData, &users); decErr != nil {
		return nil, nil, fmt.Errorf("decoding users fixture json: %w", decErr)
	}

	memesData, err := fixturesFS.ReadFile("fixtures/memes.json")
	if err != nil {
		return nil, nil, fmt.Errorf("reading memes fixture file: %w", err)
	}

	var memes []MemeFixture
	if decErr := json.Unmarshal(memesData, &memes); decErr != nil {
		return nil, nil, fmt.Errorf("decoding memes fixture json: %w", decErr)
	}

	return users, memes, nil
}

// Seed populates the database and search index with test fixtures.
func (s *Seeder) Seed(ctx context.Context) (*Summary, error) {
	users, memes, err := s.LoadFixtures()
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning seed transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	userMap := make(map[string]int, len(users))
	userNames := make(map[string]string, len(users))
	userAvatars := make(map[string]string, len(users))
	summary := &Summary{}

	userStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO users (email, name, avatar)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, avatar = EXCLUDED.avatar
		RETURNING id;
	`)
	if err != nil {
		return nil, fmt.Errorf("preparing user insert query: %w", err)
	}
	defer func() {
		_ = userStmt.Close()
	}()

	for _, u := range users {
		var id int
		if queryErr := userStmt.QueryRowContext(ctx, u.Email, u.Name, u.Avatar).Scan(&id); queryErr != nil {
			return nil, fmt.Errorf("seeding user %s: %w", u.Email, queryErr)
		}
		userMap[u.Email] = id
		userNames[u.Email] = u.Name
		userAvatars[u.Email] = u.Avatar
		summary.UsersCount++
	}

	// Collect user IDs for generating realistic voter records.
	var allUserIDs []int
	for _, id := range userMap {
		allUserIDs = append(allUserIDs, id)
	}

	memeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (user_id, body, media_url, upvotes, downvotes, status, rejection_reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id;
	`)
	if err != nil {
		return nil, fmt.Errorf("preparing message insert query: %w", err)
	}
	defer func() {
		_ = memeStmt.Close()
	}()

	commentStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO comments (message_id, user_id, body, created_at)
		VALUES ($1, $2, $3, $4);
	`)
	if err != nil {
		return nil, fmt.Errorf("preparing comment insert query: %w", err)
	}
	defer func() {
		_ = commentStmt.Close()
	}()

	voteStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO message_votes (message_id, user_id, value, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (message_id, user_id) DO UPDATE SET value = EXCLUDED.value;
	`)
	if err != nil {
		return nil, fmt.Errorf("preparing vote insert query: %w", err)
	}
	defer func() {
		_ = voteStmt.Close()
	}()

	type indexedMeme struct {
		doc search.Document
	}
	var docsToIndex []indexedMeme

	now := time.Now().UTC()
	for _, m := range memes {
		authorID, ok := userMap[m.UserEmail]
		if !ok {
			slog.Warn("skipping meme with unknown author email", "email", m.UserEmail)
			continue
		}

		createdAt := now.Add(-time.Duration(m.DaysAgo) * 24 * time.Hour)
		var messageID int
		if scanErr := memeStmt.QueryRowContext(
			ctx,
			authorID,
			m.Text,
			m.MediaURL,
			m.Upvotes,
			m.Downvotes,
			string(repository.MessageStatusApproved),
			"",
			createdAt,
		).Scan(&messageID); scanErr != nil {
			return nil, fmt.Errorf("inserting meme message: %w", scanErr)
		}
		summary.MemesCount++

		// Seed individual message votes corresponding to the active users.
		voterCount := 0
		for _, voterID := range allUserIDs {
			if voterID == authorID {
				continue
			}
			voteVal := 1
			if voterCount >= m.Upvotes && voterCount < (m.Upvotes+m.Downvotes) {
				voteVal = -1
			} else if voterCount >= (m.Upvotes + m.Downvotes) {
				break
			}

			if _, voteErr := voteStmt.ExecContext(ctx, messageID, voterID, voteVal, createdAt); voteErr == nil {
				summary.VotesCount++
				voterCount++
			}
		}

		// Seed comments for this meme.
		allComments := make([]CommentFixture, 0, len(m.Comments)+m.TargetComments)
		allComments = append(allComments, m.Comments...)

		targetCount := m.TargetComments
		if targetCount == 0 && len(m.Comments) > 0 {
			targetCount = len(m.Comments)
		}

		if targetCount > len(allComments) && len(users) > 1 {
			var candidateEmails []string
			for _, u := range users {
				if u.Email != m.UserEmail {
					candidateEmails = append(candidateEmails, u.Email)
				}
			}
			if len(candidateEmails) == 0 {
				candidateEmails = append(candidateEmails, m.UserEmail)
			}

			needed := targetCount - len(allComments)
			for i := 0; i < needed; i++ {
				commenterEmail := candidateEmails[(i+summary.MemesCount*7)%len(candidateEmails)]
				commentText := seedCommentTemplates[(i+summary.MemesCount*11)%len(seedCommentTemplates)]
				allComments = append(allComments, CommentFixture{
					UserEmail: commenterEmail,
					Text:      commentText,
				})
			}
		}

		for idx, c := range allComments {
			commenterID, cOk := userMap[c.UserEmail]
			if !cOk {
				continue
			}
			commentTime := createdAt.Add(time.Duration(idx+1) * 7 * time.Minute)
			if _, commentErr := commentStmt.ExecContext(ctx, messageID, commenterID, c.Text, commentTime); commentErr != nil {
				return nil, fmt.Errorf("inserting comment for message %d: %w", messageID, commentErr)
			}
			summary.CommentsCount++
		}

		docsToIndex = append(docsToIndex, indexedMeme{
			doc: search.Document{
				ID:        messageID,
				UserID:    authorID,
				Author:    userNames[m.UserEmail],
				Avatar:    userAvatars[m.UserEmail],
				Text:      m.Text,
				MediaURL:  m.MediaURL,
				Upvotes:   m.Upvotes,
				Downvotes: m.Downvotes,
				Score:     m.Upvotes - m.Downvotes,
				CreatedAt: createdAt,
			},
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing seed transaction: %w", err)
	}

	// Index approved messages into search engine.
	if s.search != nil {
		for _, item := range docsToIndex {
			if idxErr := s.search.IndexMessage(ctx, item.doc); idxErr != nil {
				slog.WarnContext(ctx, "failed to index seed meme into search engine", "err", idxErr, "messageID", item.doc.ID)
			} else {
				summary.SearchIndexed++
			}
		}
	}

	return summary, nil
}

// Clean removes all seeded tables from PostgreSQL, deletes the search index, and clears active sessions.
func (s *Seeder) Clean(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `TRUNCATE TABLE comments, message_votes, messages, users RESTART IDENTITY CASCADE;`)
	if err != nil {
		return fmt.Errorf("truncating postgres tables: %w", err)
	}

	if s.search != nil {
		if delErr := s.search.DeleteIndex(ctx); delErr != nil {
			slog.WarnContext(ctx, "failed to delete elasticsearch index during clean", "err", delErr)
		}
	}

	if s.sessions != nil {
		if sessErr := s.sessions.ClearAll(ctx); sessErr != nil {
			slog.WarnContext(ctx, "failed to clear active sessions during clean", "err", sessErr)
		}
	}

	return nil
}

// Reset clears all existing data and seeds fresh fixtures.
func (s *Seeder) Reset(ctx context.Context) (*Summary, error) {
	if err := s.Clean(ctx); err != nil {
		return nil, fmt.Errorf("cleaning existing data before reset: %w", err)
	}
	return s.Seed(ctx)
}
