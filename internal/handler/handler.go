// Package handler implements HTTP handlers, routing, and user session management.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"meme-hub/internal/broker"
	"meme-hub/internal/config"
	"meme-hub/internal/media"
	"meme-hub/internal/repository"
	"meme-hub/internal/search"
	"meme-hub/internal/validator"
)

// Server represents the HTTP application server and its dependencies.
type Server struct {
	cfg       config.Config
	repo      repository.Repository
	sessions  repository.SessionStore
	publisher broker.Publisher
	searcher  search.Service
	oauth     *oauth2.Config
	tmpl      *template.Template
}

// pageData contains data passed to the HTML template for rendering.
type pageData struct {
	User     *repository.SessionUser
	Messages []repository.MessageDTO
	Query    string
	Sort     string
}

// googleProfile represents user information returned by the Google OAuth2 API.
type googleProfile struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// New creates a new Server instance.
func New(
	cfg config.Config,
	repo repository.Repository,
	tmpl *template.Template,
	sessions repository.SessionStore,
	publisher broker.Publisher,
	searcher search.Service,
) *Server {
	return &Server{
		cfg:       cfg,
		repo:      repo,
		sessions:  sessions,
		publisher: publisher,
		searcher:  searcher,
		tmpl:      tmpl,
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
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/messages", s.handleMessages)
	mux.HandleFunc("/api/messages", s.handleAPIMessages)
	mux.HandleFunc("POST /api/messages/{id}/vote", s.handleVote)
	mux.HandleFunc("POST /messages/{id}/vote", s.handleVote)
	mux.HandleFunc("POST /api/messages/{id}/comments", s.handleCreateComment)
	mux.HandleFunc("GET /api/messages/{id}/comments", s.handleGetComments)
	mux.HandleFunc("POST /messages/{id}/comments", s.handleCreateComment)
	mux.HandleFunc("/api/me", s.handleAPIMe)

	staticDir := "web/static"
	if _, err := os.Stat(staticDir); err != nil {
		if _, err2 := os.Stat("../../web/static"); err2 == nil {
			staticDir = "../../web/static"
		} else if _, err3 := os.Stat("../web/static"); err3 == nil {
			staticDir = "../web/static"
		}
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

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

// handleFavicon handles favicon.ico requests returning HTTP 204 No Content.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleIndex handles GET requests to the root path and renders the meme-hub page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sort != "fresh" {
		sort = "hot"
	}

	currentUserID := 0
	var currentUser *repository.SessionUser
	if user, ok := s.CurrentUser(r); ok {
		currentUserID = user.ID
		currentUser = &user
	}

	var messages []repository.MessageDTO
	var err error

	if query != "" && s.searcher != nil {
		docs, searchErr := s.searcher.SearchMessages(r.Context(), query, 100)
		if searchErr != nil {
			slog.ErrorContext(r.Context(), "search query failed, falling back to database", "err", searchErr, "q", query)
			messages, err = s.repo.GetMessages(r.Context(), currentUserID, sort, 100)
		} else {
			messages = make([]repository.MessageDTO, len(docs))
			for i, d := range docs {
				messages[i] = repository.MessageDTO{
					ID: d.ID,
					User: repository.SessionUser{
						ID:     d.UserID,
						Name:   d.Author,
						Avatar: d.Avatar,
					},
					Text:      d.Text,
					MediaURL:  d.MediaURL,
					Upvotes:   d.Upvotes,
					Downvotes: d.Downvotes,
					Score:     d.Score,
					CreatedAt: d.CreatedAt,
					Status:    repository.MessageStatusApproved,
				}
			}
		}
	} else {
		messages, err = s.repo.GetMessages(r.Context(), currentUserID, sort, 100)
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "error loading messages", "err", err)
		http.Error(w, "could not load messages", http.StatusInternalServerError)
		return
	}

	if len(messages) > 0 {
		ids := make([]int, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		if commentsMap, cErr := s.repo.GetCommentsByMessageIDs(r.Context(), ids); cErr == nil {
			for i := range messages {
				if list, exists := commentsMap[messages[i].ID]; exists {
					messages[i].Comments = list
					messages[i].CommentsCount = len(list)
				}
			}
		} else {
			slog.ErrorContext(r.Context(), "failed to load comments for messages", "err", cErr)
		}
	}

	data := pageData{
		User:     currentUser,
		Messages: messages,
		Query:    query,
		Sort:     sort,
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

	if s.publisher != nil && profile.Email != "" {
		welcomeTask := broker.EmailTask{
			To:        []string{profile.Email},
			Subject:   "Welcome to Meme Hub!",
			TextBody:  fmt.Sprintf("Hello %s!\n\nWelcome to Meme Hub. Thank you for signing in.", profile.Name),
			HTMLBody:  fmt.Sprintf("<h2>Welcome to Meme Hub!</h2><p>Hello <strong>%s</strong>,</p><p>Thank you for signing in. We are delighted to have you join Meme Hub!</p>", template.HTMLEscapeString(profile.Name)),
			CreatedAt: time.Now().UTC(),
		}
		if pubErr := s.publisher.PublishEmailTask(ctx, welcomeTask); pubErr != nil {
			slog.ErrorContext(ctx, "failed to publish welcome email task", "err", pubErr, "email", profile.Email)
		}
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

// handleMessages handles POST submissions for new meme entries.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isJSON := strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.Contains(r.Header.Get("Content-Type"), "application/json") ||
		r.Header.Get("X-Requested-With") == "XMLHttpRequest"

	user, ok := s.CurrentUser(r)
	if !ok {
		if isJSON {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			if isJSON {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid form data"})
				return
			}
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			if isJSON {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid form data"})
				return
			}
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
	}
	text := strings.TrimSpace(r.FormValue("text"))
	mediaURL := strings.TrimSpace(r.FormValue("media_url"))

	if err := validator.ValidateMessageText(text); err != nil {
		if isJSON {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if mediaURL != "" {
		if err := validator.ValidateMediaURL(r.Context(), mediaURL); err != nil {
			errMsg := fmt.Sprintf("invalid media link: %s", err.Error())
			if isJSON {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
				return
			}
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	}

	msgID, err := s.repo.CreateMessage(r.Context(), user.ID, text, mediaURL)
	if err != nil {
		slog.ErrorContext(r.Context(), "error saving message", "err", err)
		if isJSON {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "could not save message"})
			return
		}
		http.Error(w, "could not save message", http.StatusInternalServerError)
		return
	}

	task := broker.ModerationTask{
		MessageID:  msgID,
		UserID:     user.ID,
		AuthorName: user.Name,
		Avatar:     user.Avatar,
		Text:       text,
		MediaURL:   mediaURL,
		CreatedAt:  time.Now().UTC(),
	}
	if pubErr := s.publisher.PublishModerationTask(r.Context(), task); pubErr != nil {
		slog.ErrorContext(r.Context(), "failed to publish moderation task to broker", "err", pubErr, "messageID", msgID)
	}

	if isJSON {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		if encErr := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message_id": msgID}); encErr != nil {
			slog.ErrorContext(r.Context(), "failed to encode message creation response", "err", encErr)
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleAPIMessages returns the list of meme messages as JSON.
func (s *Server) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sort != "fresh" {
		sort = "hot"
	}

	currentUserID := 0
	if user, ok := s.CurrentUser(r); ok {
		currentUserID = user.ID
	}

	var messages []repository.MessageDTO
	var err error

	if query != "" && s.searcher != nil {
		docs, searchErr := s.searcher.SearchMessages(r.Context(), query, 100)
		if searchErr != nil {
			slog.ErrorContext(r.Context(), "api search query failed, falling back to database", "err", searchErr, "q", query)
			messages, err = s.repo.GetMessages(r.Context(), currentUserID, sort, 100)
		} else {
			messages = make([]repository.MessageDTO, len(docs))
			for i, d := range docs {
				messages[i] = repository.MessageDTO{
					ID: d.ID,
					User: repository.SessionUser{
						ID:     d.UserID,
						Name:   d.Author,
						Avatar: d.Avatar,
					},
					Text:      d.Text,
					MediaURL:  d.MediaURL,
					Upvotes:   d.Upvotes,
					Downvotes: d.Downvotes,
					Score:     d.Score,
					CreatedAt: d.CreatedAt,
					Status:    repository.MessageStatusApproved,
				}
			}
		}
	} else {
		messages, err = s.repo.GetMessages(r.Context(), currentUserID, sort, 100)
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "error loading api messages", "err", err)
		http.Error(w, "could not load messages", http.StatusInternalServerError)
		return
	}

	if len(messages) > 0 {
		ids := make([]int, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		if commentsMap, cErr := s.repo.GetCommentsByMessageIDs(r.Context(), ids); cErr == nil {
			for i := range messages {
				if list, exists := commentsMap[messages[i].ID]; exists {
					messages[i].Comments = list
					messages[i].CommentsCount = len(list)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		slog.ErrorContext(r.Context(), "error encoding messages", "err", err)
	}
}

// handleVote applies, toggles, or updates a user vote on a message.
func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := s.CurrentUser(r)
	if !ok {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[len(parts)-1] == "vote" {
			idStr = parts[len(parts)-2]
		}
	}
	messageID, err := strconv.Atoi(idStr)
	if err != nil || messageID <= 0 {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}

	var voteValue int
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Value int `json:"value"`
		}
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		voteValue = req.Value
	} else {
		if parseErr := r.ParseForm(); parseErr != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}
		v, parseErr := strconv.Atoi(r.FormValue("value"))
		if parseErr != nil {
			http.Error(w, "invalid vote value", http.StatusBadRequest)
			return
		}
		voteValue = v
	}

	if voteValue != 1 && voteValue != -1 {
		http.Error(w, "vote value must be 1 or -1", http.StatusBadRequest)
		return
	}

	result, err := s.repo.VoteMessage(r.Context(), messageID, user.ID, voteValue)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to process vote", "err", err, "messageID", messageID, "userID", user.ID)
		http.Error(w, "failed to record vote", http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if encErr := json.NewEncoder(w).Encode(result); encErr != nil {
			slog.ErrorContext(r.Context(), "failed to encode vote result", "err", encErr)
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleCreateComment creates a new user comment under a specified message.
func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := s.CurrentUser(r)
	if !ok {
		if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[len(parts)-1] == "comments" {
			idStr = parts[len(parts)-2]
		}
	}
	messageID, err := strconv.Atoi(idStr)
	if err != nil || messageID <= 0 {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}

	var text string
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Text string `json:"text"`
		}
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		text = req.Text
	} else {
		if parseErr := r.ParseForm(); parseErr != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}
		text = r.FormValue("text")
	}

	text = strings.TrimSpace(text)
	if err := validator.ValidateCommentText(text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	comment, err := s.repo.CreateComment(r.Context(), messageID, user.ID, text)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to create comment", "err", err, "messageID", messageID, "userID", user.ID)
		http.Error(w, "failed to save comment", http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		if encErr := json.NewEncoder(w).Encode(comment); encErr != nil {
			slog.ErrorContext(r.Context(), "failed to encode comment response", "err", encErr)
		}
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/#message-card-%d", messageID), http.StatusSeeOther)
}

// handleGetComments returns all comments for a specified message as JSON.
func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[len(parts)-1] == "comments" {
			idStr = parts[len(parts)-2]
		}
	}
	messageID, err := strconv.Atoi(idStr)
	if err != nil || messageID <= 0 {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}

	comments, err := s.repo.GetCommentsByMessageID(r.Context(), messageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to get comments", "err", err, "messageID", messageID)
		http.Error(w, "failed to load comments", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if encErr := json.NewEncoder(w).Encode(comments); encErr != nil {
		slog.ErrorContext(r.Context(), "failed to encode comments", "err", encErr)
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
	dbUser, err := s.repo.GetUserByID(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			_ = s.sessions.Delete(r.Context(), c.Value)
			return repository.SessionUser{}, false
		}
		return user, true
	}
	if user.Email != "" && dbUser.Email != "" && user.Email != dbUser.Email {
		_ = s.sessions.Delete(r.Context(), c.Value)
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

// IsImageURL checks if the provided URL points to an image format or visual media.
func IsImageURL(rawURL string) bool {
	return media.IsImageURL(rawURL)
}

// IsVideoURL checks if the provided URL points to a direct video file.
func IsVideoURL(rawURL string) bool {
	return media.IsVideoURL(rawURL)
}

// YouTubeEmbedURL extracts the YouTube video identifier and returns an embed iframe URL.
func YouTubeEmbedURL(rawURL string) string {
	return media.YouTubeEmbedURL(rawURL)
}

// VideoEmbedURL converts known platform video URLs into embeddable iframe URLs.
func VideoEmbedURL(rawURL string) string {
	return media.VideoEmbedURL(rawURL)
}

// IsVerticalEmbed reports whether the embed URL is a vertical video (e.g. TikTok, Shorts, Instagram Reel).
func IsVerticalEmbed(rawURL string) bool {
	return media.IsVerticalEmbed(rawURL)
}

// PlatformInfo holds branding and visual metadata for external link cards.
type PlatformInfo = media.PlatformInfo

// LinkPlatform returns platform metadata for external media URLs.
func LinkPlatform(rawURL string) PlatformInfo {
	return media.LinkPlatform(rawURL)
}

// TemplateFuncs returns the template helper functions map.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"initials":        Initials,
		"formatTime":      FormatTime,
		"isImageURL":      IsImageURL,
		"isVideoURL":      IsVideoURL,
		"youtubeEmbedURL": YouTubeEmbedURL,
		"videoEmbedURL":   VideoEmbedURL,
		"isVerticalEmbed": IsVerticalEmbed,
		"linkPlatform":    LinkPlatform,
		"resolveMedia":    media.Resolve,
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
