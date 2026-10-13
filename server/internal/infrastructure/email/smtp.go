package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/usecase/auth"
)

// errNoTLS is returned instead of sending a mail in clear text. Reset and
// activation mails carry a live token in their link.
var errNoTLS = errors.New("refusing to send e-mail without TLS: the mail server offers no STARTTLS")

// errNoAuth is returned when a user is configured but the server offers no AUTH.
var errNoAuth = errors.New("smtp: server doesn't support AUTH")

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

	subject := "Verify your account"
	body := fmt.Sprintf("Verify your account: %s", link)

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", s.cfg.From, to, subject, body)

	return s.send(to, []byte(msg))
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

	return s.send(to, []byte(msg))
}

// send delivers one message. It replaces smtp.SendMail, which upgrades to TLS
// only when the server offers STARTTLS and otherwise sends in clear text.
func (s *smtpEmailService) send(to string, msg []byte) error {
	c, err := smtp.Dial(net.JoinHostPort(s.cfg.Host, s.cfg.Port))
	if err != nil {
		return err
	}
	defer c.Close()
	return deliver(c, s.cfg, to, msg)
}

// deliver runs one SMTP transaction over c. The TLS and AUTH rules live here:
// a non-local host is never spoken to in clear text, and credentials are sent
// only when a user is configured.
func deliver(c *smtp.Client, cfg config.SMTPConfig, to string, msg []byte) error {
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return err
		}
	} else if !isLocalHost(cfg.Host) {
		return errNoTLS
	}
	// A relay that takes mail without a login does not advertise AUTH, so no
	// AUTH is attempted when no user is set. Configuring a user against a
	// server that does not offer AUTH fails here, not silently.
	if cfg.User != "" {
		// Client.Auth does not check the advertised mechanisms itself (SendMail
		// does), so a configured user is refused here rather than sent to a
		// server that never asked for it.
		if ok, _ := c.Extension("AUTH"); !ok {
			return errNoAuth
		}
		if err := c.Auth(smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// isLocalHost reports whether host is localhost or a loopback address, the only
// places where a mail may travel in clear text.
func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
