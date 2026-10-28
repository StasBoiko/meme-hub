// Package handler implements HTTP handlers, routing, and user session management.
package handler

import (
	"context"
	"errors"
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

	sessions := repository.NewMemorySessionStore()
	return New(config.Config{
		Port: "8080",
		Session: config.SessionConfig{
			CookieName: "session",
			Duration:   7 * 24 * time.Hour,
		},
	}, repo, tmpl, sessions)
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
