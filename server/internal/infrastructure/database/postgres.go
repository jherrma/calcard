package database

import (
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type postgresDB struct {
	db *gorm.DB
}

// NewPostgres creates a new PostgreSQL database connection
func NewPostgres(cfg *config.Config) (Database, error) {
	dsn := cfg.Database.DSN("")

	var db *gorm.DB
	var err error

	// Automatic retry on initial connection failure (3 attempts, exponential backoff)
	backoff := 1 * time.Second
	for i := 0; i < 3; i++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			break
		}
		if i < 2 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres after retries: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	return &postgresDB{db: db}, nil
}

func (p *postgresDB) DB() *gorm.DB {
	return p.db
}

func (p *postgresDB) Close() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (p *postgresDB) Ping() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (p *postgresDB) Migrate(models ...interface{}) error {
	return p.db.AutoMigrate(models...)
}
