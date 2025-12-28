// Package tunnel manages ngrok tunnel connections
package tunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/zymawy/hz/pkg/types"
	"golang.ngrok.com/ngrok"
	ngrokconfig "golang.ngrok.com/ngrok/config"
	"gopkg.in/yaml.v3"
)

// Manager handles ngrok tunnel lifecycle
type Manager struct {
	config    *types.TunnelConfig
	tunnel    ngrok.Tunnel
	listener  net.Listener
	status    types.TunnelStatus
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *log.Logger
	handler   http.Handler
}

// ngrokSystemConfig represents ngrok's native config structure
type ngrokSystemConfig struct {
	Version string `yaml:"version"`
	Agent   struct {
		AuthToken string `yaml:"authtoken"`
	} `yaml:"agent"`
	// v2 format
	AuthToken string `yaml:"authtoken"`
	Tunnels   map[string]struct {
		Domain string `yaml:"domain"`
	} `yaml:"tunnels"`
}

// getNgrokConfigPaths returns possible ngrok config file locations
func getNgrokConfigPaths() []string {
	home, _ := os.UserHomeDir()
	paths := []string{}

	switch runtime.GOOS {
	case "darwin":
		// macOS: ngrok v3 location
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "ngrok", "ngrok.yml"))
	case "linux":
		// Linux: XDG config
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			paths = append(paths, filepath.Join(xdg, "ngrok", "ngrok.yml"))
		}
		paths = append(paths, filepath.Join(home, ".config", "ngrok", "ngrok.yml"))
	case "windows":
		// Windows
		if appData := os.Getenv("APPDATA"); appData != "" {
			paths = append(paths, filepath.Join(appData, "ngrok", "ngrok.yml"))
		}
	}

	// Common fallback: ngrok v2 location
	paths = append(paths, filepath.Join(home, ".ngrok2", "ngrok.yml"))

	return paths
}

// LoadSystemNgrokConfig attempts to load ngrok config from system locations
func LoadSystemNgrokConfig() (authToken, domain string, err error) {
	paths := getNgrokConfigPaths()

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg ngrokSystemConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		// Get auth token (v3 uses agent.authtoken, v2 uses authtoken)
		if cfg.Agent.AuthToken != "" {
			authToken = cfg.Agent.AuthToken
		} else if cfg.AuthToken != "" {
			authToken = cfg.AuthToken
		}

		// Get domain from tunnels if available
		for _, tunnel := range cfg.Tunnels {
			if tunnel.Domain != "" {
				domain = tunnel.Domain
				break
			}
		}

		if authToken != "" {
			return authToken, domain, nil
		}
	}

	return "", "", fmt.Errorf("no ngrok config found in system locations")
}

// New creates a new tunnel manager
func New(config *types.TunnelConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config: config,
		ctx:    ctx,
		cancel: cancel,
		logger: log.Default(),
		status: types.TunnelStatus{
			Active: false,
		},
	}
}

// Start establishes the ngrok tunnel
func (m *Manager) Start(handler http.Handler) error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.handler = handler

	// Auto-detect ngrok credentials if not configured
	authToken := m.config.AuthToken
	domain := m.config.Domain

	if authToken == "" {
		m.logger.Println("[tunnel] No auth token in config, checking system ngrok config...")
		if sysToken, sysDomain, err := LoadSystemNgrokConfig(); err == nil {
			authToken = sysToken
			m.logger.Println("[tunnel] Found ngrok auth token in system config")
			// Use system domain if not set in hz config
			if domain == "" && sysDomain != "" {
				domain = sysDomain
				m.logger.Printf("[tunnel] Using system domain: %s", domain)
			}
		} else {
			return fmt.Errorf("no ngrok auth token configured and none found in system: %w\n\nRun 'ngrok config add-authtoken <token>' or 'hz tunnel --token <token>'", err)
		}
	}

	// Build ngrok options
	opts := []ngrokconfig.HTTPEndpointOption{}

	// Add custom domain if configured
	if domain != "" {
		opts = append(opts, ngrokconfig.WithDomain(domain))
	}

	// Create listener
	var err error
	m.listener, err = ngrok.Listen(m.ctx,
		ngrokconfig.HTTPEndpoint(opts...),
		ngrok.WithAuthtoken(authToken),
	)
	if err != nil {
		m.status.Error = err.Error()
		return fmt.Errorf("failed to create ngrok tunnel: %w", err)
	}

	// Store tunnel reference if available
	if tun, ok := m.listener.(ngrok.Tunnel); ok {
		m.tunnel = tun
	}

	// Update status
	m.status = types.TunnelStatus{
		Active:    true,
		PublicURL: m.listener.Addr().String(),
		StartedAt: time.Now(),
	}

	m.logger.Printf("[tunnel] ngrok tunnel established: %s", m.status.PublicURL)

	// Start serving in background
	go m.serve()

	return nil
}

// serve handles incoming connections
func (m *Manager) serve() {
	if m.handler == nil {
		m.logger.Println("[tunnel] no handler configured, tunnel inactive")
		return
	}

	server := &http.Server{
		Handler:      m.handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	if err := server.Serve(m.listener); err != nil && err != http.ErrServerClosed {
		m.logger.Printf("[tunnel] serve error: %v", err)
		m.mu.Lock()
		m.status.Error = err.Error()
		m.status.Active = false
		m.mu.Unlock()
	}
}

// Stop closes the ngrok tunnel
func (m *Manager) Stop() error {
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listener != nil {
		if err := m.listener.Close(); err != nil {
			return fmt.Errorf("failed to close tunnel: %w", err)
		}
	}

	m.status.Active = false
	m.logger.Println("[tunnel] ngrok tunnel closed")

	return nil
}

// GetPublicURL returns the public tunnel URL
func (m *Manager) GetPublicURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.PublicURL
}

// Status returns the current tunnel status
func (m *Manager) Status() types.TunnelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// IsActive returns whether the tunnel is active
func (m *Manager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.Active
}

// SetLogger sets the logger for the tunnel manager
func (m *Manager) SetLogger(logger *log.Logger) {
	m.logger = logger
}

// Restart recreates the tunnel
func (m *Manager) Restart(handler http.Handler) error {
	if err := m.Stop(); err != nil {
		return fmt.Errorf("failed to stop tunnel: %w", err)
	}

	// Create new context
	m.ctx, m.cancel = context.WithCancel(context.Background())

	return m.Start(handler)
}

// UpdateConfig updates tunnel configuration
func (m *Manager) UpdateConfig(config *types.TunnelConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config
}
