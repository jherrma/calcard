package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid configuration (SQLite)",
			config: Config{
				BaseURL: "http://localhost:8080",
				JWT: JWTConfig{
					Secret: "secure-secret-16",
				},
				Database: DatabaseConfig{
					Driver: "sqlite",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid configuration (Postgres)",
			config: Config{
				BaseURL: "http://localhost:8080",
				JWT: JWTConfig{
					Secret: "secure-secret-16",
				},
				Database: DatabaseConfig{
					Driver: "postgres",
					Host:   "db",
					User:   "caldav",
					Name:   "caldav",
				},
			},
			wantErr: false,
		},
		{
			// Empty means "derive from the request" for URLs returned to the
			// caller, so it is valid on its own.
			name: "Empty BaseURL without SMTP or OAuth",
			config: Config{
				BaseURL: "",
				JWT: JWTConfig{
					Secret: "secure-secret-16",
				},
			},
			wantErr: false,
		},
		{
			// E-mailed links must never come from a request's Host header.
			name: "Empty BaseURL with SMTP",
			config: Config{
				SMTP: SMTPConfig{Host: "smtp.example.com"},
			},
			wantErr: true,
		},
		{
			name: "Empty BaseURL with an OAuth provider",
			config: Config{
				OAuth: OAuthConfig{Custom: OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"}},
			},
			wantErr: true,
		},
		{
			name: "BaseURL without scheme",
			config: Config{
				BaseURL: "nas.local:8751",
			},
			wantErr: true,
		},
		{
			name: "BaseURL with a non-http scheme",
			config: Config{
				BaseURL: "ftp://nas.local",
			},
			wantErr: true,
		},
		{
			name: "Insecure JWT Secret (default)",
			config: Config{
				BaseURL: "http://localhost:8080",
				JWT: JWTConfig{
					Secret: "change-me-in-production",
				},
			},
			wantErr: true,
		},
		{
			name: "Insecure JWT Secret (too short)",
			config: Config{
				BaseURL: "http://localhost:8080",
				JWT: JWTConfig{
					Secret: "short",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
