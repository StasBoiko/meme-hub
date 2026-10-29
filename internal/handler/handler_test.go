// Package handler implements HTTP handlers, routing, and user session management.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"meme-hub/internal/broker"
	"meme-hub/internal/config"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
)

// mockSearchService implements search.Service for handler unit tests.
type mockSearchService struct {
	results   []search.Document
	searchErr error
}

func (m *mockSearchService) IndexMessage(ctx context.Context, doc search.Document) error {
	return nil
}

func (m *mockSearchService) SearchMessages(ctx context.Context, query string, limit int) ([]search.Document, error) {
	return m.results, m.searchErr
}

func (m *mockSearchService) Ping(ctx context.Context) error {
	return nil
}

// mockRepository is an in-memory implementation of the Repository interface for testing.
type mockRepository struct {
	messages         []repository.MessageDTO
	comments         []repository.CommentDTO
	getMessagesErr   error
	createMessageErr error
	saveUserErr      error
	pingErr          error
	voteErr          error
	commentErr       error
	votes            map[string]int
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

// CreateMessage simulates saving a message in memory and returns a mock message ID or configured error.
func (m *mockRepository) CreateMessage(ctx context.Context, userID int, text string, mediaURL string) (int, error) {
	if m.createMessageErr != nil {
		return 0, m.createMessageErr
	}
	m.messages = append(m.messages, repository.MessageDTO{
		ID:        len(m.messages) + 1,
		Text:      text,
		MediaURL:  mediaURL,
		CreatedAt: time.Now(),
		Status:    repository.MessageStatusPending,
		User:      repository.SessionUser{ID: userID, Name: "Tester"},
	})
	return len(m.messages), nil
}

// UpdateMessageStatus simulates updating message status in memory.
func (m *mockRepository) UpdateMessageStatus(ctx context.Context, messageID int, status repository.MessageStatus, reason string) error {
	for i := range m.messages {
		if m.messages[i].ID == messageID {
			m.messages[i].Status = status
			m.messages[i].RejectionReason = reason
			return nil
		}
	}
	return nil
}

// VoteMessage simulates voting on a message in memory.
func (m *mockRepository) VoteMessage(ctx context.Context, messageID int, userID int, value int) (repository.VoteResult, error) {
	if m.voteErr != nil {
		return repository.VoteResult{}, m.voteErr
	}
	if value != 1 && value != -1 {
		return repository.VoteResult{}, errors.New("invalid vote")
	}
	if m.votes == nil {
		m.votes = make(map[string]int)
	}
	key := fmt.Sprintf("%d:%d", messageID, userID)
	existing := m.votes[key]
	var userVote int
	var upDelta, downDelta int
	if existing == 0 {
		m.votes[key] = value
		userVote = value
		if value == 1 {
			upDelta = 1
		} else {
			downDelta = 1
		}
	} else if existing == value {
		delete(m.votes, key)
		userVote = 0
		if value == 1 {
			upDelta = -1
		} else {
			downDelta = -1
		}
	} else {
		m.votes[key] = value
		userVote = value
		if value == 1 {
			upDelta = 1
			downDelta = -1
		} else {
			upDelta = -1
			downDelta = 1
		}
	}

	for i := range m.messages {
		if m.messages[i].ID == messageID {
			m.messages[i].Upvotes += upDelta
			m.messages[i].Downvotes += downDelta
			m.messages[i].Score = m.messages[i].Upvotes - m.messages[i].Downvotes
			return repository.VoteResult{
				MessageID: messageID,
				Upvotes:   m.messages[i].Upvotes,
				Downvotes: m.messages[i].Downvotes,
				Score:     m.messages[i].Score,
				UserVote:  userVote,
			}, nil
		}
	}

	return repository.VoteResult{
		MessageID: messageID,
		Upvotes:   max(0, upDelta),
		Downvotes: max(0, downDelta),
		Score:     upDelta - downDelta,
		UserVote:  userVote,
	}, nil
}

// GetMessages simulates retrieving messages from memory or returns a configured error.
func (m *mockRepository) GetMessages(ctx context.Context, currentUserID int, sortBy string, limit int) ([]repository.MessageDTO, error) {
	if m.getMessagesErr != nil {
		return nil, m.getMessagesErr
	}
	result := make([]repository.MessageDTO, len(m.messages))
	copy(result, m.messages)
	for i := range result {
		if m.votes != nil {
			key := fmt.Sprintf("%d:%d", result[i].ID, currentUserID)
			result[i].UserVote = m.votes[key]
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

// CreateComment simulates creating a comment in memory.
func (m *mockRepository) CreateComment(ctx context.Context, messageID int, userID int, text string) (repository.CommentDTO, error) {
	if m.commentErr != nil {
		return repository.CommentDTO{}, m.commentErr
	}
	c := repository.CommentDTO{
		ID:        len(m.comments) + 1,
		MessageID: messageID,
		Text:      text,
		CreatedAt: time.Now(),
		User:      repository.SessionUser{ID: userID, Name: "Commenter"},
	}
	m.comments = append(m.comments, c)
	for i := range m.messages {
		if m.messages[i].ID == messageID {
			m.messages[i].Comments = append(m.messages[i].Comments, c)
			m.messages[i].CommentsCount++
		}
	}
	return c, nil
}

// GetCommentsByMessageIDs simulates retrieving comments grouped by message IDs.
func (m *mockRepository) GetCommentsByMessageIDs(ctx context.Context, messageIDs []int) (map[int][]repository.CommentDTO, error) {
	if m.commentErr != nil {
		return nil, m.commentErr
	}
	result := make(map[int][]repository.CommentDTO)
	for _, id := range messageIDs {
		result[id] = make([]repository.CommentDTO, 0)
	}
	for _, c := range m.comments {
		for _, id := range messageIDs {
			if c.MessageID == id {
				result[id] = append(result[id], c)
			}
		}
	}
	return result, nil
}

// GetCommentsByMessageID simulates retrieving comments for a single message ID.
func (m *mockRepository) GetCommentsByMessageID(ctx context.Context, messageID int) ([]repository.CommentDTO, error) {
	if m.commentErr != nil {
		return nil, m.commentErr
	}
	result := make([]repository.CommentDTO, 0)
	for _, c := range m.comments {
		if c.MessageID == messageID {
			result = append(result, c)
		}
	}
	return result, nil
}

// newTestServer creates an initialized server instance with a mock repository for testing.
func newTestServer(t *testing.T, repo repository.Repository) *Server {
	t.Helper()
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template for test server: %v", err)
	}

	sessions := repository.NewMemorySessionStore()
	return New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   7 * 24 * time.Hour,
		},
	}, repo, tmpl, sessions, broker.NewNopPublisher(), search.NewNopService())
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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 42, Name: "Bob"}, time.Hour)

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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 42, Name: "Bob"}, time.Hour)

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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 42, Name: "Bob"}, time.Hour)

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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 42, Name: "Bob"}, time.Hour)

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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 1, Name: "Alice"}, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status 302 Found, got %d", rec.Code)
	}

	if _, ok, _ := s.sessions.Get(context.Background(), sessionID); ok {
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
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 10, Name: "AuthUser"}, time.Hour)

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

// roundTripperFunc adapts a function to the http.RoundTripper interface for testing.
type roundTripperFunc func(req *http.Request) (*http.Response, error)

// RoundTrip executes the mock round trip function.
func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestHandleLogin tests initiating Google OAuth authentication flow.
func TestHandleLogin(t *testing.T) {
	tests := []struct {
		name           string
		clientID       string
		clientSecret   string
		wantStatusCode int
		wantBodySubstr string
		checkCookie    bool
	}{
		{
			name:           "unconfigured oauth credentials return 500",
			clientID:       "",
			clientSecret:   "",
			wantStatusCode: http.StatusInternalServerError,
			wantBodySubstr: "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set",
			checkCookie:    false,
		},
		{
			name:           "configured credentials set state cookie and redirect",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			wantStatusCode: http.StatusFound,
			wantBodySubstr: "",
			checkCookie:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, &mockRepository{})
			s.oauth.ClientID = tc.clientID
			s.oauth.ClientSecret = tc.clientSecret

			req := httptest.NewRequest(http.MethodGet, "/login", nil)
			rec := httptest.NewRecorder()

			s.Routes().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Fatalf("expected status %d, got %d", tc.wantStatusCode, rec.Code)
			}
			if tc.wantBodySubstr != "" && !strings.Contains(rec.Body.String(), tc.wantBodySubstr) {
				t.Errorf("expected body to contain %q, got %q", tc.wantBodySubstr, rec.Body.String())
			}
			if tc.checkCookie {
				var stateCookie *http.Cookie
				for _, c := range rec.Result().Cookies() {
					if c.Name == "oauth_state" {
						stateCookie = c
						break
					}
				}
				if stateCookie == nil {
					t.Fatal("expected oauth_state cookie to be set")
				}
				if stateCookie.Value == "" {
					t.Error("expected oauth_state cookie value to be non-empty")
				}
				if !stateCookie.HttpOnly {
					t.Error("expected oauth_state cookie to be HttpOnly")
				}
				if stateCookie.SameSite != http.SameSiteLaxMode {
					t.Errorf("expected SameSiteLax, got %v", stateCookie.SameSite)
				}
				loc := rec.Header().Get("Location")
				if !strings.Contains(loc, "client_id=test-client-id") {
					t.Errorf("expected redirect location to contain client_id, got %q", loc)
				}
				if !strings.Contains(loc, "state="+stateCookie.Value) {
					t.Errorf("expected redirect location to contain state, got %q", loc)
				}
			}
		})
	}
}

// TestHandleCallback tests OAuth callback error validation and successful session setup.
func TestHandleCallback(t *testing.T) {
	tests := []struct {
		name           string
		stateQuery     string
		cookieState    string
		hasStateCookie bool
		code           string
		transport      http.RoundTripper
		saveUserErr    error
		wantStatusCode int
		wantBodySubstr string
		checkSession   bool
	}{
		{
			name:           "missing oauth state cookie",
			stateQuery:     "abc",
			hasStateCookie: false,
			code:           "123",
			wantStatusCode: http.StatusBadRequest,
			wantBodySubstr: "invalid oauth state",
		},
		{
			name:           "mismatched state query and cookie",
			stateQuery:     "abc",
			cookieState:    "xyz",
			hasStateCookie: true,
			code:           "123",
			wantStatusCode: http.StatusBadRequest,
			wantBodySubstr: "invalid oauth state",
		},
		{
			name:           "missing code parameter",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "",
			wantStatusCode: http.StatusBadRequest,
			wantBodySubstr: "missing code",
		},
		{
			name:           "token exchange failure returns 502 Bad Gateway",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error on token exchange")
			}),
			wantStatusCode: http.StatusBadGateway,
			wantBodySubstr: "token exchange failed",
		},
		{
			name:           "profile fetch failure returns 502 Bad Gateway",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "userinfo") {
					return nil, errors.New("network error on userinfo")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
				}, nil
			}),
			wantStatusCode: http.StatusBadGateway,
			wantBodySubstr: "failed to fetch profile",
		},
		{
			name:           "invalid profile json returns 502 Bad Gateway",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`not-valid-json`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
				}, nil
			}),
			wantStatusCode: http.StatusBadGateway,
			wantBodySubstr: "invalid profile response",
		},
		{
			name:           "invalid profile email returns 400 Bad Request",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"email":"not-an-email","name":"Test"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
				}, nil
			}),
			wantStatusCode: http.StatusBadRequest,
			wantBodySubstr: "google account has no valid email",
		},
		{
			name:           "repository save user error returns 500",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			saveUserErr:    errors.New("db insert failure"),
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"email":"valid@example.com","name":"Valid User","picture":"https://pic.jpg"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
				}, nil
			}),
			wantStatusCode: http.StatusInternalServerError,
			wantBodySubstr: "could not save user",
		},
		{
			name:           "successful oauth callback sets session and redirects",
			stateQuery:     "abc",
			cookieState:    "abc",
			hasStateCookie: true,
			code:           "valid-code",
			transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "userinfo") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"email":"valid@example.com","name":"Valid User","picture":"https://pic.jpg"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
				}, nil
			}),
			wantStatusCode: http.StatusFound,
			checkSession:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepository{saveUserErr: tc.saveUserErr}
			s := newTestServer(t, repo)

			targetURL := "/callback"
			if tc.stateQuery != "" || tc.code != "" {
				vals := url.Values{}
				if tc.stateQuery != "" {
					vals.Set("state", tc.stateQuery)
				}
				if tc.code != "" {
					vals.Set("code", tc.code)
				}
				targetURL += "?" + vals.Encode()
			}

			req := httptest.NewRequest(http.MethodGet, targetURL, nil)
			if tc.hasStateCookie {
				req.AddCookie(&http.Cookie{Name: "oauth_state", Value: tc.cookieState})
			}

			if tc.transport != nil {
				mockHTTPClient := &http.Client{Transport: tc.transport}
				ctx := context.WithValue(req.Context(), oauth2.HTTPClient, mockHTTPClient)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.wantStatusCode, rec.Code, rec.Body.String())
			}
			if tc.wantBodySubstr != "" && !strings.Contains(rec.Body.String(), tc.wantBodySubstr) {
				t.Errorf("expected body to contain %q, got %q", tc.wantBodySubstr, rec.Body.String())
			}
			if tc.checkSession {
				var sessionCookie *http.Cookie
				for _, c := range rec.Result().Cookies() {
					if c.Name == s.cfg.Session.CookieName {
						sessionCookie = c
						break
					}
				}
				if sessionCookie == nil {
					t.Fatal("expected session cookie to be set")
				}
				u, exists, err := s.sessions.Get(context.Background(), sessionCookie.Value)
				if err != nil || !exists {
					t.Fatal("expected session to be stored in server sessions store")
				}
				if u.Email != "valid@example.com" {
					t.Errorf("expected user email valid@example.com, got %q", u.Email)
				}
				if loc := rec.Header().Get("Location"); loc != "/" {
					t.Errorf("expected redirect to /, got %q", loc)
				}
			}
		})
	}
}

// TestHandleIndex_NotFound tests that non-root paths return 404 Not Found.
func TestHandleIndex_NotFound(t *testing.T) {
	s := newTestServer(t, &mockRepository{})
	req := httptest.NewRequest(http.MethodGet, "/unknown-page", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 Not Found, got %d", rec.Code)
	}
}

// TestHandleIndex_Authenticated tests rendering the index page with active user session.
func TestHandleIndex_Authenticated(t *testing.T) {
	s := newTestServer(t, &mockRepository{})
	sessionID := "auth-session-123"
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{
		ID:     99,
		Name:   "Authenticated User",
		Email:  "auth@example.com",
		Avatar: "https://example.com/avatar.jpg",
	}, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Authenticated User") {
		t.Errorf("expected body to contain user name 'Authenticated User', got: %s", body)
	}
	if !strings.Contains(body, "https://example.com/avatar.jpg") {
		t.Errorf("expected body to contain user avatar, got: %s", body)
	}
}

// TestHandleIndex_XSSEscaping tests that HTML template automatically escapes malicious scripts.
func TestHandleIndex_XSSEscaping(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{
				ID:        1,
				Text:      `<script>alert("xss-message")</script>`,
				CreatedAt: time.Now(),
				User: repository.SessionUser{
					ID:   1,
					Name: `<b onmouseover="alert('xss-user')">Hacker</b>`,
				},
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
	if strings.Contains(body, `<script>alert("xss-message")</script>`) {
		t.Error("body contains unescaped script tag in message text")
	}
	if strings.Contains(body, `<b onmouseover=`) {
		t.Error("body contains unescaped html in user name")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected escaped script tag in body")
	}
}

// TestHandleMessages_MethodNotAllowed tests that non-POST methods to /messages return 405.
func TestHandleMessages_MethodNotAllowed(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete}
	s := newTestServer(t, &mockRepository{})

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/messages", nil)
			rec := httptest.NewRecorder()

			s.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405 Method Not Allowed for %s, got %d", method, rec.Code)
			}
		})
	}
}

// TestHandleAPIMessages_MethodNotAllowed tests that non-GET methods to /api/messages return 405.
func TestHandleAPIMessages_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, &mockRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/messages", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 Method Not Allowed, got %d", rec.Code)
	}
}

// TestHandleAPIMessages_RepositoryError tests that database failure returns 500 on /api/messages.
func TestHandleAPIMessages_RepositoryError(t *testing.T) {
	repo := &mockRepository{getMessagesErr: errors.New("database connection down")}
	s := newTestServer(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 Internal Server Error, got %d", rec.Code)
	}
}

// TestHandleLogout_NoCookie tests that logout without cookie still redirects and clears cookie.
func TestHandleLogout_NoCookie(t *testing.T) {
	s := newTestServer(t, &mockRepository{})
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status 302 Found, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}

// TestFormatTime tests timestamp formatting helper for templates.
func TestFormatTime(t *testing.T) {
	fixedTime := time.Date(2026, time.October, 28, 14, 30, 0, 0, time.Local)
	got := FormatTime(fixedTime)
	want := "28.10.2026, 14:30"
	if got != want {
		t.Fatalf("FormatTime() = %q, want %q", got, want)
	}
}

// TestNewTemplate tests template parser initialization and fallback handling.
func TestNewTemplate(t *testing.T) {
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("expected template to parse successfully, got err: %v", err)
	}
	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}

	_, err = NewTemplate("nonexistent/path/template.html")
	if err == nil {
		t.Error("expected error for nonexistent template path, got nil")
	}
}

// TestConcurrentSessions verifies concurrent safety of session store during simultaneous operations.
func TestConcurrentSessions(t *testing.T) {
	s := newTestServer(t, &mockRepository{})
	const count = 50

	for i := 0; i < count; i++ {
		sid := "session-" + strconv.Itoa(i)
		_ = s.sessions.Set(context.Background(), sid, repository.SessionUser{ID: i, Name: "User"}, time.Hour)
	}

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(2)
		sessionID := "session-" + strconv.Itoa(i)

		go func(sid string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sid})
			s.CurrentUser(req)
		}(sessionID)

		go func(sid string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/logout", nil)
			req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sid})
			rec := httptest.NewRecorder()
			s.handleLogout(rec, req)
		}(sessionID)
	}

	wg.Wait()
}

// mockPublisher records tasks published during handler tests.
type mockPublisher struct {
	tasks      []broker.ModerationTask
	emailTasks []broker.EmailTask
	err        error
}

func (m *mockPublisher) PublishModerationTask(ctx context.Context, task broker.ModerationTask) error {
	if m.err != nil {
		return m.err
	}
	m.tasks = append(m.tasks, task)
	return nil
}

func (m *mockPublisher) PublishEmailTask(ctx context.Context, task broker.EmailTask) error {
	if m.err != nil {
		return m.err
	}
	m.emailTasks = append(m.emailTasks, task)
	return nil
}

func (m *mockPublisher) Close() error {
	return nil
}

// TestHandleMessages_PublisherInvoked verifies that submitting a new message triggers task publication to the broker.
func TestHandleMessages_PublisherInvoked(t *testing.T) {
	repo := &mockRepository{}
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}
	sessions := repository.NewMemorySessionStore()
	pub := &mockPublisher{}

	s := New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   time.Hour,
		},
	}, repo, tmpl, sessions, pub, search.NewNopService())

	_ = sessions.Set(context.Background(), "test-sid", repository.SessionUser{ID: 99, Name: "Tester"}, time.Hour)

	form := url.Values{}
	form.Set("text", "A wonderful guest message for testing publisher.")

	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: "test-sid"})

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rec.Code)
	}

	if len(pub.tasks) != 1 {
		t.Fatalf("expected 1 published moderation task, got %d", len(pub.tasks))
	}

	if pub.tasks[0].UserID != 99 {
		t.Errorf("task.UserID = %d, want 99", pub.tasks[0].UserID)
	}
	if pub.tasks[0].Text != "A wonderful guest message for testing publisher." {
		t.Errorf("task.Text = %q, want expected message", pub.tasks[0].Text)
	}
}

// TestHandleCallback_WelcomeEmailDispatched verifies that successful login triggers a welcome email task.
func TestHandleCallback_WelcomeEmailDispatched(t *testing.T) {
	repo := &mockRepository{}
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}
	sessions := repository.NewMemorySessionStore()
	pub := &mockPublisher{}

	s := New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   time.Hour,
		},
	}, repo, tmpl, sessions, pub, search.NewNopService())

	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "userinfo") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"email":"newuser@example.com","name":"New User","picture":"https://pic.jpg"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"mock-token","token_type":"Bearer"}`)),
		}, nil
	})

	mockHTTPClient := &http.Client{Transport: transport}
	req := httptest.NewRequest(http.MethodGet, "/callback?state=teststate&code=testcode", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "teststate"})
	ctx := context.WithValue(req.Context(), oauth2.HTTPClient, mockHTTPClient)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rec.Code)
	}

	if len(pub.emailTasks) != 1 {
		t.Fatalf("expected 1 published email task, got %d", len(pub.emailTasks))
	}

	if pub.emailTasks[0].To[0] != "newuser@example.com" {
		t.Errorf("email task recipient = %q, want %q", pub.emailTasks[0].To[0], "newuser@example.com")
	}
	if pub.emailTasks[0].Subject != "Welcome to Smart Guestbook!" {
		t.Errorf("email task subject = %q, want %q", pub.emailTasks[0].Subject, "Welcome to Smart Guestbook!")
	}
}

// TestHandleIndex_SearchQuery tests searching entries via query parameter q.
func TestHandleIndex_SearchQuery(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{ID: 1, Text: "Fallback message from DB", Status: repository.MessageStatusApproved},
		},
	}
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	tests := []struct {
		name           string
		queryURL       string
		searcher       search.Service
		wantBodySubstr string
	}{
		{
			name:     "matching search result rendered",
			queryURL: "/?q=golang",
			searcher: &mockSearchService{
				results: []search.Document{
					{ID: 10, Author: "Gopher", Text: "Golang search hit!"},
				},
			},
			wantBodySubstr: "Golang search hit!",
		},
		{
			name:     "search failure falls back to database entries",
			queryURL: "/?q=error",
			searcher: &mockSearchService{
				searchErr: errors.New("es down"),
			},
			wantBodySubstr: "Fallback message from DB",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := New(config.Config{
				Port: "8080",
				Session: config.SessionConfig{
					CookieName: "session",
					Duration:   time.Hour,
				},
			}, repo, tmpl, repository.NewMemorySessionStore(), broker.NewNopPublisher(), tc.searcher)

			req := httptest.NewRequest(http.MethodGet, tc.queryURL, nil)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBodySubstr) {
				t.Errorf("body missing substring %q", tc.wantBodySubstr)
			}
		})
	}
}

// TestHandleAPIMessages_SearchQuery tests searching entries via /api/messages?q=...
func TestHandleAPIMessages_SearchQuery(t *testing.T) {
	repo := &mockRepository{}
	tmpl, err := NewTemplate("../../web/template/index.html")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	srch := &mockSearchService{
		results: []search.Document{
			{ID: 20, Author: "ElasticUser", Text: "API search message"},
		},
	}

	s := New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   time.Hour,
		},
	}, repo, tmpl, repository.NewMemorySessionStore(), broker.NewNopPublisher(), srch)

	req := httptest.NewRequest(http.MethodGet, "/api/messages?q=API", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "API search message") {
		t.Errorf("expected json response to contain %q, got %s", "API search message", rec.Body.String())
	}
}

// TestHandleVote tests message voting via API and form submissions.
func TestHandleVote(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{ID: 1, Text: "Meme 1", Upvotes: 0, Downvotes: 0, Score: 0},
		},
	}
	s := newTestServer(t, repo)

	sessionID := "vote-session"
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 10, Name: "Voter"}, time.Hour)

	t.Run("authenticated JSON upvote succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/vote", strings.NewReader(`{"value":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"user_vote":1`) {
			t.Errorf("expected user_vote 1 in response, got %s", rec.Body.String())
		}
	})

	t.Run("unauthenticated vote returns 401 Unauthorized for JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/vote", strings.NewReader(`{"value":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("invalid vote value returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/vote", strings.NewReader(`{"value":5}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("form vote submission redirects", func(t *testing.T) {
		form := url.Values{}
		form.Set("value", "1")
		req := httptest.NewRequest(http.MethodPost, "/messages/1/vote", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected status 303 See Other, got %d", rec.Code)
		}
	})
}

// TestHandleMessages_WithMediaURL tests creating a message with optional media URL.
func TestHandleMessages_WithMediaURL(t *testing.T) {
	repo := &mockRepository{}
	s := newTestServer(t, repo)

	sessionID := "media-session"
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 15, Name: "Poster"}, time.Hour)

	t.Run("valid media url accepted", func(t *testing.T) {
		form := url.Values{}
		form.Set("text", "Check this funny cat!")
		form.Set("media_url", "https://example.com/cat.gif")

		req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected status 303 See Other, got %d", rec.Code)
		}
		if len(repo.messages) == 0 || repo.messages[len(repo.messages)-1].MediaURL != "https://example.com/cat.gif" {
			t.Errorf("media_url was not stored properly")
		}
	})

	t.Run("invalid url scheme rejected", func(t *testing.T) {
		form := url.Values{}
		form.Set("text", "Bad scheme")
		form.Set("media_url", "ftp://example.com/file.mp4")

		req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})
}

// TestIsImageURL tests detection of image file formats.
func TestIsImageURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/pic.png", true},
		{"https://example.com/photo.JPG", true},
		{"https://example.com/anim.gif?size=large", true},
		{"https://example.com/art.webp", true},
		{"https://example.com/video.mp4", false},
		{"https://youtube.com/watch?v=123", false},
		{"", false},
	}

	for _, tc := range tests {
		got := IsImageURL(tc.url)
		if got != tc.want {
			t.Errorf("IsImageURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// TestIsVideoURL tests detection of video file formats.
func TestIsVideoURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/clip.mp4", true},
		{"https://example.com/clip.webm?source=web", true},
		{"https://example.com/pic.jpg", false},
		{"https://youtube.com/watch?v=abc", false},
		{"", false},
	}

	for _, tc := range tests {
		got := IsVideoURL(tc.url)
		if got != tc.want {
			t.Errorf("IsVideoURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// TestYouTubeEmbedURL tests generation of YouTube embed iframe URLs.
func TestYouTubeEmbedURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://example.com/video.mp4", ""},
		{"", ""},
	}

	for _, tc := range tests {
		got := YouTubeEmbedURL(tc.url)
		if got != tc.want {
			t.Errorf("YouTubeEmbedURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// TestHandleCreateComment tests creating comments under meme messages.
func TestHandleCreateComment(t *testing.T) {
	repo := &mockRepository{
		messages: []repository.MessageDTO{
			{ID: 1, Text: "Meme 1", Status: repository.MessageStatusApproved},
		},
	}
	s := newTestServer(t, repo)

	sessionID := "comment-session"
	_ = s.sessions.Set(context.Background(), sessionID, repository.SessionUser{ID: 25, Name: "MemeCritic"}, time.Hour)

	t.Run("authenticated JSON comment succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/comments", strings.NewReader(`{"text":"Great meme!"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var c repository.CommentDTO
		if err := json.NewDecoder(rec.Body).Decode(&c); err != nil {
			t.Fatalf("failed to decode comment json: %v", err)
		}
		if c.Text != "Great meme!" || c.User.ID != 25 {
			t.Errorf("unexpected comment DTO: %+v", c)
		}
	})

	t.Run("unauthenticated comment returns 401 for JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/comments", strings.NewReader(`{"text":"Hello"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated comment redirects to login for form", func(t *testing.T) {
		form := url.Values{}
		form.Set("text", "Hello")
		req := httptest.NewRequest(http.MethodPost, "/messages/1/comments", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected status 303 See Other, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Errorf("expected redirect to /login, got %s", loc)
		}
	})

	t.Run("empty comment text returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/comments", strings.NewReader(`{"text":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("comment exceeding max length returns 400 Bad Request", func(t *testing.T) {
		longText := strings.Repeat("A", 1001)
		req := httptest.NewRequest(http.MethodPost, "/api/messages/1/comments", strings.NewReader(fmt.Sprintf(`{"text":%q}`, longText)))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("form comment submission redirects to card", func(t *testing.T) {
		form := url.Values{}
		form.Set("text", "Nice punchline!")
		req := httptest.NewRequest(http.MethodPost, "/messages/1/comments", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected status 303 See Other, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/#message-card-1" {
			t.Errorf("expected redirect to /#message-card-1, got %s", loc)
		}
	})

	t.Run("invalid message ID returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/invalid/comments", strings.NewReader(`{"text":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: s.cfg.Session.CookieName, Value: sessionID})
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})
}

// TestHandleGetComments tests retrieving comments for a message via JSON API.
func TestHandleGetComments(t *testing.T) {
	repo := &mockRepository{
		comments: []repository.CommentDTO{
			{
				ID:        1,
				MessageID: 10,
				Text:      "First comment",
				CreatedAt: time.Now(),
				User:      repository.SessionUser{ID: 1, Name: "Alice"},
			},
			{
				ID:        2,
				MessageID: 10,
				Text:      "Second comment",
				CreatedAt: time.Now(),
				User:      repository.SessionUser{ID: 2, Name: "Bob"},
			},
		},
	}
	s := newTestServer(t, repo)

	t.Run("successful JSON retrieval of comments", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/10/comments", nil)
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d", rec.Code)
		}
		var list []repository.CommentDTO
		if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 comments, got %d", len(list))
		}
		if list[0].Text != "First comment" || list[1].Text != "Second comment" {
			t.Errorf("unexpected comment texts: %+v", list)
		}
	})

	t.Run("invalid message id returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/messages/abc/comments", nil)
		rec := httptest.NewRecorder()

		s.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})
}
