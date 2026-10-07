// Package validator provides email and input validation helpers for the application.
package validator

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxMessageLength is the maximum allowed rune length for a guestbook message.
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
