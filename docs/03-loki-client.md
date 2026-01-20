# Loki Client

This document describes the Loki client implementation in kube-sentinel, which provides a Go interface for querying logs from [Grafana Loki](https://grafana.com/oss/loki/).

## Overview

The Loki client (`internal/loki/client.go`) is responsible for communicating with Loki's HTTP API to retrieve log data. It serves as the primary interface between kube-sentinel and a Loki log aggregation system, enabling the application to query logs for analysis, alerting, or other operational purposes.

### Key Responsibilities

- Execute LogQL queries against Loki's query APIs
- Handle authentication and multi-tenancy headers
- Parse and transform Loki's JSON response format into strongly-typed Go structs
- Provide health check capabilities via the readiness endpoint

## Architecture

```
┌─────────────────┐     HTTP/HTTPS      ┌─────────────────┐
│  kube-sentinel  │ ──────────────────> │   Loki Server   │
│   Loki Client   │                     │                 │
│                 │ <────────────────── │   /loki/api/v1  │
└─────────────────┘    JSON Response    └─────────────────┘
```

## Client Configuration

### Creating a Client

The client is instantiated using the `NewClient` function with a base URL and optional configuration functions:

```go
client := loki.NewClient("http://loki:3100")
```

### Configuration Options

The client uses the functional options pattern for configuration, providing a clean and extensible API.

#### WithTenantID

Sets the `X-Scope-OrgID` header for multi-tenant Loki deployments:

```go
client := loki.NewClient(
    "http://loki:3100",
    loki.WithTenantID("my-tenant"),
)
```

This option is required when connecting to a multi-tenant Loki instance where tenant isolation is enforced.

#### WithBasicAuth

Configures HTTP Basic Authentication credentials:

```go
client := loki.NewClient(
    "http://loki:3100",
    loki.WithBasicAuth("username", "password"),
)
```

Use this option when Loki is deployed behind a reverse proxy that requires authentication or when using Grafana Cloud Loki.

#### WithHTTPClient

Provides a custom `*http.Client` for advanced configuration:

```go
customClient := &http.Client{
    Timeout: 60 * time.Second,
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            // Custom TLS settings
        },
    },
}

client := loki.NewClient(
    "https://loki:3100",
    loki.WithHTTPClient(customClient),
)
```

This option enables:
- Custom timeout settings (default is 30 seconds)
- TLS/SSL configuration
- Proxy settings
- Connection pooling configuration

### Default Behavior

When no options are provided, the client uses:
- A default HTTP client with a 30-second timeout
- No tenant ID header
- No authentication

## API Methods

### QueryRange

Executes a range query to retrieve logs within a specified time window.

#### Signature

```go
func (c *Client) QueryRange(
    ctx context.Context,
    query string,
    start, end time.Time,
    limit int,
) ([]LogEntry, error)
```

#### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Context for cancellation and timeouts |
| `query` | `string` | LogQL query expression |
| `start` | `time.Time` | Start of the time range (inclusive) |
| `end` | `time.Time` | End of the time range (inclusive) |
| `limit` | `int` | Maximum number of log entries to return |

#### Returns

- `[]LogEntry`: Slice of parsed log entries in reverse chronological order (newest first)
- `error`: Non-nil if the request fails or Loki returns an error

#### Example

```go
entries, err := client.QueryRange(
    ctx,
    `{namespace="production"} |= "error"`,
    time.Now().Add(-1*time.Hour),
    time.Now(),
    1000,
)
if err != nil {
    log.Fatalf("query failed: %v", err)
}

for _, entry := range entries {
    fmt.Printf("[%s] %s\n", entry.Timestamp, entry.Line)
}
```

#### Loki API Endpoint

`GET /loki/api/v1/query_range`

### Query

Executes an instant query to retrieve logs at a specific point in time.

#### Signature

```go
func (c *Client) Query(
    ctx context.Context,
    query string,
    at time.Time,
    limit int,
) ([]LogEntry, error)
```

#### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Context for cancellation and timeouts |
| `query` | `string` | LogQL query expression |
| `at` | `time.Time` | Point in time to evaluate the query |
| `limit` | `int` | Maximum number of log entries to return |

#### Returns

- `[]LogEntry`: Slice of parsed log entries
- `error`: Non-nil if the request fails or Loki returns an error

#### Example

```go
entries, err := client.Query(
    ctx,
    `{app="nginx"} |~ "5[0-9]{2}"`,
    time.Now(),
    100,
)
```

#### Loki API Endpoint

`GET /loki/api/v1/query`

### Ready

Checks if the Loki server is ready to accept requests.

#### Signature

```go
func (c *Client) Ready(ctx context.Context) error
```

#### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Context for cancellation and timeouts |

#### Returns

- `error`: `nil` if Loki is ready, otherwise an error describing the issue

#### Example

```go
if err := client.Ready(ctx); err != nil {
    log.Printf("Loki is not ready: %v", err)
    // Implement backoff or alternative logic
}
```

#### Loki API Endpoint

`GET /ready`

## Data Types

### LogEntry

Represents a single parsed log entry with its metadata.

```go
type LogEntry struct {
    Timestamp time.Time         // When the log was recorded
    Labels    map[string]string // Stream labels (e.g., namespace, pod)
    Line      string            // The actual log content
}
```

### QueryResponse

Internal type representing the raw JSON response from Loki.

```go
type QueryResponse struct {
    Status string    `json:"status"` // "success" or "error"
    Data   QueryData `json:"data"`
}
```

### Stream

Represents a log stream with its labels and values.

```go
type Stream struct {
    Stream map[string]string `json:"stream"` // Label set
    Values [][]string        `json:"values"` // [timestamp_ns, log_line]
}
```

## Error Handling

The client wraps all errors with contextual information using Go's error wrapping pattern (`fmt.Errorf("context: %w", err)`).

### Error Categories

| Error Condition | Description |
|-----------------|-------------|
| Request creation failure | Invalid URL or context issues |
| Network errors | Connection refused, DNS failures, timeouts |
| Non-200 HTTP status | Loki returned an error (body included in error message) |
| JSON decode failure | Malformed response from Loki |
| Query failure | Loki reported `status != "success"` |

### Example Error Handling

```go
entries, err := client.QueryRange(ctx, query, start, end, limit)
if err != nil {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        // Handle timeout
    case errors.Is(err, context.Canceled):
        // Handle cancellation
    default:
        // Log and handle other errors
        log.Printf("Loki query error: %v", err)
    }
    return nil, err
}
```

### HTTP Status Code Handling

The client considers only `200 OK` as a successful response. All other status codes result in an error that includes:
- The HTTP status code
- The response body (for debugging)

## Request Configuration

### Headers

The client automatically sets the following headers on every request:

| Header | Value | Condition |
|--------|-------|-----------|
| `Accept` | `application/json` | Always |
| `X-Scope-OrgID` | Configured tenant ID | When `WithTenantID` is used |
| `Authorization` | Basic auth credentials | When `WithBasicAuth` is used |

### Query Direction

Both `Query` and `QueryRange` methods set `direction=backward`, returning logs in reverse chronological order (newest first).

## Future Improvements

The following enhancements could improve the client's robustness and functionality:

### Retry Logic

Implement automatic retries with exponential backoff for transient failures:

```go
type RetryConfig struct {
    MaxRetries     int
    InitialBackoff time.Duration
    MaxBackoff     time.Duration
    RetryableFunc  func(error) bool
}

func WithRetry(config RetryConfig) ClientOption
```

Considerations:
- Retry only on network errors and 5xx status codes
- Implement jitter to prevent thundering herd
- Respect `Retry-After` headers when present

### Response Caching

Add optional caching for frequently executed queries:

```go
type CacheConfig struct {
    TTL         time.Duration
    MaxSize     int
    KeyFunc     func(query string, start, end time.Time) string
}

func WithCache(config CacheConfig) ClientOption
```

Considerations:
- Cache invalidation strategy
- Memory vs. external cache (Redis, memcached)
- Query result deduplication

### Additional Authentication Methods

Extend authentication support:

```go
// Bearer token authentication
func WithBearerToken(token string) ClientOption

// OAuth2 client credentials flow
func WithOAuth2(config oauth2.Config) ClientOption

// Custom header-based authentication
func WithCustomAuth(headerName, headerValue string) ClientOption
```

### Connection Pooling and Keep-Alive

Optimize connection reuse:

```go
type PoolConfig struct {
    MaxIdleConns        int
    MaxIdleConnsPerHost int
    IdleConnTimeout     time.Duration
}

func WithConnectionPool(config PoolConfig) ClientOption
```

### Metrics and Observability

Add instrumentation for monitoring:

```go
type MetricsConfig struct {
    Namespace string
    Subsystem string
    Registry  prometheus.Registerer
}

func WithMetrics(config MetricsConfig) ClientOption
```

Metrics to consider:
- Request latency histogram
- Request count by status code
- Active request gauge
- Response size histogram

### Streaming Support

Implement Loki's tail API for real-time log streaming:

```go
func (c *Client) Tail(
    ctx context.Context,
    query string,
    delay time.Duration,
) (<-chan LogEntry, <-chan error)
```

### Query Builder

Provide a type-safe LogQL query builder:

```go
query := loki.NewQueryBuilder().
    Selector("namespace", "production").
    Selector("app", "api").
    LineFilter(loki.Contains, "error").
    JSONParser().
    LineFormat("{{.level}}: {{.message}}").
    Build()
```

### Rate Limiting

Implement client-side rate limiting to prevent overwhelming Loki:

```go
func WithRateLimit(requestsPerSecond float64) ClientOption
```

### Circuit Breaker

Add circuit breaker pattern for fault tolerance:

```go
type CircuitBreakerConfig struct {
    FailureThreshold int
    ResetTimeout     time.Duration
    HalfOpenRequests int
}

func WithCircuitBreaker(config CircuitBreakerConfig) ClientOption
```

## Usage Recommendations

1. **Always use contexts**: Pass appropriate contexts with timeouts to prevent hanging requests.

2. **Handle pagination**: For large result sets, implement pagination using the `limit` parameter and time-based cursoring.

3. **Optimize queries**: Use specific label matchers to reduce the data Loki needs to scan.

4. **Monitor client health**: Periodically call `Ready()` to verify Loki availability.

5. **Secure credentials**: When using `WithBasicAuth`, ensure credentials are loaded from secure sources (environment variables, secrets management).

## See Also

- [Loki HTTP API Documentation](https://grafana.com/docs/loki/latest/reference/loki-http-api/)
- [LogQL Query Language](https://grafana.com/docs/loki/latest/query/)
- [Loki Best Practices](https://grafana.com/docs/loki/latest/best-practices/)
