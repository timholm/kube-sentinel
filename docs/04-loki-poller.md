# Loki Poller

The Loki Poller is a core component of Kube Sentinel responsible for continuously monitoring log streams from Grafana Loki, parsing error logs, and delivering deduplicated error events to downstream handlers for prioritization and remediation.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [How the Poller Works](#how-the-poller-works)
- [Polling Intervals and the Polling Loop](#polling-intervals-and-the-polling-loop)
- [Deduplication Logic and Fingerprinting](#deduplication-logic-and-fingerprinting)
- [Log Parsing and Message Extraction](#log-parsing-and-message-extraction)
- [Common Failure Scenarios](#common-failure-scenarios)
- [Configuration Options](#configuration-options)
- [Future Enhancements](#future-enhancements)

## Overview

The Loki Poller bridges Grafana Loki's log aggregation capabilities with Kube Sentinel's error prioritization and remediation system. It performs three primary functions:

1. **Continuous Polling**: Executes LogQL queries against Loki at configurable intervals
2. **Smart Deduplication**: Prevents alert fatigue by fingerprinting and tracking seen errors
3. **Log Parsing**: Extracts structured error information from various log formats

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Loki Poller                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────┐    ┌──────────────┐    ┌───────────────────┐    │
│   │  Poll Loop   │───▶│  Log Parser  │───▶│  Deduplication    │    │
│   │              │    │              │    │  Engine           │    │
│   │ - Ticker     │    │ - JSON       │    │                   │    │
│   │ - Lookback   │    │ - Regex      │    │ - Fingerprint     │    │
│   │ - Windowing  │    │ - Normalize  │    │ - Time Window     │    │
│   └──────────────┘    └──────────────┘    └───────────────────┘    │
│          │                                         │                │
│          ▼                                         ▼                │
│   ┌──────────────┐                        ┌───────────────────┐    │
│   │ Loki Client  │                        │  Error Handler    │    │
│   │ (HTTP API)   │                        │  (Callback)       │    │
│   └──────────────┘                        └───────────────────┘    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
         │                                           │
         ▼                                           ▼
    ┌─────────┐                              ┌─────────────────┐
    │  Loki   │                              │  Rule Engine /  │
    │  API    │                              │  Remediator     │
    └─────────┘                              └─────────────────┘
```

## How the Poller Works

### Initialization

The poller is created using the `NewPoller` constructor, which accepts:

- A configured Loki `Client` for API communication
- A LogQL query string defining which logs to monitor
- Poll interval and lookback duration parameters
- An `ErrorHandler` callback function for processing discovered errors
- Optional configuration via functional options

```go
poller := loki.NewPoller(
    client,
    `{namespace=~".+"} |~ "(?i)(error|fatal|panic)"`,
    30*time.Second,  // poll interval
    5*time.Minute,   // lookback window
    handleErrors,
    loki.WithLogger(logger),
    loki.WithWindowSize(30*time.Minute),
)
```

### Startup Sequence

When `Start(ctx)` is called:

1. The poller logs its configuration (query, poll interval, lookback)
2. An **immediate initial poll** is executed to catch any existing errors
3. Two ticker goroutines are started:
   - **Poll ticker**: Fires at the configured poll interval
   - **Cleanup ticker**: Fires every 5 minutes to prune the deduplication cache

### Data Flow

For each poll cycle:

1. **Time Window Calculation**: Determine the query time range using lookback duration and last poll timestamp
2. **Loki Query**: Execute a range query via the Loki HTTP API
3. **Entry Parsing**: Transform raw log entries into structured `ParsedError` objects
4. **Deduplication Check**: Compare each error's fingerprint against the seen cache
5. **Handler Invocation**: Pass new (unseen) errors to the registered handler
6. **Cache Update**: Mark processed fingerprints as seen with current timestamp

## Polling Intervals and the Polling Loop

### Poll Interval

The poll interval determines how frequently the poller queries Loki for new errors. This value should be tuned based on:

- **Latency requirements**: Lower intervals mean faster error detection
- **Loki load**: Higher intervals reduce query load on Loki
- **Log volume**: High-volume environments may need shorter intervals to avoid missing the query limit

Typical values range from 10 seconds to 2 minutes, with 30 seconds being a reasonable default.

### Lookback Duration

The lookback duration defines how far back in time each query extends. This creates an overlap with previous queries to ensure no logs are missed due to timing issues or query latency.

```
Time ─────────────────────────────────────────────────────────────▶

Poll 1:  [========lookback========]
                                  │
Poll 2:          [========lookback========]
                                          │
Poll 3:                  [========lookback========]
```

### Intelligent Windowing

The poller implements smart time windowing to optimize queries:

```go
if !p.lastPollEnd.IsZero() && p.lastPollEnd.After(start) {
    start = p.lastPollEnd
}
```

After the first poll, subsequent queries start from where the last poll ended, reducing redundant data transfer while maintaining the lookback as a safety net for the initial poll or after restarts.

### Poll Loop Structure

The main loop uses Go's `select` statement to handle multiple event sources:

```go
for {
    select {
    case <-ctx.Done():
        // Graceful shutdown
        return ctx.Err()

    case <-ticker.C:
        // Regular poll cycle
        if err := p.poll(ctx); err != nil {
            p.logger.Error("poll failed", "error", err)
        }

    case <-cleanupTicker.C:
        // Periodic cache maintenance
        p.cleanupSeenErrors()
    }
}
```

Key characteristics:
- **Context-aware**: Responds immediately to cancellation signals
- **Non-blocking**: Poll failures are logged but do not halt the loop
- **Self-maintaining**: Automatic cleanup prevents memory growth

## Deduplication Logic and Fingerprinting

### The Deduplication Problem

Kubernetes errors often repeat rapidly. A single failing pod can generate hundreds of identical error logs within minutes. Without deduplication, this would result in:

- Alert fatigue from repeated notifications
- Unnecessary remediation attempts
- Database/storage bloat from duplicate records

### Fingerprint Generation

The poller creates unique fingerprints by hashing normalized error attributes:

```go
func generateFingerprint(namespace, pod, container, message string) string {
    podBase := normalizePodName(pod)
    normalizedMsg := normalizeMessage(message)

    data := fmt.Sprintf("%s|%s|%s|%s", namespace, podBase, container, normalizedMsg)
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:8])
}
```

The fingerprint is composed of:

| Component | Description | Example |
|-----------|-------------|---------|
| Namespace | Kubernetes namespace | `production` |
| Pod Base Name | Pod name with random suffix removed | `api-server` (from `api-server-7d4f8b9c5d-abc12`) |
| Container | Container name within the pod | `app` |
| Normalized Message | Message with variable parts removed | `connection refused to <IP>` |

### Pod Name Normalization

Pod names in Kubernetes include random suffixes that differ across restarts. The poller normalizes these to group errors from the same workload:

| Pattern | Example Input | Normalized Output |
|---------|---------------|-------------------|
| Deployment | `my-app-7d4f8b9c5d-abc12` | `my-app` |
| StatefulSet | `redis-master-0` | `redis-master` |
| Job | `backup-job-x9k2m` | `backup-job` |

### Message Normalization

Variable parts of error messages are replaced with placeholders:

| Pattern | Example | Replacement |
|---------|---------|-------------|
| Timestamps | `2024-01-15T10:30:00Z` | (removed) |
| UUIDs | `550e8400-e29b-41d4-a716-446655440000` | `<UUID>` |
| Hex IDs | `507f1f77bcf86cd799439011` | `<ID>` |
| IP Addresses | `192.168.1.100:8080` | `<IP>` |
| Large Numbers | `1705312200000` | `<NUM>` |

This ensures that errors like "Connection to 10.0.0.1:5432 failed" and "Connection to 10.0.0.2:5432 failed" produce the same fingerprint.

### Time-Windowed Deduplication

The deduplication cache uses a sliding time window:

```go
type Poller struct {
    seenErrors    map[string]time.Time  // fingerprint -> first seen time
    windowSize    time.Duration          // default: 30 minutes
}
```

- Errors are tracked for the duration of the window (default 30 minutes)
- After the window expires, the same error can be reported again
- This balances noise reduction with ensuring persistent issues remain visible

### Cache Cleanup

A background routine runs every 5 minutes to remove expired entries:

```go
func (p *Poller) cleanupSeenErrors() {
    cutoff := time.Now().Add(-p.windowSize)
    for fp, seenAt := range p.seenErrors {
        if seenAt.Before(cutoff) {
            delete(p.seenErrors, fp)
        }
    }
}
```

### Thread Safety

All cache operations are protected by a read-write mutex:

- `isNew()`: Uses `RLock` for concurrent read access
- `markSeen()`: Uses `Lock` for exclusive write access
- `cleanupSeenErrors()`: Uses `Lock` for safe iteration and deletion

## Log Parsing and Message Extraction

### Parsing Pipeline

Each log entry passes through a multi-stage parsing pipeline:

```
Raw Log Line
     │
     ▼
┌─────────────────────┐
│ Label Extraction    │  Extract namespace, pod, container from Loki labels
└─────────────────────┘
     │
     ▼
┌─────────────────────┐
│ Message Extraction  │  Parse message from JSON or regex patterns
└─────────────────────┘
     │
     ▼
┌─────────────────────┐
│ Fingerprint Gen     │  Create deduplication fingerprint
└─────────────────────┘
     │
     ▼
┌─────────────────────┐
│ ID Generation       │  Create unique identifier for this occurrence
└─────────────────────┘
     │
     ▼
ParsedError struct
```

### ParsedError Structure

```go
type ParsedError struct {
    ID          string            // Unique identifier for this error instance
    Fingerprint string            // Deduplication key
    Timestamp   time.Time         // When the error occurred
    Namespace   string            // Kubernetes namespace
    Pod         string            // Pod name
    Container   string            // Container name
    Message     string            // Extracted error message
    Labels      map[string]string // All Loki labels
    Raw         string            // Original log line
}
```

### Message Extraction Strategies

The poller attempts multiple extraction methods in order:

#### 1. JSON Field Extraction

For structured JSON logs, the poller searches for common message fields:

```go
extractJSONField(line, "message", "msg", "error", "err")
```

Example input:
```json
{"level":"error","message":"Database connection failed","timestamp":"2024-01-15T10:30:00Z"}
```

Extracted message: `Database connection failed`

#### 2. Regex Pattern Matching

For semi-structured logs, regex patterns extract the message portion:

**Error Keyword Pattern:**
```
(?i)\b(?:error|fatal|panic|exception|fail(?:ed|ure)?)\b[:\s]+(.+)
```

Matches lines like:
- `ERROR: Something went wrong`
- `Fatal: Cannot connect to database`
- `PANIC: nil pointer dereference`

**Timestamp-Prefixed Pattern:**
```
\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[^\s]*\s+\w+\s+(.+)
```

Matches lines like:
- `2024-01-15 10:30:00 ERROR Database timeout`
- `2024-01-15T10:30:00Z FATAL OOM killed`

#### 3. Fallback: Raw Line

If no patterns match, the entire log line is used as the message, truncated to 500 characters if necessary:

```go
if len(line) > 500 {
    return line[:500] + "..."
}
return line
```

## Common Failure Scenarios

### Loki Connection Failures

**Symptoms:**
- Poll errors logged with "querying loki" prefix
- No new errors appearing in the dashboard

**Causes:**
- Loki service unavailable
- Network connectivity issues
- Authentication failures (for secured Loki instances)

**Behavior:**
- The poller logs the error and continues the polling loop
- The next poll cycle will retry automatically
- No errors are lost due to windowed lookback

**Handling:**
```go
if err := p.poll(ctx); err != nil {
    p.logger.Error("poll failed", "error", err)
    // Loop continues, retry on next tick
}
```

### Query Timeout

**Symptoms:**
- Context deadline exceeded errors
- Incomplete result sets

**Causes:**
- Query too broad (matches too many logs)
- Loki under heavy load
- Network latency

**Behavior:**
- Current poll fails, logged as error
- Next poll uses the same time window (no data loss)
- Lookback ensures coverage after recovery

### Memory Pressure from Cache Growth

**Symptoms:**
- Increasing memory usage over time
- Potential OOM conditions

**Causes:**
- Very high error volume with unique fingerprints
- Cleanup not running (should not happen in normal operation)

**Behavior:**
- Cleanup runs every 5 minutes regardless of poll success
- Entries older than window size are automatically removed

### Handler Panics

**Symptoms:**
- Poller crashes unexpectedly
- Error processing stops

**Causes:**
- Bug in downstream error handler
- Resource exhaustion in handler

**Behavior:**
- Currently, panics in the handler will propagate up
- The poller does not wrap handler calls in recovery logic

**Recommendation:** Handlers should implement their own panic recovery.

### Rate Limiting by Loki

**Symptoms:**
- HTTP 429 responses
- "too many requests" errors

**Causes:**
- Poll interval too aggressive
- Query matches excessive log volume
- Loki configured with strict rate limits

**Behavior:**
- Error logged, poll continues on next interval
- No automatic backoff (see Future Enhancements)

## Configuration Options

### Poller Constructor Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `client` | `*Client` | Configured Loki HTTP client |
| `query` | `string` | LogQL query for filtering logs |
| `pollInterval` | `time.Duration` | Time between poll cycles |
| `lookback` | `time.Duration` | How far back to query |
| `handler` | `ErrorHandler` | Callback for new errors |

### Functional Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithLogger(logger)` | `slog.Default()` | Custom structured logger |
| `WithWindowSize(duration)` | `30 * time.Minute` | Deduplication window size |

### Example Configuration

```go
// Create Loki client
client := loki.NewClient(
    "http://loki.monitoring:3100",
    loki.WithTenantID("my-tenant"),
)

// Create poller with custom settings
poller := loki.NewPoller(
    client,
    `{namespace=~"production|staging"} |~ "(?i)error"`,
    15*time.Second,  // Fast polling
    2*time.Minute,   // Short lookback
    myHandler,
    loki.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil))),
    loki.WithWindowSize(1*time.Hour),  // Longer dedup window
)

// Start polling
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

if err := poller.Start(ctx); err != nil && err != context.Canceled {
    log.Fatal(err)
}
```

## Future Enhancements

The following enhancements are under consideration for future versions of the Loki Poller:

### Exponential Backoff Strategy

**Current State:** Failed polls are retried immediately on the next interval with no delay adjustment.

**Proposed Enhancement:** Implement exponential backoff with jitter when Loki is unavailable or rate limiting:

```go
type BackoffConfig struct {
    InitialInterval time.Duration
    MaxInterval     time.Duration
    Multiplier      float64
    MaxRetries      int
}
```

Benefits:
- Reduces load on struggling Loki instances
- Prevents thundering herd after outages
- Configurable recovery behavior

### Pluggable Poller Interface

**Current State:** The poller is tightly coupled to Loki's HTTP API.

**Proposed Enhancement:** Define a generic `LogPoller` interface:

```go
type LogPoller interface {
    Start(ctx context.Context) error
    Stop() error
    SetHandler(ErrorHandler)
}

type LogSource interface {
    Query(ctx context.Context, query string, start, end time.Time) ([]LogEntry, error)
    Ready(ctx context.Context) error
}
```

Benefits:
- Support for alternative log backends (Elasticsearch, CloudWatch, etc.)
- Easier testing with mock implementations
- Modular architecture for different deployment environments

### Metrics and Observability

**Current State:** Basic structured logging only.

**Proposed Enhancement:** Export Prometheus metrics:

```go
var (
    pollDuration = prometheus.NewHistogramVec(...)
    pollErrors   = prometheus.NewCounterVec(...)
    errorsFound  = prometheus.NewCounterVec(...)
    cacheSize    = prometheus.NewGauge(...)
)
```

Proposed metrics:
- `kube_sentinel_loki_poll_duration_seconds`: Histogram of poll latencies
- `kube_sentinel_loki_poll_errors_total`: Counter of poll failures by type
- `kube_sentinel_loki_errors_found_total`: Counter of errors discovered
- `kube_sentinel_loki_cache_entries`: Current deduplication cache size

### Query Result Pagination

**Current State:** Queries are limited to 1000 entries.

**Proposed Enhancement:** Implement pagination for high-volume environments:

```go
func (p *Poller) pollPaginated(ctx context.Context) error {
    var allEntries []LogEntry
    var lastTimestamp time.Time

    for {
        entries, err := p.client.QueryRange(ctx, p.query, start, end, limit)
        if err != nil {
            return err
        }
        allEntries = append(allEntries, entries...)
        if len(entries) < limit {
            break
        }
        // Adjust window for next page
        end = entries[len(entries)-1].Timestamp
    }
    // Process allEntries...
}
```

### Configurable Fingerprint Strategies

**Current State:** Fixed fingerprinting algorithm.

**Proposed Enhancement:** Allow custom fingerprint functions:

```go
type FingerprintFunc func(namespace, pod, container, message string) string

func WithFingerprinter(fn FingerprintFunc) PollerOption {
    return func(p *Poller) {
        p.fingerprinter = fn
    }
}
```

Use cases:
- Per-namespace fingerprinting strategies
- Include additional labels in fingerprint
- Application-specific deduplication rules

### Health Check Integration

**Current State:** No built-in health reporting.

**Proposed Enhancement:** Expose health status for Kubernetes probes:

```go
func (p *Poller) Health() HealthStatus {
    return HealthStatus{
        LastPollTime:    p.lastPollEnd,
        LastPollSuccess: p.lastPollSuccess,
        CacheSize:       len(p.seenErrors),
        Errors:          p.recentErrors,
    }
}
```

### Graceful Degradation

**Current State:** All-or-nothing error handling.

**Proposed Enhancement:** Partial failure handling with circuit breaker:

```go
type CircuitBreaker struct {
    failures    int
    threshold   int
    resetAfter  time.Duration
    lastFailure time.Time
    state       CircuitState  // Closed, Open, HalfOpen
}
```

Benefits:
- Prevent cascading failures
- Automatic recovery detection
- Configurable failure thresholds

---

## Related Documentation

- [Loki Client](./loki-client.md) - HTTP client for Loki API
- [Rule Engine](./rule-engine.md) - Error classification and prioritization
- [Remediation Engine](./remediation-engine.md) - Automated response actions
- [Configuration Guide](./configuration.md) - Full configuration reference
