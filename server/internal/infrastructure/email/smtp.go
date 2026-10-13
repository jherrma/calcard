package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/usecase/auth"
)

type smtpEmailService struct {
	cfg config.SMTPConfig
}

// NewEmailService creates a new SMTP-based email service
func NewEmailService(cfg config.SMTPConfig) auth.EmailService {
	return &smtpEmailService{cfg: cfg}
}

func (s *smtpEmailService) SendActivationEmail(ctx context.Context, to, link string) error {
	if s.cfg.Host == "" {
		return nil // SMTP not configured, skip sending
	}

	subject := "Activate your account"
	body := fmt.Sprintf("Activate your account: %s", link)

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", s.cfg.From, to, subject, body)

	addr := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)

	return smtp.SendMail(addr, s.auth(), s.cfg.From, []string{to}, []byte(msg))
}

// auth returns the AUTH PLAIN credentials, or nil when no user is configured.
// A relay that needs no login does not advertise AUTH, and net/smtp then
// refuses to send with "smtp: server doesn't support AUTH".
func (s *smtpEmailService) auth() smtp.Auth {
	if s.cfg.User == "" {
		return nil
	}
	return smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
}

func (s *smtpEmailService) SendEmail(ctx context.Context, to, subject, body string) error {
	if s.cfg.Host == "" {
		// Never log the recipient or body: reset and activation mails carry a
		// live token in their link, and container logs are far less protected
		// than the mailbox the token was meant for.
		slog.Warn("SMTP not configured, e-mail not sent", slog.String("subject", subject))
		return nil
	}

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", s.cfg.From, to, subject, body)

	addr := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)

	return smtp.SendMail(addr, s.auth(), s.cfg.From, []string{to}, []byte(msg))
}
