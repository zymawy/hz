// Package proxy implements the HTTP and WebSocket reverse proxy
package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zymawy/hz/internal/inspector"
	"github.com/zymawy/hz/internal/registry"
	"github.com/zymawy/hz/internal/router"
	"github.com/zymawy/hz/pkg/types"
)

// responseCapture wraps ResponseWriter to capture status code
type responseCapture struct {
	http.ResponseWriter
	statusCode int
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if rc.statusCode == 0 {
		rc.statusCode = http.StatusOK
	}
	return rc.ResponseWriter.Write(b)
}

// ErrorHandler is called when proxy encounters an error
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// Proxy is the main reverse proxy handler
type Proxy struct {
	registry     *registry.Registry
	router       *router.Router
	reverseProxy *httputil.ReverseProxy
	wsUpgrader   websocket.Upgrader
	errorHandler ErrorHandler
	stats        *types.ProxyStats
	statsMu      sync.RWMutex
	logger       *log.Logger
	inspector    *inspector.Inspector
}

// New creates a new proxy instance
func New(reg *registry.Registry, rtr *router.Router) *Proxy {
	p := &Proxy{
		registry: reg,
		router:   rtr,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		stats:  &types.ProxyStats{},
		logger: log.Default(),
	}

	// Create reverse proxy with director
	p.reverseProxy = &httputil.ReverseProxy{
		Director:       p.director,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.handleProxyError,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	p.errorHandler = p.defaultErrorHandler

	return p
}

// ServeHTTP handles incoming HTTP requests
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	atomic.AddInt64(&p.stats.TotalRequests, 1)
	atomic.AddInt64(&p.stats.ActiveRequests, 1)
	defer atomic.AddInt64(&p.stats.ActiveRequests, -1)

	// Check if this is a WebSocket upgrade request
	if p.isWebSocketRequest(r) {
		p.HandleWebSocket(w, r)
		return
	}

	// Route the request
	route, err := p.router.Match(r)
	if err != nil {
		p.captureRequest(r, nil, 0, time.Since(start), err)
		p.errorHandler(w, r, err)
		return
	}

	if route == nil {
		p.captureRequest(r, nil, 0, time.Since(start), fmt.Errorf("no matching route found"))
		p.errorHandler(w, r, fmt.Errorf("no matching route found"))
		return
	}

	// Store route info in context for director
	r = r.WithContext(withRoute(r.Context(), route))

	// Update service stats
	route.Service.IncrementRequests()

	// Apply URL rewriting if configured
	router.RewriteURL(r, route.Service.Rewrite)

	// Wrap response writer to capture status code
	rc := &responseCapture{ResponseWriter: w}

	// Proxy the request
	p.reverseProxy.ServeHTTP(rc, r)

	// Capture the request for inspector
	p.captureRequest(r, route, rc.statusCode, time.Since(start), nil)
}

// captureRequest sends request info to the inspector if enabled
func (p *Proxy) captureRequest(r *http.Request, route *types.Route, statusCode int, duration time.Duration, err error) {
	if p.inspector == nil {
		return
	}

	req := inspector.Request{
		Timestamp:     time.Now(),
		Method:        r.Method,
		Path:          r.URL.Path,
		Host:          r.Host,
		Headers:       r.Header,
		Query:         r.URL.RawQuery,
		ContentLength: r.ContentLength,
		RemoteAddr:    r.RemoteAddr,
		StatusCode:    statusCode,
		Duration:      duration,
	}

	if route != nil {
		req.Service = route.Service.Name
		req.Target = route.Service.Target
	}

	if err != nil {
		req.Error = err.Error()
	}

	p.inspector.Capture(req)
}

// HandleWebSocket handles WebSocket upgrade requests
func (p *Proxy) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&p.stats.WebSocketConns, 1)
	defer atomic.AddInt64(&p.stats.WebSocketConns, -1)

	// Route the request
	route, err := p.router.Match(r)
	if err != nil {
		p.errorHandler(w, r, err)
		return
	}

	if route == nil {
		p.errorHandler(w, r, fmt.Errorf("no matching route for WebSocket"))
		return
	}

	// Build target WebSocket URL
	targetURL := *route.Service.TargetURL
	if targetURL.Scheme == "http" {
		targetURL.Scheme = "ws"
	} else if targetURL.Scheme == "https" {
		targetURL.Scheme = "wss"
	}
	targetURL.Path = r.URL.Path
	targetURL.RawQuery = r.URL.RawQuery

	// Connect to backend
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	backendConn, resp, err := dialer.Dial(targetURL.String(), nil)
	if err != nil {
		if resp != nil {
			p.logger.Printf("[ws] backend dial failed: %v (status: %d)", err, resp.StatusCode)
		} else {
			p.logger.Printf("[ws] backend dial failed: %v", err)
		}
		p.errorHandler(w, r, err)
		return
	}
	defer backendConn.Close()

	// Upgrade client connection
	clientConn, err := p.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Printf("[ws] client upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	// Bidirectional proxy
	errChan := make(chan error, 2)

	// Client -> Backend
	go func() {
		errChan <- p.copyWebSocket(backendConn, clientConn, "client->backend")
	}()

	// Backend -> Client
	go func() {
		errChan <- p.copyWebSocket(clientConn, backendConn, "backend->client")
	}()

	// Wait for either direction to close
	<-errChan
}

// copyWebSocket copies messages between WebSocket connections
func (p *Proxy) copyWebSocket(dst, src *websocket.Conn, direction string) error {
	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			return err
		}

		if err := dst.WriteMessage(msgType, msg); err != nil {
			return err
		}
	}
}

// isWebSocketRequest checks if request is a WebSocket upgrade
func (p *Proxy) isWebSocketRequest(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket" &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// director modifies requests before proxying
func (p *Proxy) director(req *http.Request) {
	route := routeFromContext(req.Context())
	if route == nil {
		return
	}

	target := route.Service.TargetURL

	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.Host = target.Host

	// Add forwarding headers
	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
			clientIP = prior + ", " + clientIP
		}
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	req.Header.Set("X-Forwarded-Host", req.Host)
	req.Header.Set("X-Forwarded-Proto", "http")

	// Add custom headers from service config
	for key, value := range route.Service.Headers {
		req.Header.Set(key, value)
	}
}

// modifyResponse allows modification of backend responses
func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Could add response headers, logging, etc.
	return nil
}

// handleProxyError handles errors from the reverse proxy
func (p *Proxy) handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	atomic.AddInt64(&p.stats.TotalErrors, 1)

	route := routeFromContext(r.Context())
	if route != nil {
		route.Service.IncrementErrors()
	}

	p.errorHandler(w, r, err)
}

// defaultErrorHandler is the default error handler
func (p *Proxy) defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	p.logger.Printf("[error] %s %s: %v", r.Method, r.URL.Path, err)

	if err == io.EOF {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
		return
	}

	http.Error(w, "Bad Gateway", http.StatusBadGateway)
}

// SetErrorHandler sets a custom error handler
func (p *Proxy) SetErrorHandler(fn ErrorHandler) {
	p.errorHandler = fn
}

// SetLogger sets the logger for the proxy
func (p *Proxy) SetLogger(logger *log.Logger) {
	p.logger = logger
}

// SetInspector sets the request inspector
func (p *Proxy) SetInspector(insp *inspector.Inspector) {
	p.inspector = insp
}

// Stats returns current proxy statistics
func (p *Proxy) Stats() types.ProxyStats {
	return types.ProxyStats{
		TotalRequests:  atomic.LoadInt64(&p.stats.TotalRequests),
		ActiveRequests: atomic.LoadInt64(&p.stats.ActiveRequests),
		TotalErrors:    atomic.LoadInt64(&p.stats.TotalErrors),
		WebSocketConns: atomic.LoadInt64(&p.stats.WebSocketConns),
	}
}
