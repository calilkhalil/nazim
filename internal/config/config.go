// Package config handles XDG-compliant configuration and paths.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
	"nazim/internal/service"
)

const (
	// AppName is used for XDG directory names.
	AppName = "nazim"
)

// Config holds application configuration.
type Config struct {
	ConfigDir  string
	ConfigFile string
	services   map[string]*service.Service
}

// New creates a Config with XDG-compliant paths.
func New() (*Config, error) {
	configDir := xdgPath("XDG_CONFIG_HOME", getDefaultConfigDir())

	cfg := &Config{
		ConfigDir: filepath.Join(configDir, AppName),
		services:  make(map[string]*service.Service),
	}

	cfg.ConfigFile = filepath.Join(cfg.ConfigDir, "services.yaml")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}

	// Load existing services
	if err := cfg.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	return cfg, nil
}

// xdgPath returns the XDG base directory or falls back to default.
func xdgPath(envVar, fallback string) string {
	if dir := os.Getenv(envVar); dir != "" {
		return dir
	}
	return fallback
}

// getDefaultConfigDir returns the default config directory based on OS.
func getDefaultConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			// Fallback to user home
			home, _ := os.UserHomeDir()
			return home
		}
		return appData
	case "darwin", "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return "/"
		}
		return filepath.Join(home, ".config")
	default:
		home, _ := os.UserHomeDir()
		return home
	}
}

// Load loads services from the configuration file.
func (c *Config) Load() error {
	data, err := os.ReadFile(c.ConfigFile)
	if err != nil {
		return err
	}

	var services []*service.Service
	if err := yaml.Unmarshal(data, &services); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	c.services = make(map[string]*service.Service)
	for _, svc := range services {
		c.services[svc.Name] = svc
	}

	return nil
}

// Save saves services to the configuration file.
func (c *Config) Save() error {
	services := make([]*service.Service, 0, len(c.services))
	for _, svc := range c.services {
		services = append(services, svc)
	}

	data, err := yaml.Marshal(services)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(c.ConfigFile, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// AddService adds a service.
func (c *Config) AddService(svc *service.Service) error {
	if err := svc.Validate(); err != nil {
		return err
	}

	if _, exists := c.services[svc.Name]; exists {
		return fmt.Errorf("service %s already exists", svc.Name)
	}

	c.services[svc.Name] = svc
	return c.Save()
}

// RemoveService removes a service.
func (c *Config) RemoveService(name string) error {
	if _, exists := c.services[name]; !exists {
		return fmt.Errorf("service %s does not exist", name)
	}

	delete(c.services, name)
	return c.Save()
}

// UpdateService updates an existing service.
func (c *Config) UpdateService(svc *service.Service) error {
	if err := svc.Validate(); err != nil {
		return err
	}

	if _, exists := c.services[svc.Name]; !exists {
		return fmt.Errorf("service %s does not exist", svc.Name)
	}

	c.services[svc.Name] = svc
	return c.Save()
}

// GetService returns a service by name.
func (c *Config) GetService(name string) (*service.Service, error) {
	svc, exists := c.services[name]
	if !exists {
		return nil, fmt.Errorf("service %s does not exist", name)
	}
	return svc, nil
}

// ListServices returns all services.
func (c *Config) ListServices() []*service.Service {
	services := make([]*service.Service, 0, len(c.services))
	for _, svc := range c.services {
		services = append(services, svc)
	}
	return services
}

// GetConfigPath returns the configuration file path.
func (c *Config) GetConfigPath() string {
	return c.ConfigFile
}

// GetScriptsDir returns the directory where scripts are stored.
func (c *Config) GetScriptsDir() string {
	return filepath.Join(c.ConfigDir, "scripts")
}
