package email

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
)

// Without SMTP the mail is dropped. Its body holds a live reset/activation
// token, so neither the body nor the recipient may reach stdout or the logs.
func TestSendEmailWithoutSMTPDoesNotLogTokenOrRecipient(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w

	svc := NewEmailService(config.SMTPConfig{})
	err = svc.SendEmail(context.Background(), "victim@example.test", "Reset your password",
		"https://x/reset?token=SECRET-TOKEN-123")

	os.Stdout = stdout
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("SendEmail without SMTP must not fail: %v", err)
	}

	all := string(out) + logs.String()
	for _, secret := range []string{"SECRET-TOKEN-123", "victim@example.test"} {
		if strings.Contains(all, secret) {
			t.Errorf("%q leaked into output: %s", secret, all)
		}
	}
	if !strings.Contains(logs.String(), "SMTP not configured") {
		t.Errorf("expected a warning that the mail was dropped, got: %q", logs.String())
	}
}
