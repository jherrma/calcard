package config

import (
	"fmt"
	"os"
	"path/filepath"

	"time"

	"github.com/caarlos0/env/v10"
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
	Enabled bool `yaml:"enabled" env:"CALDAV_RATE_LIMIT_ENABLED"`
}

// OAuthConfig contains OAuth2/OIDC settings
type OAuthConfig struct {
	Google    OAuthProviderConfig `yaml:"google"`
	Microsoft OAuthProviderConfig `yaml:"microsoft"`
	Custom    OAuthProviderConfig `yaml:"custom"`
}

// OAuthProviderConfig contains settings for an OAuth/OIDC provider
type OAuthProviderConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	Issuer       string `yaml:"issuer"`
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

// Load initialization the configuration from environment variables and an optional YAML file
func Load(configPath string) (*Config, error) {
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
			Enabled: true,
		},
		OAuth: OAuthConfig{
			Google: OAuthProviderConfig{
				Issuer: "https://accounts.google.com",
			},
			Microsoft: OAuthProviderConfig{
				Issuer: "https://login.microsoftonline.com/common/v2.0",
			},
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
