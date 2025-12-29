// Package inspector provides HTTP request inspection similar to ngrok's inspector
package inspector

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"
)

// Request represents a captured HTTP request
type Request struct {
	ID            string              `json:"id"`
	Timestamp     time.Time           `json:"timestamp"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Host          string              `json:"host"`
	Headers       map[string][]string `json:"headers"`
	Query         string              `json:"query"`
	ContentLength int64               `json:"content_length"`
	RemoteAddr    string              `json:"remote_addr"`
	Service       string              `json:"service"`
	Target        string              `json:"target"`
	StatusCode    int                 `json:"status_code"`
	Duration      time.Duration       `json:"duration"`
	DurationMs    float64             `json:"duration_ms"`
	Error         string              `json:"error,omitempty"`

	// Enhanced fields for detailed inspection
	RequestBody     string              `json:"request_body,omitempty"`
	ResponseBody    string              `json:"response_body,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	ContentType     string              `json:"content_type,omitempty"`
	Scheme          string              `json:"scheme,omitempty"`
}

// Inspector captures and displays HTTP requests
type Inspector struct {
	requests   []Request
	mu         sync.RWMutex
	maxSize    int
	logger     *log.Logger
	port       int
	server     *http.Server
	clients    map[chan Request]bool
	clientsMu  sync.RWMutex
	requestSeq int
}

// New creates a new inspector
func New(port int) *Inspector {
	return &Inspector{
		requests: make([]Request, 0, 100),
		maxSize:  100,
		port:     port,
		logger:   log.Default(),
		clients:  make(map[chan Request]bool),
	}
}

// SetLogger sets the logger
func (i *Inspector) SetLogger(logger *log.Logger) {
	i.logger = logger
}

// Capture records a request
func (i *Inspector) Capture(req Request) {
	i.mu.Lock()
	i.requestSeq++
	req.ID = fmt.Sprintf("req_%d", i.requestSeq)
	req.DurationMs = float64(req.Duration.Microseconds()) / 1000.0

	// Prepend to show newest first
	i.requests = append([]Request{req}, i.requests...)

	// Trim to max size
	if len(i.requests) > i.maxSize {
		i.requests = i.requests[:i.maxSize]
	}
	i.mu.Unlock()

	// Notify SSE clients
	i.clientsMu.RLock()
	for ch := range i.clients {
		select {
		case ch <- req:
		default:
			// Client too slow, skip
		}
	}
	i.clientsMu.RUnlock()
}

// Start starts the inspector web server
func (i *Inspector) Start() error {
	mux := http.NewServeMux()

	// Main UI
	mux.HandleFunc("/", i.handleUI)
	mux.HandleFunc("/inspect/http", i.handleUI)

	// API endpoints
	mux.HandleFunc("/api/requests", i.handleRequests)
	mux.HandleFunc("/api/requests/sse", i.handleSSE)
	mux.HandleFunc("/api/requests/clear", i.handleClear)
	mux.HandleFunc("/api/request/", i.handleRequestDetail)

	addr := fmt.Sprintf("127.0.0.1:%d", i.port)
	i.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	i.logger.Printf("[inspector] Web inspector available at http://%s", addr)

	go func() {
		if err := i.server.ListenAndServe(); err != http.ErrServerClosed {
			i.logger.Printf("[inspector] server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the inspector server
func (i *Inspector) Stop() {
	if i.server != nil {
		i.server.Close()
	}
}

// handleUI serves the web interface
func (i *Inspector) handleUI(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("inspector").Parse(inspectorHTML))
	if err := tmpl.Execute(w, map[string]interface{}{
		"Port": i.port,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// handleRequests returns captured requests as JSON
func (i *Inspector) handleRequests(w http.ResponseWriter, r *http.Request) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(i.requests)
}

// handleSSE provides server-sent events for live updates
func (i *Inspector) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create client channel
	ch := make(chan Request, 10)
	i.clientsMu.Lock()
	i.clients[ch] = true
	i.clientsMu.Unlock()

	defer func() {
		i.clientsMu.Lock()
		delete(i.clients, ch)
		i.clientsMu.Unlock()
		close(ch)
	}()

	// Send initial data
	i.mu.RLock()
	for _, req := range i.requests {
		data, _ := json.Marshal(req)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	i.mu.RUnlock()
	flusher.Flush()

	// Stream new requests
	for {
		select {
		case req := <-ch:
			data, _ := json.Marshal(req)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleClear clears all captured requests
func (i *Inspector) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	i.mu.Lock()
	i.requests = i.requests[:0]
	i.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"cleared"}`))
}

// handleRequestDetail returns a single request by ID
func (i *Inspector) handleRequestDetail(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/request/{id}
	id := r.URL.Path[len("/api/request/"):]
	if id == "" {
		http.Error(w, "Request ID required", http.StatusBadRequest)
		return
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	for _, req := range i.requests {
		if req.ID == id {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(req)
			return
		}
	}

	http.Error(w, "Request not found", http.StatusNotFound)
}

const inspectorHTML = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>hz Inspector</title>
    <!-- DaisyUI CSS (must load before Tailwind) -->
    <link href="https://cdn.jsdelivr.net/npm/daisyui@4.12.14/dist/full.min.css" rel="stylesheet" type="text/css" />
    <!-- Tailwind CSS CDN -->
    <script src="https://cdn.tailwindcss.com"></script>
    <!-- Google Fonts -->
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
    <script>
        tailwind.config = {
            theme: {
                extend: {
                    fontFamily: {
                        sans: ['Inter', 'sans-serif'],
                        mono: ['JetBrains Mono', 'monospace'],
                    }
                }
            }
        }
    </script>
    <style>
        /* Custom scrollbar */
        ::-webkit-scrollbar { width: 8px; height: 8px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: oklch(0.3 0 0); border-radius: 4px; }
        ::-webkit-scrollbar-thumb:hover { background: oklch(0.4 0 0); }

        /* Live pulse animation */
        @keyframes pulse-live {
            0%, 100% { opacity: 1; transform: scale(1); }
            50% { opacity: 0.6; transform: scale(0.95); }
        }
        .animate-pulse-live { animation: pulse-live 2s infinite; }

        /* Method badge colors */
        .method-GET { --tw-bg-opacity: 0.2; background-color: oklch(0.723 0.191 142.5 / 0.2); color: oklch(0.723 0.191 142.5); }
        .method-POST { --tw-bg-opacity: 0.2; background-color: oklch(0.623 0.214 259.815 / 0.2); color: oklch(0.623 0.214 259.815); }
        .method-PUT { --tw-bg-opacity: 0.2; background-color: oklch(0.768 0.165 75.834 / 0.2); color: oklch(0.768 0.165 75.834); }
        .method-DELETE { --tw-bg-opacity: 0.2; background-color: oklch(0.704 0.191 22.216 / 0.2); color: oklch(0.704 0.191 22.216); }
        .method-PATCH { --tw-bg-opacity: 0.2; background-color: oklch(0.702 0.183 293.541 / 0.2); color: oklch(0.702 0.183 293.541); }

        /* Status colors */
        .status-2xx { color: oklch(0.723 0.191 142.5); }
        .status-3xx { color: oklch(0.623 0.214 259.815); }
        .status-4xx { color: oklch(0.768 0.165 75.834); }
        .status-5xx { color: oklch(0.704 0.191 22.216); }

        /* Selected row */
        .row-selected { background-color: oklch(0.646 0.222 41.116 / 0.1) !important; }
    </style>
</head>
<body class="bg-base-100 text-base-content font-sans antialiased">
    <!-- Navbar -->
    <div class="navbar bg-base-200 border-b border-base-300 sticky top-0 z-50 backdrop-blur-md px-6">
        <div class="flex-1">
            <div class="flex items-center gap-2">
                <div class="w-8 h-8 rounded-lg bg-primary flex items-center justify-center text-primary-content font-bold text-sm">Hz</div>
                <span class="text-xl font-semibold text-base-content/70">Inspector</span>
            </div>
        </div>
        <div class="flex-none gap-3">
            <div class="flex items-center gap-2 text-success text-sm font-medium">
                <span class="w-2 h-2 rounded-full bg-success animate-pulse-live"></span>
                Live
            </div>
            <button class="btn btn-outline btn-error btn-sm gap-2" onclick="clearRequests()">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/></svg>
                Clear All
            </button>
        </div>
    </div>

    <!-- Main Layout -->
    <div class="flex h-[calc(100vh-64px)]">
        <!-- Requests Panel -->
        <div class="flex-1 overflow-auto border-r border-base-300" id="requests-panel-container">
            <div class="p-6">
                <!-- Stats -->
                <div class="stats stats-horizontal shadow-lg w-full mb-6 bg-base-200">
                    <div class="stat">
                        <div class="stat-title">Total Requests</div>
                        <div class="stat-value text-primary" id="total-count">0</div>
                    </div>
                    <div class="stat">
                        <div class="stat-title">Avg Duration</div>
                        <div class="stat-value text-secondary" id="avg-duration">0ms</div>
                    </div>
                    <div class="stat">
                        <div class="stat-title">Errors (4xx/5xx)</div>
                        <div class="stat-value text-error" id="error-count">0</div>
                    </div>
                </div>

                <!-- Requests Table -->
                <div class="card bg-base-200 shadow-lg">
                    <div class="overflow-x-auto">
                        <table class="table table-zebra">
                            <thead>
                                <tr>
                                    <th>Time</th>
                                    <th>Method</th>
                                    <th>Path</th>
                                    <th>Service</th>
                                    <th>Status</th>
                                    <th>Duration</th>
                                </tr>
                            </thead>
                            <tbody id="requests-body">
                                <tr>
                                    <td colspan="6" class="text-center py-16 text-base-content/50">
                                        <svg class="w-12 h-12 mx-auto mb-4 opacity-50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 6v6l4 2"/><circle cx="12" cy="12" r="10"/></svg>
                                        <div class="text-lg">Waiting for requests...</div>
                                        <div class="text-sm mt-2">Make HTTP requests through the proxy to see them here</div>
                                    </td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </div>

        <!-- Detail Panel -->
        <div class="w-[55%] h-full overflow-auto bg-base-100 hidden" id="detail-panel">
            <!-- Detail Header -->
            <div class="sticky top-0 z-10 bg-base-200 border-b border-base-300 px-6 py-4 flex items-center justify-between">
                <div class="flex items-center gap-3">
                    <span class="badge badge-lg font-semibold" id="detail-method">GET</span>
                    <span class="font-mono text-base-content/70" id="detail-path">/api/endpoint</span>
                </div>
                <div class="flex items-center gap-2">
                    <button class="btn btn-primary btn-sm gap-2" onclick="showCurlModal()">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
                        Copy as cURL
                    </button>
                    <button class="btn btn-ghost btn-sm btn-square" onclick="closeDetail()">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                </div>
            </div>

            <!-- Tabs -->
            <div role="tablist" class="tabs tabs-bordered bg-base-200 sticky top-[73px] z-10 px-6">
                <input type="radio" name="detail-tabs" role="tab" class="tab" aria-label="Overview" checked data-tab="overview" />
                <input type="radio" name="detail-tabs" role="tab" class="tab" aria-label="Headers" data-tab="headers" />
                <input type="radio" name="detail-tabs" role="tab" class="tab" aria-label="Parameters" data-tab="params" />
                <input type="radio" name="detail-tabs" role="tab" class="tab" aria-label="Request Body" data-tab="request" />
                <input type="radio" name="detail-tabs" role="tab" class="tab" aria-label="Response" data-tab="response" />
            </div>

            <!-- Overview Tab -->
            <div class="p-6 tab-panel" id="tab-overview">
                <div class="grid grid-cols-2 gap-4">
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">URL</div>
                        <div class="font-mono text-sm break-all" id="info-url">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Status</div>
                        <div class="font-mono text-sm" id="info-status">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Duration</div>
                        <div class="font-mono text-sm" id="info-duration">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Service</div>
                        <div class="font-mono text-sm" id="info-service">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Target</div>
                        <div class="font-mono text-sm break-all" id="info-target">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Remote Address</div>
                        <div class="font-mono text-sm" id="info-remote">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Content Type</div>
                        <div class="font-mono text-sm" id="info-content-type">-</div>
                    </div>
                    <div class="bg-base-200 p-4 rounded-lg border border-base-300">
                        <div class="text-xs text-base-content/50 uppercase tracking-wider font-semibold mb-1">Timestamp</div>
                        <div class="font-mono text-sm" id="info-timestamp">-</div>
                    </div>
                </div>
            </div>

            <!-- Headers Tab -->
            <div class="p-6 tab-panel hidden" id="tab-headers">
                <div class="mb-6">
                    <div class="flex items-center justify-between mb-3">
                        <span class="text-xs text-base-content/50 uppercase tracking-wider font-semibold">Request Headers</span>
                        <button class="btn btn-ghost btn-xs gap-1" onclick="copySection('request-headers')">
                            <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                            Copy
                        </button>
                    </div>
                    <div class="overflow-x-auto rounded-lg border border-base-300">
                        <table class="table table-sm" id="request-headers-table">
                            <thead class="bg-base-300">
                                <tr>
                                    <th class="text-xs uppercase tracking-wider">Name</th>
                                    <th class="text-xs uppercase tracking-wider">Value</th>
                                </tr>
                            </thead>
                            <tbody id="request-headers" class="font-mono text-sm"></tbody>
                        </table>
                    </div>
                </div>
                <div>
                    <div class="flex items-center justify-between mb-3">
                        <span class="text-xs text-base-content/50 uppercase tracking-wider font-semibold">Response Headers</span>
                        <button class="btn btn-ghost btn-xs gap-1" onclick="copySection('response-headers')">
                            <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                            Copy
                        </button>
                    </div>
                    <div class="overflow-x-auto rounded-lg border border-base-300">
                        <table class="table table-sm" id="response-headers-table">
                            <thead class="bg-base-300">
                                <tr>
                                    <th class="text-xs uppercase tracking-wider">Name</th>
                                    <th class="text-xs uppercase tracking-wider">Value</th>
                                </tr>
                            </thead>
                            <tbody id="response-headers" class="font-mono text-sm"></tbody>
                        </table>
                    </div>
                </div>
            </div>

            <!-- Parameters Tab -->
            <div class="p-6 tab-panel hidden" id="tab-params">
                <div class="flex items-center justify-between mb-3">
                    <span class="text-xs text-base-content/50 uppercase tracking-wider font-semibold">Query Parameters</span>
                    <button class="btn btn-ghost btn-xs gap-1" onclick="copySection('query-params')">
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                        Copy
                    </button>
                </div>
                <div class="overflow-x-auto rounded-lg border border-base-300">
                    <table class="table table-sm" id="query-params-table">
                        <thead class="bg-base-300">
                            <tr>
                                <th class="text-xs uppercase tracking-wider">Name</th>
                                <th class="text-xs uppercase tracking-wider">Value</th>
                            </tr>
                        </thead>
                        <tbody id="query-params" class="font-mono text-sm"></tbody>
                    </table>
                </div>
            </div>

            <!-- Request Body Tab -->
            <div class="p-6 tab-panel hidden" id="tab-request">
                <div class="flex items-center justify-between mb-3">
                    <span class="text-xs text-base-content/50 uppercase tracking-wider font-semibold">Request Body</span>
                    <button class="btn btn-ghost btn-xs gap-1" onclick="copySection('request-body')">
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                        Copy
                    </button>
                </div>
                <div class="mockup-code bg-base-300 max-h-96 overflow-auto">
                    <pre id="request-body" class="px-4 py-2 text-sm"><code class="text-base-content/50 italic">No request body</code></pre>
                </div>
            </div>

            <!-- Response Tab -->
            <div class="p-6 tab-panel hidden" id="tab-response">
                <div class="flex items-center justify-between mb-3">
                    <span class="text-xs text-base-content/50 uppercase tracking-wider font-semibold">Response Body</span>
                    <button class="btn btn-ghost btn-xs gap-1" onclick="copySection('response-body')">
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                        Copy
                    </button>
                </div>
                <div class="mockup-code bg-base-300 max-h-96 overflow-auto">
                    <pre id="response-body" class="px-4 py-2 text-sm"><code class="text-base-content/50 italic">No response body</code></pre>
                </div>
            </div>
        </div>
    </div>

    <!-- cURL Modal using DaisyUI dialog -->
    <dialog id="curl-modal" class="modal">
        <div class="modal-box max-w-2xl">
            <h3 class="text-lg font-bold mb-4">cURL Command</h3>
            <div class="mockup-code bg-base-300 max-h-80 overflow-auto">
                <pre id="curl-command" class="px-4 py-2 text-sm whitespace-pre-wrap break-all"></pre>
            </div>
            <div class="modal-action">
                <button class="btn btn-ghost" onclick="document.getElementById('curl-modal').close()">Close</button>
                <button class="btn btn-primary gap-2" onclick="copyCurl()">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor"><path d="M0 6.75C0 5.784.784 5 1.75 5h1.5a.75.75 0 0 1 0 1.5h-1.5a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-1.5a.75.75 0 0 1 1.5 0v1.5A1.75 1.75 0 0 1 9.25 16h-7.5A1.75 1.75 0 0 1 0 14.25Z"/><path d="M5 1.75C5 .784 5.784 0 6.75 0h7.5C15.216 0 16 .784 16 1.75v7.5A1.75 1.75 0 0 1 14.25 11h-7.5A1.75 1.75 0 0 1 5 9.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h7.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>
                    Copy to Clipboard
                </button>
            </div>
        </div>
        <form method="dialog" class="modal-backdrop"><button>close</button></form>
    </dialog>

    <!-- Toast container using DaisyUI -->
    <div class="toast toast-end" id="toast-container">
        <div class="alert alert-success hidden" id="toast-alert">
            <span id="toast-message">Copied!</span>
        </div>
    </div>

    <script>
        let requests = [];
        let selectedRequest = null;

        function formatTime(timestamp) {
            const d = new Date(timestamp);
            return d.toLocaleTimeString();
        }

        function formatFullTime(timestamp) {
            const d = new Date(timestamp);
            return d.toLocaleString();
        }

        function getStatusClass(code) {
            if (code >= 200 && code < 300) return 'badge-success';
            if (code >= 300 && code < 400) return 'badge-info';
            if (code >= 400 && code < 500) return 'badge-warning';
            return 'badge-error';
        }

        function parseQueryString(query) {
            if (!query) return {};
            const params = {};
            query.split('&').forEach(pair => {
                const [key, value] = pair.split('=').map(decodeURIComponent);
                if (key) params[key] = value || '';
            });
            return params;
        }

        function formatHeaders(headers) {
            if (!headers) return {};
            const result = {};
            for (const [key, values] of Object.entries(headers)) {
                result[key] = Array.isArray(values) ? values.join(', ') : values;
            }
            return result;
        }

        function formatJSON(str) {
            try {
                const obj = JSON.parse(str);
                return JSON.stringify(obj, null, 2);
            } catch {
                return str;
            }
        }

        function isJSON(str) {
            try {
                JSON.parse(str);
                return true;
            } catch {
                return false;
            }
        }

        function getMethodClass(method) {
            const classes = {
                'GET': 'badge-info',
                'POST': 'badge-success',
                'PUT': 'badge-warning',
                'PATCH': 'badge-warning',
                'DELETE': 'badge-error',
                'OPTIONS': 'badge-ghost',
                'HEAD': 'badge-ghost'
            };
            return classes[method] || 'badge-ghost';
        }

        function renderRequests() {
            const tbody = document.getElementById('requests-body');

            if (requests.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" class="text-center text-base-content/50 py-8">Waiting for requests...</td></tr>';
                return;
            }

            tbody.innerHTML = requests.map(req => ` + "`" + `
                <tr onclick="selectRequest('${req.id}')" class="hover cursor-pointer ${selectedRequest && selectedRequest.id === req.id ? 'bg-primary/10' : ''}">
                    <td class="font-mono text-sm opacity-70">${formatTime(req.timestamp)}</td>
                    <td><span class="badge badge-sm ${getMethodClass(req.method)}">${req.method}</span></td>
                    <td class="font-mono text-sm max-w-xs truncate" title="${req.path}${req.query ? '?' + req.query : ''}">${req.path}${req.query ? '?' + req.query : ''}</td>
                    <td><span class="badge badge-sm badge-outline">${req.service || 'unknown'}</span></td>
                    <td><span class="badge badge-sm ${getStatusClass(req.status_code)}">${req.status_code || '-'}</span></td>
                    <td class="font-mono text-sm">${req.duration_ms ? req.duration_ms.toFixed(1) + 'ms' : '-'}</td>
                </tr>
            ` + "`" + `).join('');

            // Update stats
            document.getElementById('total-count').textContent = requests.length;

            const durations = requests.filter(r => r.duration_ms).map(r => r.duration_ms);
            const avgDuration = durations.length > 0
                ? (durations.reduce((a, b) => a + b, 0) / durations.length).toFixed(1)
                : 0;
            document.getElementById('avg-duration').textContent = avgDuration + 'ms';

            const errors = requests.filter(r => r.status_code >= 400).length;
            document.getElementById('error-count').textContent = errors;
        }

        function selectRequest(id) {
            const req = requests.find(r => r.id === id);
            if (!req) return;

            selectedRequest = req;
            renderRequests();
            showDetail(req);
        }

        function showDetail(req) {
            const panel = document.getElementById('detail-panel');
            panel.classList.remove('hidden');

            // Header
            const methodEl = document.getElementById('detail-method');
            methodEl.textContent = req.method;
            methodEl.className = 'badge badge-lg ' + getMethodClass(req.method);
            document.getElementById('detail-path').textContent = req.path + (req.query ? '?' + req.query : '');

            // Overview
            const scheme = req.scheme || 'http';
            document.getElementById('info-url').textContent = scheme + '://' + req.host + req.path + (req.query ? '?' + req.query : '');
            document.getElementById('info-status').innerHTML = '<span class="badge ' + getStatusClass(req.status_code) + '">' + (req.status_code || '-') + '</span>';
            document.getElementById('info-duration').textContent = req.duration_ms ? req.duration_ms.toFixed(2) + 'ms' : '-';
            document.getElementById('info-service').textContent = req.service || '-';
            document.getElementById('info-target').textContent = req.target || '-';
            document.getElementById('info-remote').textContent = req.remote_addr || '-';
            document.getElementById('info-content-type').textContent = req.content_type || '-';
            document.getElementById('info-timestamp').textContent = formatFullTime(req.timestamp);

            // Request Headers
            const reqHeaders = formatHeaders(req.headers);
            document.getElementById('request-headers').innerHTML = Object.entries(reqHeaders)
                .map(([k, v]) => '<tr><td class="font-semibold text-primary">' + escapeHtml(k) + '</td><td class="font-mono text-sm">' + escapeHtml(v) + '</td></tr>')
                .join('') || '<tr><td colspan="2" class="text-center text-base-content/50">No headers</td></tr>';

            // Response Headers
            const resHeaders = formatHeaders(req.response_headers);
            document.getElementById('response-headers').innerHTML = Object.entries(resHeaders)
                .map(([k, v]) => '<tr><td class="font-semibold text-primary">' + escapeHtml(k) + '</td><td class="font-mono text-sm">' + escapeHtml(v) + '</td></tr>')
                .join('') || '<tr><td colspan="2" class="text-center text-base-content/50">No headers</td></tr>';

            // Query Parameters
            const params = parseQueryString(req.query);
            document.getElementById('query-params').innerHTML = Object.entries(params)
                .map(([k, v]) => '<tr><td class="font-semibold text-primary">' + escapeHtml(k) + '</td><td class="font-mono text-sm">' + escapeHtml(v) + '</td></tr>')
                .join('') || '<tr><td colspan="2" class="text-center text-base-content/50">No query parameters</td></tr>';

            // Request Body
            const reqBody = req.request_body || '';
            if (reqBody) {
                const formatted = isJSON(reqBody) ? formatJSON(reqBody) : reqBody;
                document.getElementById('request-body').textContent = formatted;
            } else {
                document.getElementById('request-body').textContent = 'No request body';
            }

            // Response Body
            const resBody = req.response_body || '';
            if (resBody) {
                const formatted = isJSON(resBody) ? formatJSON(resBody) : resBody;
                document.getElementById('response-body').textContent = formatted;
            } else {
                document.getElementById('response-body').textContent = 'No response body';
            }

            // Reset to first tab
            switchTab('overview');
        }

        function closeDetail() {
            document.getElementById('detail-panel').classList.add('hidden');
            selectedRequest = null;
            renderRequests();
        }

        function switchTab(tabName) {
            // Hide all tab contents
            document.querySelectorAll('.tab-panel').forEach(c => c.classList.add('hidden'));
            // Show selected tab content
            const tabContent = document.getElementById('tab-' + tabName);
            if (tabContent) tabContent.classList.remove('hidden');
            // Update radio button
            const radio = document.querySelector('input[name="detail-tabs"][data-tab="' + tabName + '"]');
            if (radio) radio.checked = true;
        }

        // Tab click handlers for DaisyUI radio tabs
        document.querySelectorAll('input[name="detail-tabs"]').forEach(radio => {
            radio.addEventListener('change', () => switchTab(radio.dataset.tab));
        });

        function escapeHtml(str) {
            if (!str) return '';
            const div = document.createElement('div');
            div.textContent = str;
            return div.innerHTML;
        }

        function showToast(message) {
            const toast = document.getElementById('toast-alert');
            const toastMsg = document.getElementById('toast-message');
            toastMsg.textContent = message;
            toast.classList.remove('hidden');
            setTimeout(() => toast.classList.add('hidden'), 2000);
        }

        function copySection(section) {
            if (!selectedRequest) return;

            let text = '';
            const req = selectedRequest;

            switch(section) {
                case 'request-headers':
                    const reqH = formatHeaders(req.headers);
                    text = Object.entries(reqH).map(([k, v]) => k + ': ' + v).join('\n');
                    break;
                case 'response-headers':
                    const resH = formatHeaders(req.response_headers);
                    text = Object.entries(resH).map(([k, v]) => k + ': ' + v).join('\n');
                    break;
                case 'query-params':
                    const params = parseQueryString(req.query);
                    text = Object.entries(params).map(([k, v]) => k + '=' + v).join('\n');
                    break;
                case 'request-body':
                    text = req.request_body || '';
                    if (isJSON(text)) text = formatJSON(text);
                    break;
                case 'response-body':
                    text = req.response_body || '';
                    if (isJSON(text)) text = formatJSON(text);
                    break;
            }

            navigator.clipboard.writeText(text).then(() => {
                showToast('Copied to clipboard!');
            });
        }

        function generateCurl() {
            if (!selectedRequest) return '';

            const req = selectedRequest;
            const scheme = req.scheme || 'http';
            const url = scheme + '://' + req.host + req.path + (req.query ? '?' + req.query : '');

            let curl = 'curl';

            // Method
            if (req.method !== 'GET') {
                curl += ' -X ' + req.method;
            }

            // URL
            curl += " '" + url + "'";

            // Headers
            const headers = formatHeaders(req.headers);
            for (const [key, value] of Object.entries(headers)) {
                // Skip some headers that curl handles automatically
                if (['Host', 'Content-Length', 'Accept-Encoding'].includes(key)) continue;
                curl += " \\\n  -H '" + key + ": " + value.replace(/'/g, "'\\''") + "'";
            }

            // Body
            if (req.request_body) {
                const body = req.request_body.replace(/'/g, "'\\''");
                curl += " \\\n  -d '" + body + "'";
            }

            return curl;
        }

        function showCurlModal() {
            const curl = generateCurl();
            document.getElementById('curl-command').textContent = curl;
            document.getElementById('curl-modal').showModal();
        }

        function closeCurlModal() {
            document.getElementById('curl-modal').close();
        }

        function copyCurl() {
            const curl = generateCurl();
            navigator.clipboard.writeText(curl).then(() => {
                showToast('cURL command copied!');
                closeCurlModal();
            });
        }

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                if (document.getElementById('curl-modal').open) {
                    closeCurlModal();
                } else if (selectedRequest) {
                    closeDetail();
                }
            }
        });

        function clearRequests() {
            fetch('/api/requests/clear', { method: 'POST' })
                .then(() => {
                    requests = [];
                    selectedRequest = null;
                    document.getElementById('detail-panel').classList.add('hidden');
                    renderRequests();
                });
        }

        // SSE connection
        const evtSource = new EventSource('/api/requests/sse');
        evtSource.onmessage = (event) => {
            const req = JSON.parse(event.data);
            const exists = requests.some(r => r.id === req.id);
            if (!exists) {
                requests.unshift(req);
                if (requests.length > 100) requests.pop();
                renderRequests();
            }
        };

        evtSource.onerror = () => {
            console.log('SSE connection error, will retry...');
        };

        // Initial load
        fetch('/api/requests')
            .then(r => r.json())
            .then(data => {
                requests = data || [];
                renderRequests();
            });
    </script>
</body>
</html>`
