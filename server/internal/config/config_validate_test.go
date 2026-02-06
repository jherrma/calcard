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
			name: "Missing BaseURL",
			config: Config{
				BaseURL: "",
				JWT: JWTConfig{
					Secret: "secure-secret-16",
				},
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
