package email

import (
	"context"
	"fmt"
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

	auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
	addr := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)

	return smtp.SendMail(addr, auth, s.cfg.From, []string{to}, []byte(msg))
}
