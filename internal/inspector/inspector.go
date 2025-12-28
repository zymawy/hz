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
	tmpl.Execute(w, map[string]interface{}{
		"Port": i.port,
	})
}

// handleRequests returns captured requests as JSON
func (i *Inspector) handleRequests(w http.ResponseWriter, r *http.Request) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i.requests)
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
	w.Write([]byte(`{"status":"cleared"}`))
}

const inspectorHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>hz Inspector</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: #0d1117;
            color: #c9d1d9;
            line-height: 1.5;
        }
        .header {
            background: #161b22;
            border-bottom: 1px solid #30363d;
            padding: 16px 24px;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }
        .logo {
            font-size: 24px;
            font-weight: 700;
            color: #58a6ff;
        }
        .logo span { color: #8b949e; font-weight: 400; }
        .actions { display: flex; gap: 12px; }
        .btn {
            background: #21262d;
            border: 1px solid #30363d;
            color: #c9d1d9;
            padding: 8px 16px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 14px;
            transition: all 0.2s;
        }
        .btn:hover { background: #30363d; }
        .btn-danger { border-color: #f85149; color: #f85149; }
        .btn-danger:hover { background: #f8514922; }
        .container { padding: 24px; }
        .stats {
            display: flex;
            gap: 24px;
            margin-bottom: 24px;
        }
        .stat {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 8px;
            padding: 16px 24px;
        }
        .stat-value { font-size: 32px; font-weight: 700; color: #58a6ff; }
        .stat-label { color: #8b949e; font-size: 14px; }
        .requests-table {
            width: 100%;
            border-collapse: collapse;
            background: #161b22;
            border-radius: 8px;
            overflow: hidden;
            border: 1px solid #30363d;
        }
        .requests-table th {
            background: #21262d;
            text-align: left;
            padding: 12px 16px;
            font-weight: 600;
            color: #8b949e;
            font-size: 12px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }
        .requests-table td {
            padding: 12px 16px;
            border-top: 1px solid #21262d;
            font-size: 14px;
        }
        .requests-table tr:hover td { background: #1c2128; }
        .method {
            font-weight: 600;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 12px;
        }
        .method-GET { background: #238636; color: white; }
        .method-POST { background: #1f6feb; color: white; }
        .method-PUT { background: #9e6a03; color: white; }
        .method-DELETE { background: #da3633; color: white; }
        .method-PATCH { background: #8957e5; color: white; }
        .status { font-weight: 600; }
        .status-2xx { color: #3fb950; }
        .status-3xx { color: #58a6ff; }
        .status-4xx { color: #d29922; }
        .status-5xx { color: #f85149; }
        .service {
            background: #30363d;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 12px;
        }
        .duration { color: #8b949e; font-family: monospace; }
        .path { font-family: monospace; color: #c9d1d9; }
        .empty {
            text-align: center;
            padding: 48px;
            color: #8b949e;
        }
        .live-indicator {
            display: inline-flex;
            align-items: center;
            gap: 8px;
            color: #3fb950;
            font-size: 14px;
        }
        .live-dot {
            width: 8px;
            height: 8px;
            background: #3fb950;
            border-radius: 50%;
            animation: pulse 2s infinite;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        .time { color: #8b949e; font-size: 12px; font-family: monospace; }
    </style>
</head>
<body>
    <div class="header">
        <div class="logo">hz <span>inspector</span></div>
        <div class="actions">
            <div class="live-indicator">
                <div class="live-dot"></div>
                Live
            </div>
            <button class="btn btn-danger" onclick="clearRequests()">Clear</button>
        </div>
    </div>
    <div class="container">
        <div class="stats">
            <div class="stat">
                <div class="stat-value" id="total-count">0</div>
                <div class="stat-label">Total Requests</div>
            </div>
            <div class="stat">
                <div class="stat-value" id="avg-duration">0ms</div>
                <div class="stat-label">Avg Duration</div>
            </div>
            <div class="stat">
                <div class="stat-value" id="error-count">0</div>
                <div class="stat-label">Errors (4xx/5xx)</div>
            </div>
        </div>
        <table class="requests-table">
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
                <tr class="empty"><td colspan="6">Waiting for requests...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        let requests = [];

        function formatTime(timestamp) {
            const d = new Date(timestamp);
            return d.toLocaleTimeString();
        }

        function getStatusClass(code) {
            if (code >= 200 && code < 300) return 'status-2xx';
            if (code >= 300 && code < 400) return 'status-3xx';
            if (code >= 400 && code < 500) return 'status-4xx';
            return 'status-5xx';
        }

        function renderRequests() {
            const tbody = document.getElementById('requests-body');

            if (requests.length === 0) {
                tbody.innerHTML = '<tr class="empty"><td colspan="6">Waiting for requests...</td></tr>';
                return;
            }

            tbody.innerHTML = requests.map(req => ` + "`" + `
                <tr>
                    <td class="time">${formatTime(req.timestamp)}</td>
                    <td><span class="method method-${req.method}">${req.method}</span></td>
                    <td class="path">${req.path}${req.query ? '?' + req.query : ''}</td>
                    <td><span class="service">${req.service || 'unknown'}</span> → ${req.target || ''}</td>
                    <td><span class="status ${getStatusClass(req.status_code)}">${req.status_code || '-'}</span></td>
                    <td class="duration">${req.duration_ms ? req.duration_ms.toFixed(1) + 'ms' : '-'}</td>
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

        function clearRequests() {
            fetch('/api/requests/clear', { method: 'POST' })
                .then(() => {
                    requests = [];
                    renderRequests();
                });
        }

        // SSE connection
        const evtSource = new EventSource('/api/requests/sse');
        evtSource.onmessage = (event) => {
            const req = JSON.parse(event.data);
            // Check if request already exists (initial load vs new)
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
