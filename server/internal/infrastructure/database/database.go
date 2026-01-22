package database

import (
	"fmt"

	"github.com/jherrma/caldav-server/internal/config"
	"gorm.io/gorm"
)

// Database defines the interface for database operations
type Database interface {
	DB() *gorm.DB
	Close() error
	Ping() error
	Migrate(models ...interface{}) error
}

// New creates a new database connection based on the provided configuration
func New(cfg *config.Config) (Database, error) {
	if cfg.Database.IsSQLite() {
		return NewSQLite(cfg)
	}
	if cfg.Database.IsPostgres() {
		return NewPostgres(cfg)
	}
	return nil, fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
}
