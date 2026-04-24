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

func TestConfigExists_MissingFile(t *testing.T) {
	dir := t.TempDir()
	exists, err := ConfigExists(filepath.Join(dir, "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false for missing file")
	}
}

func TestConfigExists_PresentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte("database: {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err := ConfigExists(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true for present file")
	}
}

func TestConfigExists_Directory(t *testing.T) {
	// A directory at the config path is user error — we surface it so
	// `dbagent config show` can print a clearer message than "file not
	// found".
	dir := t.TempDir()
	_, err := ConfigExists(dir)
	if err == nil {
		t.Errorf("expected error when path is a directory")
	}
}

func TestConfigExists_EmptyPath(t *testing.T) {
	if _, err := ConfigExists(""); err == nil {
		t.Errorf("expected error for empty path")
	}
}

func TestDeleteConfig_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte("database: {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConfig(path); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected file to be gone, stat err=%v", err)
	}
}

func TestDeleteConfig_IdempotentOnMissing(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteConfig(filepath.Join(dir, "nope.yaml")); err != nil {
		t.Errorf("DeleteConfig on missing file should return nil, got %v", err)
	}
}

func TestDeleteConfig_EmptyPath(t *testing.T) {
	if err := DeleteConfig(""); err == nil {
		t.Errorf("expected error for empty path")
	}
}

func TestMarshal_RedactsPassword(t *testing.T) {
	c := validConfig()
	c.Database.Password = "hunter2"
	out := string(Marshal(c.Redacted()))
	if strings.Contains(out, "hunter2") {
		t.Errorf("Marshal(Redacted()) leaked password:\n%s", out)
	}
	if !strings.Contains(out, "password: ***") {
		t.Errorf("Marshal(Redacted()) missing redaction marker:\n%s", out)
	}
}

func TestMarshal_NilConfig(t *testing.T) {
	if got := Marshal(nil); got != nil {
		t.Errorf("Marshal(nil) = %q, want nil", got)
	}
}
