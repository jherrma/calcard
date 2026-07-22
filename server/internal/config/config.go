package config

import (
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
	Server       ServerConfig       `yaml:"server"`
	Database     DatabaseConfig     `yaml:"database"`
	DataDir      string             `yaml:"data_dir" env:"CALDAV_DATA_DIR"`
	LogLevel     string             `yaml:"log_level" env:"CALDAV_LOG_LEVEL"`
	BaseURL      string             `yaml:"base_url" env:"CALDAV_BASE_URL"`
	SMTP         SMTPConfig         `yaml:"smtp"`
	JWT          JWTConfig          `yaml:"jwt"`
	RateLimit    RateLimitConfig    `yaml:"rate_limit"`
	OAuth        OAuthConfig        `yaml:"oauth"`
	TLS          TLSConfig          `yaml:"tls"`
	CORS         CORSConfig         `yaml:"cors"`
	Security     SecurityConfig     `yaml:"security"`
	Registration RegistrationConfig `yaml:"registration"`
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

	// AuthIPRequests and AuthEmailRequests bound the auth-specific limiters on
	// login / forgot-password / reset-password (all within Window). The per-IP
	// allowance MUST stay above the per-email allowance: with IP <= email the
	// IP limiter always trips first from a single source address, so the tighter
	// per-account (per-email) control is never reached — masked in production
	// behind a NAT/reverse proxy where every client shares one c.IP(), and
	// impossible to exercise from a single-connection test. reset-password has
	// no email to key on, so it uses only the per-IP allowance.
	AuthIPRequests    int `yaml:"auth_ip_requests" env:"CALDAV_RATE_LIMIT_AUTH_IP_REQUESTS"`
	AuthEmailRequests int `yaml:"auth_email_requests" env:"CALDAV_RATE_LIMIT_AUTH_EMAIL_REQUESTS"`
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
	// AssumeEmailVerified treats any email this provider returns as verified.
	// Needed for providers that never send the email_verified claim (e.g.
	// Microsoft's userinfo endpoint). Only enable it for providers you trust to
	// return addresses their users actually control. Defaults to false so the
	// verified-email guard stays on for providers that do send the claim.
	AssumeEmailVerified bool `yaml:"assume_email_verified" env:"ASSUME_EMAIL_VERIFIED"`
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

// RegistrationConfig controls self-service account registration. The zero value
// (Disabled=false) keeps registration ENABLED so tests and directly-constructed
// configs behave as before.
type RegistrationConfig struct {
	Disabled bool `yaml:"disabled" env:"CALDAV_REGISTRATION_DISABLED"`
}

// DSN returns the database connection string based on the driver
func (c *DatabaseConfig) DSN(dataDir string) string {
	if c.IsSQLite() {
		return filepath.Join(dataDir, "caldav.db")
	}
	// Quote each value per libpq rules so values with spaces, quotes or
	// backslashes don't break the key=value DSN.
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		quoteDSNValue(c.Host), quoteDSNValue(c.Port), quoteDSNValue(c.User),
		quoteDSNValue(c.Password), quoteDSNValue(c.Name), quoteDSNValue(c.SSLMode))
}

// quoteDSNValue wraps a libpq key=value DSN value in single quotes, escaping
// backslashes and single quotes.
func quoteDSNValue(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "'", "\\'")
	return "'" + v + "'"
}

// IsSQLite returns true if the driver is sqlite
func (c *DatabaseConfig) IsSQLite() bool {
	return c.Driver == "sqlite"
}

// IsPostgres returns true if the driver is postgres
func (c *DatabaseConfig) IsPostgres() bool {
	return c.Driver == "postgres"
}

// Load initialization the configuration from environment variables and an optional YAML file
func Load(configPath string) (*Config, error) {
	// 0. Load .env files if they exist. Load each separately and ignore its
	// error individually — godotenv.Load(".env", ".env.local") stops at the
	// first missing file, so a missing .env used to block .env.local entirely.
	_ = godotenv.Load(".env")
	_ = godotenv.Load(".env.local")

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
			// Per-IP allowance sits ABOVE the per-email allowance on purpose so
			// the tighter per-account control is reachable from one IP (and
			// behind a proxy). 20/min per IP still throttles one address
			// spraying many accounts while being friendly to shared/office NAT;
			// 10/min per email remains the tighter per-account brute-force /
			// mail-flood control.
			AuthIPRequests:    20,
			AuthEmailRequests: 10,
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
			AllowCredentials: false,
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

	// 1. Load from YAML file. A non-empty configPath is always an explicitly
	// requested file: resolveConfigPath in cmd/server only returns non-empty
	// for the --config flag / CALDAV_CONFIG_PATH / CALDAV_CONFIG_FILE and
	// returns "" for the implicit env-and-defaults case. An explicitly
	// requested file that is missing or unreadable must be a fatal error
	// rather than silently booting on defaults; only the implicit ("") case
	// may be absent without error.
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			return nil, fmt.Errorf("config file %q was explicitly requested but cannot be read: %w", configPath, err)
		}
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

	// 2. Override with environment variables
	// Note: We use env.Parse(cfg) which will only override if the env var is PRESENT
	// since we've removed envDefault from the struct tags.
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	// An unset JWT secret is left empty here on purpose: JWTManager.EnsureSecret
	// loads it from (or generates and persists it to) system_settings at startup,
	// so it survives restarts. Generating one here would shadow that and mint a
	// fresh secret every boot, invalidating all existing tokens.

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

	// Clamp non-positive auth rate-limit thresholds back to their defaults. A
	// misconfigured 0 (or negative) value would otherwise reach Fiber's limiter
	// as Max<=0, which silently falls back to 5 — collapsing BOTH auth limiters
	// to 5/5 and resurrecting the IP-trips-first ordering bug (per-IP must stay
	// strictly above per-email so the per-email limiter is reachable). Mirror
	// the default literals from the defaults block above so an operator can't
	// accidentally disable the ordering guarantee.
	if cfg.RateLimit.AuthIPRequests <= 0 {
		cfg.RateLimit.AuthIPRequests = 20
	}
	if cfg.RateLimit.AuthEmailRequests <= 0 {
		cfg.RateLimit.AuthEmailRequests = 10
	}

	return cfg, nil
}

// Validate checks the configuration for errors and security issues
func (c *Config) Validate() error {
	var errs []string

	// An empty secret is allowed at validation time: JWTManager.EnsureSecret
	// fills it from system_settings (loading or generating+persisting) during
	// startup. Only reject an explicitly-provided secret that is insecure.
	if c.JWT.Secret != "" {
		if c.JWT.Secret == "change-me-in-production" {
			errs = append(errs, "CALDAV_JWT_SECRET must be set to a secure value (default value is insecure)")
		}
		if len(c.JWT.Secret) < 16 {
			errs = append(errs, "CALDAV_JWT_SECRET must be at least 16 characters")
		}
	}
	if c.BaseURL == "" {
		errs = append(errs, "CALDAV_BASE_URL must be set")
	}

	// Fiber's CORS middleware panics when credentials are allowed together with
	// a wildcard origin. Fiber treats BOTH a literal "*" origin AND an empty
	// origin list as "allow all", so reject either combination here with a clear
	// message instead of crashing at startup (M15).
	if c.CORS.Enabled && c.CORS.AllowCredentials {
		if len(c.CORS.AllowedOrigins) == 0 {
			errs = append(errs, "CORS cannot combine AllowCredentials with an empty origin list (Fiber treats it as a wildcard); set explicit CALDAV_CORS_ALLOWED_ORIGINS")
		}
		for _, o := range c.CORS.AllowedOrigins {
			if o == "*" {
				errs = append(errs, `CORS cannot combine AllowCredentials with a wildcard origin "*"; set explicit CALDAV_CORS_ALLOWED_ORIGINS`)
				break
			}
		}
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
