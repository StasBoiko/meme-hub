// Package validator provides email and input validation helpers for the application.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"meme-hub/internal/media"
)

// MaxMessageLength is the maximum allowed rune length for a meme message.
const MaxMessageLength = 2000

// maxEmailLength is the maximum allowed length for an email address per RFC 5321.
const maxEmailLength = 254

// maxLocalPartLength is the maximum allowed length for the local part per RFC 5321.
const maxLocalPartLength = 64

// emailRegex matches standard email address structures with a valid domain.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{\|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

// ErrEmptyEmail indicates that the email address is empty.
var ErrEmptyEmail = errors.New("email address cannot be empty")

// ErrEmailTooLong indicates that the email address exceeds the maximum length.
var ErrEmailTooLong = errors.New("email address exceeds maximum length")

// ErrInvalidEmailFormat indicates that the email address has an invalid format.
var ErrInvalidEmailFormat = errors.New("invalid email address format")

// ErrEmptyMessage indicates that the message text is empty.
var ErrEmptyMessage = errors.New("message is empty")

// ErrMessageTooLong indicates that the message text exceeds the maximum allowed length.
var ErrMessageTooLong = errors.New("message is too long")

// MaxCommentLength is the maximum allowed rune length for a comment.
const MaxCommentLength = 1000

// ErrEmptyComment indicates that the comment text is empty.
var ErrEmptyComment = errors.New("comment is empty")

// ErrCommentTooLong indicates that the comment text exceeds the maximum allowed length.
var ErrCommentTooLong = errors.New("comment is too long")

// ValidateEmail checks whether the provided string is a valid email address.
func ValidateEmail(email string) error {
	if utf8.RuneCountInString(strings.TrimSpace(email)) == 0 {
		return ErrEmptyEmail
	}

	if utf8.RuneCountInString(email) > maxEmailLength {
		return ErrEmailTooLong
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return ErrInvalidEmailFormat
	}

	if !emailRegex.MatchString(email) {
		return ErrInvalidEmailFormat
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 || utf8.RuneCountInString(parts[0]) > maxLocalPartLength {
		return ErrInvalidEmailFormat
	}

	return nil
}

// IsValidEmail reports whether the provided string is a valid email address.
func IsValidEmail(email string) bool {
	return ValidateEmail(email) == nil
}

// ValidateMessageText checks whether the provided message text is non-empty and does not exceed the maximum length.
func ValidateMessageText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ErrEmptyMessage
	}

	if utf8.RuneCountInString(trimmed) > MaxMessageLength {
		return ErrMessageTooLong
	}

	return nil
}

// IsValidMessageText reports whether the provided message text is valid.
func IsValidMessageText(text string) bool {
	return ValidateMessageText(text) == nil
}

// ValidateCommentText checks whether the provided comment text is non-empty and does not exceed the maximum length.
func ValidateCommentText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ErrEmptyComment
	}

	if utf8.RuneCountInString(trimmed) > MaxCommentLength {
		return ErrCommentTooLong
	}

	return nil
}

// IsValidCommentText reports whether the provided comment text is valid.
func IsValidCommentText(text string) bool {
	return ValidateCommentText(text) == nil
}

// MaxMediaURLLength is the maximum allowed rune length for a media URL.
const MaxMediaURLLength = 2000

// ErrMediaURLTooLong indicates that the media URL exceeds the maximum length.
var ErrMediaURLTooLong = errors.New("media url is too long")

// ErrInvalidMediaURLScheme indicates that the media URL does not have an http or https scheme.
var ErrInvalidMediaURLScheme = errors.New("media url must start with http:// or https://")

// ErrInvalidMediaURLFormat indicates that the media URL cannot be parsed.
var ErrInvalidMediaURLFormat = errors.New("invalid media url format")

// ErrMediaURLUnreachable indicates that the media URL could not be resolved or returned an HTTP error.
var ErrMediaURLUnreachable = errors.New("media url is unreachable or does not exist")

// ErrUnsupportedMediaType indicates that the target URL is not a valid image or video resource.
var ErrUnsupportedMediaType = errors.New("url does not point to a supported image or video resource")

// MediaHTTPClient allows injecting a custom HTTP client for media validation tests.
var MediaHTTPClient = &http.Client{
	Timeout: 4 * time.Second,
}

// isSupportedVideoPlatform reports whether the parsed URL belongs to a known media platform.
func isSupportedVideoPlatform(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	return media.IsSupportedPlatform(parsed.String())
}

// hasHTMLVideoMeta checks whether an HTML response contains oEmbed video discovery or OpenGraph video meta tags.
func hasHTMLVideoMeta(body io.Reader) bool {
	lr := io.LimitReader(body, 32768)
	buf, err := io.ReadAll(lr)
	if err != nil || len(buf) == 0 {
		return false
	}
	content := strings.ToLower(string(buf))

	// OpenGraph video or Twitter player tags
	if strings.Contains(content, `property="og:video"`) ||
		strings.Contains(content, `property='og:video'`) ||
		strings.Contains(content, `property="og:video:url"`) ||
		strings.Contains(content, `property='og:video:url'`) ||
		strings.Contains(content, `name="twitter:player"`) ||
		strings.Contains(content, `name='twitter:player'`) ||
		(strings.Contains(content, `property="og:type"`) && strings.Contains(content, `content="video"`)) ||
		(strings.Contains(content, `property='og:type'`) && strings.Contains(content, `content='video'`)) {
		return true
	}

	// oEmbed discovery tag
	if strings.Contains(content, `type="application/json+oembed"`) ||
		strings.Contains(content, `type='application/json+oembed'`) {
		return true
	}

	return false
}

// checkRemoteMedia validates accessibility and Content-Type of a remote media resource.
func checkRemoteMedia(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidMediaURLFormat, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MemeHub/1.0")

	resp, err := MediaHTTPClient.Do(req)
	isGet := false
	if err != nil || (resp != nil && (resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden)) {
		getReq, getErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if getErr == nil {
			getReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MemeHub/1.0")
			getReq.Header.Set("Range", "bytes=0-32768")
			if getResp, doErr := MediaHTTPClient.Do(getReq); doErr == nil {
				if resp != nil {
					_ = resp.Body.Close()
				}
				resp = getResp
				isGet = true
				err = nil
			}
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMediaURLUnreachable, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("%w: HTTP status %d", ErrMediaURLUnreachable, resp.StatusCode)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	cleanPath := strings.ToLower(strings.Split(rawURL, "?")[0])
	hasMediaExt := false
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".ico", ".mp4", ".webm", ".ogg", ".mov", ".m4v"} {
		if strings.HasSuffix(cleanPath, ext) {
			hasMediaExt = true
			break
		}
	}

	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") || hasMediaExt || strings.Contains(contentType, "octet-stream") {
		return nil
	}

	// Universal oEmbed and OpenGraph discovery for video web pages
	if strings.Contains(contentType, "text/html") {
		if isGet && hasHTMLVideoMeta(resp.Body) {
			return nil
		}
		if !isGet {
			getReq, getErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if getErr == nil {
				getReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MemeHub/1.0")
				getReq.Header.Set("Range", "bytes=0-32768")
				if getResp, doErr := MediaHTTPClient.Do(getReq); doErr == nil {
					defer func() {
						_ = getResp.Body.Close()
					}()
					if hasHTMLVideoMeta(getResp.Body) {
						return nil
					}
				}
			}
		}
	}

	return ErrUnsupportedMediaType
}

// ValidateMediaURL checks whether the provided rawURL is a reachable media file or supported video/platform URL.
func ValidateMediaURL(ctx context.Context, rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}

	if utf8.RuneCountInString(trimmed) > MaxMediaURLLength {
		return ErrMediaURLTooLong
	}

	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return ErrInvalidMediaURLScheme
	}

	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" {
		return ErrInvalidMediaURLFormat
	}

	if isSupportedVideoPlatform(parsed) {
		return nil
	}

	return checkRemoteMedia(ctx, trimmed)
}

// IsValidMediaURL reports whether the provided media URL is valid and accessible.
func IsValidMediaURL(ctx context.Context, rawURL string) bool {
	return ValidateMediaURL(ctx, rawURL) == nil
}
