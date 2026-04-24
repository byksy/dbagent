// Package config loads, saves, and validates dbagent's on-disk
// YAML configuration. It also resolves the default config path per
// XDG Base Directory conventions.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// ErrConfigNotFound is returned by Load when the config file does not
// exist at the given path.
var ErrConfigNotFound = errors.New("config: file not found")

// Config is the root configuration structure.
type Config struct {
	Database DatabaseConfig `mapstructure:"database" yaml:"database"`
	Output   OutputConfig   `mapstructure:"output"   yaml:"output"`
	Log      LogConfig      `mapstructure:"log"      yaml:"log"`
}

// DatabaseConfig holds Postgres connection parameters.
type DatabaseConfig struct {
	Host     string `mapstructure:"host"     yaml:"host"`
	Port     int    `mapstructure:"port"     yaml:"port"`
	User     string `mapstructure:"user"     yaml:"user"`
	Password string `mapstructure:"password" yaml:"password"`
	Database string `mapstructure:"database" yaml:"database"`
	SSLMode  string `mapstructure:"sslmode"  yaml:"sslmode"`
}

// OutputConfig controls tabular output defaults.
type OutputConfig struct {
	Limit   int    `mapstructure:"limit"    yaml:"limit"`
	OrderBy string `mapstructure:"order_by" yaml:"order_by"`
}

// LogConfig controls logging verbosity.
type LogConfig struct {
	Level string `mapstructure:"level" yaml:"level"`
}

// Default returns a Config populated with the built-in defaults.
func Default() *Config {
	return &Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "",
			Database: "postgres",
			SSLMode:  "disable",
		},
		Output: OutputConfig{
			Limit:   20,
			OrderBy: "total",
		},
		Log: LogConfig{
			Level: "info",
		},
	}
}

// validSSLModes enumerates accepted values for database.sslmode.
var validSSLModes = map[string]struct{}{
	"disable":     {},
	"require":     {},
	"verify-ca":   {},
	"verify-full": {},
}

// validOrderBy enumerates accepted values for output.order_by.
// Stage 5.5 widens the set to include the I/O and cache dimensions
// the stats command also uses.
var validOrderBy = map[string]struct{}{
	"total": {},
	"mean":  {},
	"calls": {},
	"io":    {},
	"cache": {},
}

// validLogLevels enumerates accepted values for log.level.
var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

// Load reads and validates the config at path. Returns ErrConfigNotFound
// if the file does not exist.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("config: stat %s: %w", path, err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	applyDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyDefaults seeds a viper instance with default values so that
// partially-populated config files still produce valid Configs.
func applyDefaults(v *viper.Viper) {
	d := Default()
	v.SetDefault("database.host", d.Database.Host)
	v.SetDefault("database.port", d.Database.Port)
	v.SetDefault("database.user", d.Database.User)
	v.SetDefault("database.password", d.Database.Password)
	v.SetDefault("database.database", d.Database.Database)
	v.SetDefault("database.sslmode", d.Database.SSLMode)
	v.SetDefault("output.limit", d.Output.Limit)
	v.SetDefault("output.order_by", d.Output.OrderBy)
	v.SetDefault("log.level", d.Log.Level)
}

// ConfigExists reports whether a regular file exists at path. It is
// a thin wrapper around os.Stat with two differences: an empty path
// is rejected up front, and a directory at the given path returns an
// error so callers can print a clearer message than "file not
// found". No readability / permission check is performed — that is
// deferred to the caller's subsequent Load.
func ConfigExists(path string) (bool, error) {
	if path == "" {
		return false, errors.New("config: ConfigExists called with empty path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("config: stat %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("config: %s is a directory", path)
	}
	return true, nil
}

// DeleteConfig removes the config file at path. It is idempotent:
// removing a file that does not exist returns nil. All other os
// errors are wrapped and returned as-is.
func DeleteConfig(path string) error {
	if path == "" {
		return errors.New("config: DeleteConfig called with empty path")
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: remove %s: %w", path, err)
	}
	return nil
}

// Save writes the config to path, creating the parent directory if
// needed. The file is written with mode 0600 because it contains the
// database password.
func Save(cfg *Config, path string) error {
	if cfg == nil {
		return errors.New("config: Save called with nil config")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create %s: %w", dir, err)
	}

	data := marshalYAML(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// Marshal returns the YAML bytes for cfg using the same fixed layout
// Save writes to disk. Callers rendering the config for display
// (e.g., `dbagent config show`) should pass cfg.Redacted() so the
// password never escapes.
func Marshal(cfg *Config) []byte {
	if cfg == nil {
		return nil
	}
	return marshalYAML(cfg)
}

// marshalYAML produces the YAML representation of the config. We write
// this by hand rather than pulling in gopkg.in/yaml.v3, since the schema
// is small and fixed — keeps the dependency footprint minimal.
func marshalYAML(cfg *Config) []byte {
	return []byte(fmt.Sprintf(`database:
  host: %s
  port: %d
  user: %s
  password: %s
  database: %s
  sslmode: %s

output:
  limit: %d
  order_by: %s

log:
  level: %s
`,
		yamlString(cfg.Database.Host),
		cfg.Database.Port,
		yamlString(cfg.Database.User),
		yamlString(cfg.Database.Password),
		yamlString(cfg.Database.Database),
		yamlString(cfg.Database.SSLMode),
		cfg.Output.Limit,
		yamlString(cfg.Output.OrderBy),
		yamlString(cfg.Log.Level),
	))
}

// yamlString quotes a string if it contains characters that YAML would
// otherwise interpret specially. For the small whitelist of fields we
// write, this is sufficient.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r == ':' || r == '#' || r == '"' || r == '\'' || r == '\n' || r == ' ' {
			return fmt.Sprintf("%q", s)
		}
	}
	return s
}

// Validate checks all fields. Safe to call on partially-populated
// configs from interactive flows.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config: nil config")
	}
	if c.Database.Host == "" {
		return fieldErr("database.host", "", "required")
	}
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		return fieldErr("database.port", fmt.Sprint(c.Database.Port), "must be between 1 and 65535")
	}
	if c.Database.User == "" {
		return fieldErr("database.user", "", "required")
	}
	if c.Database.Database == "" {
		return fieldErr("database.database", "", "required")
	}
	if _, ok := validSSLModes[c.Database.SSLMode]; !ok {
		return enumErr("database.sslmode", c.Database.SSLMode, []string{"disable", "require", "verify-ca", "verify-full"})
	}
	if c.Output.Limit < 1 || c.Output.Limit > 500 {
		return fieldErr("output.limit", fmt.Sprint(c.Output.Limit), "must be between 1 and 500")
	}
	if _, ok := validOrderBy[c.Output.OrderBy]; !ok {
		return enumErr("output.order_by", c.Output.OrderBy, []string{"total", "mean", "calls", "io", "cache"})
	}
	if _, ok := validLogLevels[c.Log.Level]; !ok {
		return enumErr("log.level", c.Log.Level, []string{"debug", "info", "warn", "error"})
	}
	return nil
}

// Redacted returns a copy safe to print/log; password becomes "***"
// if it is non-empty.
func (c *Config) Redacted() *Config {
	if c == nil {
		return nil
	}
	cp := *c
	if cp.Database.Password != "" {
		cp.Database.Password = "***"
	}
	return &cp
}

func fieldErr(field, value, reason string) error {
	if value == "" {
		return fmt.Errorf("config: %s: %s", field, reason)
	}
	return fmt.Errorf("config: %s: invalid value %q, %s", field, value, reason)
}

func enumErr(field, value string, allowed []string) error {
	return fmt.Errorf("config: %s: invalid value %q, expected one of %v", field, value, allowed)
}
