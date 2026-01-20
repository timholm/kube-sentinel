# Web Server

This document describes the web server component of kube-sentinel, which provides a dashboard interface for monitoring detected errors and remediation activities in real-time.

## Overview

The web server serves as the primary user interface for kube-sentinel, offering:

- **Dashboard Interface**: A web-based UI for visualizing errors, rules, and remediation history
- **REST API**: JSON endpoints for programmatic access to system data
- **Real-Time Updates**: WebSocket connectivity for live streaming of errors and remediations
- **Static Asset Serving**: Embedded CSS, JavaScript, and other assets compiled into the binary
- **Health Endpoints**: Kubernetes-compatible probes for orchestration integration

The server is implemented in `internal/web/server.go` with handlers defined in `internal/web/handlers.go`.

## Architecture

### Server Structure

The `Server` struct encapsulates all web server functionality:

| Field | Type | Description |
|-------|------|-------------|
| `addr` | `string` | Listen address in `host:port` format |
| `store` | `store.Store` | Data store for querying errors and remediation logs |
| `ruleEngine` | `*rules.Engine` | Rule engine for accessing loaded rules |
| `remEngine` | `*remediation.Engine` | Remediation engine for status and configuration |
| `logger` | `*slog.Logger` | Structured logger for server events |
| `templates` | `*template.Template` | Parsed HTML templates for page rendering |
| `router` | `*mux.Router` | Gorilla Mux router for request handling |
| `httpServer` | `*http.Server` | Underlying HTTP server with timeout configuration |
| `clients` | `map[*websocket.Conn]bool` | Active WebSocket client connections |
| `upgrader` | `websocket.Upgrader` | WebSocket connection upgrader |

### Dependencies

The web server integrates with three core system components:

1. **Store**: Provides access to persisted errors and remediation logs for display and API responses
2. **Rule Engine**: Supplies the list of active rules for the rules page and pattern testing
3. **Remediation Engine**: Exposes remediation status, configuration, and rate limiting information

## Server Lifecycle

### Initialization

Create a new server instance using `NewServer`:

```go
server, err := web.NewServer(
    ":8080",           // Listen address
    dataStore,         // Store implementation
    ruleEngine,        // Rule engine instance
    remEngine,         // Remediation engine instance
    logger,            // slog.Logger instance
)
if err != nil {
    // Handle template parsing error
}
```

During initialization, the server:

1. Parses embedded HTML templates from `templates/*.html`
2. Registers custom template functions for formatting
3. Configures the router with all routes
4. Initializes the WebSocket client registry

### Starting the Server

Start serving requests with `Start`:

```go
go func() {
    if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
        log.Fatal(err)
    }
}()
```

The server configures the following timeouts:

| Timeout | Duration | Purpose |
|---------|----------|---------|
| `ReadTimeout` | 15 seconds | Maximum time to read the entire request |
| `WriteTimeout` | 15 seconds | Maximum time to write the response |
| `IdleTimeout` | 60 seconds | Maximum time to wait for the next request on keep-alive connections |

### Graceful Shutdown

Shutdown the server gracefully with `Shutdown`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := server.Shutdown(ctx); err != nil {
    log.Printf("shutdown error: %v", err)
}
```

The shutdown process:

1. Closes all active WebSocket connections
2. Stops accepting new connections
3. Waits for in-flight requests to complete (up to context deadline)
4. Returns any errors encountered

## Routes and Handlers

### Page Routes

HTML pages served with rendered templates:

| Route | Method | Handler | Description |
|-------|--------|---------|-------------|
| `/` | GET | `handleDashboard` | Main dashboard with stats, recent errors, and remediations |
| `/errors` | GET | `handleErrors` | Paginated list of all detected errors with filtering |
| `/errors/{id}` | GET | `handleErrorDetail` | Detailed view of a single error with remediation history |
| `/rules` | GET | `handleRules` | List of all loaded detection rules |
| `/history` | GET | `handleHistory` | Paginated remediation action history |
| `/settings` | GET | `handleSettings` | System configuration and remediation controls |

### API Routes

JSON API endpoints for programmatic access:

| Route | Method | Handler | Description |
|-------|--------|---------|-------------|
| `/api/errors` | GET | `handleAPIErrors` | List errors with filtering and pagination |
| `/api/errors/{id}` | GET | `handleAPIErrorDetail` | Get single error with remediations |
| `/api/rules` | GET | `handleAPIRules` | List all active rules |
| `/api/rules/test` | POST | `handleAPIRulesTest` | Test a regex pattern against sample text |
| `/api/remediations` | GET | `handleAPIRemediations` | List remediation logs with pagination |
| `/api/stats` | GET | `handleAPIStats` | Get aggregate statistics |
| `/api/settings` | GET, POST | `handleAPISettings` | Read or update remediation settings |

### WebSocket Route

| Route | Handler | Description |
|-------|---------|-------------|
| `/ws` | `handleWebSocket` | Real-time event stream for errors, remediations, and stats |

### Health Routes

Kubernetes-compatible health check endpoints:

| Route | Method | Handler | Description |
|-------|--------|---------|-------------|
| `/health` | GET | `handleHealth` | Liveness probe - returns 200 OK if server is running |
| `/ready` | GET | `handleReady` | Readiness probe - returns 200 Ready if server can serve requests |

### Static Assets

Static files are served from the `/static/` path prefix:

```
/static/css/style.css
/static/js/app.js
/static/images/logo.png
```

## Embedded Assets

The web server uses Go's `embed` package to compile static assets and templates into the binary, eliminating external file dependencies.

### Template Embedding

```go
//go:embed templates/*.html
var templatesFS embed.FS
```

All HTML templates in the `templates/` directory are embedded at compile time. Templates are parsed during server initialization with custom template functions.

### Static File Embedding

```go
//go:embed static/*
var staticFS embed.FS
```

Static assets (CSS, JavaScript, images) in the `static/` directory are embedded and served via the `/static/` route prefix.

### Benefits of Embedding

- **Single Binary Deployment**: No external files to manage or distribute
- **Atomic Updates**: Assets update with the binary, ensuring consistency
- **Reduced Complexity**: No file path configuration or permission issues
- **Container-Friendly**: Simplified Dockerfile with no COPY directives for assets

## WebSocket Support

The web server provides real-time updates via WebSocket connections, enabling live dashboards without polling.

### Connection Handling

Clients connect to the `/ws` endpoint to establish a WebSocket connection:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    switch(data.type) {
        case 'error':
            // Handle new error
            break;
        case 'remediation':
            // Handle remediation log
            break;
        case 'stats':
            // Handle stats update
            break;
    }
};
```

### Client Management

The server maintains a thread-safe registry of connected clients:

```go
type Server struct {
    mu      sync.RWMutex
    clients map[*websocket.Conn]bool
    upgrader websocket.Upgrader
}
```

- Connections are added to the registry upon successful upgrade
- Connections are removed when closed or on read error
- The mutex ensures safe concurrent access during broadcasts

### Broadcast Methods

Three broadcast methods push updates to all connected clients:

#### BroadcastError

Sends new or updated error records to all clients:

```go
func (s *Server) BroadcastError(err *store.Error)
```

Message format:
```json
{
    "type": "error",
    "error": { /* Error object */ }
}
```

#### BroadcastRemediation

Sends remediation action logs to all clients:

```go
func (s *Server) BroadcastRemediation(log *store.RemediationLog)
```

Message format:
```json
{
    "type": "remediation",
    "remediation": { /* RemediationLog object */ }
}
```

#### BroadcastStats

Pushes updated aggregate statistics to all clients:

```go
func (s *Server) BroadcastStats()
```

Message format:
```json
{
    "type": "stats",
    "stats": { /* Stats object */ }
}
```

### WebSocket Configuration

The upgrader is configured with:

| Setting | Value | Purpose |
|---------|-------|---------|
| `ReadBufferSize` | 1024 bytes | Buffer for reading incoming messages |
| `WriteBufferSize` | 1024 bytes | Buffer for writing outgoing messages |
| `CheckOrigin` | Always true | Allows connections from any origin (development setting) |

## Template Functions

Custom template functions provide formatting utilities for HTML rendering:

### formatTime

Formats a `time.Time` value as a human-readable string:

```go
formatTime(t time.Time) string
// Returns: "2006-01-02 15:04:05"
```

Template usage:
```html
<span>{{ formatTime .Timestamp }}</span>
```

### formatDuration

Formats a `time.Duration` with appropriate precision:

```go
formatDuration(d time.Duration) string
// < 1 minute: rounds to seconds (e.g., "45s")
// < 1 hour: rounds to minutes (e.g., "5m0s")
// >= 1 hour: rounds to hours (e.g., "2h0m0s")
```

Template usage:
```html
<span>Duration: {{ formatDuration .ExecutionTime }}</span>
```

### timeAgo

Converts a timestamp to a relative time string:

```go
timeAgo(t time.Time) string
// < 1 minute: "just now"
// < 1 hour: "X minutes ago" or "1 minute ago"
// < 24 hours: "X hours ago" or "1 hour ago"
// >= 24 hours: "X days ago" or "1 day ago"
```

Template usage:
```html
<span>{{ timeAgo .LastSeen }}</span>
```

### priorityColor

Returns the CSS color class for a priority level:

```go
priorityColor(p rules.Priority) string
// Maps priority to color (e.g., "critical" -> "red", "warning" -> "orange")
```

Template usage:
```html
<span class="badge badge-{{ priorityColor .Priority }}">{{ .Priority }}</span>
```

### priorityLabel

Returns a human-readable label for a priority level:

```go
priorityLabel(p rules.Priority) string
// Returns display name (e.g., "Critical", "Warning", "Info")
```

Template usage:
```html
<span>Priority: {{ priorityLabel .Priority }}</span>
```

### truncate

Truncates a string to a maximum length with ellipsis:

```go
truncate(s string, n int) string
// Returns first n characters + "..." if len(s) > n
// Returns original string if len(s) <= n
```

Template usage:
```html
<span title="{{ .Message }}">{{ truncate .Message 100 }}</span>
```

## Integration with System Architecture

The web server integrates with kube-sentinel's main event loop as follows:

```
┌─────────────────────────────────────────────────────────────────┐
│                         Main Process                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────┐    ┌──────────────┐    ┌───────────────────┐      │
│  │  Loki    │───▶│  Rule Engine │───▶│  Store            │      │
│  │  Poller  │    │  (match)     │    │  (SaveError)      │      │
│  └──────────┘    └──────────────┘    └───────────────────┘      │
│       │                                        │                 │
│       │                                        │                 │
│       │          ┌──────────────────┐          │                 │
│       └─────────▶│  Remediation     │◀─────────┘                 │
│                  │  Engine          │                            │
│                  └────────┬─────────┘                            │
│                           │                                      │
│                           ▼                                      │
│                  ┌──────────────────┐                            │
│                  │  Web Server      │                            │
│                  │  ┌────────────┐  │                            │
│                  │  │ Broadcast  │  │──────▶ WebSocket Clients   │
│                  │  │ Error      │  │                            │
│                  │  ├────────────┤  │                            │
│                  │  │ Broadcast  │  │──────▶ WebSocket Clients   │
│                  │  │ Remediation│  │                            │
│                  │  ├────────────┤  │                            │
│                  │  │ Broadcast  │  │──────▶ WebSocket Clients   │
│                  │  │ Stats      │  │                            │
│                  │  └────────────┘  │                            │
│                  └──────────────────┘                            │
│                           │                                      │
│                           ▼                                      │
│                  HTTP Clients (Dashboard, API consumers)         │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Event Flow

1. **Error Detection**: The Loki poller fetches logs and sends them to the rule engine
2. **Error Storage**: Matched errors are saved to the store
3. **WebSocket Broadcast**: The web server's `BroadcastError` method pushes the error to connected clients
4. **Remediation**: If enabled, the remediation engine processes the error
5. **Remediation Broadcast**: The `BroadcastRemediation` method notifies clients of the action
6. **Stats Update**: The `BroadcastStats` method pushes updated aggregate statistics

### Main.go Integration

The main application orchestrates the web server:

```go
// Initialize web server
webServer, err := web.NewServer(cfg.Web.Listen, dataStore, ruleEngine, remEngine, logger)
if err != nil {
    log.Fatal(err)
}

// Start web server in goroutine
go func() {
    if err := webServer.Start(); err != nil && err.Error() != "http: Server closed" {
        errCh <- fmt.Errorf("web server error: %w", err)
    }
}()

// In error handler callback:
webServer.BroadcastError(storeErr)
webServer.BroadcastRemediation(log)
webServer.BroadcastStats()

// Graceful shutdown
if err := webServer.Shutdown(shutdownCtx); err != nil {
    logger.Error("web server shutdown error", "error", err)
}
```

## API Query Parameters

### Error List API (`/api/errors`)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number (1-indexed) |
| `pageSize` | int | 20 | Results per page (max 100) |
| `namespace` | string | - | Filter by Kubernetes namespace |
| `pod` | string | - | Filter by pod name |
| `priority` | string | - | Filter by priority level |
| `search` | string | - | Free-text search in error messages |

### Remediation List API (`/api/remediations`)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number (1-indexed) |
| `pageSize` | int | 50 | Results per page (max 100) |

### Settings API (`/api/settings`)

For POST requests, accepts JSON body:

```json
{
    "enabled": true,
    "dry_run": false
}
```

## Future Improvements

### Middleware Support

The current implementation lacks a middleware layer. Future enhancements could include:

- **Logging Middleware**: Structured request logging with timing, status codes, and client information
- **Recovery Middleware**: Panic recovery to prevent server crashes from handler errors
- **Compression Middleware**: gzip/brotli response compression for reduced bandwidth
- **CORS Middleware**: Configurable cross-origin resource sharing for API consumers
- **Rate Limiting**: Request rate limiting per IP or API key to prevent abuse

Example middleware chain:

```go
handler := loggingMiddleware(
    recoveryMiddleware(
        corsMiddleware(
            rateLimitMiddleware(router),
        ),
    ),
)
```

### Authentication and Authorization

The server currently has no authentication. Production deployments should consider:

- **Basic Authentication**: Simple username/password protection for the dashboard
- **OAuth2/OIDC Integration**: Enterprise SSO with providers like Okta, Auth0, or Keycloak
- **API Keys**: Token-based authentication for API consumers
- **RBAC**: Role-based access control for different user levels (viewer, operator, admin)
- **Audit Logging**: Track who performed what actions and when

### TLS Support

The server currently uses plain HTTP. TLS support would include:

- **Direct TLS**: Configure the server with certificate and key files
- **Auto-TLS**: Integration with Let's Encrypt for automatic certificate management
- **mTLS**: Mutual TLS for client certificate authentication

Example TLS configuration:

```go
server := &http.Server{
    Addr:    ":8443",
    Handler: router,
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
        // Additional TLS settings
    },
}
server.ListenAndServeTLS("cert.pem", "key.pem")
```

### Additional Enhancements

- **API Versioning**: Version prefixes (e.g., `/api/v1/`) for backward compatibility
- **OpenAPI Specification**: Auto-generated API documentation with Swagger/OpenAPI
- **Request Validation**: Structured input validation with descriptive error messages
- **Caching Headers**: ETags and Cache-Control for improved client-side caching
- **Metrics Endpoint**: Prometheus-compatible `/metrics` endpoint for observability
- **WebSocket Heartbeats**: Ping/pong frames to detect stale connections
- **Connection Limits**: Maximum concurrent connections and WebSocket clients
- **Graceful Degradation**: Circuit breakers for external dependencies
