// Package config gestiona la configuración persistente del CLI en
// ~/.auranode/config.yaml, con soporte multi-perfil (varias cuentas/tenants).
package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultAPIURL = "https://api.auranode.app"

// Profile son las credenciales y ajustes de una cuenta concreta.
type Profile struct {
	APIURL    string `yaml:"api_url"`
	Token     string `yaml:"token,omitempty"`
	Refresh   string `yaml:"refresh_token,omitempty"`
	UserEmail string `yaml:"user_email,omitempty"`
	TenantID  string `yaml:"tenant_id,omitempty"`
}

// Config es el archivo completo: varios perfiles + ajustes globales.
type Config struct {
	DefaultProfile string             `yaml:"default_profile"`
	DefaultFormat  string             `yaml:"default_format"`
	Profiles       map[string]Profile `yaml:"profiles"`

	path string // ruta del archivo cargado (no se serializa)
}

// ErrNotAuthenticated indica que el perfil activo no tiene token.
var ErrNotAuthenticated = errors.New("no autenticado: ejecuta 'auranode auth login'")

// Dir devuelve el directorio de configuración (~/.auranode).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".auranode"), nil
}

// Load lee la configuración del disco; si no existe, devuelve una vacía con
// valores por defecto (no es error: el primer uso no tiene config aún).
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

// Save persiste la configuración con permisos 0600 (contiene tokens).
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

// Profile devuelve el perfil indicado (o el por defecto si name == "").
// Aplica el APIURL por defecto si el perfil no define uno.
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

// SetProfile guarda/actualiza un perfil.
func (c *Config) SetProfile(name string, p Profile) {
	if name == "" {
		name = c.DefaultProfile
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}
