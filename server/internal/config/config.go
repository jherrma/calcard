package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	DataDir   string          `yaml:"data_dir" env:"CALDAV_DATA_DIR"`
	LogLevel  string          `yaml:"log_level" env:"CALDAV_LOG_LEVEL"`
	BaseURL   string          `yaml:"base_url" env:"CALDAV_BASE_URL"`
	SMTP      SMTPConfig      `yaml:"smtp"`
	JWT       JWTConfig       `yaml:"jwt"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	OAuth     OAuthConfig     `yaml:"oauth"`
	SAML      SAMLConfig      `yaml:"saml"`
	TLS       TLSConfig       `yaml:"tls"`
	CORS      CORSConfig      `yaml:"cors"`
	Security  SecurityConfig  `yaml:"security"`
}

// ServerConfig contains server-specific settings
type ServerConfig struct {
	Host string `yaml:"host" env:"CALDAV_SERVER_HOST"`
	Port string `yaml:"port" env:"CALDAV_SERVER_PORT"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Driver      string `yaml:"driver" env:"CALDAV_DB_DRIVER"`
	Host        string `yaml:"host" env:"CALDAV_DB_HOST"`
	Port        string `yaml:"port" env:"CALDAV_DB_PORT"`
	User        string `yaml:"user" env:"CALDAV_DB_USER"`
	Password    string `yaml:"password" env:"CALDAV_DB_PASSWORD"`
	Name        string `yaml:"name" env:"CALDAV_DB_NAME"`
	SSLMode     string `yaml:"ssl_mode" env:"CALDAV_DB_SSLMODE"`
	AutoMigrate bool   `yaml:"auto_migrate" env:"CALDAV_DB_AUTO_MIGRATE"`
}

// SMTPConfig contains SMTP settings for email verification
type SMTPConfig struct {
	Host     string `yaml:"host" env:"CALDAV_SMTP_HOST"`
	Port     string `yaml:"port" env:"CALDAV_SMTP_PORT"`
	User     string `yaml:"user" env:"CALDAV_SMTP_USER"`
	Password string `yaml:"password" env:"CALDAV_SMTP_PASSWORD"`
	From     string `yaml:"from" env:"CALDAV_SMTP_FROM"`
}

// JWTConfig contains JWT settings
type JWTConfig struct {
	Secret        string        `yaml:"secret" env:"CALDAV_JWT_SECRET"`
	AccessExpiry  time.Duration `yaml:"access_expiry" env:"CALDAV_JWT_ACCESS_EXPIRY"`
	RefreshExpiry time.Duration `yaml:"refresh_expiry" env:"CALDAV_JWT_REFRESH_EXPIRY"`
	ResetExpiry   time.Duration `yaml:"reset_expiry" env:"CALDAV_PASSWORD_RESET_EXPIRY"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled  bool          `yaml:"enabled" env:"CALDAV_RATE_LIMIT_ENABLED"`
	Requests int           `yaml:"requests" env:"CALDAV_RATE_LIMIT_REQUESTS"`
	Window   time.Duration `yaml:"window" env:"CALDAV_RATE_LIMIT_WINDOW"`
}

// OAuthConfig contains OAuth2/OIDC settings
type OAuthConfig struct {
	Google    OAuthProviderConfig `yaml:"google" envPrefix:"CALDAV_OAUTH_GOOGLE_"`
	Microsoft OAuthProviderConfig `yaml:"microsoft" envPrefix:"CALDAV_OAUTH_MICROSOFT_"`
	Custom    OAuthProviderConfig `yaml:"custom" envPrefix:"CALDAV_OAUTH_CUSTOM_"`
}

// OAuthProviderConfig contains settings for an OAuth/OIDC provider
type OAuthProviderConfig struct {
	ClientID     string `yaml:"client_id" env:"CLIENT_ID"`
	ClientSecret string `yaml:"client_secret" env:"CLIENT_SECRET"`
	Issuer       string `yaml:"issuer" env:"ISSUER"`
}

// SAMLConfig contains settings for SAML authentication
type SAMLConfig struct {
	EntityID             string `yaml:"entity_id" env:"CALDAV_SAML_ENTITY_ID"`
	IDPMetadataURL       string `yaml:"idp_metadata_url" env:"CALDAV_SAML_IDP_METADATA_URL"`
	IDPSSOURL            string `yaml:"idp_sso_url" env:"CALDAV_SAML_IDP_SSO_URL"`
	IDPCert              string `yaml:"idp_cert" env:"CALDAV_SAML_IDP_CERT"` // Path to cert or content
	SPCert               string `yaml:"sp_cert" env:"CALDAV_SAML_SP_CERT"`   // Path to cert or content
	SPKey                string `yaml:"sp_key" env:"CALDAV_SAML_SP_KEY"`     // Path to key or content
	SignRequests         bool   `yaml:"sign_requests" env:"CALDAV_SAML_SIGN_REQUESTS"`
	WantSignedAssertions bool   `yaml:"want_signed_assertions" env:"CALDAV_SAML_WANT_SIGNED_ASSERTIONS"`
}

// TLSConfig contains TLS/SSL settings
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled" env:"CALDAV_TLS_ENABLED"`
	CertFile string `yaml:"cert_file" env:"CALDAV_TLS_CERT_FILE"`
	KeyFile  string `yaml:"key_file" env:"CALDAV_TLS_KEY_FILE"`
}

// CORSConfig contains CORS settings
type CORSConfig struct {
	Enabled          bool     `yaml:"enabled" env:"CALDAV_CORS_ENABLED"`
	AllowedOrigins   []string `yaml:"allowed_origins" env:"CALDAV_CORS_ALLOWED_ORIGINS" envSeparator:","`
	ExposeHeaders    []string `yaml:"expose_headers" env:"CALDAV_CORS_EXPOSE_HEADERS" envSeparator:","`
	AllowCredentials bool     `yaml:"allow_credentials" env:"CALDAV_CORS_ALLOW_CREDENTIALS"`
	MaxAge           int      `yaml:"max_age" env:"CALDAV_CORS_MAX_AGE"`
}

// SecurityConfig contains general security settings
type SecurityConfig struct {
	Enabled        bool          `yaml:"enabled" env:"CALDAV_SECURITY_HEADERS_ENABLED"`
	HSTSEnabled    bool          `yaml:"hsts_enabled" env:"CALDAV_HSTS_ENABLED"`
	HSTSMaxAge     int           `yaml:"hsts_max_age" env:"CALDAV_HSTS_MAX_AGE"`
	MaxRequestSize int64         `yaml:"max_request_size" env:"CALDAV_MAX_REQUEST_SIZE"` // Bytes
	RequestTimeout time.Duration `yaml:"request_timeout" env:"CALDAV_REQUEST_TIMEOUT"`
}

// DSN returns the database connection string based on the driver
func (c *DatabaseConfig) DSN(dataDir string) string {
	if c.IsSQLite() {
		return filepath.Join(dataDir, "caldav.db")
	}
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

// IsSQLite returns true if the driver is sqlite
func (c *DatabaseConfig) IsSQLite() bool {
	return c.Driver == "sqlite"
}

// IsPostgres returns true if the driver is postgres
func (c *DatabaseConfig) IsPostgres() bool {
	return c.Driver == "postgres"
}

// generateJWTSecret generates a secure random JWT secret
func generateJWTSecret() (string, error) {
	// Generate 32 random bytes (256 bits) which will be base64 encoded to ~43 characters
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random JWT secret: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Load initialization the configuration from environment variables and an optional YAML file
func Load(configPath string) (*Config, error) {
	// 0. Load .env files if they exist
	// We ignore errors because .env files are optional
	_ = godotenv.Load(".env", ".env.local")

	// Set hardcoded defaults
	cfg := &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: "8080",
		},
		Database: DatabaseConfig{
			Driver:      "sqlite",
			Port:        "5432",
			Name:        "caldav",
			SSLMode:     "disable",
			AutoMigrate: true,
		},
		DataDir:  "./data",
		LogLevel: "info",
		BaseURL:  "http://localhost:8080",
		SMTP: SMTPConfig{
			Port: "587",
		},
		JWT: JWTConfig{
			AccessExpiry:  10 * time.Minute,
			RefreshExpiry: 7 * 24 * time.Hour,
			ResetExpiry:   time.Hour,
		},
		RateLimit: RateLimitConfig{
			Enabled:  true,
			Requests: 100,
			Window:   time.Minute,
		},
		OAuth: OAuthConfig{
			Google: OAuthProviderConfig{
				Issuer: "https://accounts.google.com",
			},
			Microsoft: OAuthProviderConfig{
				Issuer: "https://login.microsoftonline.com/common/v2.0",
			},
		},
		TLS: TLSConfig{
			Enabled: false,
		},
		CORS: CORSConfig{
			Enabled:          false,
			AllowedOrigins:   []string{"*"},
			ExposeHeaders:    []string{"ETag", "DAV", "Allow", "Link"},
			AllowCredentials: true,
			MaxAge:           86400,
		},
		Security: SecurityConfig{
			Enabled:        true,
			HSTSEnabled:    false,
			HSTSMaxAge:     31536000,
			MaxRequestSize: 10 * 1024 * 1024, // 10MB
			RequestTimeout: 30 * time.Second,
		},
	}

	// 1. Load from YAML file if it exists
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			file, err := os.Open(configPath)
			if err != nil {
				return nil, fmt.Errorf("failed to open config file: %w", err)
			}
			defer file.Close()

			decoder := yaml.NewDecoder(file)
			if err := decoder.Decode(cfg); err != nil {
				return nil, fmt.Errorf("failed to decode config file: %w", err)
			}
		}
	}

	// 2. Override with environment variables
	// Note: We use env.Parse(cfg) which will only override if the env var is PRESENT
	// since we've removed envDefault from the struct tags.
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	// 3. Generate JWT secret if not provided
	if cfg.JWT.Secret == "" {
		secret, err := generateJWTSecret()
		if err != nil {
			return nil, err
		}
		cfg.JWT.Secret = secret
		fmt.Fprintf(os.Stderr, "WARNING: No JWT secret provided. Generated a random secret for this session.\n")
		fmt.Fprintf(os.Stderr, "         To persist this secret, set CALDAV_JWT_SECRET=%s\n", secret)
	}

	// Auto-detect PostgreSQL mode
	if cfg.Database.Host != "" {
		cfg.Database.Driver = "postgres"
	}

	// Validation
	if cfg.Database.IsPostgres() {
		if cfg.Database.Host == "" || cfg.Database.User == "" || cfg.Database.Name == "" {
			return nil, fmt.Errorf("postgres driver requires host, user, and name to be set")
		}
	}

	return cfg, nil
}

// Validate checks the configuration for errors and security issues
func (c *Config) Validate() error {
	var errs []string

	if c.JWT.Secret == "" {
		errs = append(errs, "CALDAV_JWT_SECRET must be set (this should have been auto-generated)")
	}
	if c.JWT.Secret == "change-me-in-production" {
		errs = append(errs, "CALDAV_JWT_SECRET must be set to a secure value (default value is insecure)")
	}
	if len(c.JWT.Secret) < 16 {
		errs = append(errs, "CALDAV_JWT_SECRET must be at least 16 characters")
	}
	if c.BaseURL == "" {
		errs = append(errs, "CALDAV_BASE_URL must be set")
	}

	if c.TLS.Enabled {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			errs = append(errs, "CALDAV_TLS_CERT_FILE and CALDAV_TLS_KEY_FILE must be set when TLS is enabled")
		}
		// Basic file existence check
		if _, err := os.Stat(c.TLS.CertFile); err != nil && !os.IsNotExist(err) {
			// Don't fail if file doesn't exist yet (might be generated), but fail on permission errors
			errs = append(errs, fmt.Sprintf("Cannot access TLS cert file: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
