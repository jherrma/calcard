package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// configDocPath is CONFIGURATION.md at the repository root, relative to this
// package directory (server/internal/config).
var configDocPath = filepath.Join("..", "..", "..", "CONFIGURATION.md")

// configLeaf is one settable configuration value as the config code binds it.
type configLeaf struct {
	yamlPath  string // e.g. "smtp.port", for messages
	yamlKey   string // the leaf key, e.g. "port"
	envPrefix string // envPrefix of the enclosing struct(s), e.g. "CALDAV_OAUTH_GOOGLE_"
	envName   string // the field's own env tag, e.g. "CALDAV_SMTP_PORT" or "CLIENT_ID"
}

// collectConfigLeaves walks the Config struct and returns every field that
// carries an env tag, together with its YAML key and any envPrefix inherited
// from enclosing structs — the same composition caarlos0/env performs.
func collectConfigLeaves(t reflect.Type, yamlPrefix, envPrefix string) []configLeaf {
	var leaves []configLeaf
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		yamlKey := strings.Split(f.Tag.Get("yaml"), ",")[0]
		path := yamlKey
		if yamlPrefix != "" {
			path = yamlPrefix + "." + yamlKey
		}
		if env := f.Tag.Get("env"); env != "" {
			leaves = append(leaves, configLeaf{yamlPath: path, yamlKey: yamlKey, envPrefix: envPrefix, envName: env})
			continue
		}
		if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == t.PkgPath() {
			leaves = append(leaves, collectConfigLeaves(f.Type, path, envPrefix+f.Tag.Get("envPrefix"))...)
		}
	}
	return leaves
}

// TestConfigurationDocCoversEveryKey fails when a configuration value the
// server reads is not documented in CONFIGURATION.md (F30: the SMTP port, user,
// password and from keys were readable but undocumented).
//
// For each env-tagged field it requires a line of the doc that names both the
// YAML key and the env var in backticks — the shape of every table row, so a
// key documented under the wrong variable (or not at all) is caught without
// tying the test to the table layout or the section order. Fields under an
// envPrefix (the OAuth providers) are documented once as a prefix plus a list
// of suffixes, so there the prefix must appear in the doc and the YAML key and
// suffix must share a line.
func TestConfigurationDocCoversEveryKey(t *testing.T) {
	raw, err := os.ReadFile(configDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", configDocPath, err)
	}
	doc := string(raw)
	lines := strings.Split(doc, "\n")

	hasLineWith := func(parts ...string) bool {
		for _, line := range lines {
			ok := true
			for _, p := range parts {
				if !strings.Contains(line, p) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}
	tick := func(s string) string { return "`" + s + "`" }

	leaves := collectConfigLeaves(reflect.TypeOf(Config{}), "", "")
	if len(leaves) < 40 {
		// Guards the walker itself: if it stopped recursing, the test would
		// pass while checking almost nothing.
		t.Fatalf("found only %d env-bound config fields; the struct walk is broken", len(leaves))
	}
	for _, l := range leaves {
		if l.envPrefix == "" {
			if !hasLineWith(tick(l.yamlKey), tick(l.envName)) {
				t.Errorf("CONFIGURATION.md has no line documenting %s together with %s", tick(l.yamlPath), tick(l.envName))
			}
			continue
		}
		if !strings.Contains(doc, tick(l.envPrefix)) {
			t.Errorf("CONFIGURATION.md does not mention the env prefix %s (for %s)", tick(l.envPrefix), tick(l.yamlPath))
		}
		if !hasLineWith(tick(l.yamlKey), tick(l.envName)) {
			t.Errorf("CONFIGURATION.md has no line documenting %s together with its env suffix %s (%s%s)",
				tick(l.yamlPath), tick(l.envName), l.envPrefix, l.envName)
		}
	}
}

// TestConfigurationDocCoversEntrypointEnvVars covers the CALDAV_* variables
// that cmd/server reads directly instead of through the Config struct (the
// config-file path and its alias).
func TestConfigurationDocCoversEntrypointEnvVars(t *testing.T) {
	raw, err := os.ReadFile(configDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", configDocPath, err)
	}
	doc := string(raw)

	files, err := filepath.Glob(filepath.Join("..", "..", "cmd", "server", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files found in cmd/server (err=%v)", err)
	}
	getenv := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("(CALDAV_[A-Z0-9_]+)"\)`)
	found := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range getenv.FindAllStringSubmatch(string(src), -1) {
			found++
			if !strings.Contains(doc, "`"+m[1]+"`") {
				t.Errorf("%s reads %s, which CONFIGURATION.md does not document", filepath.Base(file), m[1])
			}
		}
	}
	if found == 0 {
		t.Fatal("found no os.Getenv(\"CALDAV_…\") call in cmd/server; the scan is broken")
	}
}
