// Package mail sends the reminder emails (M11 D22): SMTP from SMTP_URL
// (Mailpit locally, SES SMTP at DEPLOY), or a log line without one.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"net/url"
	"strings"
)

// SMTP sends through one SMTP server. smtp.SendMail uses STARTTLS whenever
// the server offers it.
type SMTP struct {
	addr, from string
	auth       smtp.Auth
}

// NewSMTP reads smtp://[user:password@]host:port?from=address.
func NewSMTP(raw string) (*SMTP, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "smtp" || u.Host == "" {
		return nil, fmt.Errorf("SMTP_URL must be smtp://[user:password@]host:port?from=address")
	}
	from := u.Query().Get("from")
	if !strings.Contains(from, "@") {
		return nil, fmt.Errorf("SMTP_URL needs ?from=address")
	}
	s := &SMTP{addr: u.Host, from: from}
	if u.User != nil {
		pw, _ := u.User.Password()
		host, _, _ := net.SplitHostPort(u.Host)
		s.auth = smtp.PlainAuth("", u.User.Username(), pw, host)
	}
	return s, nil
}

func (s *SMTP) Delivers() bool { return true }

func (s *SMTP) Send(_ context.Context, to, subject, body string) error {
	if strings.ContainsAny(to+subject, "\r\n") {
		return fmt.Errorf("refusing a header with a line break")
	}
	msg := "From: " + s.from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
	return smtp.SendMail(s.addr, s.auth, s.from, []string{to}, []byte(msg))
}

// Log only logs (EMAIL_MODE=log): its emails are recorded 'logged'.
type Log struct{}

func (Log) Delivers() bool { return false }

func (Log) Send(_ context.Context, to, subject, _ string) error {
	slog.Info("email logged, not sent (EMAIL_MODE=log)", "to", to, "subject", subject)
	return nil
}
