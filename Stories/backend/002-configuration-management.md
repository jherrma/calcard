# Story 002: Configuration Management

## Title
Implement Configuration Loading with Environment Variable Support

## Description
As a developer, I want a configuration system that loads settings from environment variables and optional config files, so that the application can be configured for different environments without code changes.

## Acceptance Criteria

- [ ] Configuration struct defined in `internal/config/config.go`
- [ ] Configuration loads from environment variables with `CALDAV_` prefix
- [ ] Optional `config.yaml` file support (overridden by env vars)
- [ ] Database configuration supports two modes:
  - **Default (no config)**: Uses SQLite at `./data/caldav.db`
  - **PostgreSQL**: When `CALDAV_DB_HOST` is set, uses PostgreSQL
- [ ] Environment variables supported:
  ```
  CALDAV_SERVER_HOST       (default: "0.0.0.0")
  CALDAV_SERVER_PORT       (default: "8080")
  CALDAV_DB_DRIVER         (default: "sqlite", options: "sqlite", "postgres")
  CALDAV_DB_HOST           (PostgreSQL only)
  CALDAV_DB_PORT           (default: "5432")
  CALDAV_DB_USER           (PostgreSQL only)
  CALDAV_DB_PASSWORD       (PostgreSQL only)
  CALDAV_DB_NAME           (default: "caldav")
  CALDAV_DB_SSLMODE        (default: "disable")
  CALDAV_DATA_DIR          (default: "./data")
  CALDAV_LOG_LEVEL         (default: "info")
  ```
- [ ] SQLite file path defaults to `{CALDAV_DATA_DIR}/caldav.db`
- [ ] Validation ensures required PostgreSQL vars are set when driver is "postgres"
- [ ] Config struct is immutable after loading
- [ ] Example config file provided at `configs/config.yaml.example`

## Technical Notes

- Use `github.com/caarlos0/env/v10` for environment parsing
- Use `gopkg.in/yaml.v3` for YAML config file
- Env vars take precedence over config file values
- Auto-detect PostgreSQL mode: if `CALDAV_DB_HOST` is set, assume postgres

## Configuration Struct

```go
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig
    DataDir  string
    LogLevel string
}

type ServerConfig struct {
    Host string
    Port string
}

type DatabaseConfig struct {
    Driver   string // "sqlite" or "postgres"
    Host     string
    Port     string
    User     string
    Password string
    Name     string
    SSLMode  string
}

func (c *DatabaseConfig) DSN() string // Returns connection string
func (c *DatabaseConfig) IsSQLite() bool
func (c *DatabaseConfig) IsPostgres() bool
```

## Definition of Done

- [ ] `config.Load()` returns valid config with defaults when no env vars set
- [ ] Setting `CALDAV_DB_HOST=localhost` switches to PostgreSQL mode
- [ ] Invalid configuration returns descriptive errors
- [ ] Unit tests cover default values, env override, and validation
