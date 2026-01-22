package database

import (
	"os"
	"testing"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "caldav-migrate-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := &config.Config{
		DataDir: dataDir,
		Database: config.DatabaseConfig{
			Driver:      "sqlite",
			AutoMigrate: true,
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()

	// Run migrations
	err = db.Migrate(Models()...)
	require.NoError(t, err)

	// Verify table exists by attempting to perform an operation
	gormDB := db.DB()
	hasUserTable := gormDB.Migrator().HasTable(&user.User{})
	assert.True(t, hasUserTable)

	// Verify columns
	assert.True(t, gormDB.Migrator().HasColumn(&user.User{}, "uuid"))
	assert.True(t, gormDB.Migrator().HasColumn(&user.User{}, "email"))
	assert.True(t, gormDB.Migrator().HasColumn(&user.User{}, "username"))
}
