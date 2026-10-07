// Package validator provides email and input validation helpers for the application.
package validator

import (
	"errors"
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
