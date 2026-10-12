package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sqliteDB struct {
	db *gorm.DB
}

// sqliteDriverName is mattn/go-sqlite3 with a Unicode-aware lower() (F26).
const sqliteDriverName = "sqlite3_calcard"

var registerSQLiteOnce sync.Once

// registerSQLiteDriver registers sqliteDriverName. SQLite's built-in lower()
// folds ASCII only, so a case-insensitive search for "ärztin" missed
// "Ärztin" — and the CardDAV text-match path, which lower-cases its query in
// Go, missed it even for an identically-cased query — while PostgreSQL's
// lower() folds Unicode. Overloading the built-in on every pooled connection
// (SQLite lets an application function replace one) makes LOWER() mean the
// same on both drivers, and the same as the Go-side strings.ToLower the
// search use case applies to collection names.
func registerSQLiteDriver() {
	registerSQLiteOnce.Do(func() {
		sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				return conn.RegisterFunc("lower", unicodeLower, true)
			},
		})
	})
}

// unicodeLower mirrors the built-in's contract apart from the folding: NULL
// stays NULL and any other value is lower-cased as text.
func unicodeLower(v any) any {
	switch x := v.(type) {
	case string:
		return strings.ToLower(x)
	case []byte:
		if x == nil { // how mattn/go-sqlite3 passes SQL NULL
			return nil
		}
		return strings.ToLower(string(x))
	default:
		return fmt.Sprint(x)
	}
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
	registerSQLiteDriver()
	db, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: sqliteDriverName, DSN: dsn}), &gorm.Config{
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
