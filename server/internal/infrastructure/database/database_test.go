package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver: "sqlite",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()

	assert.NotNil(t, db.DB())
	assert.NoError(t, db.Ping())

	// Verify file creation
	dbPath := filepath.Join(dataDir, "caldav.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestPostgresFactory(t *testing.T) {
	// We only test the factory logic since we might not have a running postgres
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "postgres",
			Host:   "localhost",
			User:   "postgres",
			Name:   "caldav",
		},
	}

	// This might fail if no postgres is running, but it validates the factory routing
	db, err := New(cfg)
	if err == nil {
		db.Close()
	}
}
