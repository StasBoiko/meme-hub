// Package seed provides test data generation, database seeding, and fixture cleanup utilities.
package seed

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"meme-hub/internal/search"
)

// TestLoadFixtures verifies that embedded fixtures load valid users and memes.
func TestLoadFixtures(t *testing.T) {
	seeder := NewSeeder(nil, nil)
	users, memes, err := seeder.LoadFixtures()
	if err != nil {
		t.Fatalf("unexpected LoadFixtures error: %v", err)
	}

	if len(users) == 0 {
		t.Fatal("expected at least 1 user in fixtures, got 0")
	}

	if len(memes) == 0 {
		t.Fatal("expected at least 1 meme in fixtures, got 0")
	}

	for _, u := range users {
		if u.Email == "" || u.Name == "" {
			t.Errorf("invalid user fixture: %+v", u)
		}
	}

	for _, m := range memes {
		if m.UserEmail == "" || m.Text == "" {
			t.Errorf("invalid meme fixture: %+v", m)
		}
	}
}

// TestSeeder_Clean tests that Clean executes the expected truncate query.
func TestSeeder_Clean(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	mock.ExpectExec(regexp.QuoteMeta("TRUNCATE TABLE comments, message_votes, messages, users RESTART IDENTITY CASCADE;")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	nopSearch := search.NewNopService()
	seeder := NewSeeder(db, nopSearch)

	if cleanErr := seeder.Clean(context.Background()); cleanErr != nil {
		t.Fatalf("unexpected Clean error: %v", cleanErr)
	}

	if mockErr := mock.ExpectationsWereMet(); mockErr != nil {
		t.Errorf("unfulfilled expectations: %v", mockErr)
	}
}
