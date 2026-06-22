// Package config manages the CLI's persistent configuration in
// ~/.auranode/config.yaml, with multi-profile support (multiple accounts/tenants).
package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultAPIURL = "https://api.auranode.app"

// Profile holds the credentials and settings of a specific account.
type Profile struct {
	APIURL    string `yaml:"api_url"`
	Token     string `yaml:"token,omitempty"`
	Refresh   string `yaml:"refresh_token,omitempty"`
	UserEmail string `yaml:"user_email,omitempty"`
	TenantID  string `yaml:"tenant_id,omitempty"`
}

// Config is the full file: multiple profiles + global settings.
type Config struct {
	DefaultProfile string             `yaml:"default_profile"`
	DefaultFormat  string             `yaml:"default_format"`
	Profiles       map[string]Profile `yaml:"profiles"`

	path string // path of the loaded file (not serialized)
}

// ErrNotAuthenticated indicates the active profile has no token.
var ErrNotAuthenticated = errors.New("not authenticated: run 'auranode auth login'")

// Dir returns the configuration directory (~/.auranode).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".auranode"), nil
}

// Load reads the configuration from disk; if it does not exist, returns an empty one with
// default values (not an error: first use has no config yet).
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		DefaultProfile: "default",
		DefaultFormat:  "table",
		Profiles:       map[string]Profile{},
		path:           path,
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	if cfg.DefaultFormat == "" {
		cfg.DefaultFormat = "table"
	}
	cfg.path = path
	return cfg, nil
}

// Save persists the configuration with 0600 permissions (contains tokens).
func (c *Config) Save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Profile returns the named profile (or the default if name == "").
// Applies the default APIURL if the profile does not define one.
func (c *Config) Profile(name string) Profile {
	if name == "" {
		name = c.DefaultProfile
	}
	p := c.Profiles[name]
	if p.APIURL == "" {
		p.APIURL = defaultAPIURL
	}
	return p
}

// SetProfile saves/updates a profile.
func (c *Config) SetProfile(name string, p Profile) {
	if name == "" {
		name = c.DefaultProfile
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}
