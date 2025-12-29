<p align="center">
  <img src="hz.png" alt="Hz - Smart Development Proxy" width="600" />
</p>

<p align="center">
  <strong>A sophisticated development proxy that routes traffic from multiple local services through a single endpoint with integrated ngrok tunnel support.</strong>
</p>

<p align="center">
  <a href="https://github.com/zymawy/hz/releases"><img src="https://img.shields.io/github/v/release/zymawy/hz?style=flat-square" alt="Release"></a>
  <a href="https://github.com/zymawy/hz/blob/main/LICENSE"><img src="https://img.shields.io/github/license/zymawy/hz?style=flat-square" alt="License"></a>
  <a href="https://github.com/zymawy/hz/actions"><img src="https://img.shields.io/github/actions/workflow/status/zymawy/hz/ci.yml?style=flat-square" alt="Build Status"></a>
  <a href="https://goreportcard.com/report/github.com/zymawy/hz"><img src="https://goreportcard.com/badge/github.com/zymawy/hz?style=flat-square" alt="Go Report Card"></a>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#capabilities--examples">Examples</a> •
  <a href="#configuration-reference">Configuration</a>
</p>

---

## Features

- **Multi-Service Routing** - Route requests to different backends based on path, headers, or subdomains
- **Integrated Tunnel** - Built-in ngrok integration for external access with a single command
- **Hot-Reload Configuration** - Changes to `hz.yaml` apply automatically without restart
- **Health Checking** - Automatic service health monitoring with status tracking
- **WebSocket Support** - Full bidirectional WebSocket proxy support
- **CLI Management** - Simple commands to manage services and configuration

## Installation

### Homebrew (macOS & Linux)

```bash
brew install zymawy/hz/hz
```

Or with explicit tap:
```bash
brew tap zymawy/hz
brew install zymawy/hz/hz
```

### Using Go Install

```bash
go install github.com/zymawy/hz@latest
```

### From Source

```bash
git clone https://github.com/zymawy/hz.git
cd hz
go build -o hz .
```

### Download Binary

Download the latest release from the [releases page](https://github.com/zymawy/hz/releases).

## Quick Start

```bash
# Initialize configuration
hz init

# Add your services
hz add backend 3001 --default
hz add api 8080 --route '/api/*'
hz add websocket 9000 --route 'header:upgrade=websocket'

# Start the proxy
hz start

# Enable external access via ngrok
hz tunnel --enable
```

---

## Capabilities & Examples

### 1. Header-Based Service Routing

Route requests to different services based on custom HTTP headers. Perfect for microservices development where you need to route based on service identifiers.

**Use Case**: Route requests with `b-service: sabry` header to a specific backend

```yaml
# hz.yaml
services:
  - name: sabry-service
    target: "http://localhost:3008"
    routes:
      - header: "b-service=sabry"

  - name: ahmed-service
    target: "http://localhost:3009"
    routes:
      - header: "b-service=ahmed"

  - name: default-backend
    target: "http://localhost:3001"
    default: true
```

**CLI Setup**:
```bash
hz add sabry-service 3008 --route 'header:b-service=sabry'
hz add ahmed-service 3009 --route 'header:b-service=ahmed'
hz add default-backend 3001 --default
```

**Testing**:
```bash
# Routes to localhost:3008
curl -H "b-service: sabry" http://localhost:3000/users

# Routes to localhost:3009
curl -H "b-service: ahmed" http://localhost:3000/users

# Routes to localhost:3001 (default)
curl http://localhost:3000/users
```

---

### 2. API Gateway Pattern

Route different API versions or modules to separate backend services.

**Use Case**: Microservices with separate services for users, orders, and payments

```yaml
# hz.yaml
services:
  - name: users-api
    target: "http://localhost:3001"
    routes:
      - path: "/api/users/*"
      - path: "/api/auth/*"
    rewrite:
      stripPrefix: "/api"

  - name: orders-api
    target: "http://localhost:3002"
    routes:
      - path: "/api/orders/*"
    rewrite:
      stripPrefix: "/api"

  - name: payments-api
    target: "http://localhost:3003"
    routes:
      - path: "/api/payments/*"
    rewrite:
      stripPrefix: "/api"

  - name: frontend
    target: "http://localhost:3000"
    default: true
```

**CLI Setup**:
```bash
hz add users-api 3001 --route '/api/users/*'
hz add orders-api 3002 --route '/api/orders/*'
hz add payments-api 3003 --route '/api/payments/*'
hz add frontend 3000 --default
```

**Testing**:
```bash
# Routes to users-api (localhost:3001/users/123)
curl http://localhost:3000/api/users/123

# Routes to orders-api (localhost:3002/orders)
curl http://localhost:3000/api/orders

# Routes to payments-api (localhost:3003/payments/charge)
curl -X POST http://localhost:3000/api/payments/charge

# Routes to frontend (localhost:3000)
curl http://localhost:3000/
```

---

### 3. WebSocket Proxy

Proxy WebSocket connections for real-time applications like chat, notifications, or live updates.

**Use Case**: Real-time chat application with WebSocket backend

```yaml
# hz.yaml
services:
  - name: websocket-server
    target: "http://localhost:9000"
    routes:
      - header: "upgrade=websocket"
      - path: "/ws/*"

  - name: socket-io
    target: "http://localhost:9001"
    routes:
      - path: "/socket.io/*"

  - name: main-app
    target: "http://localhost:3000"
    default: true
```

**CLI Setup**:
```bash
hz add websocket-server 9000 --route 'header:upgrade=websocket'
hz add socket-io 9001 --route '/socket.io/*'
hz add main-app 3000 --default
```

**Testing**:
```bash
# WebSocket connection
wscat -c ws://localhost:3000/ws/chat

# Socket.io connection (handled automatically by socket.io client)
# In browser: io.connect('http://localhost:3000')
```

---

### 4. Multi-Tenant Subdomain Routing

Route requests based on subdomain for multi-tenant applications.

**Use Case**: SaaS application with tenant-specific backends

```yaml
# hz.yaml
services:
  - name: admin-panel
    target: "http://localhost:3001"
    routes:
      - subdomain: "admin"

  - name: api-service
    target: "http://localhost:3002"
    routes:
      - subdomain: "api"

  - name: docs-site
    target: "http://localhost:3003"
    routes:
      - subdomain: "docs"

  - name: main-app
    target: "http://localhost:3000"
    default: true
```

**CLI Setup**:
```bash
hz add admin-panel 3001 --route 'subdomain:admin'
hz add api-service 3002 --route 'subdomain:api'
hz add docs-site 3003 --route 'subdomain:docs'
hz add main-app 3000 --default
```

**Testing** (requires local DNS or /etc/hosts):
```bash
# Add to /etc/hosts:
# 127.0.0.1 admin.myapp.local api.myapp.local docs.myapp.local myapp.local

curl http://admin.myapp.local:3000/  # Routes to admin-panel
curl http://api.myapp.local:3000/    # Routes to api-service
curl http://docs.myapp.local:3000/   # Routes to docs-site
curl http://myapp.local:3000/        # Routes to main-app
```

---

### 5. Webhook Testing with ngrok

Expose local services for webhook testing from external services (Stripe, GitHub, etc.).

**Use Case**: Testing Stripe webhooks locally

```yaml
# hz.yaml
server:
  port: 3000

tunnel:
  enabled: true
  provider: ngrok
  authtoken: "${NGROK_AUTHTOKEN}"
  domain: "myapp.ngrok.io"  # Optional: custom domain

services:
  - name: webhook-handler
    target: "http://localhost:3001"
    routes:
      - path: "/webhooks/*"

  - name: main-app
    target: "http://localhost:3000"
    default: true
```

**CLI Setup**:
```bash
export NGROK_AUTHTOKEN=your_token_here
hz add webhook-handler 3001 --route '/webhooks/*'
hz add main-app 3000 --default
hz tunnel --enable
hz start
```

**Output**:
```
🚀 hz proxy starting...
   Local:  http://0.0.0.0:3000
   Public: https://myapp.ngrok.io

📦 Services:
   • webhook-handler → http://localhost:3001
   • main-app → http://localhost:3000 (default)
```

Now configure Stripe webhook URL: `https://myapp.ngrok.io/webhooks/stripe`

---

### 6. Feature Branch Testing

Route requests to different service versions based on custom headers for A/B testing or feature branch testing.

**Use Case**: Testing new API version alongside production

```yaml
# hz.yaml
services:
  - name: api-v2-beta
    target: "http://localhost:3002"
    routes:
      - header: "x-api-version=v2"
      - header: "x-feature-flag=new-checkout"

  - name: api-v1-stable
    target: "http://localhost:3001"
    default: true
```

**CLI Setup**:
```bash
hz add api-v2-beta 3002 --route 'header:x-api-version=v2'
hz add api-v1-stable 3001 --default
```

**Testing**:
```bash
# Test new version
curl -H "x-api-version: v2" http://localhost:3000/checkout

# Production version
curl http://localhost:3000/checkout
```

---

### 7. Combined Routing Rules

Use multiple routing conditions for complex scenarios.

**Use Case**: E-commerce platform with multiple services

```yaml
# hz.yaml
services:
  # Mobile API (detected by user-agent or custom header)
  - name: mobile-api
    target: "http://localhost:4001"
    routes:
      - header: "x-client=mobile"
      - path: "/mobile/*"

  # Admin Panel
  - name: admin
    target: "http://localhost:4002"
    routes:
      - subdomain: "admin"
      - path: "/admin/*"

  # Real-time notifications
  - name: notifications
    target: "http://localhost:4003"
    routes:
      - path: "/notifications/*"
      - header: "upgrade=websocket"

  # Search service
  - name: search
    target: "http://localhost:4004"
    routes:
      - path: "/api/search/*"
      - path: "/api/suggest/*"

  # Main web app
  - name: web-app
    target: "http://localhost:4000"
    default: true
    health:
      path: /health
      interval: 30s
```

---

### 8. Development Environment Switching

Quickly switch between different backend environments.

**Use Case**: Switch between local, staging, and production APIs

```yaml
# hz.yaml (local development)
services:
  - name: api
    target: "http://localhost:3001"
    default: true
```

```yaml
# hz.staging.yaml
services:
  - name: api
    target: "https://staging-api.example.com"
    default: true
```

```yaml
# hz.prod.yaml (read-only testing)
services:
  - name: api
    target: "https://api.example.com"
    default: true
```

**Usage**:
```bash
# Local development
hz start

# Against staging
hz start -c hz.staging.yaml

# Against production (careful!)
hz start -c hz.prod.yaml
```

---

### 9. Load Balancing Simulation

Test how your frontend handles multiple backend instances.

**Use Case**: Simulate multiple backend instances

```yaml
# hz.yaml
services:
  - name: backend-1
    target: "http://localhost:3001"
    routes:
      - header: "x-instance=1"

  - name: backend-2
    target: "http://localhost:3002"
    routes:
      - header: "x-instance=2"

  - name: backend-3
    target: "http://localhost:3003"
    routes:
      - header: "x-instance=3"

  - name: default-backend
    target: "http://localhost:3001"
    default: true
```

**Testing**:
```bash
# Test specific instance
curl -H "x-instance: 2" http://localhost:3000/api/test

# Default routing
curl http://localhost:3000/api/test
```

---

### 10. Health Monitoring

Monitor service health with automatic status tracking.

**Configuration**:
```yaml
# hz.yaml
services:
  - name: api
    target: "http://localhost:3001"
    default: true
    health:
      path: /health
      interval: 30s
      timeout: 5s

  - name: database-api
    target: "http://localhost:3002"
    routes:
      - path: "/db/*"
    health:
      path: /ping
      interval: 10s
      timeout: 2s
```

**Check Status**:
```bash
hz status

# Output:
# Service Status:
#   • api         → healthy (localhost:3001)
#   • database-api → unhealthy (localhost:3002)
#
# Last check: 2024-01-15 10:30:45

hz status --json  # JSON output for scripting
```

---

## Configuration Reference

Full configuration file (`hz.yaml`):

```yaml
version: "1"

server:
  port: 3000              # Proxy listen port
  host: "0.0.0.0"         # Bind address
  readTimeout: 30s        # Request read timeout
  writeTimeout: 30s       # Response write timeout

tunnel:
  enabled: false          # Enable ngrok tunnel
  provider: ngrok         # Tunnel provider
  authtoken: "${NGROK_AUTHTOKEN}"  # Auth token (env var)
  domain: "myapp.ngrok.io"         # Custom domain (optional)
  region: "us"            # ngrok region

services:
  - name: service-name    # Unique service identifier
    target: "http://localhost:3001"  # Backend URL
    default: false        # Is default service?
    routes:
      - path: "/api/*"           # Path pattern
      - header: "x-service=name" # Header match
      - subdomain: "api"         # Subdomain match
      - method: "POST"           # HTTP method filter
      - priority: 10             # Route priority (higher wins)
    rewrite:
      stripPrefix: "/api"  # Remove prefix before forwarding
    headers:
      X-Custom-Header: "value"  # Add custom headers
    health:
      path: /health        # Health check endpoint
      interval: 30s        # Check interval
      timeout: 5s          # Request timeout

logging:
  level: info             # Log level: debug, info, warn, error
  format: text            # Log format: text, json
```

---

## CLI Commands

### `hz init`

Create a new configuration file:

```bash
hz init              # Create hz.yaml
hz init --force      # Overwrite existing
```

### `hz start`

Start the proxy server:

```bash
hz start                    # Start with defaults
hz start -p 8080            # Custom port
hz start --no-tunnel        # Disable tunnel
hz start -c custom.yaml     # Custom config file
hz start -w                 # Watch for config changes (default)
```

### `hz add`

Add a service to configuration:

```bash
hz add <name> <port|url> [flags]

# Examples
hz add backend 3001 --default
hz add api http://localhost:8080 --route '/api/*'
hz add ws 9000 --route 'header:upgrade=websocket'
hz add admin 3002 --route 'subdomain:admin'
hz add mobile 3003 --route 'header:x-client=mobile'
```

### `hz remove`

Remove a service:

```bash
hz remove <name>
hz rm backend
```

### `hz status`

Show proxy status:

```bash
hz status           # Formatted output
hz status --json    # JSON output
```

### `hz tunnel`

Configure ngrok tunnel:

```bash
hz tunnel                            # Show current config
hz tunnel --enable                   # Enable tunnel
hz tunnel --disable                  # Disable tunnel
hz tunnel --domain myapp.ngrok.io    # Set custom domain
hz tunnel --token YOUR_TOKEN         # Set auth token
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     hz Proxy Server                          │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │  Router  │──│  Proxy   │──│ Registry │──│  Config  │    │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘    │
│       │              │              │              │         │
│       │              │              │              │         │
│  ┌────▼────┐   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐    │
│  │ Path    │   │ HTTP    │   │ Health  │   │ Hot     │    │
│  │ Header  │   │ WS      │   │ Checks  │   │ Reload  │    │
│  │ Subdmn  │   │ Forward │   │         │   │         │    │
│  └─────────┘   └─────────┘   └─────────┘   └─────────┘    │
├─────────────────────────────────────────────────────────────┤
│                    ngrok Tunnel (optional)                   │
└─────────────────────────────────────────────────────────────┘
```

## Project Structure

```
hz/
├── main.go                 # Entry point
├── cmd/hz/                 # CLI commands
│   ├── root.go            # Root command setup
│   ├── start.go           # Start server command
│   ├── add.go             # Add service command
│   ├── remove.go          # Remove service command
│   ├── status.go          # Status command
│   ├── tunnel.go          # Tunnel config command
│   └── init.go            # Init command
├── internal/
│   ├── config/            # Configuration management
│   ├── proxy/             # HTTP/WebSocket proxy
│   ├── registry/          # Service registry
│   ├── router/            # Route matching
│   └── tunnel/            # ngrok integration
└── pkg/types/             # Shared types
```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.
