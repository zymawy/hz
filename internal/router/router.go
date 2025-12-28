// Package router handles request routing to backend services
package router

import (
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/zymawy/hz/pkg/types"
)

// Router matches incoming requests to backend services
type Router struct {
	routes       []*types.Route
	defaultRoute *types.Route
	mu           sync.RWMutex
}

// New creates a new router
func New() *Router {
	return &Router{
		routes: make([]*types.Route, 0),
	}
}

// Build compiles routes from service configurations
func (r *Router) Build(services []*types.Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.routes = make([]*types.Route, 0)
	r.defaultRoute = nil

	for _, svc := range services {
		// Handle default service
		if svc.Default {
			r.defaultRoute = &types.Route{
				Pattern: "*",
				Service: svc,
				MatchFunc: func(req *http.Request) bool {
					return true
				},
			}
		}

		// Build routes from service configuration
		for _, cfg := range svc.Routes {
			route := r.buildRoute(svc, cfg)
			if route != nil {
				r.routes = append(r.routes, route)
			}
		}
	}

	// Sort routes by priority (higher first) and specificity
	sort.Slice(r.routes, func(i, j int) bool {
		if r.routes[i].Config.Priority != r.routes[j].Config.Priority {
			return r.routes[i].Config.Priority > r.routes[j].Config.Priority
		}
		// More specific paths first
		return len(r.routes[i].Pattern) > len(r.routes[j].Pattern)
	})

	return nil
}

// buildRoute creates a Route from configuration
func (r *Router) buildRoute(svc *types.Service, cfg types.RouteConfig) *types.Route {
	route := &types.Route{
		Service: svc,
		Config:  cfg,
	}

	// Build match function based on configuration
	matchers := make([]func(*http.Request) bool, 0)

	// Path matcher
	if cfg.Path != "" {
		route.Pattern = cfg.Path
		pathPattern := cfg.Path
		matchers = append(matchers, func(req *http.Request) bool {
			return matchPath(req.URL.Path, pathPattern)
		})
	}

	// Header matcher
	if cfg.Header != "" {
		parts := strings.SplitN(cfg.Header, ":", 2)
		if len(parts) == 2 {
			headerName := strings.TrimSpace(parts[0])
			headerValue := strings.TrimSpace(parts[1])
			matchers = append(matchers, func(req *http.Request) bool {
				return req.Header.Get(headerName) == headerValue
			})
		}
	}

	// Subdomain matcher
	if cfg.Subdomain != "" {
		subdomain := cfg.Subdomain
		matchers = append(matchers, func(req *http.Request) bool {
			return matchSubdomain(req.Host, subdomain)
		})
	}

	// Method matcher
	if cfg.Method != "" {
		method := strings.ToUpper(cfg.Method)
		matchers = append(matchers, func(req *http.Request) bool {
			return req.Method == method
		})
	}

	// Combine all matchers
	if len(matchers) == 0 {
		return nil
	}

	route.MatchFunc = func(req *http.Request) bool {
		for _, match := range matchers {
			if !match(req) {
				return false
			}
		}
		return true
	}

	return route
}

// Match finds the best matching route for a request
func (r *Router) Match(req *http.Request) (*types.Route, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try explicit routes first (in priority/specificity order)
	for _, route := range r.routes {
		if route.MatchFunc(req) {
			return route, nil
		}
	}

	// Fall back to default route
	if r.defaultRoute != nil {
		return r.defaultRoute, nil
	}

	return nil, nil
}

// AddRoute adds a single route
func (r *Router) AddRoute(route *types.Route) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.routes = append(r.routes, route)

	// Re-sort after adding
	sort.Slice(r.routes, func(i, j int) bool {
		if r.routes[i].Config.Priority != r.routes[j].Config.Priority {
			return r.routes[i].Config.Priority > r.routes[j].Config.Priority
		}
		return len(r.routes[i].Pattern) > len(r.routes[j].Pattern)
	})

	return nil
}

// RemoveRoute removes a route by pattern
func (r *Router) RemoveRoute(pattern string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, route := range r.routes {
		if route.Pattern == pattern {
			r.routes = append(r.routes[:i], r.routes[i+1:]...)
			return nil
		}
	}

	return nil
}

// Reload rebuilds routes from services
func (r *Router) Reload(services []*types.Service) error {
	return r.Build(services)
}

// Routes returns all configured routes
func (r *Router) Routes() []*types.Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]*types.Route, len(r.routes))
	copy(routes, r.routes)
	return routes
}

// matchPath matches URL path against a pattern
// Supports wildcards: /api/* matches /api/foo, /api/foo/bar
func matchPath(urlPath, pattern string) bool {
	// Exact match
	if urlPath == pattern {
		return true
	}

	// Normalize paths
	urlPath = path.Clean("/" + urlPath)
	pattern = path.Clean("/" + pattern)

	// Handle wildcard patterns
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return urlPath == prefix || strings.HasPrefix(urlPath, prefix+"/")
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(urlPath, prefix)
	}

	// Check if pattern is a prefix (for backward compatibility)
	return strings.HasPrefix(urlPath, pattern)
}

// matchSubdomain matches host against subdomain pattern
func matchSubdomain(host, subdomain string) bool {
	// Remove port from host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Check if subdomain is a prefix
	return strings.HasPrefix(host, subdomain+".")
}

// RewriteURL applies rewrite rules to a request URL
func RewriteURL(req *http.Request, rewrite *types.RewriteConfig) {
	if rewrite == nil {
		return
	}

	// Strip prefix
	if rewrite.StripPrefix != "" {
		req.URL.Path = strings.TrimPrefix(req.URL.Path, rewrite.StripPrefix)
		if !strings.HasPrefix(req.URL.Path, "/") {
			req.URL.Path = "/" + req.URL.Path
		}
	}

	// Add prefix
	if rewrite.Prefix != "" {
		if !strings.HasPrefix(req.URL.Path, rewrite.Prefix) {
			req.URL.Path = rewrite.Prefix + req.URL.Path
		}
	}

	// Replace path
	if rewrite.Replace != "" {
		req.URL.Path = rewrite.Replace
	}
}
