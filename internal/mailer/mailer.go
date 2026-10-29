// Package mailer provides email delivery abstractions and SMTP implementations.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message represents an email message payload.
type Message struct {
	// To specifies the recipient email addresses.
	To []string
	// Subject specifies the subject line of the email.
	Subject string
	// TextBody specifies the plain text body content.
	TextBody string
	// HTMLBody specifies the HTML body content.
	HTMLBody string
}

// Sender defines operations for delivering email messages.
type Sender interface {
	// Send delivers the provided email message.
	Send(ctx context.Context, msg Message) error
}

// NopSender implements Sender with a no-op behavior, useful in tests and disabled setups.
type NopSender struct{}

// NewNopSender creates a new NopSender instance.
func NewNopSender() *NopSender {
	return &NopSender{}
}

// Send performs a no-op email delivery.
func (s *NopSender) Send(ctx context.Context, msg Message) error {
	return nil
}

// SMTPSender implements Sender using standard library net/smtp over TLS/STARTTLS.
type SMTPSender struct {
	host       string
	port       string
	username   string
	password   string
	from       string
	sendMailFn func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPSender creates a new SMTPSender instance.
func NewSMTPSender(host, port, username, password, from string) *SMTPSender {
	return &SMTPSender{
		host:       host,
		port:       port,
		username:   username,
		password:   password,
		from:       from,
		sendMailFn: smtp.SendMail,
	}
}

// BuildRawMessage constructs an RFC 822 compliant MIME email message bytes.
func (s *SMTPSender) BuildRawMessage(msg Message) []byte {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("From: %s\r\n", s.from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	sb.WriteString("MIME-Version: 1.0\r\n")

	hasHTML := strings.TrimSpace(msg.HTMLBody) != ""
	hasText := strings.TrimSpace(msg.TextBody) != ""

	if hasHTML && hasText {
		boundary := "boundary_guestbook_mime_separator"
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary))

		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		sb.WriteString(msg.TextBody)
		sb.WriteString("\r\n\r\n")

		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		sb.WriteString(msg.HTMLBody)
		sb.WriteString("\r\n\r\n")

		sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if hasHTML {
		sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		sb.WriteString(msg.HTMLBody)
	} else {
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		sb.WriteString(msg.TextBody)
	}

	return []byte(sb.String())
}

// Send delivers the message via SMTP.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return errors.New("cannot send email: recipient address list is empty")
	}

	addr := net.JoinHostPort(s.host, s.port)
	var auth smtp.Auth
	if s.username != "" && s.password != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	raw := s.BuildRawMessage(msg)
	if err := s.sendMailFn(addr, auth, s.from, msg.To, raw); err != nil {
		return fmt.Errorf("sending email to %s: %w", strings.Join(msg.To, ", "), err)
	}

	return nil
}
