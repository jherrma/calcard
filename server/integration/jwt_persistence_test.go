//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	infraserver "github.com/jherrma/caldav-server/internal/infrastructure/server"
	"github.com/stretchr/testify/require"
)

// TestJWTSecretPersistsAcrossRestart is the regression test for H14: with no
// CALDAV_JWT_SECRET configured, the server must load (or generate-and-persist)
// the secret from the database, so an access token issued before a restart
// still validates afterwards. Before the fix config.Load minted a fresh random
// secret every boot, leaving the EnsureSecret DB path dead code and breaking
// every token on restart.
func TestJWTSecretPersistsAcrossRestart(t *testing.T) {
	dataDir := t.TempDir() // shared across both boots; cleaned up by the framework

	boot := func() (string, func()) {
		cfg := &config.Config{
			Server:   config.ServerConfig{Host: "127.0.0.1", Port: "0"},
			Database: config.DatabaseConfig{Driver: "sqlite", AutoMigrate: true},
			DataDir:  dataDir,
			BaseURL:  "http://127.0.0.1",
			LogLevel: "error",
			SMTP:     config.SMTPConfig{}, // empty -> users auto-activate
			JWT: config.JWTConfig{
				// Intentionally empty: EnsureSecret must supply it from the DB.
				AccessExpiry:  time.Hour,
				RefreshExpiry: 24 * time.Hour,
				ResetExpiry:   15 * time.Minute,
			},
			Security: config.SecurityConfig{MaxRequestSize: 10 * 1024 * 1024, RequestTimeout: 30 * time.Second},
		}
		db, err := database.New(cfg)
		require.NoError(t, err)
		require.NoError(t, db.Migrate(database.Models()...))
		srv := infraserver.New(cfg, db)
		addr, err := srv.Start("127.0.0.1:0")
		require.NoError(t, err)
		base := "http://" + addr
		require.NoError(t, waitForReady(base+"/health", 5*time.Second))
		return base, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
			_ = db.Close()
		}
	}

	// --- Boot 1: register + login, capture the access token --------------
	base1, shutdown1 := boot()
	email := "jwt-persist@example.test"
	password := "persistSecret!123"

	regCode, regRaw := rawCall(t, http.MethodPost, base1+"/api/v1/auth/register", "",
		map[string]string{"email": email, "password": password, "display_name": "JWT Persist"}, nil)
	require.Equalf(t, http.StatusOK, regCode, "register: %s", string(regRaw))

	loginStatus, loginRaw := rawCall(t, http.MethodPost, base1+"/api/v1/auth/login", "",
		map[string]string{"email": email, "password": password}, nil)
	require.Equalf(t, http.StatusOK, loginStatus, "login: %s", string(loginRaw))
	token := unwrapAccessToken(t, loginRaw)
	require.NotEmpty(t, token)

	// The token works on boot 1.
	meStatus, _ := rawCall(t, http.MethodGet, base1+"/api/v1/users/me", token, nil, nil)
	require.Equal(t, http.StatusOK, meStatus, "token should work on the issuing server")
	shutdown1()

	// --- Boot 2: same DB, still no configured secret ---------------------
	base2, shutdown2 := boot()
	defer shutdown2()

	meStatus2, meRaw2 := rawCall(t, http.MethodGet, base2+"/api/v1/users/me", token, nil, nil)
	require.Equalf(t, http.StatusOK, meStatus2,
		"access token must survive a restart when the secret is DB-persisted: %s", string(meRaw2))
}

func unwrapAccessToken(t *testing.T, raw []byte) string {
	t.Helper()
	var resp struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &resp))
	return resp.Data.AccessToken
}
