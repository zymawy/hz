// Package config handles configuration loading, parsing, and hot-reload
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zymawy/hz/pkg/types"
	"gopkg.in/yaml.v3"
)

// Manager handles configuration loading and hot-reload
type Manager struct {
	path      string
	config    *types.Config
	mu        sync.RWMutex
	watcher   *fsnotify.Watcher
	listeners []func(*types.Config)
	stopCh    chan struct{}
}

// NewManager creates a new configuration manager
func NewManager(path string) (*Manager, error) {
	m := &Manager{
		path:      path,
		listeners: make([]func(*types.Config), 0),
		stopCh:    make(chan struct{}),
	}

	// Load initial configuration
	if err := m.Load(); err != nil {
		return nil, err
	}

	return m, nil
}

// Load reads and parses the configuration file
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	config := &types.Config{}
	if err := yaml.Unmarshal([]byte(expanded), config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	m.applyDefaults(config)

	// Validate and parse URLs
	if err := m.validateAndParse(config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	m.config = config
	return nil
}

// applyDefaults sets default values for missing configuration
func (m *Manager) applyDefaults(c *types.Config) {
	if c.Version == "" {
		c.Version = "1"
	}

	// Server defaults
	if c.Server.Port == 0 {
		c.Server.Port = 3000
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 30 * time.Second
	}

	// Tunnel defaults
	if c.Tunnel.Provider == "" {
		c.Tunnel.Provider = "ngrok"
	}
	if c.Tunnel.Region == "" {
		c.Tunnel.Region = "us"
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}

	// Service defaults
	for _, svc := range c.Services {
		if svc.Health != nil {
			if svc.Health.Interval == 0 {
				svc.Health.Interval = 30 * time.Second
			}
			if svc.Health.Timeout == 0 {
				svc.Health.Timeout = 5 * time.Second
			}
		}
		svc.Status = types.HealthStatusUnknown
	}
}

// validateAndParse validates configuration and parses URLs
func (m *Manager) validateAndParse(c *types.Config) error {
	if len(c.Services) == 0 {
		return fmt.Errorf("at least one service must be defined")
	}

	hasDefault := false
	serviceNames := make(map[string]bool)

	for i, svc := range c.Services {
		// Check for duplicate names
		if serviceNames[svc.Name] {
			return fmt.Errorf("duplicate service name: %s", svc.Name)
		}
		serviceNames[svc.Name] = true

		// Validate name
		if svc.Name == "" {
			return fmt.Errorf("service at index %d has no name", i)
		}

		// Parse and validate target URL
		if svc.Target == "" {
			return fmt.Errorf("service %s has no target", svc.Name)
		}

		targetURL, err := url.Parse(svc.Target)
		if err != nil {
			return fmt.Errorf("invalid target URL for service %s: %w", svc.Name, err)
		}
		c.Services[i].TargetURL = targetURL

		// Track default service
		if svc.Default {
			if hasDefault {
				return fmt.Errorf("multiple default services defined")
			}
			hasDefault = true
		}
	}

	// If no explicit default, use first service
	if !hasDefault && len(c.Services) > 0 {
		c.Services[0].Default = true
	}

	return nil
}

// Get returns the current configuration
func (m *Manager) Get() *types.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GetService returns a service by name
func (m *Manager) GetService(name string) *types.Service {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, svc := range m.config.Services {
		if svc.Name == name {
			return svc
		}
	}
	return nil
}

// GetDefaultService returns the default service
func (m *Manager) GetDefaultService() *types.Service {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, svc := range m.config.Services {
		if svc.Default {
			return svc
		}
	}
	return nil
}

// OnReload registers a callback for configuration changes
func (m *Manager) OnReload(fn func(*types.Config)) {
	m.listeners = append(m.listeners, fn)
}

// Watch starts watching the config file for changes
func (m *Manager) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	m.watcher = watcher

	// Watch the directory containing the config file
	dir := filepath.Dir(m.path)
	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to watch directory: %w", err)
	}

	go m.watchLoop()
	return nil
}

// watchLoop handles file system events
func (m *Manager) watchLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}

			// Only react to writes on our config file
			if event.Op&fsnotify.Write == fsnotify.Write {
				if filepath.Base(event.Name) == filepath.Base(m.path) {
					// Small delay to ensure file write is complete
					time.Sleep(100 * time.Millisecond)

					if err := m.Load(); err != nil {
						fmt.Printf("[hz] config reload failed: %v\n", err)
						continue
					}

					fmt.Println("[hz] configuration reloaded")

					// Notify listeners
					config := m.Get()
					for _, fn := range m.listeners {
						fn(config)
					}
				}
			}
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[hz] watcher error: %v\n", err)
		}
	}
}

// Stop stops the configuration watcher
func (m *Manager) Stop() {
	close(m.stopCh)
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// FindConfigFile searches for hz.yaml in common locations
func FindConfigFile() (string, error) {
	searchPaths := []string{
		"hz.yaml",
		"hz.yml",
		".hz.yaml",
		".hz.yml",
		filepath.Join(os.Getenv("HOME"), ".hz", "config.yaml"),
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			abs, _ := filepath.Abs(path)
			return abs, nil
		}
	}

	return "", fmt.Errorf("no config file found, searched: %s", strings.Join(searchPaths, ", "))
}

// CreateDefaultConfig creates a default configuration file
func CreateDefaultConfig(path string) error {
	defaultConfig := `# hz - Development Proxy Configuration
version: "1"

server:
  port: 3000
  host: "0.0.0.0"

tunnel:
  enabled: false
  provider: ngrok
  authtoken: "${NGROK_AUTHTOKEN}"

services:
  - name: backend
    target: "http://localhost:3001"
    default: true
    health:
      path: /health
      interval: 30s

logging:
  level: info
  format: text
`
	return os.WriteFile(path, []byte(defaultConfig), 0644)
}
