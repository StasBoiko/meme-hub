// Package mailer provides email delivery abstractions and SMTP implementations.
package mailer

import (
	"context"
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

// TestNopSender tests that NopSender operations succeed without error.
func TestNopSender(t *testing.T) {
	sender := NewNopSender()
	msg := Message{
		To:       []string{"alice@example.com"},
		Subject:  "Test",
		TextBody: "Hello",
	}

	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("NopSender.Send() error = %v, want nil", err)
	}
}

// TestSMTPSender_BuildRawMessage tests MIME formatting for plain text, HTML, and multipart messages.
func TestSMTPSender_BuildRawMessage(t *testing.T) {
	sender := NewSMTPSender("localhost", "1025", "", "", "noreply@guestbook.local")

	tests := []struct {
		name        string
		msg         Message
		wantSubstrs []string
	}{
		{
			name: "multipart plain and html message",
			msg: Message{
				To:       []string{"user@example.com"},
				Subject:  "Welcome!",
				TextBody: "Plain text version",
				HTMLBody: "<p>HTML version</p>",
			},
			wantSubstrs: []string{
				"From: noreply@guestbook.local",
				"To: user@example.com",
				"Subject: Welcome!",
				"multipart/alternative",
				"Plain text version",
				"<p>HTML version</p>",
			},
		},
		{
			name: "plain text only message",
			msg: Message{
				To:       []string{"bob@example.com"},
				Subject:  "Notice",
				TextBody: "Plain text notice only",
			},
			wantSubstrs: []string{
				"From: noreply@guestbook.local",
				"To: bob@example.com",
				"Subject: Notice",
				"Content-Type: text/plain",
				"Plain text notice only",
			},
		},
		{
			name: "html only message",
			msg: Message{
				To:       []string{"charlie@example.com"},
				Subject:  "HTML Alert",
				HTMLBody: "<h1>Important</h1>",
			},
			wantSubstrs: []string{
				"From: noreply@guestbook.local",
				"To: charlie@example.com",
				"Subject: HTML Alert",
				"Content-Type: text/html",
				"<h1>Important</h1>",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := string(sender.BuildRawMessage(tc.msg))
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(raw, want) {
					t.Errorf("BuildRawMessage() missing expected substring %q in:\n%s", want, raw)
				}
			}
		})
	}
}

// TestSMTPSender_Send tests SMTPSender delivery, error handling, and authentication.
func TestSMTPSender_Send(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		password      string
		msg           Message
		sendMailErr   error
		wantErr       bool
		wantAuthCheck bool
	}{
		{
			name:     "successful delivery without auth",
			username: "",
			password: "",
			msg: Message{
				To:       []string{"recipient@example.com"},
				Subject:  "Test",
				TextBody: "Body text",
			},
			sendMailErr:   nil,
			wantErr:       false,
			wantAuthCheck: false,
		},
		{
			name:     "successful delivery with auth credentials",
			username: "smtp_user",
			password: "smtp_password",
			msg: Message{
				To:       []string{"auth@example.com"},
				Subject:  "Auth Test",
				HTMLBody: "<p>Secure</p>",
			},
			sendMailErr:   nil,
			wantErr:       false,
			wantAuthCheck: true,
		},
		{
			name:     "empty recipient list returns error",
			username: "",
			password: "",
			msg: Message{
				To:       []string{},
				Subject:  "No Recipient",
				TextBody: "Body",
			},
			sendMailErr:   nil,
			wantErr:       true,
			wantAuthCheck: false,
		},
		{
			name:     "send mail failure returns wrapped error",
			username: "user",
			password: "pass",
			msg: Message{
				To:       []string{"fail@example.com"},
				Subject:  "Failure",
				TextBody: "Body",
			},
			sendMailErr:   errors.New("connection refused"),
			wantErr:       true,
			wantAuthCheck: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sender := NewSMTPSender("localhost", "1025", tc.username, tc.password, "noreply@guestbook.local")

			var recordedAddr string
			var recordedAuth smtp.Auth
			var recordedTo []string

			sender.sendMailFn = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
				recordedAddr = addr
				recordedAuth = a
				recordedTo = to
				return tc.sendMailErr
			}

			err := sender.Send(context.Background(), tc.msg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SMTPSender.Send() error = %v, wantErr = %v", err, tc.wantErr)
			}

			if !tc.wantErr {
				if recordedAddr != "localhost:1025" {
					t.Errorf("recordedAddr = %q, want %q", recordedAddr, "localhost:1025")
				}
				if len(recordedTo) != len(tc.msg.To) {
					t.Errorf("recordedTo len = %d, want %d", len(recordedTo), len(tc.msg.To))
				}
				if tc.wantAuthCheck && recordedAuth == nil {
					t.Error("expected non-nil smtp.Auth when credentials provided")
				}
				if !tc.wantAuthCheck && recordedAuth != nil {
					t.Error("expected nil smtp.Auth when credentials are empty")
				}
			}
		})
	}
}
