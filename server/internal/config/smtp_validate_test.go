package config

import (
	"strings"
	"testing"
)

// A password without a user would be ignored, so the mail would go out without
// a login. Validation refuses it while SMTP is on.
func TestValidateRejectsSMTPPasswordWithoutUser(t *testing.T) {
	c := &Config{BaseURL: "https://cal.example.test"}
	c.SMTP = SMTPConfig{Host: "smtp.example.test", Password: "secret", From: "noreply@example.test"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "CALDAV_SMTP_USER is empty") {
		t.Fatalf("expected a password without a user to be rejected, got: %v", err)
	}
}

func TestValidateAllowsSMTPWithoutLogin(t *testing.T) {
	c := &Config{BaseURL: "https://cal.example.test"}
	c.SMTP = SMTPConfig{Host: "relay.example.test", From: "noreply@example.test"}
	if err := c.Validate(); err != nil {
		t.Fatalf("a relay without a login should pass, got: %v", err)
	}
}

// The check is for an active SMTP setup only: a leftover password with SMTP
// switched off stays harmless.
func TestValidateIgnoresSMTPPasswordWhenSMTPOff(t *testing.T) {
	c := &Config{}
	c.SMTP = SMTPConfig{Password: "secret"}
	if err := c.Validate(); err != nil {
		t.Fatalf("a password with SMTP off should pass, got: %v", err)
	}
}
