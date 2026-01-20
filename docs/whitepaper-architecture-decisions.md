# Kube Sentinel Architecture Decision Records

## Technical Whitepaper v1.0

---

## Executive Summary

This document captures the key architecture decisions made during the design and implementation of Kube Sentinel, a Kubernetes error detection and auto-remediation system. Each decision is documented using the Architecture Decision Record (ADR) format, providing context, rationale, and consequences for future maintainers and contributors.

Architecture decisions are not made in isolation. Each choice influences and constrains subsequent decisions, creating a coherent system design. Understanding these decisions helps explain why Kube Sentinel works the way it does and what tradeoffs were accepted.

---

## Table of Contents

1. [ADR-001: Programming Language Selection](#adr-001-programming-language-selection)
2. [ADR-002: Log Source Selection](#adr-002-log-source-selection)
3. [ADR-003: Data Storage Strategy](#adr-003-data-storage-strategy)
4. [ADR-004: Rule Matching Algorithm](#adr-004-rule-matching-algorithm)
5. [ADR-005: Web Framework Selection](#adr-005-web-framework-selection)
6. [ADR-006: Template Engine Selection](#adr-006-template-engine-selection)
7. [ADR-007: Real-time Communication Protocol](#adr-007-real-time-communication-protocol)
8. [ADR-008: Configuration Management Strategy](#adr-008-configuration-management-strategy)
9. [ADR-009: Remediation Safety Architecture](#adr-009-remediation-safety-architecture)

---

## ADR-001: Programming Language Selection

### Status

**Accepted** - January 2026

### Context

Kube Sentinel requires a programming language suitable for building a Kubernetes-native application with the following requirements:

- Low resource footprint for running as a sidecar or lightweight deployment
- Strong Kubernetes client library support
- Efficient concurrent processing for log polling and WebSocket connections
- Easy deployment with minimal runtime dependencies
- Strong type safety for reliability in production environments

The primary candidates evaluated were:

| Language | Pros | Cons |
|----------|------|------|
| **Go** | Native K8s ecosystem, single binary, excellent concurrency | Verbose error handling, limited generics (historically) |
| **Python** | Rapid development, rich ecosystem | Runtime dependency, GIL limits concurrency, slower execution |
| **Rust** | Memory safety, performance | Steeper learning curve, smaller K8s ecosystem |
| **Java** | Enterprise adoption, mature tooling | JVM overhead, larger memory footprint |

### Decision

**We chose Go as the primary implementation language.**

### Rationale

1. **Kubernetes Ecosystem Alignment**

   Kubernetes itself is written in Go, and the official client-go library provides first-class support:

   ```go
   import (
       "k8s.io/client-go/kubernetes"
       "k8s.io/client-go/rest"
   )
   ```

   The client-go library offers:
   - Type-safe API interactions
   - Automatic retry and backoff
   - Watch/informer patterns for real-time updates
   - In-cluster authentication support

2. **Single Binary Distribution**

   Go compiles to a single static binary with no external dependencies:

   ```bash
   # Build produces a single ~20MB binary
   CGO_ENABLED=0 go build -o kube-sentinel ./cmd/kube-sentinel

   # Minimal Docker image
   FROM scratch
   COPY kube-sentinel /kube-sentinel
   ENTRYPOINT ["/kube-sentinel"]
   ```

   This simplifies deployment, reduces attack surface, and eliminates dependency management in production.

3. **Concurrency Model**

   Go's goroutines and channels are ideal for Kube Sentinel's concurrent workloads:

   ```go
   // Concurrent components in main.go
   go func() {
       poller.Start(ctx)  // Log polling goroutine
   }()

   go func() {
       webServer.Start()  // HTTP server goroutine
   }()

   go func() {
       // Periodic cleanup goroutine
       ticker := time.NewTicker(time.Hour)
       for range ticker.C {
           dataStore.DeleteOldErrors(cutoff)
       }
   }()
   ```

4. **Performance Characteristics**

   Go provides predictable performance with low memory overhead:
   - Compiled code with no interpreter overhead
   - Efficient garbage collector with sub-millisecond pauses
   - Native JSON marshaling at high throughput

### Consequences

**Positive:**
- Seamless integration with Kubernetes API
- Minimal container image size (~20MB)
- Excellent performance for concurrent log processing
- Strong type safety catches errors at compile time
- Large pool of Go developers familiar with K8s patterns

**Negative:**
- More verbose than Python for simple operations
- Error handling boilerplate (though explicit)
- Slower iteration during development compared to interpreted languages

**Mitigations:**
- Use code generation for repetitive patterns
- Adopt structured logging with slog for consistency
- Leverage Go 1.22+ features (generics, improved error handling)

---

## ADR-002: Log Source Selection

### Status

**Accepted** - January 2026

### Context

Kube Sentinel needs to consume logs from Kubernetes workloads to detect errors. The log aggregation system must:

- Support Kubernetes-native log collection
- Provide efficient querying for error patterns
- Scale with cluster size
- Be accessible from within the cluster

Evaluated alternatives:

| System | Strengths | Weaknesses |
|--------|-----------|------------|
| **Loki** | K8s-native, LogQL, resource efficient | Less mature than ES, limited aggregations |
| **Elasticsearch** | Mature, powerful queries | Resource intensive, complex operations |
| **CloudWatch** | Managed service, EKS integration | Vendor lock-in, cost at scale |
| **Direct pod logs** | No dependencies | No aggregation, no history |

### Decision

**We chose Grafana Loki as the log source.**

### Rationale

1. **Kubernetes-Native Design**

   Loki was designed specifically for Kubernetes log aggregation:

   ```yaml
   # Loki integrates with K8s labels automatically
   {namespace="production", pod=~"api-.*"} |~ "error"
   ```

   Kubernetes labels flow directly into Loki's label index, enabling efficient queries by namespace, deployment, pod, or custom labels.

2. **LogQL Query Language**

   LogQL provides powerful filtering capabilities that map well to error detection:

   ```go
   // Default error detection query in config.go
   Query: `{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"`
   ```

   LogQL features used by Kube Sentinel:
   - Label matchers: `{namespace="production"}`
   - Regex filtering: `|~ "pattern"`
   - Line parsing: `| json` or `| logfmt`
   - Metric queries for aggregations

3. **Resource Efficiency**

   Loki's index-only-labels approach dramatically reduces storage and memory requirements:

   ```
   Traditional (Elasticsearch):
   - Index: log message + all fields
   - Storage: ~10x raw log size
   - Memory: GB per million docs

   Loki:
   - Index: labels only (namespace, pod, container)
   - Storage: ~1.5x raw log size (compressed)
   - Memory: MB per million log lines
   ```

   This efficiency allows Kube Sentinel deployments alongside Loki without significant infrastructure overhead.

4. **Grafana Ecosystem Integration**

   Loki integrates with Promtail for collection and Grafana for visualization:

   ```
   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
   │  Promtail   │───▶│    Loki     │◀───│ Kube        │
   │  (collect)  │    │  (store)    │    │ Sentinel    │
   └─────────────┘    └─────────────┘    └─────────────┘
                            │
                            ▼
                     ┌─────────────┐
                     │   Grafana   │
                     │ (visualize) │
                     └─────────────┘
   ```

### Consequences

**Positive:**
- Native Kubernetes label support
- Low resource requirements
- LogQL provides powerful error filtering
- Common pairing with Prometheus/Grafana stack
- Multi-tenant support via X-Scope-OrgID

**Negative:**
- Less mature than Elasticsearch for complex queries
- Limited aggregation capabilities
- Requires Loki infrastructure (though lightweight)

**Implementation Details:**

```go
// Loki client configuration in loki/client.go
type Client struct {
    baseURL    string
    httpClient *http.Client
    tenantID   string      // Multi-tenant support
    username   string      // Basic auth support
    password   string
}

// Query execution
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) ([]LogEntry, error)
```

---

## ADR-003: Data Storage Strategy

### Status

**Accepted** - January 2026

### Context

Kube Sentinel needs to store:
- Detected errors with metadata and occurrence counts
- Remediation action history for auditing
- Aggregate statistics for the dashboard

Storage requirements:
- Fast read/write for real-time error tracking
- Deduplication by error fingerprint
- Configurable retention (7 days errors, 30 days remediation logs)
- Minimal operational overhead

Options evaluated:

| Storage | Pros | Cons |
|---------|------|------|
| **In-memory** | Zero setup, fastest access | Data loss on restart, memory limits |
| **SQLite** | Persistent, no server needed | Disk I/O, file locking considerations |
| **PostgreSQL** | Full ACID, rich queries | Operational overhead, external dependency |
| **Redis** | Fast, persistence options | External dependency, memory cost |

### Decision

**We chose in-memory storage as the default, with SQLite as an optional persistent backend.**

### Rationale

1. **Operational Simplicity**

   In-memory storage requires zero configuration and no external dependencies:

   ```go
   // Single line initialization in main.go
   dataStore := store.NewMemoryStore()
   ```

   This aligns with Kube Sentinel's goal of being a single-binary deployment with minimal operational burden.

2. **Performance Characteristics**

   In-memory maps provide O(1) lookups for error deduplication:

   ```go
   type MemoryStore struct {
       errors     map[string]*Error      // by ID
       errorsByFP map[string]*Error      // by fingerprint (dedup)
       // ...
   }

   func (s *MemoryStore) SaveError(err *Error) error {
       // O(1) fingerprint lookup for deduplication
       if existing, ok := s.errorsByFP[err.Fingerprint]; ok {
           existing.Count++
           existing.LastSeen = err.Timestamp
           return nil
       }
       // ...
   }
   ```

3. **Bounded Memory Usage**

   Memory is bounded by configurable limits with automatic cleanup:

   ```go
   type MemoryStore struct {
       maxErrors          int  // Default: 10,000
       maxRemediationLogs int  // Default: 5,000
   }

   func (s *MemoryStore) cleanupOldErrors() {
       // Remove oldest 10% when limit reached
       toRemove := len(errors) / 10
       for i := 0; i < toRemove; i++ {
           delete(s.errors, errors[i].ID)
       }
   }
   ```

4. **Ephemeral Data is Acceptable**

   For Kube Sentinel's use case, data loss on restart is acceptable because:
   - Errors will be re-detected on the next polling cycle
   - Historical analysis is available via Loki/Grafana
   - Kubernetes events provide additional audit trail
   - Remediation safety (cooldowns) reset safely

5. **SQLite Option for Persistence**

   When persistence is required, SQLite provides it without operational overhead:

   ```yaml
   # Configuration option
   store:
     type: sqlite
     path: /data/kube-sentinel.db
   ```

### Consequences

**Positive:**
- Zero external dependencies for default deployment
- Microsecond-level read/write performance
- Predictable memory usage with automatic bounds
- Simple code with no ORM complexity

**Negative:**
- Data loss on pod restart (mitigated by re-detection)
- Memory limits constrain history depth
- No cross-instance data sharing

**Tradeoff Analysis:**

```
Scenario: 1000 unique errors/hour, 50 remediations/hour

Memory usage (estimated):
- Error: ~2KB each (message, labels, metadata)
- RemediationLog: ~500 bytes each
- 10,000 errors + 5,000 logs ≈ 25MB

Pod restart impact:
- Errors re-detected within poll_interval (30s default)
- Cooldowns reset (may trigger extra remediations)
- Statistics reset (temporary dashboard gap)
```

---

## ADR-004: Rule Matching Algorithm

### Status

**Accepted** - January 2026

### Context

The rule engine must match incoming errors against configured rules to:
- Assign priority levels
- Determine remediation actions
- Provide consistent, predictable behavior

Two primary approaches were considered:

| Approach | Description | Use Case |
|----------|-------------|----------|
| **First-match-wins** | Stop at first matching rule | Simple, predictable |
| **Weighted scoring** | Score all rules, pick highest | Complex priorities |

### Decision

**We chose first-match-wins (ordered rule evaluation).**

### Rationale

1. **Predictability**

   First-match-wins produces deterministic, easily understood behavior:

   ```go
   // Rule matching in engine.go
   func (e *Engine) Match(err loki.ParsedError) *MatchedError {
       // Try rules in order (first match wins)
       for _, rule := range e.rules {
           if !rule.Enabled {
               continue
           }
           if e.matchRule(rule, err) {
               return &MatchedError{
                   Priority: rule.Priority,
                   RuleName: rule.Name,
                   // ...
               }
           }
       }
       // Default fallback
       return &MatchedError{Priority: PriorityLow, RuleName: "default"}
   }
   ```

   Users can understand exactly which rule will match by reading rules top-to-bottom.

2. **Performance**

   First-match-wins can short-circuit evaluation:

   ```
   Rules: [specific-app-error, generic-error, catch-all]

   First-match: Stop at first match (1-3 comparisons)
   Weighted: Evaluate ALL rules (always 3 comparisons)

   With 50 rules and 1000 errors/minute:
   - First-match: ~25 avg comparisons/error = 25,000/min
   - Weighted: 50 comparisons/error = 50,000/min
   ```

3. **Simplicity**

   Rule ordering is a familiar concept (firewall rules, nginx locations, etc.):

   ```yaml
   rules:
     # Most specific rules first
     - name: payment-service-timeout
       match:
         namespaces: [payments]
         pattern: "timeout.*stripe"
       priority: P1

     # More general rules later
     - name: generic-timeout
       match:
         pattern: "timeout"
       priority: P3

     # Catch-all last
     - name: all-errors
       match:
         pattern: "error|fatal|panic"
       priority: P4
   ```

4. **Explicit Priority Control**

   Rule order provides explicit control over priority:

   ```yaml
   # User controls priority by ordering:
   # 1. Namespace-specific rules
   # 2. Error-type-specific rules
   # 3. General patterns
   # 4. Default catch-all
   ```

### Consequences

**Positive:**
- Easy to understand and debug
- Consistent behavior across invocations
- Better performance with early termination
- Familiar pattern for operators

**Negative:**
- Rule order is significant (user responsibility)
- No automatic conflict resolution
- Adding rules requires considering position

**Best Practice Guidance:**

```yaml
# Recommended rule ordering:
# 1. Namespace + specific pattern (most specific)
# 2. Namespace only
# 3. Specific pattern only
# 4. General patterns
# 5. Catch-all (least specific)
```

---

## ADR-005: Web Framework Selection

### Status

**Accepted** - January 2026

### Context

Kube Sentinel needs a web interface for:
- Dashboard with real-time error visualization
- Rule management and testing
- Remediation history and audit logs
- Settings configuration

Framework requirements:
- Minimal dependencies
- Support for embedded static assets
- WebSocket support for real-time updates
- Compatible with single-binary distribution

Options evaluated:

| Framework | Approach | Dependencies | Build Complexity |
|-----------|----------|--------------|------------------|
| **gorilla/mux + htmx** | Server-rendered + JS sprinkles | Minimal | None |
| **React/Vue SPA** | Client-side rendering | npm, webpack | Complex |
| **Gin** | Full-featured Go web framework | Moderate | None |
| **Echo** | High-performance Go framework | Moderate | None |

### Decision

**We chose gorilla/mux with htmx for progressive enhancement.**

### Rationale

1. **Embedded Assets**

   Go's embed directive allows static assets in the binary:

   ```go
   //go:embed templates/*.html
   var templatesFS embed.FS

   //go:embed static/*
   var staticFS embed.FS
   ```

   This eliminates external file dependencies and simplifies deployment.

2. **No Build Step**

   Unlike SPA frameworks, this approach requires no JavaScript build pipeline:

   ```
   SPA approach:
   1. npm install (downloads 500MB+ of node_modules)
   2. npm run build (webpack/vite compilation)
   3. Copy dist/ to Docker image
   4. Serve static files

   gorilla/mux + htmx:
   1. go build (compiles Go + embeds assets)
   2. Done
   ```

3. **htmx for Interactivity**

   htmx provides SPA-like interactivity with HTML attributes:

   ```html
   <!-- Real-time error list updates -->
   <div hx-get="/api/errors"
        hx-trigger="every 30s"
        hx-swap="innerHTML">
   </div>

   <!-- Form submission without page reload -->
   <form hx-post="/api/rules/test" hx-target="#result">
     <input name="pattern" />
     <button type="submit">Test</button>
   </form>
   ```

   htmx is a single 14KB file with no dependencies.

4. **gorilla/mux Maturity**

   gorilla/mux is a battle-tested router with extensive features:

   ```go
   router := mux.NewRouter()

   // Path parameters
   router.HandleFunc("/errors/{id}", s.handleErrorDetail)

   // Method routing
   router.HandleFunc("/api/rules", s.handleAPIRules).Methods("GET")
   router.HandleFunc("/api/rules/test", s.handleAPIRulesTest).Methods("POST")

   // Static file serving
   router.PathPrefix("/static/").Handler(
       http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))
   )
   ```

5. **WebSocket Support**

   gorilla/websocket provides WebSocket support:

   ```go
   upgrader := websocket.Upgrader{
       ReadBufferSize:  1024,
       WriteBufferSize: 1024,
   }

   func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
       conn, _ := s.upgrader.Upgrade(w, r, nil)
       s.clients[conn] = true
   }
   ```

### Consequences

**Positive:**
- Single binary with all assets embedded
- No JavaScript build pipeline
- Fast page loads (server-rendered HTML)
- Progressive enhancement (works without JS)
- Minimal attack surface

**Negative:**
- Less interactive than full SPA
- Server round-trips for each interaction
- Limited offline capabilities

**htmx Patterns Used:**

```html
<!-- Polling for updates -->
<div hx-get="/api/stats" hx-trigger="every 5s" hx-swap="outerHTML">

<!-- Click to load details -->
<tr hx-get="/errors/{{.ID}}" hx-target="#detail-panel">

<!-- Search with debounce -->
<input hx-get="/api/errors" hx-trigger="keyup changed delay:300ms">
```

---

## ADR-006: Template Engine Selection

### Status

**Accepted** - January 2026

### Context

The web UI requires a template engine to render HTML pages with dynamic data. The template engine must:
- Integrate cleanly with Go
- Support template inheritance (base layouts)
- Allow custom functions for formatting
- Work with embedded filesystem

Options evaluated:

| Engine | Language | Integration | Complexity |
|--------|----------|-------------|------------|
| **Go html/template** | Go | Native | Simple |
| **Pongo2** | Django-like | Go library | Moderate |
| **React SSR** | JavaScript | Node.js | Complex |
| **Vue SSR** | JavaScript | Node.js | Complex |

### Decision

**We chose Go's built-in html/template package.**

### Rationale

1. **Native Integration**

   html/template is part of Go's standard library:

   ```go
   import "html/template"

   // Parse templates from embedded FS
   tmpl, err := template.New("").Funcs(funcs).ParseFS(
       templatesFS,
       "templates/base.html",
       "templates/dashboard.html",
   )
   ```

2. **Automatic HTML Escaping**

   html/template automatically escapes content to prevent XSS:

   ```html
   <!-- User input is automatically escaped -->
   <p>{{.Message}}</p>

   <!-- Renders: <script>alert('xss')</script> as text, not executed -->
   ```

3. **Template Inheritance**

   Go templates support composition via define/template:

   ```html
   <!-- base.html -->
   <!DOCTYPE html>
   <html>
   <head>{{template "title" .}}</head>
   <body>
     <nav>...</nav>
     {{template "content" .}}
   </body>
   </html>

   <!-- dashboard.html -->
   {{define "title"}}Dashboard{{end}}
   {{define "content"}}
     <h1>Error Dashboard</h1>
     {{range .Errors}}...{{end}}
   {{end}}
   ```

4. **Custom Functions**

   Template functions provide formatting and utilities:

   ```go
   func (s *Server) templateFuncs() template.FuncMap {
       return template.FuncMap{
           "formatTime": func(t time.Time) string {
               return t.Format("2006-01-02 15:04:05")
           },
           "timeAgo": func(t time.Time) string {
               d := time.Since(t)
               if d < time.Minute {
                   return "just now"
               }
               // ...
           },
           "priorityColor": func(p rules.Priority) string {
               return p.Color()
           },
           "truncate": func(s string, n int) string {
               if len(s) <= n {
                   return s
               }
               return s[:n] + "..."
           },
       }
   }
   ```

5. **No JavaScript Runtime**

   Unlike React/Vue SSR, Go templates require no Node.js:

   ```
   React SSR requirements:
   - Node.js runtime
   - V8 JavaScript engine
   - npm dependencies
   - Build step for hydration

   Go templates:
   - Just Go compiler
   - Templates embedded in binary
   ```

### Consequences

**Positive:**
- Zero additional dependencies
- Automatic XSS protection
- Compile-time template validation (optional)
- Fast rendering performance
- Single-binary deployment maintained

**Negative:**
- Less expressive than JavaScript frameworks
- Limited component reusability
- More verbose for complex UIs
- No client-side hydration

**Template Organization:**

```
internal/web/
├── templates/
│   ├── base.html          # Layout with nav, header, footer
│   ├── dashboard.html     # Main dashboard
│   ├── errors.html        # Error list
│   ├── error_detail.html  # Single error view
│   ├── rules.html         # Rule management
│   ├── history.html       # Remediation history
│   └── settings.html      # Configuration
└── static/
    ├── css/
    └── js/
        └── htmx.min.js
```

---

## ADR-007: Real-time Communication Protocol

### Status

**Accepted** - January 2026

### Context

The dashboard requires real-time updates for:
- New error notifications
- Remediation action status
- Statistics refresh

Protocol options:

| Protocol | Direction | Complexity | Browser Support |
|----------|-----------|------------|-----------------|
| **WebSocket** | Bidirectional | Moderate | Universal |
| **Server-Sent Events (SSE)** | Server→Client | Simple | Universal |
| **Long Polling** | Request→Response | Simple | Universal |
| **WebRTC** | Bidirectional | Complex | Modern browsers |

### Decision

**We chose WebSocket for real-time communication.**

### Rationale

1. **Bidirectional Potential**

   WebSocket supports both server→client and client→server communication:

   ```go
   // Server broadcasts to all clients
   func (s *Server) BroadcastError(err *store.Error) {
       s.mu.RLock()
       defer s.mu.RUnlock()

       msg := map[string]interface{}{
           "type":  "error",
           "error": err,
       }

       for client := range s.clients {
           client.WriteJSON(msg)
       }
   }
   ```

   While currently used for server→client pushes, bidirectional capability enables future features like:
   - Acknowledge/dismiss errors from dashboard
   - Trigger manual remediation
   - Real-time rule testing

2. **Efficient Connection**

   WebSocket maintains a single TCP connection per client:

   ```
   Long Polling:
   - New HTTP request every N seconds
   - Connection overhead each time
   - Server-side request handling overhead

   WebSocket:
   - Single connection upgrade
   - Frames sent over existing connection
   - Minimal overhead per message
   ```

3. **Native Browser Support**

   WebSocket is supported in all modern browsers:

   ```javascript
   // Client-side connection (embedded in templates)
   const ws = new WebSocket(`ws://${location.host}/ws`);

   ws.onmessage = function(event) {
       const data = JSON.parse(event.data);
       if (data.type === 'error') {
           updateErrorList(data.error);
       } else if (data.type === 'stats') {
           updateStats(data.stats);
       }
   };
   ```

4. **gorilla/websocket Integration**

   gorilla/websocket provides mature WebSocket support:

   ```go
   type Server struct {
       clients  map[*websocket.Conn]bool
       upgrader websocket.Upgrader
   }

   func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
       conn, err := s.upgrader.Upgrade(w, r, nil)
       if err != nil {
           return
       }

       s.mu.Lock()
       s.clients[conn] = true
       s.mu.Unlock()

       // Keep connection alive, handle disconnects
       for {
           if _, _, err := conn.ReadMessage(); err != nil {
               s.mu.Lock()
               delete(s.clients, conn)
               s.mu.Unlock()
               conn.Close()
               return
           }
       }
   }
   ```

### Consequences

**Positive:**
- Low-latency updates (sub-second)
- Efficient for high-frequency updates
- Bidirectional capability for future features
- Well-supported across browsers and proxies

**Negative:**
- Slightly more complex than SSE
- Requires connection state management
- May require WebSocket-aware load balancers

**Message Types:**

```go
// Error notification
{"type": "error", "error": {...}}

// Remediation notification
{"type": "remediation", "remediation": {...}}

// Statistics update
{"type": "stats", "stats": {...}}
```

---

## ADR-008: Configuration Management Strategy

### Status

**Accepted** - January 2026

### Context

Kube Sentinel requires configuration for:
- Loki connection details
- Rule definitions
- Remediation settings
- Web server options

Configuration approaches in Kubernetes:

| Approach | GitOps | Complexity | K8s Integration |
|----------|--------|------------|-----------------|
| **YAML files** | Excellent | Simple | Via ConfigMap mount |
| **Custom Resource Definitions** | Excellent | Complex | Native |
| **ConfigMaps directly** | Good | Simple | Native |
| **Environment variables** | Limited | Simple | Native |

### Decision

**We chose YAML configuration files, designed for ConfigMap mounting.**

### Rationale

1. **GitOps Compatibility**

   YAML files stored in Git enable GitOps workflows:

   ```
   kube-sentinel-config/
   ├── config.yaml           # Main configuration
   └── rules.yaml            # Rule definitions
   ```

   Configuration changes follow the same PR/review process as application code.

2. **ConfigMap Integration**

   YAML files map directly to Kubernetes ConfigMaps:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: kube-sentinel-config
   data:
     config.yaml: |
       loki:
         url: http://loki.monitoring:3100
         query: '{namespace=~".+"} |~ "error"'
         poll_interval: 30s
       remediation:
         enabled: true
         dry_run: false

     rules.yaml: |
       rules:
         - name: crashloop
           match:
             pattern: CrashLoopBackOff
           priority: P1
   ```

   Mounted as files in the container:

   ```yaml
   volumes:
     - name: config
       configMap:
         name: kube-sentinel-config
   volumeMounts:
     - name: config
       mountPath: /etc/kube-sentinel
   ```

3. **Sensible Defaults**

   Configuration has complete defaults allowing zero-config startup:

   ```go
   func DefaultConfig() *Config {
       return &Config{
           Loki: LokiConfig{
               URL:          "http://loki.monitoring:3100",
               Query:        `{namespace=~".+"} |~ "(?i)(error|fatal|panic)"`,
               PollInterval: 30 * time.Second,
               Lookback:     5 * time.Minute,
           },
           Web: WebConfig{
               Listen: ":8080",
           },
           Remediation: RemediationConfig{
               Enabled:           true,
               DryRun:            false,
               MaxActionsPerHour: 50,
               ExcludedNamespaces: []string{"kube-system", "monitoring"},
           },
       }
   }
   ```

4. **Why Not CRDs?**

   Custom Resource Definitions were considered but rejected:

   ```
   CRD approach:
   ✗ Requires CRD installation (cluster-admin)
   ✗ Operator pattern complexity
   ✗ Controller reconciliation logic
   ✗ RBAC for custom resources

   YAML file approach:
   ✓ No special permissions needed
   ✓ Works with any Kubernetes RBAC
   ✓ Simple file-based reload
   ✓ Familiar to operators
   ```

5. **Validation at Load Time**

   Configuration is validated when loaded:

   ```go
   func (c *Config) Validate() error {
       if c.Loki.URL == "" {
           return fmt.Errorf("loki.url is required")
       }
       if c.Loki.PollInterval < time.Second {
           return fmt.Errorf("loki.poll_interval must be at least 1s")
       }
       if c.Loki.Lookback < c.Loki.PollInterval {
           return fmt.Errorf("loki.lookback must be >= poll_interval")
       }
       return nil
   }
   ```

### Consequences

**Positive:**
- Familiar YAML format
- GitOps workflow support
- ConfigMap integration
- No cluster-admin required
- Complete defaults for quick start

**Negative:**
- No live reload (requires pod restart)
- No built-in schema validation (kubectl apply)
- Configuration drift possible without GitOps

**Configuration Structure:**

```yaml
# config.yaml
loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)error"'
  poll_interval: 30s
  lookback: 5m
  tenant_id: ""          # For multi-tenant Loki

kubernetes:
  in_cluster: true       # Use service account
  kubeconfig: ""         # Or specify kubeconfig path

web:
  listen: ":8080"
  base_path: ""          # For reverse proxy

remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
    - monitoring

rules_file: /etc/kube-sentinel/rules.yaml
```

---

## ADR-009: Remediation Safety Architecture

### Status

**Accepted** - January 2026

### Context

Auto-remediation is powerful but dangerous. Without proper safeguards, automated actions could:
- Create cascading failures
- Exceed rate limits on Kubernetes API
- Act on critical system components
- Loop endlessly on recurring errors

Safety requirements:
- Prevent runaway automation
- Protect critical namespaces
- Enable gradual rollout
- Provide audit trail

### Decision

**We implemented multiple layers of safety controls following defense-in-depth principles.**

### Rationale

1. **Defense in Depth**

   No single safety mechanism is sufficient. Multiple layers ensure failures are contained:

   ```
   ┌──────────────────────────────────────────────────────────────┐
   │                    Safety Control Flow                        │
   ├──────────────────────────────────────────────────────────────┤
   │  Error ──▶ Engine Enabled? ──▶ Action != none? ──▶          │
   │            Namespace OK? ──▶ Cooldown Clear? ──▶             │
   │            Rate Limit OK? ──▶ Dry Run? ──▶ Execute           │
   └──────────────────────────────────────────────────────────────┘
   ```

   Each check is independent - disabling one doesn't disable others.

2. **Master Kill Switch**

   Global enable/disable for all remediation:

   ```go
   if !e.enabled {
       logEntry.Status = "skipped"
       logEntry.Message = "remediation disabled"
       return logEntry, nil
   }
   ```

   Operators can instantly stop all automated actions.

3. **Dry Run Mode**

   Test remediation logic without executing:

   ```go
   if e.dryRun {
       logEntry.Status = "success"
       logEntry.Message = "dry run - would execute action"
       e.logger.Info("dry run remediation",
           "action", rule.Remediation.Action,
           "target", target.String(),
       )
       return logEntry, nil
   }
   ```

   Recommended for initial deployment and rule testing.

4. **Namespace Exclusions**

   Protect critical infrastructure:

   ```go
   excludedNamespaces: map[string]bool{
       "kube-system": true,
       "kube-public": true,
       "monitoring":  true,
       "logging":     true,
   }

   if e.excludedNamespaces[err.Namespace] {
       logEntry.Status = "skipped"
       logEntry.Message = fmt.Sprintf("namespace %s is excluded", err.Namespace)
       return logEntry, nil
   }
   ```

5. **Per-Rule Cooldowns**

   Prevent action spam on the same target:

   ```go
   // Cooldown key: "ruleName:namespace/pod"
   cooldownKey := fmt.Sprintf("%s:%s", rule.Name, target.String())

   if expiresAt, ok := e.cooldowns[cooldownKey]; ok && time.Now().Before(expiresAt) {
       logEntry.Status = "skipped"
       logEntry.Message = fmt.Sprintf("cooldown active until %s", expiresAt.Format(time.RFC3339))
       return logEntry, nil
   }

   // After execution, set cooldown
   e.cooldowns[cooldownKey] = time.Now().Add(rule.Remediation.Cooldown)
   ```

   Different errors on the same pod have independent cooldowns.

6. **Global Hourly Rate Limit**

   Prevent runaway automation during cascading failures:

   ```go
   // Sliding window rate limit
   func (e *Engine) cleanupHourlyLog() {
       cutoff := time.Now().Add(-time.Hour)
       var kept []time.Time
       for _, t := range e.hourlyLog {
           if t.After(cutoff) {
               kept = append(kept, t)
           }
       }
       e.hourlyLog = kept
   }

   if len(e.hourlyLog) >= e.maxActionsPerHour {
       logEntry.Status = "skipped"
       logEntry.Message = "hourly limit reached"
       return logEntry, nil
   }
   ```

7. **Action-Level Safety**

   Each action type has built-in limits:

   ```go
   // Scale-up action respects max_replicas
   if max, ok := params["max_replicas"]; ok {
       if newReplicas > max {
           return fmt.Errorf("would exceed max replicas limit")
       }
   }

   // Scale-down action respects min_replicas
   if newReplicas < minReplicas {
       newReplicas = minReplicas
   }
   ```

8. **Complete Audit Trail**

   Every action attempt is logged:

   ```go
   type RemediationLog struct {
       ID        string
       ErrorID   string
       Action    string
       Target    string
       Status    string    // success, failed, skipped
       Message   string    // Human-readable reason
       Timestamp time.Time
       DryRun    bool
   }
   ```

### Consequences

**Positive:**
- Multiple independent safety layers
- Gradual rollout path (dry run → limited → full)
- Complete audit trail for compliance
- Protection of critical infrastructure
- Predictable behavior under failure conditions

**Negative:**
- Legitimate remediations may be blocked by safety checks
- Cooldowns may delay necessary actions
- Rate limits may be hit during legitimate incidents

**Safety Configuration Example:**

```yaml
remediation:
  enabled: true
  dry_run: false              # Set true for testing
  max_actions_per_hour: 50    # Global rate limit
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
    - logging
    - istio-system

rules:
  - name: crashloop-restart
    match:
      pattern: CrashLoopBackOff
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m            # Per-rule cooldown
```

**Recommended Rollout Path:**

1. Deploy with `dry_run: true` - observe logs
2. Enable for non-critical namespaces first
3. Set conservative rate limits (10-20/hour)
4. Gradually increase as confidence builds
5. Add critical namespaces to exclusion list

---

## Summary

These nine architecture decisions form the foundation of Kube Sentinel's design:

| ADR | Decision | Key Benefit |
|-----|----------|-------------|
| 001 | Go language | K8s ecosystem alignment, single binary |
| 002 | Loki log source | K8s-native, resource efficient |
| 003 | In-memory storage | Zero dependencies, operational simplicity |
| 004 | First-match-wins | Predictability, performance |
| 005 | gorilla/mux + htmx | No build step, embedded assets |
| 006 | Go templates | Native integration, XSS protection |
| 007 | WebSocket | Low-latency, bidirectional potential |
| 008 | YAML configuration | GitOps friendly, ConfigMap integration |
| 009 | Defense-in-depth safety | Multiple independent safety layers |

Each decision was made with operational simplicity and reliability as primary concerns, accepting tradeoffs where they aligned with Kube Sentinel's goals as a lightweight, single-binary Kubernetes tool.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
