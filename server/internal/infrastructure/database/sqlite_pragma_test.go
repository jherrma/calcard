package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
)

// TestSQLitePragmasApplyToEveryConnection is the regression test for H13: the
// PRAGMAs must hold on a freshly-opened pooled connection, not just the first.
func TestSQLitePragmasApplyToEveryConnection(t *testing.T) {
	cfg := &config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite"},
	}
	dbi, err := NewSQLite(cfg)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer dbi.Close()

	sqlDB, err := dbi.DB().DB()
	if err != nil {
		t.Fatalf("DB(): %v", err)
	}
	sqlDB.SetMaxOpenConns(2)

	ctx := context.Background()
	c1, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn1: %v", err)
	}
	defer c1.Close()
	c2, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn2: %v", err)
	}
	defer c2.Close()

	for i, c := range []*sql.Conn{c1, c2} {
		var fk int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d: foreign_keys = %d, want 1", i, fk)
		}
		var bt int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if bt != 5000 {
			t.Errorf("conn %d: busy_timeout = %d, want 5000", i, bt)
		}
	}
}
