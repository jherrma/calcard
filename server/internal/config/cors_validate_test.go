package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsCORSWildcardWithCredentials(t *testing.T) {
	c := &Config{BaseURL: "http://localhost:8080"}
	c.CORS.Enabled = true
	c.CORS.AllowCredentials = true
	c.CORS.AllowedOrigins = []string{"*"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "AllowCredentials") {
		t.Fatalf("expected wildcard+credentials to be rejected, got: %v", err)
	}
}

func TestValidateAllowsCORSWildcardWithoutCredentials(t *testing.T) {
	c := &Config{BaseURL: "http://localhost:8080"}
	c.CORS.Enabled = true
	c.CORS.AllowCredentials = false
	c.CORS.AllowedOrigins = []string{"*"}
	if err := c.Validate(); err != nil {
		t.Fatalf("wildcard without credentials should pass, got: %v", err)
	}
}
