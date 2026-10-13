package email

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"
	"testing"
	"time"

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

// fakeSMTP speaks just enough SMTP for one delivery. It records every command
// it receives and, when offerAuth is set, advertises and accepts AUTH PLAIN. It
// never offers STARTTLS. done closes once the connection is finished.
type fakeSMTP struct {
	addr     string
	commands chan string
	done     chan struct{}
}

func startFakeSMTP(t *testing.T, offerAuth bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String(), commands: make(chan string, 100), done: make(chan struct{})}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(f.done)
			return
		}
		defer close(f.done)
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		reply("220 fake ESMTP")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					reply("250 queued")
				}
				continue
			}
			f.commands <- line
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				if offerAuth {
					reply("250-fake")
					reply("250 AUTH PLAIN")
				} else {
					reply("250 fake")
				}
			case strings.HasPrefix(upper, "AUTH PLAIN"):
				reply("235 authenticated")
			case strings.HasPrefix(upper, "DATA"):
				inData = true
				reply("354 go ahead")
			case strings.HasPrefix(upper, "QUIT"):
				reply("221 bye")
				return
			default:
				reply("250 ok")
			}
		}
	}()
	return f
}

// deliverTo runs deliver against a fake server, with cfg's host names as given.
// It returns the delivery error and every command the server received.
func deliverTo(t *testing.T, f *fakeSMTP, cfg config.SMTPConfig) (error, []string) {
	t.Helper()
	conn, err := net.Dial("tcp", f.addr)
	if err != nil {
		t.Fatal(err)
	}
	// The name is the host only, as smtp.Dial sets it: PlainAuth checks it.
	c, err := smtp.NewClient(conn, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	err = deliver(c, cfg, "victim@example.test", []byte("Subject: x\r\n\r\nbody\r\n"))
	_ = c.Close()
	<-f.done
	close(f.commands)
	var got []string
	for cmd := range f.commands {
		got = append(got, cmd)
	}
	return err, got
}

func hasCommand(cmds []string, prefix string) bool {
	for _, c := range cmds {
		if strings.HasPrefix(strings.ToUpper(c), prefix) {
			return true
		}
	}
	return false
}

// A local relay without a login takes the mail in clear text, with no AUTH.
func TestDeliverLocalRelayWithoutLoginSendsPlain(t *testing.T) {
	f := startFakeSMTP(t, false)
	err, cmds := deliverTo(t, f, config.SMTPConfig{Host: "127.0.0.1", From: "noreply@example.test"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if hasCommand(cmds, "AUTH") {
		t.Errorf("no user configured: AUTH must not be attempted, got %q", cmds)
	}
	if !hasCommand(cmds, "DATA") {
		t.Errorf("mail was not sent: %q", cmds)
	}
}

// A remote host that offers no STARTTLS must never receive the mail: it is
// refused before MAIL FROM, with or without a login.
func TestDeliverRefusesPlainTextToRemoteHost(t *testing.T) {
	for _, user := range []string{"", "mailer"} {
		f := startFakeSMTP(t, true)
		err, cmds := deliverTo(t, f, config.SMTPConfig{Host: "mail.example.test", User: user, Password: "pw", From: "noreply@example.test"})
		if !errors.Is(err, errNoTLS) {
			t.Errorf("user %q: want errNoTLS, got %v", user, err)
		}
		if hasCommand(cmds, "MAIL") || hasCommand(cmds, "AUTH") {
			t.Errorf("user %q: nothing may be sent or authenticated over clear text, got %q", user, cmds)
		}
	}
}

// A configured user against a server that offers no AUTH fails, and no mail
// goes out.
func TestDeliverFailsWhenUserSetButNoAuthOffered(t *testing.T) {
	f := startFakeSMTP(t, false)
	err, cmds := deliverTo(t, f, config.SMTPConfig{Host: "127.0.0.1", User: "mailer", Password: "pw", From: "noreply@example.test"})
	if !errors.Is(err, errNoAuth) {
		t.Errorf("want errNoAuth, got %v", err)
	}
	if hasCommand(cmds, "MAIL") {
		t.Errorf("mail must not be sent when AUTH fails, got %q", cmds)
	}
}

// With a user and an AUTH-capable server, the credentials are used and the
// mail goes out.
func TestDeliverAuthenticatesWhenUserSet(t *testing.T) {
	f := startFakeSMTP(t, true)
	err, cmds := deliverTo(t, f, config.SMTPConfig{Host: "127.0.0.1", User: "mailer", Password: "pw", From: "noreply@example.test"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !hasCommand(cmds, "AUTH PLAIN") || !hasCommand(cmds, "DATA") {
		t.Errorf("want AUTH PLAIN then DATA, got %q", cmds)
	}
}
