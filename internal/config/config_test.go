package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() *Config {
	c := Default()
	c.Database.Password = "secret"
	return c
}

func TestValidate_Defaults(t *testing.T) {
	c := validConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"empty host", func(c *Config) { c.Database.Host = "" }, "database.host"},
		{"port too low", func(c *Config) { c.Database.Port = 0 }, "database.port"},
		{"port too high", func(c *Config) { c.Database.Port = 70000 }, "database.port"},
		{"empty user", func(c *Config) { c.Database.User = "" }, "database.user"},
		{"empty database", func(c *Config) { c.Database.Database = "" }, "database.database"},
		{"bad sslmode", func(c *Config) { c.Database.SSLMode = "bogus" }, "database.sslmode"},
		{"limit too low", func(c *Config) { c.Output.Limit = 0 }, "output.limit"},
		{"limit too high", func(c *Config) { c.Output.Limit = 9999 }, "output.limit"},
		{"bad order_by", func(c *Config) { c.Output.OrderBy = "nope" }, "output.order_by"},
		{"bad log level", func(c *Config) { c.Log.Level = "trace" }, "log.level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not mention field %q", err, tt.wantSub)
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")

	orig := validConfig()
	orig.Database.Host = "db.example.com"
	orig.Database.Port = 6543
	orig.Database.User = "analyst"
	orig.Database.Password = "p@ssw0rd: tricky"
	orig.Database.Database = "analytics"
	orig.Database.SSLMode = "require"
	orig.Output.Limit = 50
	orig.Output.OrderBy = "mean"
	orig.Log.Level = "debug"

	if err := Save(orig, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *loaded != *orig {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", *loaded, *orig)
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(filepath.Join(dir, "does-not-exist.yaml"))
	if !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")
	partial := []byte(`database:
  host: otherhost
  user: me
  database: mydb
`)
	if err := os.WriteFile(path, partial, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("expected default port 5432, got %d", cfg.Database.Port)
	}
	if cfg.Database.SSLMode != "disable" {
		t.Errorf("expected default sslmode disable, got %q", cfg.Database.SSLMode)
	}
	if cfg.Output.Limit != 20 {
		t.Errorf("expected default output.limit 20, got %d", cfg.Output.Limit)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected default log.level info, got %q", cfg.Log.Level)
	}
}

func TestRedacted_HidesPassword(t *testing.T) {
	c := validConfig()
	c.Database.Password = "super-secret"
	r := c.Redacted()
	if r.Database.Password != "***" {
		t.Errorf("password not redacted: %q", r.Database.Password)
	}
	if c.Database.Password != "super-secret" {
		t.Errorf("Redacted mutated original password")
	}
}

func TestRedacted_EmptyPasswordStaysEmpty(t *testing.T) {
	c := validConfig()
	c.Database.Password = ""
	r := c.Redacted()
	if r.Database.Password != "" {
		t.Errorf("expected empty password unchanged, got %q", r.Database.Password)
	}
}

func TestSavedFile_NoPlaintextPasswordInUnrelatedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	c := validConfig()
	c.Database.Password = "hunter2"
	if err := Save(c, path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The password lives under the password: key; make sure it does not
	// show up under host/user/database by accident (regression guard).
	content := string(b)
	hostLine := grepLine(content, "host:")
	userLine := grepLine(content, "user:")
	if strings.Contains(hostLine, "hunter2") || strings.Contains(userLine, "hunter2") {
		t.Errorf("password leaked into unrelated field:\n%s", content)
	}
}

func grepLine(content, needle string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
