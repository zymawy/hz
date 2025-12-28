# Contributing to hz

Thank you for your interest in contributing to hz! This document provides guidelines and instructions for contributing.

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help others learn and grow

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git
- ngrok account (for tunnel testing)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/zymawy/hz.git
cd hz

# Install dependencies
go mod download

# Build the project
go build -o hz .

# Run tests
go test ./...
```

### Project Structure

```
hz/
├── cmd/hz/          # CLI command implementations
├── internal/        # Private packages
│   ├── config/      # Configuration loading and hot-reload
│   ├── proxy/       # HTTP/WebSocket reverse proxy
│   ├── registry/    # Service registry and health checks
│   ├── router/      # Route matching engine
│   └── tunnel/      # ngrok tunnel management
├── pkg/types/       # Public shared types
└── main.go          # Entry point
```

## Development Workflow

### 1. Fork and Clone

```bash
# Fork the repository on GitHub, then:
git clone https://github.com/YOUR_USERNAME/hz.git
cd hz
git remote add upstream https://github.com/zymawy/hz.git
```

### 2. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/your-bug-fix
```

### 3. Make Changes

Follow these guidelines:

- **Code Style**: Follow standard Go conventions (`gofmt`, `golint`)
- **Testing**: Add tests for new functionality
- **Documentation**: Update docs for user-facing changes
- **Commits**: Write clear, descriptive commit messages

### 4. Test Your Changes

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Build and test manually
go build -o hz .
./hz init --force
./hz add test 8080
./hz status
```

### 5. Submit a Pull Request

```bash
git push origin feature/your-feature-name
```

Then open a pull request on GitHub with:

- Clear description of changes
- Related issue numbers (if applicable)
- Screenshots for UI changes

## Code Guidelines

### Package Organization

| Package | Purpose |
|---------|---------|
| `cmd/hz` | CLI commands (Cobra) |
| `internal/config` | YAML parsing, hot-reload |
| `internal/proxy` | Request forwarding |
| `internal/registry` | Service management |
| `internal/router` | Route matching |
| `internal/tunnel` | ngrok integration |
| `pkg/types` | Shared type definitions |

### Error Handling

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}

// Use meaningful error messages
return fmt.Errorf("service '%s' not found", name)
```

### Logging

```go
// Use the provided logger
logger.Printf("starting proxy on %s", addr)

// Include relevant context
logger.Printf("health check failed for %s: %v", svc.Name, err)
```

### Testing

```go
func TestRouter_Match(t *testing.T) {
    // Setup
    router := router.New()

    // Test
    route := router.Match(req)

    // Assert
    if route == nil {
        t.Error("expected route match")
    }
}
```

## Adding New Features

### New CLI Command

1. Create `cmd/hz/yourcommand.go`:

```go
package hz

import (
    "github.com/spf13/cobra"
)

var yourCmd = &cobra.Command{
    Use:   "yourcommand",
    Short: "Brief description",
    RunE:  runYourCommand,
}

func init() {
    rootCmd.AddCommand(yourCmd)
}

func runYourCommand(cmd *cobra.Command, args []string) error {
    // Implementation
    return nil
}
```

2. Add tests in `cmd/hz/yourcommand_test.go`
3. Update README.md with usage examples

### New Routing Type

1. Update `pkg/types/types.go` with new RouteConfig field
2. Update `internal/router/router.go` with matching logic
3. Update `cmd/hz/add.go` to parse new route type
4. Add tests and documentation

### New Configuration Option

1. Add field to appropriate struct in `pkg/types/types.go`
2. Add default value in `internal/config/config.go`
3. Add validation if needed
4. Update documentation

## Pull Request Checklist

- [ ] Code follows Go conventions
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] Commit messages are clear
- [ ] No breaking changes (or clearly documented)
- [ ] Works on Linux, macOS, and Windows

## Issue Reporting

When reporting issues, include:

- hz version (`hz --version`)
- Go version (`go version`)
- Operating system
- Steps to reproduce
- Expected vs actual behavior
- Relevant config (sanitized)

## Questions?

- Open a GitHub issue for bugs or features
- Start a discussion for questions or ideas

Thank you for contributing!
