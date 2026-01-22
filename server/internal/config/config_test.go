package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any CALDAV_ env vars to test defaults
	os.Clearenv()

	cfg, err := Load("")
	assert.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, "8080", cfg.Server.Port)
	assert.Equal(t, "sqlite", cfg.Database.Driver)
	assert.Equal(t, "./data", cfg.DataDir)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoadEnvOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("CALDAV_SERVER_PORT", "9090")
	os.Setenv("CALDAV_DB_HOST", "localhost")
	os.Setenv("CALDAV_DB_USER", "postgres")
	os.Setenv("CALDAV_DB_NAME", "testdb")

	cfg, err := Load("")
	assert.NoError(t, err)

	assert.Equal(t, "9090", cfg.Server.Port)
	assert.Equal(t, "postgres", cfg.Database.Driver) // Auto-detected
	assert.Equal(t, "localhost", cfg.Database.Host)
	assert.Equal(t, "postgres", cfg.Database.User)
	assert.Equal(t, "testdb", cfg.Database.Name)
}

func TestLoadYAML(t *testing.T) {
	os.Clearenv()
	yamlContent := `
server:
  port: "7070"
database:
  driver: "sqlite"
data_dir: "/tmp/data"
`
	tmpfile, err := os.CreateTemp("", "config.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(yamlContent))
	assert.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	assert.NoError(t, err)

	assert.Equal(t, "7070", cfg.Server.Port)
	assert.Equal(t, "sqlite", cfg.Database.Driver)
	assert.Equal(t, "/tmp/data", cfg.DataDir)

	// Env override YAML
	os.Setenv("CALDAV_SERVER_PORT", "6060")
	cfg, err = Load(tmpfile.Name())
	assert.NoError(t, err)
	assert.Equal(t, "6060", cfg.Server.Port)
}

func TestValidation(t *testing.T) {
	os.Clearenv()
	os.Setenv("CALDAV_DB_DRIVER", "postgres")
	// Missing host, user, name

	_, err := Load("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "postgres driver requires host, user, and name")
}
