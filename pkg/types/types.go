// Package types defines core data structures for hz proxy
package types

import (
	"net/http"
	"net/url"
	"sync"
	"time"
)

// HealthStatus represents service health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Service represents a backend service that can receive proxied requests
type Service struct {
	Name      string            `yaml:"name" json:"name"`
	Target    string            `yaml:"target" json:"target"`
	TargetURL *url.URL          `yaml:"-" json:"-"`
	Default   bool              `yaml:"default,omitempty" json:"default,omitempty"`
	Health    *HealthConfig     `yaml:"health,omitempty" json:"health,omitempty"`
	Routes    []RouteConfig     `yaml:"routes,omitempty" json:"routes,omitempty"`
	Rewrite   *RewriteConfig    `yaml:"rewrite,omitempty" json:"rewrite,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Runtime state
	Status       HealthStatus `yaml:"-" json:"status"`
	LastCheck    time.Time    `yaml:"-" json:"lastCheck,omitempty"`
	RequestCount int64        `yaml:"-" json:"requestCount"`
	ErrorCount   int64        `yaml:"-" json:"errorCount"`
	mu           sync.RWMutex `yaml:"-" json:"-"`
}

// HealthConfig defines health check parameters for a service
type HealthConfig struct {
	Path     string        `yaml:"path" json:"path"`
	Interval time.Duration `yaml:"interval" json:"interval"`
	Timeout  time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// RouteConfig defines how requests are matched to a service
type RouteConfig struct {
	Path      string `yaml:"path,omitempty" json:"path,omitempty"`
	Header    string `yaml:"header,omitempty" json:"header,omitempty"`
	Subdomain string `yaml:"subdomain,omitempty" json:"subdomain,omitempty"`
	Method    string `yaml:"method,omitempty" json:"method,omitempty"`
	Priority  int    `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// RewriteConfig defines URL rewriting rules
type RewriteConfig struct {
	Prefix      string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	StripPrefix string `yaml:"stripPrefix,omitempty" json:"stripPrefix,omitempty"`
	Replace     string `yaml:"replace,omitempty" json:"replace,omitempty"`
}

// Route represents a compiled route ready for matching
type Route struct {
	Pattern   string
	Service   *Service
	Config    RouteConfig
	MatchFunc func(r *http.Request) bool
}

// TunnelConfig defines ngrok tunnel settings
type TunnelConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Provider  string `yaml:"provider" json:"provider"`
	AuthToken string `yaml:"authtoken" json:"authtoken"`
	Domain    string `yaml:"domain,omitempty" json:"domain,omitempty"`
	Region    string `yaml:"region,omitempty" json:"region,omitempty"`
}

// TunnelStatus represents current tunnel state
type TunnelStatus struct {
	Active    bool      `json:"active"`
	PublicURL string    `json:"publicUrl,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// ServerConfig defines the proxy server settings
type ServerConfig struct {
	Port         int           `yaml:"port" json:"port"`
	Host         string        `yaml:"host" json:"host"`
	ReadTimeout  time.Duration `yaml:"readTimeout,omitempty" json:"readTimeout,omitempty"`
	WriteTimeout time.Duration `yaml:"writeTimeout,omitempty" json:"writeTimeout,omitempty"`
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
	Output string `yaml:"output,omitempty" json:"output,omitempty"`
}

// Config is the root configuration structure
type Config struct {
	Version  string         `yaml:"version" json:"version"`
	Server   ServerConfig   `yaml:"server" json:"server"`
	Tunnel   TunnelConfig   `yaml:"tunnel" json:"tunnel"`
	Services []*Service     `yaml:"services" json:"services"`
	Logging  LoggingConfig  `yaml:"logging" json:"logging"`
}

// RegistryEvent represents a change in the service registry
type RegistryEvent struct {
	Type    RegistryEventType
	Service *Service
}

// RegistryEventType defines the type of registry event
type RegistryEventType int

const (
	EventServiceAdded RegistryEventType = iota
	EventServiceRemoved
	EventServiceUpdated
	EventServiceHealthChanged
)

// ProxyStats holds proxy performance metrics
type ProxyStats struct {
	TotalRequests   int64         `json:"totalRequests"`
	ActiveRequests  int64         `json:"activeRequests"`
	TotalErrors     int64         `json:"totalErrors"`
	AverageLatency  time.Duration `json:"averageLatency"`
	BytesIn         int64         `json:"bytesIn"`
	BytesOut        int64         `json:"bytesOut"`
	WebSocketConns  int64         `json:"websocketConns"`
}

// IncrementRequests atomically increments request count
func (s *Service) IncrementRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RequestCount++
}

// IncrementErrors atomically increments error count
func (s *Service) IncrementErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCount++
}

// SetStatus updates service health status
func (s *Service) SetStatus(status HealthStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.LastCheck = time.Now()
}

// GetStatus returns current health status
func (s *Service) GetStatus() HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}
