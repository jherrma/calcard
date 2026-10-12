package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
)

// TestSQLiteLowerFoldsUnicode is the SQLite half of F26: the built-in lower()
// folds ASCII only, so a search lower-casing both sides still missed "Ärztin"
// for "ärztin" (PostgreSQL's lower() folds it). Checked on two pooled
// connections because the function is registered per connection.
func TestSQLiteLowerFoldsUnicode(t *testing.T) {
	dbi, err := NewSQLite(&config.Config{
		DataDir:  t.TempDir(),
		Database: config.DatabaseConfig{Driver: "sqlite"},
	})
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
		var lowered string
		if err := c.QueryRowContext(ctx, "SELECT LOWER('ÄRZTIN Übergabe McpSmith')").Scan(&lowered); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		if lowered != "ärztin übergabe mcpsmith" {
			t.Errorf("conn %d: LOWER = %q, want %q", i, lowered, "ärztin übergabe mcpsmith")
		}

		var match int
		if err := c.QueryRowContext(ctx, "SELECT LOWER('Ärztin McpSmith') LIKE ? ESCAPE '\\'", "%ärztin%").Scan(&match); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		if match != 1 {
			t.Errorf("conn %d: LOWER(col) LIKE '%%ärztin%%' did not match", i)
		}

		// The built-in's contract otherwise holds: NULL in, NULL out; numbers as text.
		var null sql.NullString
		if err := c.QueryRowContext(ctx, "SELECT LOWER(NULL)").Scan(&null); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		if null.Valid {
			t.Errorf("conn %d: LOWER(NULL) = %q, want NULL", i, null.String)
		}
		var num string
		if err := c.QueryRowContext(ctx, "SELECT LOWER(42)").Scan(&num); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		if num != "42" {
			t.Errorf("conn %d: LOWER(42) = %q, want \"42\"", i, num)
		}
	}
}
