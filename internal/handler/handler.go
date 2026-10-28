// Package handler implements HTTP handlers, routing, and user session management.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"guestbook-app/internal/config"
	"guestbook-app/internal/repository"
	"guestbook-app/internal/validator"
)

// Server represents the HTTP application server and its dependencies.
type Server struct {
	cfg      config.Config
	repo     repository.Repository
	sessions repository.SessionStore
	oauth    *oauth2.Config
	tmpl     *template.Template
}

// pageData contains data passed to the HTML template for rendering.
type pageData struct {
	User     *repository.SessionUser
	Messages []repository.MessageDTO
}

// googleProfile represents user information returned by the Google OAuth2 API.
type googleProfile struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// New creates a new Server instance.
func New(cfg config.Config, repo repository.Repository, tmpl *template.Template, sessions repository.SessionStore) *Server {
	return &Server{
		cfg:      cfg,
		repo:     repo,
		sessions: sessions,
		tmpl:     tmpl,
		oauth: &oauth2.Config{
			ClientID:     cfg.OAuth.ClientID,
			ClientSecret: cfg.OAuth.ClientSecret,
			RedirectURL:  cfg.OAuth.RedirectURL,
			Scopes: []string{
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
			Endpoint: google.Endpoint,
		},
	}
}

// Routes registers HTTP handlers and returns the configured http.Handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/messages", s.handleMessages)
	mux.HandleFunc("/api/messages", s.handleAPIMessages)
	mux.HandleFunc("/api/me", s.handleAPIMe)
	return mux
}

// handleHealthz checks server health and database connectivity.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := s.repo.Ping(ctx); err != nil {
		slog.ErrorContext(ctx, "health check ping failed", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		if encErr := json.NewEncoder(w).Encode(map[string]string{
			"status": "unavailable",
			"error":  "database ping failed",
		}); encErr != nil {
			slog.ErrorContext(ctx, "failed to encode health check failure response", "err", encErr)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	}); err != nil {
		slog.ErrorContext(ctx, "failed to encode health check success response", "err", err)
	}
}

// handleIndex handles GET requests to the root path and renders the guestbook page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	messages, err := s.repo.GetMessages(r.Context(), 100)
	if err != nil {
		slog.ErrorContext(r.Context(), "error loading messages", "err", err)
		http.Error(w, "could not load messages", http.StatusInternalServerError)
		return
	}

	data := pageData{Messages: messages}
	if user, ok := s.CurrentUser(r); ok {
		data.User = &user
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		slog.ErrorContext(r.Context(), "template execution failed", "err", err)
	}
}

// handleLogin initiates the Google OAuth2 authentication flow.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauth.ClientID == "" || s.oauth.ClientSecret == "" {
		slog.ErrorContext(r.Context(), "oauth credentials not configured")
		http.Error(w, "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set", http.StatusInternalServerError)
		return
	}

	state, err := randomToken()
	if err != nil {
		slog.ErrorContext(r.Context(), "error generating oauth state", "err", err)
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
}

// handleCallback handles the Google OAuth2 redirect callback and sets up a session.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	token, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		slog.ErrorContext(ctx, "token exchange failed", "err", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	client := s.oauth.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		slog.ErrorContext(ctx, "failed to fetch profile", "err", err)
		http.Error(w, "failed to fetch profile", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var profile googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		slog.ErrorContext(ctx, "invalid profile response", "err", err)
		http.Error(w, "invalid profile response", http.StatusBadGateway)
		return
	}
	if err := validator.ValidateEmail(profile.Email); err != nil {
		slog.ErrorContext(ctx, "invalid profile email", "email", profile.Email, "err", err)
		http.Error(w, "google account has no valid email", http.StatusBadRequest)
		return
	}

	user, err := s.repo.SaveUser(ctx, profile.Email, profile.Name, profile.Picture)
	if err != nil {
		slog.ErrorContext(ctx, "error saving user", "err", err)
		http.Error(w, "could not save user", http.StatusInternalServerError)
		return
	}

	sid, err := randomToken()
	if err != nil {
		slog.ErrorContext(ctx, "error creating session", "err", err)
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	if err := s.sessions.Set(ctx, sid, user, s.cfg.Session.Duration); err != nil {
		slog.ErrorContext(ctx, "error saving session", "err", err)
		http.Error(w, "could not save session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    sid,
		Path:     "/",
		MaxAge:   int(s.cfg.Session.Duration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout clears the current user session and redirects to the home page.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cfg.Session.CookieName); err == nil && c.Value != "" {
		if err := s.sessions.Delete(r.Context(), c.Value); err != nil {
			slog.ErrorContext(r.Context(), "error deleting session", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: s.cfg.Session.CookieName, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleMessages handles POST submissions for new guestbook entries.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := s.CurrentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if err := validator.ValidateMessageText(text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.repo.CreateMessage(r.Context(), user.ID, text); err != nil {
		slog.ErrorContext(r.Context(), "error saving message", "err", err)
		http.Error(w, "could not save message", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleAPIMessages returns the list of guestbook messages as JSON.
func (s *Server) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	messages, err := s.repo.GetMessages(r.Context(), 100)
	if err != nil {
		slog.ErrorContext(r.Context(), "error loading api messages", "err", err)
		http.Error(w, "could not load messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		slog.ErrorContext(r.Context(), "error encoding messages", "err", err)
	}
}

// handleAPIMe returns the current user profile or authentication status as JSON.
func (s *Server) handleAPIMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.CurrentUser(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(map[string]any{"authenticated": false}); err != nil {
			slog.ErrorContext(r.Context(), "error encoding unauthenticated response", "err", err)
		}
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"user":          user,
	}); err != nil {
		slog.ErrorContext(r.Context(), "error encoding user response", "err", err)
	}
}

// CurrentUser retrieves the authenticated session user associated with the request cookie.
func (s *Server) CurrentUser(r *http.Request) (repository.SessionUser, bool) {
	c, err := r.Cookie(s.cfg.Session.CookieName)
	if err != nil || c.Value == "" {
		return repository.SessionUser{}, false
	}
	user, ok, err := s.sessions.Get(r.Context(), c.Value)
	if err != nil || !ok {
		return repository.SessionUser{}, false
	}
	return user, true
}

// randomToken generates a cryptographically secure random hexadecimal token.
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Initials extracts up to two uppercase initials from a person's display name.
func Initials(name string) string {
	parts := strings.Fields(name)
	var b strings.Builder
	for i, part := range parts {
		if i >= 2 {
			break
		}
		r, _ := utf8.DecodeRuneInString(part)
		b.WriteRune(unicode.ToUpper(r))
	}
	if b.Len() == 0 {
		return "?"
	}
	return b.String()
}

// FormatTime formats a timestamp for display in the template.
func FormatTime(t time.Time) string {
	return t.Local().Format("02.01.2006, 15:04")
}

// TemplateFuncs returns the template helper functions map.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"initials":   Initials,
		"formatTime": FormatTime,
	}
}

// NewTemplate creates and parses an HTML template from candidate file paths or explicit path.
func NewTemplate(paths ...string) (*template.Template, error) {
	if len(paths) == 0 {
		candidates := []string{
			"web/template/index.html",
			"../../web/template/index.html",
			"../web/template/index.html",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return template.New("index.html").Funcs(TemplateFuncs()).ParseFiles(c)
			}
		}
		return nil, errors.New("template index.html not found in candidate paths")
	}
	return template.New("index.html").Funcs(TemplateFuncs()).ParseFiles(paths[0])
}
