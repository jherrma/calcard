package server

import (
	"net"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
)

// TestRunFailsFastWhenListenFails is the regression test for M17: Run() must
// return promptly when the port can't be bound, not block forever. The
// fail-fast path returns before touching s.db, so a nil db is fine here.
func TestRunFailsFastWhenListenFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	s := &Server{
		app: fiber.New(),
		cfg: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: port}},
	}

	done := make(chan error, 1)
	go func() { done <- s.Run() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Run() to return an error when the port is busy")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not fail fast; still blocked")
	}
}
