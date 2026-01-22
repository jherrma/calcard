package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v10"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	DataDir  string         `yaml:"data_dir" env:"CALDAV_DATA_DIR" envDefault:"./data"`
	LogLevel string         `yaml:"log_level" env:"CALDAV_LOG_LEVEL" envDefault:"info"`
}

// ServerConfig contains server-specific settings
type ServerConfig struct {
	Host string `yaml:"host" env:"CALDAV_SERVER_HOST" envDefault:"0.0.0.0"`
	Port string `yaml:"port" env:"CALDAV_SERVER_PORT" envDefault:"8080"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Driver   string `yaml:"driver" env:"CALDAV_DB_DRIVER" envDefault:"sqlite"`
	Host     string `yaml:"host" env:"CALDAV_DB_HOST"`
	Port     string `yaml:"port" env:"CALDAV_DB_PORT" envDefault:"5432"`
	User     string `yaml:"user" env:"CALDAV_DB_USER"`
	Password string `yaml:"password" env:"CALDAV_DB_PASSWORD"`
	Name     string `yaml:"name" env:"CALDAV_DB_NAME" envDefault:"caldav"`
	SSLMode  string `yaml:"ssl_mode" env:"CALDAV_DB_SSLMODE" envDefault:"disable"`
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
	cfg := &Config{}

	// 1. Set defaults through env tags by parsing once with empty environment
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse default config: %w", err)
	}

	// 2. Load from YAML file if it exists
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

	// 3. Override with environment variables
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
