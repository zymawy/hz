# hz API Reference

This document provides detailed API documentation for hz internal packages.

## Table of Contents

- [Types Package](#types-package)
- [Config Package](#config-package)
- [Registry Package](#registry-package)
- [Router Package](#router-package)
- [Proxy Package](#proxy-package)
- [Tunnel Package](#tunnel-package)

---

## Types Package

`github.com/zymawy/hz/pkg/types`

Core data structures used across all hz packages.

### Config

Root configuration structure.

```go
type Config struct {
    Version  string         `yaml:"version"`
    Server   ServerConfig   `yaml:"server"`
    Tunnel   TunnelConfig   `yaml:"tunnel"`
    Services []*Service     `yaml:"services"`
    Logging  LoggingConfig  `yaml:"logging"`
}
```

### ServerConfig

HTTP server settings.

```go
type ServerConfig struct {
    Port         int           `yaml:"port"`          // Default: 3000
    Host         string        `yaml:"host"`          // Default: "0.0.0.0"
    ReadTimeout  time.Duration `yaml:"readTimeout"`   // Default: 30s
    WriteTimeout time.Duration `yaml:"writeTimeout"`  // Default: 30s
}
```

### Service

Backend service definition.

```go
type Service struct {
    Name      string            `yaml:"name"`
    Target    string            `yaml:"target"`
    TargetURL *url.URL          // Parsed URL (runtime)
    Default   bool              `yaml:"default,omitempty"`
    Health    *HealthConfig     `yaml:"health,omitempty"`
    Routes    []RouteConfig     `yaml:"routes,omitempty"`
    Rewrite   *RewriteConfig    `yaml:"rewrite,omitempty"`
    Headers   map[string]string `yaml:"headers,omitempty"`

    // Runtime state
    Status       HealthStatus
    LastCheck    time.Time
    RequestCount int64
    ErrorCount   int64
}
```

**Methods:**

| Method | Description |
|--------|-------------|
| `IncrementRequests()` | Atomically increment request counter |
| `IncrementErrors()` | Atomically increment error counter |
| `SetStatus(HealthStatus)` | Update health status with timestamp |
| `GetStatus() HealthStatus` | Get current health status |

### RouteConfig

Request matching configuration.

```go
type RouteConfig struct {
    Path      string `yaml:"path,omitempty"`      // URL path pattern
    Header    string `yaml:"header,omitempty"`    // Header match (key=value)
    Subdomain string `yaml:"subdomain,omitempty"` // Subdomain match
    Method    string `yaml:"method,omitempty"`    // HTTP method filter
    Priority  int    `yaml:"priority,omitempty"`  // Match priority (higher wins)
}
```

### HealthConfig

Health check configuration.

```go
type HealthConfig struct {
    Path     string        `yaml:"path"`     // Health endpoint path
    Interval time.Duration `yaml:"interval"` // Check interval (default: 30s)
    Timeout  time.Duration `yaml:"timeout"`  // Request timeout (default: 5s)
}
```

### TunnelConfig

ngrok tunnel settings.

```go
type TunnelConfig struct {
    Enabled   bool   `yaml:"enabled"`
    Provider  string `yaml:"provider"`   // "ngrok" (default)
    AuthToken string `yaml:"authtoken"`
    Domain    string `yaml:"domain"`     // Custom domain (optional)
    Region    string `yaml:"region"`     // Default: "us"
}
```

### HealthStatus

Service health state enumeration.

```go
type HealthStatus string

const (
    HealthStatusHealthy   HealthStatus = "healthy"
    HealthStatusUnhealthy HealthStatus = "unhealthy"
    HealthStatusUnknown   HealthStatus = "unknown"
)
```

---

## Config Package

`github.com/zymawy/hz/internal/config`

Configuration loading, validation, and hot-reload.

### Manager

Configuration manager with hot-reload support.

```go
type Manager struct {
    // private fields
}
```

**Constructor:**

```go
func NewManager(path string) (*Manager, error)
```

Creates a new configuration manager and loads the initial config.

**Methods:**

| Method | Description |
|--------|-------------|
| `Load() error` | Reload configuration from file |
| `Get() *types.Config` | Get current configuration |
| `GetService(name string) *types.Service` | Get service by name |
| `GetDefaultService() *types.Service` | Get default service |
| `OnReload(fn func(*types.Config))` | Register reload callback |
| `Watch() error` | Start watching for file changes |
| `Stop()` | Stop configuration watcher |

**Example:**

```go
mgr, err := config.NewManager("hz.yaml")
if err != nil {
    log.Fatal(err)
}

// Register reload callback
mgr.OnReload(func(cfg *types.Config) {
    fmt.Println("Config reloaded!")
})

// Start watching
mgr.Watch()

// Get current config
cfg := mgr.Get()
```

### Helper Functions

```go
// FindConfigFile searches for hz.yaml in common locations
func FindConfigFile() (string, error)

// CreateDefaultConfig creates a default configuration file
func CreateDefaultConfig(path string) error
```

---

## Registry Package

`github.com/zymawy/hz/internal/registry`

Service registry with health checking.

### Registry

Service registry with health monitoring.

```go
type Registry struct {
    // private fields
}
```

**Constructor:**

```go
func New() *Registry
```

**Methods:**

| Method | Description |
|--------|-------------|
| `Register(svc *types.Service) error` | Register a service |
| `RegisterAll(services []*types.Service) error` | Register multiple services |
| `Deregister(name string)` | Remove a service |
| `Get(name string) *types.Service` | Get service by name |
| `GetDefault() *types.Service` | Get default service |
| `List() []*types.Service` | List all services |
| `Watch() <-chan types.RegistryEvent` | Subscribe to registry events |
| `Stop()` | Stop health checking |

**Events:**

```go
type RegistryEventType int

const (
    EventServiceAdded RegistryEventType = iota
    EventServiceRemoved
    EventServiceUpdated
    EventServiceHealthChanged
)
```

**Example:**

```go
reg := registry.New()

// Register services
reg.Register(&types.Service{
    Name:   "backend",
    Target: "http://localhost:3001",
})

// Watch for events
events := reg.Watch()
go func() {
    for event := range events {
        fmt.Printf("Event: %v for %s\n", event.Type, event.Service.Name)
    }
}()

// Get service
svc := reg.Get("backend")
```

---

## Router Package

`github.com/zymawy/hz/internal/router`

Request routing and matching.

### Router

Route matching engine.

```go
type Router struct {
    // private fields
}
```

**Constructor:**

```go
func New() *Router
```

**Methods:**

| Method | Description |
|--------|-------------|
| `Build(services []*types.Service) error` | Build routes from services |
| `Match(r *http.Request) *types.Route` | Find matching route |
| `GetRoutes() []*types.Route` | List all routes |

**Example:**

```go
rtr := router.New()

// Build routes from config
err := rtr.Build(cfg.Services)

// Match incoming request
route := rtr.Match(req)
if route != nil {
    // Forward to route.Service
}
```

### Matching Priority

Routes are matched in priority order:

1. Explicit `Priority` field (higher wins)
2. Header routes (exact match)
3. Subdomain routes
4. Path routes (longest prefix wins)
5. Default service (fallback)

### Path Patterns

| Pattern | Matches |
|---------|---------|
| `/api/*` | `/api/`, `/api/users`, `/api/v1/items` |
| `/users` | `/users` (exact) |
| `/v1/*` | `/v1/anything` |

### Helper Functions

```go
// RewriteURL applies rewrite rules to a URL
func RewriteURL(u *url.URL, route *types.Route) *url.URL
```

---

## Proxy Package

`github.com/zymawy/hz/internal/proxy`

HTTP and WebSocket reverse proxy.

### Proxy

Reverse proxy handler.

```go
type Proxy struct {
    // private fields
}
```

**Constructor:**

```go
func New(registry *registry.Registry, router *router.Router) *Proxy
```

**Methods:**

| Method | Description |
|--------|-------------|
| `ServeHTTP(w, r)` | Handle HTTP requests (implements http.Handler) |
| `SetLogger(logger *log.Logger)` | Set logger |
| `GetStats() *types.ProxyStats` | Get proxy statistics |

**Example:**

```go
prx := proxy.New(reg, rtr)
prx.SetLogger(logger)

// Use as HTTP handler
server := &http.Server{
    Addr:    ":3000",
    Handler: prx,
}
```

### Request Flow

1. Router matches request to service
2. URL rewriting applied (if configured)
3. Headers added (X-Forwarded-*, custom)
4. Request forwarded to backend
5. Response streamed back to client

### WebSocket Support

WebSocket connections are automatically detected and proxied bidirectionally.

Detection: `Upgrade: websocket` header

---

## Tunnel Package

`github.com/zymawy/hz/internal/tunnel`

ngrok tunnel management.

### Manager

Tunnel lifecycle manager.

```go
type Manager struct {
    // private fields
}
```

**Constructor:**

```go
func New(config *types.TunnelConfig) *Manager
```

**Methods:**

| Method | Description |
|--------|-------------|
| `Start(handler http.Handler) error` | Start tunnel |
| `Stop()` | Stop tunnel |
| `Restart() error` | Restart tunnel |
| `GetPublicURL() string` | Get public tunnel URL |
| `GetStatus() *types.TunnelStatus` | Get tunnel status |
| `SetLogger(logger *log.Logger)` | Set logger |

**Example:**

```go
mgr := tunnel.New(&types.TunnelConfig{
    Enabled:   true,
    AuthToken: os.Getenv("NGROK_AUTHTOKEN"),
    Domain:    "myapp.ngrok.io",
})

err := mgr.Start(httpHandler)
if err != nil {
    log.Fatal(err)
}

fmt.Println("Public URL:", mgr.GetPublicURL())

// Later...
mgr.Stop()
```

---

## Usage Examples

### Basic Proxy Setup

```go
package main

import (
    "log"
    "net/http"
    "os"

    "github.com/zymawy/hz/internal/config"
    "github.com/zymawy/hz/internal/proxy"
    "github.com/zymawy/hz/internal/registry"
    "github.com/zymawy/hz/internal/router"
)

func main() {
    // Load config
    cfg, _ := config.NewManager("hz.yaml")

    // Create components
    reg := registry.New()
    reg.RegisterAll(cfg.Get().Services)

    rtr := router.New()
    rtr.Build(cfg.Get().Services)

    prx := proxy.New(reg, rtr)
    prx.SetLogger(log.New(os.Stdout, "[hz] ", log.LstdFlags))

    // Start server
    log.Fatal(http.ListenAndServe(":3000", prx))
}
```

### With Hot-Reload

```go
cfg.OnReload(func(newCfg *types.Config) {
    reg.RegisterAll(newCfg.Services)
    rtr.Build(newCfg.Services)
})
cfg.Watch()
```

### With Tunnel

```go
tunnel := tunnel.New(&cfg.Get().Tunnel)
tunnel.Start(prx)
defer tunnel.Stop()

fmt.Println("Public URL:", tunnel.GetPublicURL())
```
