package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sqliteDB struct {
	db *gorm.DB
}

// NewSQLite creates a new SQLite database connection
func NewSQLite(cfg *config.Config) (Database, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Append SQLite PRAGMAs as DSN query parameters so they apply to EVERY
	// pooled connection (mattn/go-sqlite3 runs them on each connect). Setting
	// them once via Exec only configured a single connection — H13.
	dsn := cfg.Database.DSN(cfg.DataDir) +
		"?_busy_timeout=5000&_foreign_keys=on&_journal_mode=WAL&_synchronous=NORMAL"
	dbLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: dbLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	return &sqliteDB{db: db}, nil
}

func (s *sqliteDB) DB() *gorm.DB {
	return s.db
}

func (s *sqliteDB) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *sqliteDB) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (s *sqliteDB) Migrate(models ...interface{}) error {
	return s.db.AutoMigrate(models...)
}
