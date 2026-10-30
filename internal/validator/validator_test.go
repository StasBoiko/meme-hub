// Package validator provides email and input validation helpers for the application.
package validator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestValidateEmail tests email validation rules against valid and invalid cases using table-driven tests.
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		wantError error
		wantValid bool
	}{
		{
			name:      "valid standard email",
			email:     "user@example.com",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid email with dots in local part",
			email:     "first.middle.last@example.com",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid email with plus tag",
			email:     "user+newsletter@example.com",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid email with numeric local part",
			email:     "123456@example.com",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid email with subdomain",
			email:     "admin@mail.sub.example.com",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid email with hyphen in domain",
			email:     "user@my-domain.org",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "empty string",
			email:     "",
			wantError: ErrEmptyEmail,
			wantValid: false,
		},
		{
			name:      "whitespace only string",
			email:     "   ",
			wantError: ErrEmptyEmail,
			wantValid: false,
		},
		{
			name:      "email with leading space",
			email:     " user@example.com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "email with trailing space",
			email:     "user@example.com ",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "missing at sign",
			email:     "plainaddress",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "missing local part",
			email:     "@example.com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "missing domain part",
			email:     "user@",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "domain without top-level domain dot",
			email:     "user@localhost",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "domain starting with dot",
			email:     "user@.example.com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "domain ending with dot",
			email:     "user@example.com.",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "consecutive dots in domain",
			email:     "user@example..com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "consecutive dots in local part",
			email:     "user..name@example.com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "display name with angle brackets",
			email:     "John Doe <john@example.com>",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "local part exceeds maximum length of 64 characters",
			email:     strings.Repeat("a", 65) + "@example.com",
			wantError: ErrInvalidEmailFormat,
			wantValid: false,
		},
		{
			name:      "email exceeds total maximum length of 254 characters",
			email:     strings.Repeat("a", 60) + "@" + strings.Repeat("b", 190) + ".com",
			wantError: ErrEmailTooLong,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmail(tc.email)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("ValidateEmail(%q) error = %v, wantError = %v", tc.email, err, tc.wantError)
			}

			valid := IsValidEmail(tc.email)
			if valid != tc.wantValid {
				t.Fatalf("IsValidEmail(%q) = %v, wantValid = %v", tc.email, valid, tc.wantValid)
			}
		})
	}
}

// TestValidateMessageText tests message text validation rules against valid and invalid cases using table-driven tests.
func TestValidateMessageText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantError error
		wantValid bool
	}{
		{
			name:      "valid standard message",
			text:      "Hello, world!",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid message with unicode characters",
			text:      "Привет, мир! 🚀",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid message with surrounding whitespace",
			text:      "   Valid message with whitespace   ",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid message at exact maximum length of 2000 runes",
			text:      strings.Repeat("a", MaxMessageLength),
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid message at exact maximum length with multibyte unicode runes",
			text:      strings.Repeat("ж", MaxMessageLength),
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "empty string",
			text:      "",
			wantError: ErrEmptyMessage,
			wantValid: false,
		},
		{
			name:      "whitespace only string with spaces",
			text:      "   ",
			wantError: ErrEmptyMessage,
			wantValid: false,
		},
		{
			name:      "whitespace only string with newlines and tabs",
			text:      "\t\r\n ",
			wantError: ErrEmptyMessage,
			wantValid: false,
		},
		{
			name:      "message exceeds maximum length by one rune",
			text:      strings.Repeat("a", MaxMessageLength+1),
			wantError: ErrMessageTooLong,
			wantValid: false,
		},
		{
			name:      "message exceeds maximum length with multibyte unicode runes",
			text:      strings.Repeat("界", MaxMessageLength+1),
			wantError: ErrMessageTooLong,
			wantValid: false,
		},
		{
			name:      "message exceeds maximum length with surrounding whitespace",
			text:      "  " + strings.Repeat("b", MaxMessageLength+10) + "  ",
			wantError: ErrMessageTooLong,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMessageText(tc.text)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("ValidateMessageText(%q) error = %v, wantError = %v", tc.text, err, tc.wantError)
			}

			valid := IsValidMessageText(tc.text)
			if valid != tc.wantValid {
				t.Fatalf("IsValidMessageText(%q) = %v, wantValid = %v", tc.text, valid, tc.wantValid)
			}
		})
	}
}

// TestValidateCommentText tests comment text validation rules using table-driven tests.
func TestValidateCommentText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantError error
		wantValid bool
	}{
		{
			name:      "valid comment",
			text:      "LOL this is hilarious!",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid comment at exact max length",
			text:      strings.Repeat("c", MaxCommentLength),
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "empty comment",
			text:      "",
			wantError: ErrEmptyComment,
			wantValid: false,
		},
		{
			name:      "whitespace only comment",
			text:      "   \t\n",
			wantError: ErrEmptyComment,
			wantValid: false,
		},
		{
			name:      "comment exceeds max length",
			text:      strings.Repeat("x", MaxCommentLength+1),
			wantError: ErrCommentTooLong,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCommentText(tc.text)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("ValidateCommentText(%q) error = %v, wantError = %v", tc.text, err, tc.wantError)
			}

			valid := IsValidCommentText(tc.text)
			if valid != tc.wantValid {
				t.Fatalf("IsValidCommentText(%q) = %v, wantValid = %v", tc.text, valid, tc.wantValid)
			}
		})
	}
}

// TestValidateMediaURL tests media URL validation against various URL schemes, YouTube formats, and HTTP responses.
func TestValidateMediaURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
		case "/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
		case "/html-page":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
		case "/oembed-page":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><link rel="alternate" type="application/json+oembed" href="https://example.com/oembed.json"></head><body>Video</body></html>`))
		case "/og-video-page":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><meta property="og:video" content="https://example.com/player.mp4"></head><body>Video</body></html>`))
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
		case "/server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name      string
		rawURL    string
		wantError error
		wantValid bool
	}{
		{
			name:      "empty media url is allowed",
			rawURL:    "",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "whitespace only media url is allowed",
			rawURL:    "   \t\n",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "url exceeds maximum length",
			rawURL:    "https://example.com/" + strings.Repeat("a", MaxMediaURLLength),
			wantError: ErrMediaURLTooLong,
			wantValid: false,
		},
		{
			name:      "invalid scheme ftp",
			rawURL:    "ftp://example.com/image.jpg",
			wantError: ErrInvalidMediaURLScheme,
			wantValid: false,
		},
		{
			name:      "valid standard youtube watch url",
			rawURL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid youtube shorts url",
			rawURL:    "https://www.youtube.com/shorts/dQw4w9WgXcQ",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid youtu.be short url",
			rawURL:    "https://youtu.be/dQw4w9WgXcQ",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid vimeo video url",
			rawURL:    "https://vimeo.com/76979871",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid dailymotion video url",
			rawURL:    "https://www.dailymotion.com/video/x7tgad0",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid streamable video url",
			rawURL:    "https://streamable.com/moo",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid rumble video url",
			rawURL:    "https://rumble.com/embed/v3abcdef",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "valid coub video url",
			rawURL:    "https://coub.com/view/1hyxa1",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "reachable image endpoint",
			rawURL:    ts.URL + "/image.jpg",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "reachable video endpoint",
			rawURL:    ts.URL + "/video.mp4",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "reachable html page with oembed link is accepted",
			rawURL:    ts.URL + "/oembed-page",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "reachable html page with og:video meta tag is accepted",
			rawURL:    ts.URL + "/og-video-page",
			wantError: nil,
			wantValid: true,
		},
		{
			name:      "unsupported media type html without video meta",
			rawURL:    ts.URL + "/html-page",
			wantError: ErrUnsupportedMediaType,
			wantValid: false,
		},
		{
			name:      "404 not found media endpoint",
			rawURL:    ts.URL + "/not-found",
			wantError: ErrMediaURLUnreachable,
			wantValid: false,
		},
		{
			name:      "500 internal server error media endpoint",
			rawURL:    ts.URL + "/server-error",
			wantError: ErrMediaURLUnreachable,
			wantValid: false,
		},
		{
			name:      "relative or local path rejected with invalid scheme",
			rawURL:    "/static/videos/does_not_exist_12345.mp4",
			wantError: ErrInvalidMediaURLScheme,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMediaURL(context.Background(), tc.rawURL)
			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("ValidateMediaURL(%q) error = %v, wantError = %v", tc.rawURL, err, tc.wantError)
				}
			} else if err != nil {
				t.Fatalf("ValidateMediaURL(%q) unexpected error = %v", tc.rawURL, err)
			}

			valid := IsValidMediaURL(context.Background(), tc.rawURL)
			if valid != tc.wantValid {
				t.Fatalf("IsValidMediaURL(%q) = %v, wantValid = %v", tc.rawURL, valid, tc.wantValid)
			}
		})
	}
}
