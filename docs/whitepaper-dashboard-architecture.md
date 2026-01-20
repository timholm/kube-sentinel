# Kube Sentinel Real-Time Dashboard Architecture

## Technical Whitepaper v1.0

---

## Executive Summary

Kube Sentinel provides a real-time web dashboard for monitoring Kubernetes errors and remediation activities. The dashboard architecture combines a Go-based HTTP server using gorilla/mux routing, WebSocket connections for live updates, HTMX for reactive UI patterns, and Go's html/template system for server-side rendering. This document details the technical implementation of the dashboard infrastructure.

---

## 1. HTTP Server Architecture

### 1.1 Server Structure

The web server is implemented as a self-contained struct that encapsulates all dashboard functionality:

```go
type Server struct {
    addr        string
    basePath    string
    store       store.Store
    ruleEngine  *rules.Engine
    remEngine   *remediation.Engine
    logger      *slog.Logger
    templates   map[string]*template.Template
    router      *mux.Router
    httpServer  *http.Server

    // WebSocket clients
    mu      sync.RWMutex
    clients map[*websocket.Conn]bool
    upgrader websocket.Upgrader
}
```

**Key Components:**

| Field | Type | Purpose |
|-------|------|---------|
| `addr` | `string` | Listen address (e.g., `:8080`) |
| `basePath` | `string` | URL prefix for reverse proxy deployments |
| `store` | `store.Store` | Data persistence layer interface |
| `ruleEngine` | `*rules.Engine` | Error classification engine |
| `remEngine` | `*remediation.Engine` | Auto-remediation engine |
| `templates` | `map[string]*template.Template` | Pre-parsed HTML templates |
| `router` | `*mux.Router` | Gorilla mux router instance |
| `clients` | `map[*websocket.Conn]bool` | Active WebSocket connections |

### 1.2 Server Initialization

The `NewServer` constructor performs template parsing and route setup:

```go
func NewServer(addr string, basePath string, store store.Store,
               ruleEngine *rules.Engine, remEngine *remediation.Engine,
               logger *slog.Logger) (*Server, error) {
    s := &Server{
        addr:       addr,
        basePath:   basePath,
        store:      store,
        ruleEngine: ruleEngine,
        remEngine:  remEngine,
        logger:     logger,
        clients:    make(map[*websocket.Conn]bool),
        upgrader: websocket.Upgrader{
            ReadBufferSize:  1024,
            WriteBufferSize: 1024,
            CheckOrigin: func(r *http.Request) bool {
                return true // Allow all origins for simplicity
            },
        },
    }

    // Parse templates - each page template is parsed with base.html
    s.templates = make(map[string]*template.Template)
    pageTemplates := []string{
        "dashboard.html",
        "errors.html",
        "error_detail.html",
        "rules.html",
        "history.html",
        "settings.html",
    }
    for _, page := range pageTemplates {
        tmpl, err := template.New("").Funcs(s.templateFuncs()).ParseFS(
            templatesFS, "templates/base.html", "templates/"+page)
        if err != nil {
            return nil, fmt.Errorf("parsing template %s: %w", page, err)
        }
        s.templates[page] = tmpl
    }

    // Setup routes
    s.router = mux.NewRouter()
    s.setupRoutes()

    return s, nil
}
```

### 1.3 HTTP Server Configuration

The server uses production-ready timeouts to prevent resource exhaustion:

```go
func (s *Server) Start() error {
    s.httpServer = &http.Server{
        Addr:         s.addr,
        Handler:      s.router,
        ReadTimeout:  15 * time.Second,   // Max time to read request
        WriteTimeout: 15 * time.Second,   // Max time to write response
        IdleTimeout:  60 * time.Second,   // Max time for keep-alive
    }

    s.logger.Info("starting web server", "addr", s.addr)
    return s.httpServer.ListenAndServe()
}
```

**Timeout Configuration Rationale:**

| Timeout | Value | Purpose |
|---------|-------|---------|
| ReadTimeout | 15s | Prevents slow-loris attacks |
| WriteTimeout | 15s | Ensures responses complete in bounded time |
| IdleTimeout | 60s | Allows connection reuse while freeing resources |

### 1.4 Graceful Shutdown

The server implements graceful shutdown to properly close WebSocket connections:

```go
func (s *Server) Shutdown(ctx context.Context) error {
    s.logger.Info("shutting down web server")

    // Close all WebSocket connections
    s.mu.Lock()
    for client := range s.clients {
        client.Close()
    }
    s.mu.Unlock()

    return s.httpServer.Shutdown(ctx)
}
```

---

## 2. Gorilla/Mux Routing

### 2.1 Route Configuration

The router is configured with distinct route groups for pages, API endpoints, WebSocket, and health checks:

```go
func (s *Server) setupRoutes() {
    // Static files
    staticSub, _ := fs.Sub(staticFS, "static")
    s.router.PathPrefix("/static/").Handler(
        http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

    // Pages
    s.router.HandleFunc("/", s.handleDashboard).Methods("GET")
    s.router.HandleFunc("/errors", s.handleErrors).Methods("GET")
    s.router.HandleFunc("/errors/{id}", s.handleErrorDetail).Methods("GET")
    s.router.HandleFunc("/rules", s.handleRules).Methods("GET")
    s.router.HandleFunc("/history", s.handleHistory).Methods("GET")
    s.router.HandleFunc("/settings", s.handleSettings).Methods("GET")

    // API endpoints
    s.router.HandleFunc("/api/errors", s.handleAPIErrors).Methods("GET")
    s.router.HandleFunc("/api/errors/{id}", s.handleAPIErrorDetail).Methods("GET")
    s.router.HandleFunc("/api/rules", s.handleAPIRules).Methods("GET")
    s.router.HandleFunc("/api/rules/test", s.handleAPIRulesTest).Methods("POST")
    s.router.HandleFunc("/api/remediations", s.handleAPIRemediations).Methods("GET")
    s.router.HandleFunc("/api/stats", s.handleAPIStats).Methods("GET")
    s.router.HandleFunc("/api/settings", s.handleAPISettings).Methods("GET", "POST")

    // WebSocket for real-time updates
    s.router.HandleFunc("/ws", s.handleWebSocket)

    // Health endpoints
    s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
    s.router.HandleFunc("/ready", s.handleReady).Methods("GET")
}
```

### 2.2 Route Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Gorilla Mux Router                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                    Static File Handler                          │ │
│  │  /static/*  →  Embedded FS (go:embed)  →  CSS, JS, Images       │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                    Page Handlers (GET)                          │ │
│  │  /              →  handleDashboard    →  dashboard.html         │ │
│  │  /errors        →  handleErrors       →  errors.html            │ │
│  │  /errors/{id}   →  handleErrorDetail  →  error_detail.html      │ │
│  │  /rules         →  handleRules        →  rules.html             │ │
│  │  /history       →  handleHistory      →  history.html           │ │
│  │  /settings      →  handleSettings     →  settings.html          │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                    API Handlers (JSON)                          │ │
│  │  /api/errors         GET     →  List errors with pagination     │ │
│  │  /api/errors/{id}    GET     →  Get single error details        │ │
│  │  /api/rules          GET     →  List all rules                  │ │
│  │  /api/rules/test     POST    →  Test pattern matching           │ │
│  │  /api/remediations   GET     →  List remediation logs           │ │
│  │  /api/stats          GET     →  Get dashboard statistics        │ │
│  │  /api/settings       GET/POST→  Get/update settings             │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                    WebSocket & Health                           │ │
│  │  /ws      →  handleWebSocket  →  Real-time event streaming      │ │
│  │  /health  →  handleHealth     →  Liveness probe                 │ │
│  │  /ready   →  handleReady      →  Readiness probe                │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.3 URL Parameter Extraction

Gorilla mux provides convenient URL parameter extraction via `mux.Vars()`:

```go
func (s *Server) handleErrorDetail(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]

    errObj, err := s.store.GetError(id)
    if err != nil {
        http.NotFound(w, r)
        return
    }

    logs, _ := s.store.ListRemediationLogsForError(id)

    data := errorDetailData{
        Error:        errObj,
        Remediations: logs,
    }

    s.renderTemplate(w, "error_detail.html", data)
}
```

---

## 3. WebSocket Connection Management

### 3.1 Connection Lifecycle

WebSocket connections are managed using a thread-safe client registry:

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Upgrade HTTP connection to WebSocket
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        s.logger.Error("websocket upgrade failed", "error", err)
        return
    }

    // Register client
    s.mu.Lock()
    s.clients[conn] = true
    s.mu.Unlock()

    // Cleanup on disconnect
    defer func() {
        s.mu.Lock()
        delete(s.clients, conn)
        s.mu.Unlock()
        conn.Close()
    }()

    // Keep connection alive and handle incoming messages
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            if websocket.IsUnexpectedCloseError(err,
                websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                s.logger.Debug("websocket closed", "error", err)
            }
            break
        }
    }
}
```

### 3.2 WebSocket Connection Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                     WebSocket Connection Flow                         │
└──────────────────────────────────────────────────────────────────────┘

    Browser                          Server
       │                                │
       │  GET /ws (Upgrade: websocket)  │
       │ ──────────────────────────────▶│
       │                                │
       │                                │  upgrader.Upgrade()
       │                                │  s.clients[conn] = true
       │                                │
       │  101 Switching Protocols       │
       │ ◀──────────────────────────────│
       │                                │
       │  ══════════════════════════════│  Full-duplex connection
       │                                │
       │                                │  ┌─────────────────────┐
       │  {"type":"error",...}          │◀─│ BroadcastError()    │
       │ ◀──────────────────────────────│  └─────────────────────┘
       │                                │
       │                                │  ┌─────────────────────┐
       │  {"type":"remediation",...}    │◀─│ BroadcastRemediation│
       │ ◀──────────────────────────────│  └─────────────────────┘
       │                                │
       │                                │  ┌─────────────────────┐
       │  {"type":"stats",...}          │◀─│ BroadcastStats()    │
       │ ◀──────────────────────────────│  └─────────────────────┘
       │                                │
       │  Connection close              │
       │ ──────────────────────────────▶│  delete(s.clients, conn)
       │                                │
```

### 3.3 Client Tracking

The server maintains a registry of active WebSocket connections using a map protected by a read-write mutex:

```go
type Server struct {
    // ...
    mu      sync.RWMutex
    clients map[*websocket.Conn]bool
    // ...
}
```

**Concurrency Strategy:**

- `RLock()` for reading (broadcasting) - allows multiple concurrent broadcasts
- `Lock()` for writing (adding/removing clients) - exclusive access for modifications

---

## 4. Event Broadcasting Patterns

### 4.1 BroadcastError

Sends new error events to all connected clients:

```go
func (s *Server) BroadcastError(err *store.Error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    msg := map[string]interface{}{
        "type":  "error",
        "error": err,
    }

    for client := range s.clients {
        if err := client.WriteJSON(msg); err != nil {
            s.logger.Debug("failed to send to websocket client", "error", err)
        }
    }
}
```

**Message Format:**
```json
{
    "type": "error",
    "error": {
        "id": "err-abc123",
        "namespace": "production",
        "pod": "api-server-7d8f9",
        "message": "CrashLoopBackOff",
        "priority": "P1",
        "count": 5,
        "lastSeen": "2026-01-19T10:30:00Z"
    }
}
```

### 4.2 BroadcastRemediation

Sends remediation action results to all connected clients:

```go
func (s *Server) BroadcastRemediation(log *store.RemediationLog) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    msg := map[string]interface{}{
        "type":        "remediation",
        "remediation": log,
    }

    for client := range s.clients {
        if err := client.WriteJSON(msg); err != nil {
            s.logger.Debug("failed to send to websocket client", "error", err)
        }
    }
}
```

**Message Format:**
```json
{
    "type": "remediation",
    "remediation": {
        "id": "rem-xyz789",
        "errorId": "err-abc123",
        "action": "restart-pod",
        "target": "production/api-server-7d8f9",
        "status": "success",
        "message": "Pod restarted successfully",
        "timestamp": "2026-01-19T10:30:05Z",
        "dryRun": false
    }
}
```

### 4.3 BroadcastStats

Sends updated statistics to all connected clients:

```go
func (s *Server) BroadcastStats() {
    stats, err := s.store.GetStats()
    if err != nil {
        return
    }

    s.mu.RLock()
    defer s.mu.RUnlock()

    msg := map[string]interface{}{
        "type":  "stats",
        "stats": stats,
    }

    for client := range s.clients {
        if err := client.WriteJSON(msg); err != nil {
            s.logger.Debug("failed to send to websocket client", "error", err)
        }
    }
}
```

**Message Format:**
```json
{
    "type": "stats",
    "stats": {
        "totalErrors": 42,
        "errorsByPriority": {"P1": 5, "P2": 12, "P3": 15, "P4": 10},
        "errorsByNamespace": {"production": 20, "staging": 22},
        "remediationCount": 18,
        "successfulActions": 15,
        "failedActions": 3
    }
}
```

### 4.4 Broadcasting Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Event Broadcasting System                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌───────────────┐    ┌───────────────┐    ┌───────────────┐        │
│  │  Log Poller   │    │ Rule Engine   │    │ Rem. Engine   │        │
│  └───────┬───────┘    └───────┬───────┘    └───────┬───────┘        │
│          │                    │                    │                 │
│          ▼                    ▼                    ▼                 │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                     Web Server                                 │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │              Broadcast Methods                           │  │  │
│  │  │  BroadcastError()  BroadcastRemediation()  BroadcastStats│  │  │
│  │  └─────────────────────────────────────────────────────────┘  │  │
│  │                           │                                    │  │
│  │                           ▼                                    │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │                 Client Registry                          │  │  │
│  │  │  map[*websocket.Conn]bool  (protected by sync.RWMutex)   │  │  │
│  │  └─────────────────────────────────────────────────────────┘  │  │
│  │                           │                                    │  │
│  │           ┌───────────────┼───────────────┐                   │  │
│  │           ▼               ▼               ▼                   │  │
│  │      ┌────────┐      ┌────────┐      ┌────────┐               │  │
│  │      │Client 1│      │Client 2│      │Client N│               │  │
│  │      │(Browser│      │(Browser│      │(Browser│               │  │
│  │      │  Tab)  │      │  Tab)  │      │  Tab)  │               │  │
│  │      └────────┘      └────────┘      └────────┘               │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                       │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 5. HTMX Reactive Update Patterns

### 5.1 Client-Side WebSocket Integration

The base template establishes a WebSocket connection and bridges events to HTMX:

```html
<script>
    // Base path configuration
    const basePath = "{{basePath}}";

    // WebSocket connection for real-time updates
    let ws;
    let reconnectTimer;

    function connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${protocol}//${window.location.host}${basePath}/ws`);

        ws.onopen = function() {
            document.getElementById('connection-status').innerHTML = `
                <span class="w-2 h-2 bg-green-500 rounded-full mr-2"></span>
                <span class="text-sm text-green-400">Connected</span>
            `;
        };

        ws.onclose = function() {
            document.getElementById('connection-status').innerHTML = `
                <span class="w-2 h-2 bg-red-500 rounded-full mr-2"></span>
                <span class="text-sm text-red-400">Disconnected</span>
            `;
            reconnectTimer = setTimeout(connectWebSocket, 3000);
        };

        ws.onmessage = function(event) {
            const data = JSON.parse(event.data);
            if (data.type === 'error') {
                // Trigger HTMX refresh of error lists
                htmx.trigger(document.body, 'newError');
            } else if (data.type === 'remediation') {
                htmx.trigger(document.body, 'newRemediation');
            } else if (data.type === 'stats') {
                htmx.trigger(document.body, 'statsUpdate');
            }
        };
    }

    connectWebSocket();
</script>
```

### 5.2 HTMX Event Bridge Pattern

```
┌─────────────────────────────────────────────────────────────────────┐
│                    HTMX Event Bridge Pattern                         │
└─────────────────────────────────────────────────────────────────────┘

  Server                    WebSocket                   Browser
     │                          │                          │
     │  BroadcastError()        │                          │
     │─────────────────────────▶│                          │
     │                          │  {"type":"error",...}    │
     │                          │─────────────────────────▶│
     │                          │                          │
     │                          │         ┌────────────────┴────────────┐
     │                          │         │  ws.onmessage              │
     │                          │         │                             │
     │                          │         │  if (data.type === 'error') │
     │                          │         │    htmx.trigger(            │
     │                          │         │      document.body,         │
     │                          │         │      'newError'             │
     │                          │         │    );                       │
     │                          │         └────────────────┬────────────┘
     │                          │                          │
     │                          │                          │  HTMX element with
     │                          │                          │  hx-trigger="newError"
     │                          │                          │  makes GET request
     │                          │                          │
     │  GET /api/errors         │                          │
     │◀────────────────────────────────────────────────────│
     │                          │                          │
     │  Updated HTML fragment   │                          │
     │─────────────────────────────────────────────────────▶│
     │                          │                          │
     │                          │                          │  DOM updated
```

### 5.3 Connection Status Indicator

The navigation bar displays real-time connection status:

```html
<div id="connection-status" class="flex items-center">
    <span class="w-2 h-2 bg-gray-500 rounded-full mr-2"></span>
    <span class="text-sm text-gray-400">Connecting...</span>
</div>
```

**Status States:**

| State | Indicator Color | Text |
|-------|-----------------|------|
| Connecting | Gray | "Connecting..." |
| Connected | Green | "Connected" |
| Disconnected | Red | "Disconnected" |

### 5.4 Automatic Reconnection

The WebSocket client implements automatic reconnection with a 3-second delay:

```javascript
ws.onclose = function() {
    // Update UI to show disconnected state
    document.getElementById('connection-status').innerHTML = `
        <span class="w-2 h-2 bg-red-500 rounded-full mr-2"></span>
        <span class="text-sm text-red-400">Disconnected</span>
    `;
    // Schedule reconnection attempt
    reconnectTimer = setTimeout(connectWebSocket, 3000);
};
```

---

## 6. Template Rendering Pipeline

### 6.1 Template Structure

Templates use Go's `html/template` package with a base template inheritance pattern:

```go
// Parse templates - each page template is parsed with base.html
s.templates = make(map[string]*template.Template)
pageTemplates := []string{
    "dashboard.html",
    "errors.html",
    "error_detail.html",
    "rules.html",
    "history.html",
    "settings.html",
}
for _, page := range pageTemplates {
    tmpl, err := template.New("").Funcs(s.templateFuncs()).ParseFS(
        templatesFS, "templates/base.html", "templates/"+page)
    if err != nil {
        return nil, fmt.Errorf("parsing template %s: %w", page, err)
    }
    s.templates[page] = tmpl
}
```

### 6.2 Base Template Definition

The base template defines the common structure with content blocks:

```html
{{define "base"}}
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{block "title" .}}Kube Sentinel{{end}}</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <style>
        /* Priority-based styling */
        .priority-red { background-color: #fee2e2; border-left: 4px solid #ef4444; }
        .priority-orange { background-color: #ffedd5; border-left: 4px solid #f97316; }
        .priority-yellow { background-color: #fef3c7; border-left: 4px solid #eab308; }
        .priority-blue { background-color: #dbeafe; border-left: 4px solid #3b82f6; }
        /* ... additional styles ... */
    </style>
</head>
<body class="bg-gray-100 min-h-screen">
    <nav class="bg-gray-800 text-white shadow-lg">
        <!-- Navigation with basePath-aware links -->
        <a href="{{basePath}}/" class="...">Dashboard</a>
        <a href="{{basePath}}/errors" class="...">Errors</a>
        <!-- ... -->
    </nav>

    <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
        {{block "content" .}}{{end}}
    </main>

    <script>
        // WebSocket initialization
    </script>
</body>
</html>
{{end}}
```

### 6.3 Page Template Extension

Page templates extend the base template by defining blocks:

```html
{{template "base" .}}

{{define "title"}}Dashboard - Kube Sentinel{{end}}

{{define "content"}}
<div class="space-y-6">
    <!-- Stats Cards -->
    <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div class="bg-white rounded-lg shadow p-6">
            <div class="text-sm font-medium text-gray-500">Total Errors</div>
            <div class="mt-2 text-3xl font-bold text-gray-900">
                {{.Stats.TotalErrors}}
            </div>
        </div>
        <!-- Additional cards -->
    </div>
</div>
{{end}}
```

### 6.4 Template Rendering

The `renderTemplate` helper executes templates with proper content type:

```go
func (s *Server) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    tmpl, ok := s.templates[name]
    if !ok {
        s.logger.Error("template not found", "template", name)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return
    }
    if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
        s.logger.Error("template render failed", "template", name, "error", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
    }
}
```

### 6.5 Custom Template Functions

The server provides custom template functions for common formatting needs:

```go
func (s *Server) templateFuncs() template.FuncMap {
    return template.FuncMap{
        "basePath": func() string {
            return s.basePath
        },
        "formatTime": func(t time.Time) string {
            return t.Format("2006-01-02 15:04:05")
        },
        "formatDuration": func(d time.Duration) string {
            if d < time.Minute {
                return d.Round(time.Second).String()
            }
            if d < time.Hour {
                return d.Round(time.Minute).String()
            }
            return d.Round(time.Hour).String()
        },
        "timeAgo": func(t time.Time) string {
            d := time.Since(t)
            if d < time.Minute {
                return "just now"
            }
            if d < time.Hour {
                mins := int(d.Minutes())
                if mins == 1 {
                    return "1 minute ago"
                }
                return fmt.Sprintf("%d minutes ago", mins)
            }
            // ... additional logic for hours/days
        },
        "priorityColor": func(p rules.Priority) string {
            return p.Color()
        },
        "priorityLabel": func(p rules.Priority) string {
            return p.Label()
        },
        "truncate": func(s string, n int) string {
            if len(s) <= n {
                return s
            }
            return s[:n] + "..."
        },
        "add": func(a, b int) int { return a + b },
        "sub": func(a, b int) int { return a - b },
        "mul": func(a, b int) int { return a * b },
    }
}
```

**Template Function Reference:**

| Function | Signature | Purpose |
|----------|-----------|---------|
| `basePath` | `() string` | Returns configured base URL path |
| `formatTime` | `(time.Time) string` | Formats timestamp as "2006-01-02 15:04:05" |
| `formatDuration` | `(time.Duration) string` | Human-readable duration |
| `timeAgo` | `(time.Time) string` | Relative time ("5 minutes ago") |
| `priorityColor` | `(Priority) string` | CSS color class for priority |
| `priorityLabel` | `(Priority) string` | Human-readable priority label |
| `truncate` | `(string, int) string` | Truncate with ellipsis |
| `add`, `sub`, `mul` | `(int, int) int` | Arithmetic for pagination |

---

## 7. Static Asset Embedding

### 7.1 go:embed Directives

Static assets are embedded directly into the binary using Go's embed package:

```go
//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS
```

### 7.2 Embedded File Structure

```
internal/web/
├── server.go              # go:embed directives here
├── handlers.go
├── templates/
│   ├── base.html          # Embedded via templatesFS
│   ├── dashboard.html
│   ├── errors.html
│   ├── error_detail.html
│   ├── rules.html
│   ├── history.html
│   └── settings.html
└── static/
    └── app.css            # Embedded via staticFS
```

### 7.3 Static File Serving

Static files are served using a sub-filesystem and HTTP file server:

```go
func (s *Server) setupRoutes() {
    // Create a sub-filesystem rooted at "static" directory
    staticSub, _ := fs.Sub(staticFS, "static")

    // Serve files with URL prefix stripped
    s.router.PathPrefix("/static/").Handler(
        http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

    // ...
}
```

**URL to File Mapping:**

| Request URL | Embedded Path | File Served |
|-------------|---------------|-------------|
| `/static/app.css` | `static/app.css` | `internal/web/static/app.css` |

### 7.4 Benefits of Embedding

1. **Single Binary Deployment**: No external file dependencies
2. **Immutable Assets**: Assets cannot be modified at runtime
3. **Simplified Distribution**: Single executable contains all resources
4. **Version Consistency**: Assets always match code version

---

## 8. Base Path Configuration

### 8.1 Reverse Proxy Support

The `basePath` configuration enables deployment behind a reverse proxy with a URL prefix:

```go
type Server struct {
    basePath string  // e.g., "/kube-sentinel"
    // ...
}
```

### 8.2 Template Integration

All URLs in templates use the `basePath` function:

```html
<!-- Navigation links -->
<a href="{{basePath}}/" class="...">Dashboard</a>
<a href="{{basePath}}/errors" class="...">Errors</a>

<!-- WebSocket connection -->
<script>
    const basePath = "{{basePath}}";
    ws = new WebSocket(`${protocol}//${window.location.host}${basePath}/ws`);
</script>

<!-- Internal links in content -->
<a href="{{basePath}}/errors/{{.ID}}" class="...">View Details</a>
```

### 8.3 Reverse Proxy Configuration Example

**Nginx configuration:**
```nginx
location /kube-sentinel/ {
    proxy_pass http://kube-sentinel-service:8080/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

**Kube Sentinel configuration:**
```yaml
web:
  addr: ":8080"
  base_path: "/kube-sentinel"
```

### 8.4 URL Resolution Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Base Path URL Resolution                          │
└─────────────────────────────────────────────────────────────────────┘

External URL: https://example.com/kube-sentinel/errors

    ┌────────────────┐      ┌─────────────────┐      ┌──────────────┐
    │   Browser      │      │  Reverse Proxy  │      │ Kube Sentinel│
    │                │      │  (Nginx/Traefik)│      │   Server     │
    └───────┬────────┘      └────────┬────────┘      └──────┬───────┘
            │                        │                       │
            │  GET /kube-sentinel/   │                       │
            │  errors                │                       │
            │───────────────────────▶│                       │
            │                        │                       │
            │                        │  GET /errors          │
            │                        │  (prefix stripped)    │
            │                        │──────────────────────▶│
            │                        │                       │
            │                        │  HTML with links      │
            │                        │  using /kube-sentinel │
            │                        │  prefix               │
            │                        │◀──────────────────────│
            │                        │                       │
            │  HTML Response         │                       │
            │◀───────────────────────│                       │
```

---

## 9. API Endpoints Reference

### 9.1 Errors API

#### GET /api/errors

List errors with filtering and pagination.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number |
| `pageSize` | int | 20 | Items per page (max 100) |
| `namespace` | string | - | Filter by namespace |
| `pod` | string | - | Filter by pod name |
| `priority` | string | - | Filter by priority (P1-P4) |
| `search` | string | - | Search in error message |

**Response:**
```json
{
    "errors": [...],
    "total": 42,
    "page": 1,
    "pageSize": 20
}
```

**Implementation:**
```go
func (s *Server) handleAPIErrors(w http.ResponseWriter, r *http.Request) {
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    if page < 1 {
        page = 1
    }
    pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
    if pageSize < 1 || pageSize > 100 {
        pageSize = 20
    }

    filter := store.ErrorFilter{
        Namespace: r.URL.Query().Get("namespace"),
        Pod:       r.URL.Query().Get("pod"),
        Search:    r.URL.Query().Get("search"),
    }

    if p := r.URL.Query().Get("priority"); p != "" {
        if priority, err := rules.ParsePriority(p); err == nil {
            filter.Priority = priority
        }
    }

    errors, total, err := s.store.ListErrors(filter, store.PaginationOptions{
        Offset: (page - 1) * pageSize,
        Limit:  pageSize,
    })
    if err != nil {
        s.jsonError(w, err.Error(), http.StatusInternalServerError)
        return
    }

    s.jsonResponse(w, map[string]interface{}{
        "errors": errors,
        "total":  total,
        "page":   page,
        "pageSize": pageSize,
    })
}
```

#### GET /api/errors/{id}

Get a single error with its remediation history.

**Response:**
```json
{
    "error": {...},
    "remediations": [...]
}
```

### 9.2 Rules API

#### GET /api/rules

List all configured rules.

**Response:**
```json
{
    "rules": [
        {
            "name": "crashloop-backoff",
            "match": {"pattern": "CrashLoopBackOff"},
            "priority": "P1",
            "enabled": true
        }
    ]
}
```

#### POST /api/rules/test

Test a pattern against sample text.

**Request Body:**
```json
{
    "pattern": "CrashLoopBackOff|OOMKilled",
    "sample": "Container killed due to OOMKilled"
}
```

**Response:**
```json
{
    "matches": true
}
```

**Implementation:**
```go
func (s *Server) handleAPIRulesTest(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Pattern string `json:"pattern"`
        Sample  string `json:"sample"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        s.jsonError(w, "invalid request body", http.StatusBadRequest)
        return
    }

    matches, err := s.ruleEngine.TestPattern(req.Pattern, req.Sample)
    if err != nil {
        s.jsonResponse(w, map[string]interface{}{
            "matches": false,
            "error":   err.Error(),
        })
        return
    }

    s.jsonResponse(w, map[string]interface{}{
        "matches": matches,
    })
}
```

### 9.3 Remediations API

#### GET /api/remediations

List remediation logs with pagination.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number |
| `pageSize` | int | 50 | Items per page (max 100) |

**Response:**
```json
{
    "remediations": [...],
    "total": 100,
    "page": 1,
    "pageSize": 50
}
```

### 9.4 Stats API

#### GET /api/stats

Get dashboard statistics.

**Response:**
```json
{
    "totalErrors": 42,
    "errorsByPriority": {"P1": 5, "P2": 12, "P3": 15, "P4": 10},
    "errorsByNamespace": {"production": 20, "staging": 22},
    "remediationCount": 18,
    "successfulActions": 15,
    "failedActions": 3
}
```

### 9.5 Settings API

#### GET /api/settings

Get current remediation settings.

**Response:**
```json
{
    "enabled": true,
    "dry_run": false,
    "actions_this_hour": 12
}
```

#### POST /api/settings

Update remediation settings.

**Request Body:**
```json
{
    "enabled": true,
    "dry_run": true
}
```

**Implementation:**
```go
func (s *Server) handleAPISettings(w http.ResponseWriter, r *http.Request) {
    if r.Method == "POST" {
        var req struct {
            Enabled bool `json:"enabled"`
            DryRun  bool `json:"dry_run"`
        }

        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            s.jsonError(w, "invalid request body", http.StatusBadRequest)
            return
        }

        s.remEngine.SetEnabled(req.Enabled)
        s.remEngine.SetDryRun(req.DryRun)
    }

    s.jsonResponse(w, map[string]interface{}{
        "enabled":           s.remEngine.IsEnabled(),
        "dry_run":           s.remEngine.IsDryRun(),
        "actions_this_hour": s.remEngine.GetActionsThisHour(),
    })
}
```

### 9.6 Health Endpoints

#### GET /health

Liveness probe endpoint.

**Response:** `200 OK` with body `OK`

#### GET /ready

Readiness probe endpoint.

**Response:** `200 OK` with body `Ready`

**Implementation:**
```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Ready"))
}
```

---

## 10. Page Handler Data Structures

### 10.1 Dashboard Data

```go
type dashboardData struct {
    Stats              *store.Stats
    RecentErrors       []*store.Error
    RecentRemediations []*store.RemediationLog
    RemEnabled         bool
    DryRun             bool
    ActionsThisHour    int
}
```

### 10.2 Errors List Data

```go
type errorsData struct {
    Errors     []*store.Error
    Total      int
    Page       int
    PageSize   int
    Filter     store.ErrorFilter
    Namespaces []string  // For filter dropdown
}
```

### 10.3 Error Detail Data

```go
type errorDetailData struct {
    Error        *store.Error
    Remediations []*store.RemediationLog
}
```

### 10.4 Rules Data

```go
type rulesData struct {
    Rules []rules.Rule
}
```

### 10.5 History Data

```go
type historyData struct {
    Logs     []*store.RemediationLog
    Total    int
    Page     int
    PageSize int
}
```

### 10.6 Settings Data

```go
type settingsData struct {
    RemEnabled        bool
    DryRun            bool
    MaxActionsPerHour int
    ActionsThisHour   int
}
```

---

## 11. JSON Response Helpers

### 11.1 Success Response

```go
func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}
```

### 11.2 Error Response

```go
func (s *Server) jsonError(w http.ResponseWriter, message string, status int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

---

## 12. Architecture Summary

```
┌─────────────────────────────────────────────────────────────────────────┐
│                  Kube Sentinel Dashboard Architecture                     │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐ │
│  │                         HTTP Layer                                   │ │
│  │  ┌───────────────┐  ┌───────────────┐  ┌───────────────────────┐   │ │
│  │  │ gorilla/mux   │  │ net/http      │  │ gorilla/websocket     │   │ │
│  │  │ Router        │  │ Server        │  │ Upgrader              │   │ │
│  │  └───────────────┘  └───────────────┘  └───────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                    │                                      │
│  ┌─────────────────────────────────┼───────────────────────────────────┐ │
│  │                         Handler Layer                                │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │ │
│  │  │ Page        │  │ API         │  │ WebSocket   │  │ Health      │ │ │
│  │  │ Handlers    │  │ Handlers    │  │ Handler     │  │ Handlers    │ │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘ │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                    │                                      │
│  ┌─────────────────────────────────┼───────────────────────────────────┐ │
│  │                       Rendering Layer                                │ │
│  │  ┌─────────────────────────┐  ┌─────────────────────────────────┐   │ │
│  │  │ html/template           │  │ encoding/json                   │   │ │
│  │  │ - base.html             │  │ - API responses                 │   │ │
│  │  │ - page templates        │  │ - WebSocket messages            │   │ │
│  │  │ - template functions    │  │                                 │   │ │
│  │  └─────────────────────────┘  └─────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                    │                                      │
│  ┌─────────────────────────────────┼───────────────────────────────────┐ │
│  │                        Data Layer                                    │ │
│  │  ┌─────────────────────────┐  ┌─────────────────────────────────┐   │ │
│  │  │ store.Store Interface   │  │ Engine Dependencies             │   │ │
│  │  │ - Errors                │  │ - rules.Engine                  │   │ │
│  │  │ - RemediationLogs       │  │ - remediation.Engine            │   │ │
│  │  │ - Stats                 │  │                                 │   │ │
│  │  └─────────────────────────┘  └─────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                    │                                      │
│  ┌─────────────────────────────────┼───────────────────────────────────┐ │
│  │                       Static Assets                                  │ │
│  │  ┌─────────────────────────┐  ┌─────────────────────────────────┐   │ │
│  │  │ go:embed templates/*    │  │ go:embed static/*               │   │ │
│  │  │ (HTML templates)        │  │ (CSS, JS, images)               │   │ │
│  │  └─────────────────────────┘  └─────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                                                           │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 13. Conclusion

The Kube Sentinel dashboard architecture provides:

1. **Real-Time Updates**: WebSocket-based event streaming with HTMX integration
2. **Efficient Rendering**: Pre-compiled templates with custom functions
3. **Single Binary Deployment**: All assets embedded via go:embed
4. **Flexible Deployment**: Base path support for reverse proxy configurations
5. **Clean API Design**: RESTful JSON endpoints with consistent error handling
6. **Production-Ready**: Proper timeouts, graceful shutdown, health endpoints
7. **Maintainable Code**: Clear separation between handlers, templates, and data

The combination of server-side rendering with Go templates, reactive updates via HTMX, and real-time WebSocket communication provides a responsive user experience without the complexity of a full SPA framework.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
