//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	infraserver "github.com/jherrma/caldav-server/internal/infrastructure/server"
)

// baseURL is the http:// root of the test server, set by TestMain.
// Individual tests read it through the exported helper baseURLForTest().
var baseURL string

// httpClient is a shared client with a short timeout; tests may create their
// own clients when they need different behavior (e.g. no redirect follow).
var httpClient = &http.Client{Timeout: 10 * time.Second}

func TestMain(m *testing.M) {
	code, err := runTests(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runTests(m *testing.M) (int, error) {
	dataDir, err := os.MkdirTemp("", "calcard-integration-*")
	if err != nil {
		return 1, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Database: config.DatabaseConfig{
			Driver:      "sqlite",
			AutoMigrate: true,
		},
		DataDir: dataDir,
		// BaseURL is rewritten after Start so app-password "server_url" points at
		// the real test port. Any non-empty value works here for Validate().
		BaseURL:  "http://127.0.0.1",
		LogLevel: "error",
		SMTP:     config.SMTPConfig{}, // empty Host → users auto-activated
		JWT: config.JWTConfig{
			Secret:        "integration-test-secret-change-me",
			AccessExpiry:  time.Hour,
			RefreshExpiry: 24 * time.Hour,
			ResetExpiry:   15 * time.Minute,
		},
		RateLimit: config.RateLimitConfig{
			// The login rate limiter fires at 5 req/min/IP and will flake in a
			// test that logs in multiple times. Keep the global middleware on
			// but disabled to avoid surprises.
			Enabled: false,
		},
		Security: config.SecurityConfig{
			MaxRequestSize: 10 * 1024 * 1024,
			RequestTimeout: 30 * time.Second,
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		return 1, fmt.Errorf("database.New: %w", err)
	}
	defer db.Close()

	if err := db.Migrate(database.Models()...); err != nil {
		return 1, fmt.Errorf("migrate: %w", err)
	}

	srv := infraserver.New(cfg, db)
	addr, err := srv.Start("127.0.0.1:0")
	if err != nil {
		return 1, fmt.Errorf("server.Start: %w", err)
	}
	baseURL = "http://" + addr
	cfg.BaseURL = baseURL // so app-password "server_url" matches

	// Poll /health until the listener actually accepts a request. app.Listener
	// runs in a goroutine so the first request can race it.
	if err := waitForReady(baseURL+"/health", 5*time.Second); err != nil {
		return 1, fmt.Errorf("readiness: %w", err)
	}

	// First-boot check: GET /system/settings must report admin_configured=false
	// on a brand-new DB. We do this in TestMain — BEFORE any test creates a
	// user — because tests share the same server and the check is only
	// meaningful at true first boot.
	if err := verifyFirstBoot(baseURL); err != nil {
		return 1, fmt.Errorf("first boot: %w", err)
	}

	code := m.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)

	return code, nil
}

func verifyFirstBoot(base string) error {
	resp, err := http.Get(base + "/api/v1/system/settings")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env struct {
		Data struct {
			AdminConfigured bool `json:"admin_configured"`
			SMTPEnabled     bool `json:"smtp_enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode settings: %w: %s", err, string(body))
	}
	if env.Data.AdminConfigured {
		return fmt.Errorf("expected admin_configured=false on a fresh DB, got true")
	}
	if env.Data.SMTPEnabled {
		return fmt.Errorf("expected smtp_enabled=false (SMTP.Host=\"\"), got true")
	}
	return nil
}

func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready within %s", timeout)
}
