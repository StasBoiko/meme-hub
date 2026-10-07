// Package handler implements HTTP handlers, routing, and user session management.
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"guestbook-app/internal/config"
	"guestbook-app/internal/repository"
)

// mockRepository is an in-memory implementation of the Repository interface for testing.
type mockRepository struct {
	messages         []repository.MessageDTO
	getMessagesErr   error
	createMessageErr error
	saveUserErr      error
	pingErr          error
}

// Ping simulates database connectivity check or returns a configured error.
func (m *mockRepository) Ping(ctx context.Context) error {
	return m.pingErr
}

// SaveUser simulates saving a user and returns a mock session user or configured error.
func (m *mockRepository) SaveUser(ctx context.Context, email, name, avatar string) (repository.SessionUser, error) {
	if m.saveUserErr != nil {
		return repository.SessionUser{}, m.saveUserErr
	}
	return repository.SessionUser{ID: 1, Email: email, Name: name, Avatar: avatar}, nil
}

// CreateMessage simulates saving a message in memory or returns a configured error.
func (m *mockRepository) CreateMessage(ctx context.Context, userID int, text string) error {
	if m.createMessageErr != nil {
		return m.createMessageErr
	}
	m.messages = append(m.messages, repository.MessageDTO{
		ID:        len(m.messages) + 1,
		Text:      text,
		CreatedAt: time.Now(),
		User:      repository.SessionUser{ID: userID, Name: "Tester"},
	})
	return nil
}

// GetMessages simulates retrieving messages from memory or returns a configured error.
func (m *mockRepository) GetMessages(ctx context.Context, limit int) ([]repository.MessageDTO, error) {
	if m.getMessagesErr != nil {
		return nil, m.getMessagesErr
	}
	if limit > 0 && len(m.messages) > limit {
		return m.messages[:limit], nil
	}
	return m.messages, nil
}

// newTestServer creates an initialized server instance with a mock repository for testing.
func newTestServer(t *testing.T, repo repository.Repository) *Server {
	t.Helper()
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template for test server: %v", err)
	}

	return New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   7 * 24 * time.Hour,
		},
	}, repo, tmpl)
}

// TestHandleHealthz tests GET /healthz behavior under various conditions using table-driven tests.
func TestHandleHealthz(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		pingErr        error
		wantStatusCode int
		wantBodySubstr string
	}{
		{
			name:           "healthy database connection returns 200 OK",
			method:         http.MethodGet,
			pingErr:        nil,
			wantStatusCode: http.StatusOK,
			wantBodySubstr: `"status":"ok"`,
		},
		{
			name:           "unhealthy database returns 503 Service Unavailable",
			method:         http.MethodGet,
			pingErr:        errors.New("db connection lost"),
			wantStatusCode: http.StatusServiceUnavailable,
			wantBodySubstr: `"status":"unavailable"`,
		},
		{
			name:           "non-GET method returns 405 Method Not Allowed",
			method:         http.MethodPost,
			pingErr:        nil,
			wantStatusCode: http.StatusMethodNotAllowed,
			wantBodySubstr: "method not allowed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepository{pingErr: tc.pingErr}
			s := newTestServer(t, repo)

			req := httptest.NewRequest(tc.method, "/healthz", nil)
			rec := httptest.NewRecorder()

			s.Routes().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Fatalf("expected status %d, got %d", tc.wantStatusCode, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBodySubstr) {
				t.Errorf("expected body to contain %q, got %q", tc.wantBodySubstr, rec.Body.String())
			}
		})
	}
}

// TestHandleIndex_Success tests that GET / renders the guestbook page with existing messages.
func TestHandleIndex_Success(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{
				ID:        1,
				Text:      "Welcome to the guestbook!",
				CreatedAt: time.Now(),
				User:      repository.SessionUser{ID: 1, Name: "Alice"},
			},
		},
	}
	s := newTestServer(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Welcome to the guestbook!") {
		t.Errorf("expected rendered page to contain message, got body: %s", body)
	}
}

// TestHandleIndex_RepositoryError tests that GET / returns a 500 error when the repository fails.
func TestHandleIndex_RepositoryError(t *testing.T) {
	repo := &mockRepository{
		getMessagesErr: errors.New("failed to query database"),
	}
	s := newTestServer(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 Internal Server Error, got %d", rec.Code)
	}
}

// TestHandleMessages_Success tests that an authenticated user can submit a message.
func TestHandleMessages_Success(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	sessionID := "test-session-id"
	s.sessions[sessionID] = repository.SessionUser{ID: 42, Name: "Bob"}

	form := url.Values{}
	form.Set("text", "A valid test message")

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 See Other redirect, got %d", rec.Code)
	}

	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 message in repository, got %d", len(repo.messages))
	}
	if repo.messages[0].Text != "A valid test message" {
		t.Errorf("expected stored message text to match, got %q", repo.messages[0].Text)
	}
}

// TestHandleMessages_TooLong tests that messages exceeding 2000 runes are rejected with 400 Bad Request.
func TestHandleMessages_TooLong(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	sessionID := "test-session-id"
	s.sessions[sessionID] = repository.SessionUser{ID: 42, Name: "Bob"}

	longText := strings.Repeat("abcd", 501) // 2004 runes
	form := url.Values{}
	form.Set("text", longText)

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "message is too long") {
		t.Errorf("expected body to contain %q, got %q", "message is too long", rec.Body.String())
	}
}

// TestHandleMessages_RepositoryError tests that repository failures during message creation return 500.
func TestHandleMessages_RepositoryError(t *testing.T) {
	repo := &mockRepository{
		createMessageErr: errors.New("database insert error"),
	}
	s := newTestServer(t, repo)

	sessionID := "test-session-id"
	s.sessions[sessionID] = repository.SessionUser{ID: 42, Name: "Bob"}

	form := url.Values{}
	form.Set("text", "Valid message that fails to save")

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 Internal Server Error, got %d", rec.Code)
	}
}

// TestHandleMessages_Empty tests that empty or whitespace-only messages are rejected with 400 Bad Request.
func TestHandleMessages_Empty(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	sessionID := "test-session-id"
	s.sessions[sessionID] = repository.SessionUser{ID: 42, Name: "Bob"}

	form := url.Values{}
	form.Set("text", "   ")

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "message is empty") {
		t.Errorf("expected body to contain %q, got %q", "message is empty", rec.Body.String())
	}
}

// TestHandleMessages_Unauthorized tests that unauthenticated requests redirect to /login.
func TestHandleMessages_Unauthorized(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	form := url.Values{}
	form.Set("text", "Message from guest without session")

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 See Other redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

// TestHandleLogout tests clearing user session and cookie.
func TestHandleLogout(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	sessionID := "active-session"
	s.sessions[sessionID] = repository.SessionUser{ID: 1, Name: "Alice"}

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status 302 Found, got %d", rec.Code)
	}

	if _, ok := s.sessions[sessionID]; ok {
		t.Error("expected session to be removed from memory")
	}
}

// TestHandleAPIMessages tests fetching messages via JSON API.
func TestHandleAPIMessages(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{
				ID:        1,
				Text:      "API Message",
				CreatedAt: time.Now(),
				User:      repository.SessionUser{ID: 1, Name: "Tester"},
			},
		},
	}
	s := newTestServer(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "API Message") {
		t.Errorf("expected body to contain message text, got %q", rec.Body.String())
	}
}

// TestHandleAPIMe tests current user status endpoint.
func TestHandleAPIMe(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	// Unauthenticated
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", rec.Code)
	}

	// Authenticated
	sessionID := "user-session"
	s.sessions[sessionID] = repository.SessionUser{ID: 10, Name: "AuthUser"}

	authReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	authReq.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	authRec := httptest.NewRecorder()
	s.Routes().ServeHTTP(authRec, authReq)

	if authRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", authRec.Code)
	}
	if !strings.Contains(authRec.Body.String(), "AuthUser") {
		t.Errorf("expected body to contain AuthUser, got %q", authRec.Body.String())
	}
}

// TestInitials tests extracting initials from names using table-driven tests.
func TestInitials(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single name",
			in:   "Alice",
			want: "A",
		},
		{
			name: "full name",
			in:   "Bob Smith",
			want: "BS",
		},
		{
			name: "three names",
			in:   "Charlie Dave Evans",
			want: "CD",
		},
		{
			name: "empty name",
			in:   "",
			want: "?",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := Initials(tc.in); got != tc.want {
				t.Fatalf("Initials(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
