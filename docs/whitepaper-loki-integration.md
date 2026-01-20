# Kube Sentinel Loki Integration & Log Polling System

## Technical Whitepaper v1.0

---

## Executive Summary

Kube Sentinel integrates with Grafana Loki to provide real-time log monitoring and error detection for Kubernetes clusters. The Loki integration system continuously polls log streams using LogQL queries, extracts Kubernetes metadata, and processes entries through a sophisticated parsing and deduplication pipeline.

This document details the Loki client architecture, query configuration, polling mechanics, log parsing strategies, and resilience patterns implemented in the system.

---

## 1. System Architecture Overview

### 1.1 High-Level Architecture

```
+------------------------------------------------------------------+
|                     Kube Sentinel                                  |
+------------------------------------------------------------------+
|                                                                    |
|  +--------------------+      +--------------------+                |
|  |    Loki Client     |----->|    Log Poller      |                |
|  |                    |      |                    |                |
|  |  - HTTP Client     |      |  - Poll Loop       |                |
|  |  - Auth Headers    |      |  - Deduplication   |                |
|  |  - Query Builder   |      |  - Error Handler   |                |
|  +--------------------+      +--------------------+                |
|           |                          |                             |
|           v                          v                             |
|  +--------------------+      +--------------------+                |
|  |   Loki API Server  |      |   Rule Engine      |                |
|  |   (External)       |      |   (Downstream)     |                |
|  +--------------------+      +--------------------+                |
|                                                                    |
+------------------------------------------------------------------+
```

### 1.2 Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **Loki Client** | HTTP communication with Loki API, authentication, query execution |
| **Log Poller** | Continuous polling loop, time window management, deduplication |
| **Log Parser** | Kubernetes label extraction, message normalization, fingerprinting |
| **Error Handler** | Callback interface for downstream processing |

### 1.3 Data Flow

```
+------------------+    LogQL      +------------------+
|   Loki Server    | <------------ |   Loki Client    |
|                  |               |                  |
|  Stores logs     |  JSON/HTTP    |  QueryRange()    |
|  from Promtail   | ------------> |  Query()         |
+------------------+               +------------------+
                                           |
                                           | []LogEntry
                                           v
                                   +------------------+
                                   |   Log Poller     |
                                   |                  |
                                   |  - parseEntry()  |
                                   |  - deduplicate() |
                                   |  - callback()    |
                                   +------------------+
                                           |
                                           | []ParsedError
                                           v
                                   +------------------+
                                   |  Error Handler   |
                                   |  (Rule Engine)   |
                                   +------------------+
```

---

## 2. Loki API Client Architecture

### 2.1 Client Structure

The Loki client encapsulates all HTTP communication with the Loki API server:

```go
type Client struct {
    baseURL    string
    httpClient *http.Client
    tenantID   string
    username   string
    password   string
}
```

**Field Descriptions:**

| Field | Purpose |
|-------|---------|
| `baseURL` | Base URL of Loki server (e.g., `http://loki.monitoring:3100`) |
| `httpClient` | Configurable HTTP client with timeout settings |
| `tenantID` | Multi-tenant Loki support via X-Scope-OrgID header |
| `username` | Basic authentication username |
| `password` | Basic authentication password |

### 2.2 Functional Options Pattern

The client uses the functional options pattern for flexible configuration:

```go
// NewClient creates a new Loki client
func NewClient(baseURL string, opts ...ClientOption) *Client {
    c := &Client{
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
    }

    for _, opt := range opts {
        opt(c)
    }

    return c
}
```

**Available Options:**

```go
// WithTenantID sets the X-Scope-OrgID header for multi-tenant Loki
func WithTenantID(tenantID string) ClientOption {
    return func(c *Client) {
        c.tenantID = tenantID
    }
}

// WithBasicAuth sets basic authentication credentials
func WithBasicAuth(username, password string) ClientOption {
    return func(c *Client) {
        c.username = username
        c.password = password
    }
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
    return func(c *Client) {
        c.httpClient = httpClient
    }
}
```

**Usage Example:**

```go
client := loki.NewClient(
    "http://loki.monitoring:3100",
    loki.WithTenantID("my-tenant"),
    loki.WithBasicAuth("user", "secret"),
    loki.WithHTTPClient(&http.Client{
        Timeout: 60 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 10,
        },
    }),
)
```

### 2.3 Response Data Structures

The client defines structured types for Loki API responses:

```go
// QueryResponse represents the response from Loki query API
type QueryResponse struct {
    Status string     `json:"status"`
    Data   QueryData  `json:"data"`
}

// QueryData holds the result data from a query
type QueryData struct {
    ResultType string   `json:"resultType"`
    Result     []Stream `json:"result"`
}

// Stream represents a log stream from Loki
type Stream struct {
    Stream map[string]string `json:"stream"`   // Label set
    Values [][]string        `json:"values"`   // [timestamp_ns, log_line]
}

// LogEntry represents a parsed log entry
type LogEntry struct {
    Timestamp time.Time
    Labels    map[string]string
    Line      string
}
```

---

## 3. LogQL Query Construction and Configuration

### 3.1 Query Anatomy

Loki uses LogQL, a query language inspired by PromQL. The default query configured for Kube Sentinel:

```logql
{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"
```

**Query Breakdown:**

| Component | Meaning |
|-----------|---------|
| `{namespace=~".+"}` | Stream selector: match all namespaces |
| `\|~` | Filter operator: regex match |
| `"(?i)..."` | Case-insensitive regex pattern |
| `error\|fatal\|panic\|exception\|fail` | Common error keywords |

### 3.2 Query Configuration

The query is configured via the ConfigMap:

```yaml
loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'
  poll_interval: 30s
  lookback: 5m
```

### 3.3 Advanced Query Examples

**Filter by specific namespaces:**
```logql
{namespace=~"production|staging"} |~ "(?i)error"
```

**Exclude system namespaces:**
```logql
{namespace!~"kube-system|kube-public"} |~ "(?i)(error|fatal)"
```

**Target specific applications:**
```logql
{namespace="production", app="api-server"} |~ "(?i)exception"
```

**Include severity levels:**
```logql
{namespace=~".+"} |~ "level=(error|fatal|panic)"
```

### 3.4 Query Parameter Encoding

The client properly encodes query parameters for the Loki API:

```go
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) ([]LogEntry, error) {
    params := url.Values{}
    params.Set("query", query)
    params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
    params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
    params.Set("limit", strconv.Itoa(limit))
    params.Set("direction", "backward")

    reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())
    // ...
}
```

**Parameter Details:**

| Parameter | Format | Purpose |
|-----------|--------|---------|
| `query` | URL-encoded LogQL | The query to execute |
| `start` | Unix nanoseconds | Start of time range |
| `end` | Unix nanoseconds | End of time range |
| `limit` | Integer | Maximum entries to return |
| `direction` | `forward`/`backward` | Sort order by timestamp |

---

## 4. Polling Intervals and Lookback Windows

### 4.1 Poller Configuration

The poller manages continuous log fetching with configurable timing:

```go
type Poller struct {
    client       *Client
    query        string
    pollInterval time.Duration   // Time between polls
    lookback     time.Duration   // How far back to query
    handler      ErrorHandler
    logger       *slog.Logger

    // Deduplication state
    mu            sync.RWMutex
    seenErrors    map[string]time.Time
    windowSize    time.Duration
    lastPollEnd   time.Time
}
```

### 4.2 Timing Configuration

```
Timeline Visualization:

    |<-------- lookback (5m) -------->|
    |                                 |
----+---+---+---+---+---+---+---+---+-+---> time
    |   |       |       |       |   |
 start  |    poll 1   poll 2  poll 3 end (now)
        |
    lastPollEnd (optimization)
```

**Configuration Example:**

```yaml
loki:
  poll_interval: 30s    # Poll every 30 seconds
  lookback: 5m          # Look back 5 minutes on each poll
```

### 4.3 Intelligent Time Window Management

The poller optimizes queries by tracking the last successful poll:

```go
func (p *Poller) poll(ctx context.Context) error {
    end := time.Now()
    start := end.Add(-p.lookback)

    // If we have a last poll time, use that to avoid re-fetching
    if !p.lastPollEnd.IsZero() && p.lastPollEnd.After(start) {
        start = p.lastPollEnd
    }

    p.logger.Debug("polling loki", "start", start, "end", end)

    entries, err := p.client.QueryRange(ctx, p.query, start, end, 1000)
    if err != nil {
        return fmt.Errorf("querying loki: %w", err)
    }

    p.lastPollEnd = end
    // Process entries...
}
```

**Optimization Benefits:**

| Scenario | Without Optimization | With Optimization |
|----------|---------------------|-------------------|
| First poll | Query 5 minutes | Query 5 minutes |
| Subsequent polls (30s interval) | Query 5 minutes | Query ~30 seconds |
| Data transfer | High redundancy | Minimal redundancy |
| Loki load | Higher | Lower |

### 4.4 Polling Loop Implementation

```go
func (p *Poller) Start(ctx context.Context) error {
    p.logger.Info("starting loki poller",
        "query", p.query,
        "poll_interval", p.pollInterval,
        "lookback", p.lookback,
    )

    // Do an initial poll immediately
    if err := p.poll(ctx); err != nil {
        p.logger.Error("initial poll failed", "error", err)
    }

    ticker := time.NewTicker(p.pollInterval)
    defer ticker.Stop()

    cleanupTicker := time.NewTicker(5 * time.Minute)
    defer cleanupTicker.Stop()

    for {
        select {
        case <-ctx.Done():
            p.logger.Info("stopping loki poller")
            return ctx.Err()

        case <-ticker.C:
            if err := p.poll(ctx); err != nil {
                p.logger.Error("poll failed", "error", err)
            }

        case <-cleanupTicker.C:
            p.cleanupSeenErrors()
        }
    }
}
```

**Loop Features:**

1. **Immediate first poll**: Doesn't wait for first interval
2. **Graceful shutdown**: Responds to context cancellation
3. **Error resilience**: Logs errors but continues polling
4. **Memory cleanup**: Periodic deduplication cache cleanup

---

## 5. Log Parsing and Kubernetes Label Extraction

### 5.1 Parsed Error Structure

```go
type ParsedError struct {
    ID          string              // Unique identifier
    Fingerprint string              // Deduplication key
    Timestamp   time.Time           // When the error occurred
    Namespace   string              // Kubernetes namespace
    Pod         string              // Pod name
    Container   string              // Container name
    Message     string              // Extracted error message
    Labels      map[string]string   // All Loki labels
    Raw         string              // Original log line
}
```

### 5.2 Kubernetes Label Extraction

Labels are extracted from the Loki stream metadata (set by Promtail):

```go
func (p *Poller) parseEntry(entry LogEntry) *ParsedError {
    namespace := entry.Labels["namespace"]
    pod := entry.Labels["pod"]
    container := entry.Labels["container"]

    // Try to extract structured info from the log line
    message := extractMessage(entry.Line)

    // Generate fingerprint for deduplication
    fingerprint := generateFingerprint(namespace, pod, container, message)

    return &ParsedError{
        ID:          generateID(),
        Fingerprint: fingerprint,
        Timestamp:   entry.Timestamp,
        Namespace:   namespace,
        Pod:         pod,
        Container:   container,
        Message:     message,
        Labels:      entry.Labels,
        Raw:         entry.Line,
    }
}
```

**Common Loki Labels from Promtail:**

| Label | Source | Example |
|-------|--------|---------|
| `namespace` | Kubernetes metadata | `production` |
| `pod` | Kubernetes metadata | `api-server-7d4f8b9c5d-abc12` |
| `container` | Kubernetes metadata | `api-server` |
| `node_name` | Kubernetes metadata | `node-1` |
| `stream` | Log stream | `stderr` |
| `app` | Pod label | `api-server` |

### 5.3 Message Extraction

The parser attempts to extract clean error messages from various log formats:

```go
func extractMessage(line string) string {
    // Try to extract JSON message field
    if msg := extractJSONField(line, "message", "msg", "error", "err"); msg != "" {
        return msg
    }

    // Try to extract common log format message
    patterns := []*regexp.Regexp{
        regexp.MustCompile(`(?i)\b(?:error|fatal|panic|exception|fail(?:ed|ure)?)\b[:\s]+(.+)`),
        regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[^\s]*\s+\w+\s+(.+)`),
    }

    for _, pattern := range patterns {
        if matches := pattern.FindStringSubmatch(line); len(matches) > 1 {
            return strings.TrimSpace(matches[1])
        }
    }

    // Return the whole line, truncated
    if len(line) > 500 {
        return line[:500] + "..."
    }
    return line
}
```

**Supported Log Formats:**

```
JSON structured logging:
{"level":"error","message":"connection failed","timestamp":"2024-01-15T10:00:00Z"}
                     ^-- extracted

Standard log format:
2024-01-15 10:00:00 ERROR connection failed to database
                          ^-- extracted

Error prefix format:
error: failed to connect to service
       ^-- extracted

Plain text (fallback):
Some error occurred in the application
^-- returned as-is (truncated if > 500 chars)
```

### 5.4 JSON Field Extraction

Optimized regex-based JSON field extraction (faster than full JSON parsing):

```go
func extractJSONField(line string, fields ...string) string {
    for _, field := range fields {
        // Simple regex-based extraction
        pattern := regexp.MustCompile(fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, field))
        if matches := pattern.FindStringSubmatch(line); len(matches) > 1 {
            return matches[1]
        }
    }
    return ""
}
```

---

## 6. Deduplication and Fingerprinting

### 6.1 Fingerprint Generation

Fingerprints enable intelligent deduplication by grouping similar errors:

```go
func generateFingerprint(namespace, pod, container, message string) string {
    // Normalize pod name by removing random suffix
    podBase := normalizePodName(pod)

    // Normalize message by removing variable parts
    normalizedMsg := normalizeMessage(message)

    data := fmt.Sprintf("%s|%s|%s|%s", namespace, podBase, container, normalizedMsg)
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:8])
}
```

### 6.2 Pod Name Normalization

Removes dynamic suffixes to group errors from the same workload:

```go
func normalizePodName(pod string) string {
    // Match deployment pods: name-<replicaset-hash>-<pod-hash>
    deployPattern := regexp.MustCompile(`^(.+)-[a-z0-9]{8,10}-[a-z0-9]{5}$`)
    if matches := deployPattern.FindStringSubmatch(pod); len(matches) > 1 {
        return matches[1]
    }

    // Match statefulset pods: name-<ordinal>
    stsPattern := regexp.MustCompile(`^(.+)-\d+$`)
    if matches := stsPattern.FindStringSubmatch(pod); len(matches) > 1 {
        return matches[1]
    }

    // Match job pods: name-<random>
    jobPattern := regexp.MustCompile(`^(.+)-[a-z0-9]{5}$`)
    if matches := jobPattern.FindStringSubmatch(pod); len(matches) > 1 {
        return matches[1]
    }

    return pod
}
```

**Normalization Examples:**

| Original Pod Name | Normalized |
|-------------------|------------|
| `api-server-7d4f8b9c5d-abc12` | `api-server` |
| `api-server-6f8b7c4d3e-xyz99` | `api-server` |
| `redis-0` | `redis` |
| `redis-1` | `redis` |
| `backup-job-x9k2m` | `backup-job` |

### 6.3 Message Normalization

Removes variable components to match semantically similar errors:

```go
func normalizeMessage(message string) string {
    // Remove timestamps
    msg := regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[.\d]*[Z]?`).
        ReplaceAllString(message, "")

    // Remove UUIDs
    msg = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).
        ReplaceAllString(msg, "<UUID>")

    // Remove hex IDs
    msg = regexp.MustCompile(`\b[a-f0-9]{24,}\b`).ReplaceAllString(msg, "<ID>")

    // Remove IP addresses
    msg = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?`).
        ReplaceAllString(msg, "<IP>")

    // Remove numbers (but keep error codes like 404, 500)
    msg = regexp.MustCompile(`\b\d{6,}\b`).ReplaceAllString(msg, "<NUM>")

    return strings.TrimSpace(msg)
}
```

**Normalization Examples:**

| Original Message | Normalized |
|------------------|------------|
| `Connection to 192.168.1.100:5432 failed` | `Connection to <IP> failed` |
| `Request abc12def-3456-7890-abcd-ef1234567890 failed` | `Request <UUID> failed` |
| `User 5f4dcc3b5aa765d61d8327deb882cf99 not found` | `User <ID> not found` |
| `Processed 1234567 records` | `Processed <NUM> records` |

### 6.4 Deduplication Window

The poller maintains a sliding window for deduplication:

```go
func (p *Poller) isNew(fingerprint string) bool {
    p.mu.RLock()
    defer p.mu.RUnlock()
    _, seen := p.seenErrors[fingerprint]
    return !seen
}

func (p *Poller) markSeen(fingerprint string) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.seenErrors[fingerprint] = time.Now()
}

func (p *Poller) cleanupSeenErrors() {
    p.mu.Lock()
    defer p.mu.Unlock()

    cutoff := time.Now().Add(-p.windowSize)
    for fp, seenAt := range p.seenErrors {
        if seenAt.Before(cutoff) {
            delete(p.seenErrors, fp)
        }
    }

    p.logger.Debug("cleaned up seen errors", "remaining", len(p.seenErrors))
}
```

**Deduplication Flow:**

```
                       +------------------+
                       |  Log Entry       |
                       +------------------+
                               |
                               v
                       +------------------+
                       |  Parse Entry     |
                       +------------------+
                               |
                               v
                       +------------------+
                       | Generate         |
                       | Fingerprint      |
                       +------------------+
                               |
                               v
                    +--------------------+
                    | Fingerprint seen?  |
                    +--------------------+
                     /                \
                  Yes                  No
                   |                    |
                   v                    v
            +-------------+     +------------------+
            | Skip entry  |     | Mark as seen     |
            +-------------+     +------------------+
                                        |
                                        v
                                +------------------+
                                | Forward to       |
                                | Error Handler    |
                                +------------------+
```

---

## 7. Connection Handling and Authentication

### 7.1 HTTP Request Configuration

All requests are configured with proper headers and authentication:

```go
func (c *Client) setHeaders(req *http.Request) {
    req.Header.Set("Accept", "application/json")

    if c.tenantID != "" {
        req.Header.Set("X-Scope-OrgID", c.tenantID)
    }

    if c.username != "" && c.password != "" {
        req.SetBasicAuth(c.username, c.password)
    }
}
```

### 7.2 Authentication Methods

**Basic Authentication:**
```yaml
loki:
  url: http://loki.monitoring:3100
  username: kube-sentinel
  password: ${LOKI_PASSWORD}  # From secret
```

**Multi-Tenant Support:**
```yaml
loki:
  url: http://loki.monitoring:3100
  tenant_id: my-organization
```

### 7.3 Connection Timeout Configuration

Default timeout is 30 seconds, configurable via custom HTTP client:

```go
func NewClient(baseURL string, opts ...ClientOption) *Client {
    c := &Client{
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
    // ...
}
```

**Custom Timeout Configuration:**

```go
customClient := &http.Client{
    Timeout: 60 * time.Second,
    Transport: &http.Transport{
        DialContext: (&net.Dialer{
            Timeout:   10 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
    },
}

client := loki.NewClient(
    "http://loki.monitoring:3100",
    loki.WithHTTPClient(customClient),
)
```

### 7.4 Health Check

The client provides a readiness check:

```go
func (c *Client) Ready(ctx context.Context) error {
    reqURL := fmt.Sprintf("%s/ready", c.baseURL)

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }

    c.setHeaders(req)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("executing request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("loki not ready, status: %d", resp.StatusCode)
    }

    return nil
}
```

---

## 8. Error Handling and Resilience

### 8.1 Query Error Handling

The client handles various error scenarios:

```go
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) ([]LogEntry, error) {
    // ... build request ...

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("executing request: %w", err)
    }
    defer resp.Body.Close()

    // Handle non-200 responses
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(body))
    }

    var queryResp QueryResponse
    if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
        return nil, fmt.Errorf("decoding response: %w", err)
    }

    // Handle Loki-level errors
    if queryResp.Status != "success" {
        return nil, fmt.Errorf("query failed with status: %s", queryResp.Status)
    }

    return c.parseStreams(queryResp.Data.Result), nil
}
```

**Error Categories:**

| Error Type | Handling | Example |
|------------|----------|---------|
| Network errors | Wrapped with context | `executing request: dial tcp: timeout` |
| HTTP errors | Status code + body | `loki returned status 429: rate limit exceeded` |
| JSON decode errors | Wrapped with context | `decoding response: unexpected EOF` |
| Loki errors | Status field check | `query failed with status: error` |

### 8.2 Resilient Polling

The polling loop continues despite individual failures:

```go
for {
    select {
    case <-ctx.Done():
        p.logger.Info("stopping loki poller")
        return ctx.Err()

    case <-ticker.C:
        if err := p.poll(ctx); err != nil {
            p.logger.Error("poll failed", "error", err)
            // Note: Does NOT return - continues polling
        }
    }
}
```

### 8.3 Graceful Degradation

```
+------------------------------------------------------------------+
|                    Resilience Strategy                            |
+------------------------------------------------------------------+

Poll Failure Handling:
+------------------+     +------------------+     +------------------+
| Poll attempt 1   |---->| Log error        |---->| Wait interval    |
| (fails)          |     |                  |     |                  |
+------------------+     +------------------+     +------------------+
                                                          |
                                                          v
+------------------+     +------------------+     +------------------+
| Poll attempt 2   |---->| Success!         |---->| Process entries  |
| (succeeds)       |     |                  |     |                  |
+------------------+     +------------------+     +------------------+

Benefits:
- Temporary network issues don't stop monitoring
- Loki restarts are handled gracefully
- No data loss if Loki catches up (within retention)
```

### 8.4 Stream Parsing Resilience

The parser handles malformed data gracefully:

```go
func (c *Client) parseStreams(streams []Stream) []LogEntry {
    var entries []LogEntry

    for _, stream := range streams {
        for _, value := range stream.Values {
            // Skip malformed entries
            if len(value) < 2 {
                continue
            }

            timestampNs, err := strconv.ParseInt(value[0], 10, 64)
            if err != nil {
                continue  // Skip entries with invalid timestamps
            }

            entries = append(entries, LogEntry{
                Timestamp: time.Unix(0, timestampNs),
                Labels:    stream.Stream,
                Line:      value[1],
            })
        }
    }

    return entries
}
```

---

## 9. Configuration Reference

### 9.1 Complete Configuration Example

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-sentinel-config
  namespace: kube-sentinel
data:
  config.yaml: |
    loki:
      # Loki server URL
      url: http://loki.monitoring:3100

      # LogQL query for error detection
      query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'

      # How often to poll Loki
      poll_interval: 30s

      # How far back to look on each poll
      lookback: 5m

      # Multi-tenant Loki support (optional)
      tenant_id: ""

      # Basic authentication (optional)
      username: ""
      password: ""
```

### 9.2 Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `url` | string | required | Loki server base URL |
| `query` | string | required | LogQL query for error detection |
| `poll_interval` | duration | `30s` | Time between poll cycles |
| `lookback` | duration | `5m` | Time window to query on each poll |
| `tenant_id` | string | `""` | X-Scope-OrgID header value |
| `username` | string | `""` | Basic auth username |
| `password` | string | `""` | Basic auth password |

### 9.3 Environment Variables

Sensitive values can be injected via environment variables:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel
spec:
  template:
    spec:
      containers:
      - name: kube-sentinel
        env:
        - name: LOKI_PASSWORD
          valueFrom:
            secretKeyRef:
              name: kube-sentinel-secrets
              key: loki-password
```

### 9.4 Tuning Guidelines

**Poll Interval Tuning:**

| Scenario | Recommended Interval | Rationale |
|----------|---------------------|-----------|
| Production critical | 15-30s | Fast detection of issues |
| General monitoring | 30-60s | Balance between speed and load |
| Low-traffic clusters | 60-120s | Reduce Loki load |

**Lookback Window Tuning:**

| Scenario | Recommended Lookback | Rationale |
|----------|---------------------|-----------|
| Standard | 2-3x poll interval | Handles minor delays |
| Unreliable network | 5-10x poll interval | Recovers from outages |
| High-volume logs | Equal to poll interval | Reduces duplicate processing |

---

## 10. Operational Considerations

### 10.1 Loki Requirements

**Minimum Loki Configuration:**

```yaml
# Loki config.yaml
auth_enabled: false  # Or configure tenant_id

limits_config:
  max_entries_limit_per_query: 5000
  max_query_length: 721h
  max_query_parallelism: 32

query_range:
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
```

### 10.2 Promtail Label Configuration

Ensure Promtail is configured to extract Kubernetes labels:

```yaml
# Promtail config
scrape_configs:
  - job_name: kubernetes-pods
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_namespace]
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: pod
      - source_labels: [__meta_kubernetes_pod_container_name]
        target_label: container
```

### 10.3 Resource Considerations

**Memory Usage:**

The deduplication cache grows based on error variety:

```
Memory per fingerprint: ~100 bytes
Default window: 30 minutes
Example: 1000 unique errors/hour = ~100KB
```

**Network Usage:**

```
Per poll: ~1-50KB (depends on log volume)
At 30s interval: ~2-100KB/minute
Daily: ~3-150MB
```

### 10.4 Monitoring the Integration

Key metrics to monitor:

```
# Successful polls
kube_sentinel_loki_polls_total{status="success"}

# Failed polls
kube_sentinel_loki_polls_total{status="error"}

# Entries processed
kube_sentinel_loki_entries_total

# New errors detected
kube_sentinel_errors_detected_total

# Deduplicated (skipped) errors
kube_sentinel_errors_deduplicated_total
```

---

## 11. Troubleshooting

### 11.1 Common Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| No errors detected | Query too restrictive | Broaden query pattern |
| Too many duplicates | Window too short | Increase `windowSize` |
| High memory usage | Many unique errors | Reduce dedup window |
| Poll timeouts | Loki overloaded | Increase HTTP timeout |
| Authentication failures | Wrong credentials | Check username/password |
| "rate limit exceeded" | Too frequent polling | Increase poll interval |

### 11.2 Debugging Commands

```bash
# Test Loki connectivity
kubectl exec -n kube-sentinel deployment/kube-sentinel -- \
  curl -s http://loki.monitoring:3100/ready

# Test query manually
kubectl exec -n kube-sentinel deployment/kube-sentinel -- \
  curl -s 'http://loki.monitoring:3100/loki/api/v1/query_range' \
    --data-urlencode 'query={namespace=~".+"}' \
    --data-urlencode "start=$(date -d '5 minutes ago' +%s)000000000" \
    --data-urlencode "end=$(date +%s)000000000" \
    --data-urlencode 'limit=10' | jq .

# Check poller logs
kubectl logs -n kube-sentinel deployment/kube-sentinel | grep -i loki

# View recent errors via API
curl http://localhost:8080/kube-sentinel/api/errors
```

### 11.3 Log Messages Reference

| Log Message | Level | Meaning |
|-------------|-------|---------|
| `starting loki poller` | INFO | Poller initialized |
| `polling loki` | DEBUG | Poll cycle started |
| `received log entries` | DEBUG | Entries retrieved |
| `found new errors` | INFO | New unique errors detected |
| `poll failed` | ERROR | Query failed (will retry) |
| `cleaned up seen errors` | DEBUG | Dedup cache pruned |
| `stopping loki poller` | INFO | Graceful shutdown |

---

## 12. Summary

The Kube Sentinel Loki integration provides:

1. **Efficient Polling**: Optimized time windows minimize redundant data transfer
2. **Flexible Queries**: LogQL support for precise error filtering
3. **Smart Deduplication**: Fingerprint-based grouping of similar errors
4. **Kubernetes-Native**: Automatic extraction of namespace, pod, and container labels
5. **Resilient Operation**: Graceful handling of failures and network issues
6. **Secure Authentication**: Support for basic auth and multi-tenant Loki

The system is designed to operate reliably in production Kubernetes environments while minimizing load on the Loki infrastructure.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
