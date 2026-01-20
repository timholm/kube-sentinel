# Web Handlers

This document provides comprehensive documentation for the web handlers in kube-sentinel. The handlers are responsible for serving the web dashboard, API endpoints, real-time WebSocket connections, and health check endpoints.

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Data Structures](#data-structures)
4. [Page Handlers](#page-handlers)
5. [API Handlers](#api-handlers)
6. [WebSocket Handler](#websocket-handler)
7. [Health and Readiness Endpoints](#health-and-readiness-endpoints)
8. [Helper Functions](#helper-functions)
9. [Error Handling](#error-handling)
10. [Future Improvements](#future-improvements)

---

## Overview

The web handlers in kube-sentinel provide both a human-readable web interface and a programmatic JSON API for interacting with the error monitoring and remediation system. The handlers are implemented in `/internal/web/handlers.go` and are registered with a Gorilla Mux router.

### Key Capabilities

- **Dashboard visualization** of system statistics and recent activity
- **Error browsing** with filtering, pagination, and detailed views
- **Rule inspection** to view configured detection rules
- **Remediation history** tracking all automated actions
- **Settings management** for enabling/disabling remediation features
- **Real-time updates** via WebSocket connections
- **Health monitoring** for Kubernetes probes

---

## Architecture

The handlers follow a consistent pattern where each handler:

1. Extracts request parameters (query strings, path variables, request body)
2. Interacts with backend services (store, rule engine, remediation engine)
3. Returns either HTML (page handlers) or JSON (API handlers)

```
┌─────────────────────────────────────────────────────────────────┐
│                        HTTP Request                              │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Gorilla Mux Router                          │
└─────────────────────────────────────────────────────────────────┘
                                │
          ┌─────────────────────┼─────────────────────┐
          ▼                     ▼                     ▼
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│  Page Handlers  │   │  API Handlers   │   │ WebSocket/Health│
│  (HTML output)  │   │  (JSON output)  │   │    Handlers     │
└─────────────────┘   └─────────────────┘   └─────────────────┘
          │                     │                     │
          └─────────────────────┼─────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│              Backend Services (Store, Engines)                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Data Structures

The handlers use several data structures to pass information to templates.

### dashboardData

Used by the dashboard page to display system overview information.

```go
type dashboardData struct {
    Stats              *store.Stats           // Overall system statistics
    RecentErrors       []*store.Error         // Latest detected errors
    RecentRemediations []*store.RemediationLog // Recent remediation actions
    RemEnabled         bool                    // Whether remediation is enabled
    DryRun             bool                    // Whether dry-run mode is active
    ActionsThisHour    int                     // Rate limiting counter
}
```

### errorsData

Used by the errors list page with pagination and filtering support.

```go
type errorsData struct {
    Errors     []*store.Error     // List of errors for current page
    Total      int                // Total number of matching errors
    Page       int                // Current page number
    PageSize   int                // Number of items per page
    Filter     store.ErrorFilter  // Active filter criteria
    Namespaces []string           // Available namespaces for filter dropdown
}
```

### errorDetailData

Used by the error detail page to show a single error with its remediation history.

```go
type errorDetailData struct {
    Error        *store.Error           // The error being viewed
    Remediations []*store.RemediationLog // Remediation actions for this error
}
```

### rulesData

Used by the rules page to display all configured detection rules.

```go
type rulesData struct {
    Rules []rules.Rule // All loaded rules
}
```

### historyData

Used by the remediation history page with pagination.

```go
type historyData struct {
    Logs     []*store.RemediationLog // Remediation logs for current page
    Total    int                      // Total number of logs
    Page     int                      // Current page number
    PageSize int                      // Number of items per page
}
```

### settingsData

Used by the settings page to display current configuration.

```go
type settingsData struct {
    RemEnabled      bool // Whether remediation is enabled
    DryRun          bool // Whether dry-run mode is active
    MaxActionsPerHour int // Rate limit configuration
    ActionsThisHour int  // Current rate counter
}
```

---

## Page Handlers

Page handlers render HTML templates for the web dashboard interface.

### handleDashboard

**Route:** `GET /` or `GET /dashboard`

**Purpose:** Renders the main dashboard page with an overview of system health and recent activity.

**Behavior:**
1. Retrieves overall statistics from the store
2. Fetches the 10 most recent errors
3. Fetches the 5 most recent remediation logs
4. Gets current remediation engine state (enabled, dry-run, actions this hour)
5. Renders the `dashboard.html` template

**Template Data:** `dashboardData`

---

### handleErrors

**Route:** `GET /errors`

**Purpose:** Displays a paginated, filterable list of all detected errors.

**Query Parameters:**

| Parameter   | Type   | Default | Description                        |
|-------------|--------|---------|-----------------------------------|
| `page`      | int    | 1       | Page number (1-indexed)           |
| `namespace` | string | ""      | Filter by Kubernetes namespace    |
| `pod`       | string | ""      | Filter by pod name                |
| `search`    | string | ""      | Free-text search in error messages|
| `priority`  | string | ""      | Filter by priority level          |

**Behavior:**
1. Parses pagination and filter parameters from query string
2. Applies filters and retrieves matching errors (20 per page)
3. Extracts unique namespaces for the filter dropdown
4. Renders the `errors.html` template

**Template Data:** `errorsData`

---

### handleErrorDetail

**Route:** `GET /errors/{id}`

**Purpose:** Displays detailed information about a specific error, including any remediation actions taken.

**Path Parameters:**
- `id`: The unique identifier of the error

**Behavior:**
1. Extracts error ID from URL path
2. Retrieves the error from the store (returns 404 if not found)
3. Fetches all remediation logs associated with this error
4. Renders the `error_detail.html` template

**Template Data:** `errorDetailData`

---

### handleRules

**Route:** `GET /rules`

**Purpose:** Displays all configured detection rules.

**Behavior:**
1. Retrieves all rules from the rule engine
2. Renders the `rules.html` template

**Template Data:** `rulesData`

---

### handleHistory

**Route:** `GET /history`

**Purpose:** Displays a paginated list of all remediation actions taken by the system.

**Query Parameters:**

| Parameter | Type | Default | Description             |
|-----------|------|---------|------------------------|
| `page`    | int  | 1       | Page number (1-indexed)|

**Behavior:**
1. Parses page number from query string
2. Retrieves remediation logs with pagination (50 per page)
3. Renders the `history.html` template

**Template Data:** `historyData`

---

### handleSettings

**Route:** `GET /settings`

**Purpose:** Displays current system settings and allows configuration changes.

**Behavior:**
1. Retrieves current remediation engine state
2. Renders the `settings.html` template

**Template Data:** `settingsData`

---

## API Handlers

API handlers return JSON responses for programmatic access.

### handleAPIErrors

**Route:** `GET /api/errors`

**Purpose:** Returns a paginated, filterable list of errors in JSON format.

**Query Parameters:**

| Parameter   | Type   | Default | Max | Description                 |
|-------------|--------|---------|-----|-----------------------------|
| `page`      | int    | 1       | -   | Page number (1-indexed)     |
| `pageSize`  | int    | 20      | 100 | Number of items per page    |
| `namespace` | string | ""      | -   | Filter by namespace         |
| `pod`       | string | ""      | -   | Filter by pod name          |
| `search`    | string | ""      | -   | Free-text search            |
| `priority`  | string | ""      | -   | Filter by priority level    |

**Response:**

```json
{
    "errors": [...],
    "total": 150,
    "page": 1,
    "pageSize": 20
}
```

**Error Responses:**
- `500 Internal Server Error`: Database query failed

---

### handleAPIErrorDetail

**Route:** `GET /api/errors/{id}`

**Purpose:** Returns detailed information about a specific error.

**Path Parameters:**
- `id`: The unique identifier of the error

**Response:**

```json
{
    "error": {
        "id": "abc123",
        "namespace": "default",
        "pod": "my-app-xyz",
        "message": "Error message...",
        ...
    },
    "remediations": [...]
}
```

**Error Responses:**
- `404 Not Found`: Error with specified ID does not exist

---

### handleAPIRules

**Route:** `GET /api/rules`

**Purpose:** Returns all configured detection rules.

**Response:**

```json
{
    "rules": [
        {
            "name": "oom-killer",
            "pattern": "OOMKilled",
            "priority": "critical",
            ...
        },
        ...
    ]
}
```

---

### handleAPIRulesTest

**Route:** `POST /api/rules/test`

**Purpose:** Tests a rule pattern against a sample string without creating a rule.

**Request Body:**

```json
{
    "pattern": "error.*connection",
    "sample": "error: connection refused"
}
```

**Response (match):**

```json
{
    "matches": true
}
```

**Response (no match or invalid pattern):**

```json
{
    "matches": false,
    "error": "invalid regex pattern"
}
```

**Error Responses:**
- `400 Bad Request`: Invalid JSON in request body

---

### handleAPIRemediations

**Route:** `GET /api/remediations`

**Purpose:** Returns a paginated list of remediation logs.

**Query Parameters:**

| Parameter  | Type | Default | Max | Description              |
|------------|------|---------|-----|--------------------------|
| `page`     | int  | 1       | -   | Page number (1-indexed)  |
| `pageSize` | int  | 50      | 100 | Number of items per page |

**Response:**

```json
{
    "remediations": [...],
    "total": 75,
    "page": 1,
    "pageSize": 50
}
```

**Error Responses:**
- `500 Internal Server Error`: Database query failed

---

### handleAPIStats

**Route:** `GET /api/stats`

**Purpose:** Returns overall system statistics.

**Response:**

```json
{
    "total_errors": 1500,
    "errors_by_priority": {
        "critical": 50,
        "high": 200,
        "medium": 500,
        "low": 750
    },
    "remediations_total": 300,
    "remediations_successful": 285,
    ...
}
```

**Error Responses:**
- `500 Internal Server Error`: Failed to retrieve statistics

---

### handleAPISettings

**Route:** `GET /api/settings` and `POST /api/settings`

**Purpose:** Retrieves or updates remediation settings.

**GET Response:**

```json
{
    "enabled": true,
    "dry_run": false,
    "actions_this_hour": 15
}
```

**POST Request Body:**

```json
{
    "enabled": true,
    "dry_run": true
}
```

**POST Response:** Same as GET response, reflecting new values.

**Error Responses:**
- `400 Bad Request`: Invalid JSON in request body

---

## WebSocket Handler

### handleWebSocket

**Route:** `GET /ws`

**Purpose:** Establishes a WebSocket connection for real-time updates.

**Behavior:**
1. Upgrades the HTTP connection to WebSocket protocol
2. Registers the client connection in the server's client map (thread-safe)
3. Maintains the connection and listens for incoming messages
4. On disconnect, removes the client from the map and closes the connection

**Connection Lifecycle:**

```
Client                                 Server
  │                                      │
  │──── HTTP Upgrade Request ──────────▶│
  │                                      │
  │◀─── 101 Switching Protocols ────────│
  │                                      │
  │◀──────── Real-time Events ──────────│
  │            (errors, remediations)    │
  │                                      │
  │──── Close/Disconnect ──────────────▶│
  │                                      │
```

**Event Types (pushed to clients):**
- New error detected
- Remediation action taken
- Statistics updated

**Client Management:**
- Thread-safe client registration using mutex
- Automatic cleanup on connection close
- Graceful handling of unexpected disconnections

---

## Health and Readiness Endpoints

These endpoints support Kubernetes liveness and readiness probes.

### handleHealth

**Route:** `GET /health` or `GET /healthz`

**Purpose:** Indicates that the server process is running.

**Response:**
- Status: `200 OK`
- Body: `OK`

**Use Case:** Kubernetes liveness probe to detect if the container needs to be restarted.

---

### handleReady

**Route:** `GET /ready` or `GET /readyz`

**Purpose:** Indicates that the server is ready to accept traffic.

**Response:**
- Status: `200 OK`
- Body: `Ready`

**Use Case:** Kubernetes readiness probe to determine when to add the pod to service endpoints.

---

## Helper Functions

### renderTemplate

**Purpose:** Renders an HTML template with the provided data.

**Behavior:**
1. Sets `Content-Type` header to `text/html; charset=utf-8`
2. Executes the named template with provided data
3. On error, logs the error and returns 500 Internal Server Error

### jsonResponse

**Purpose:** Sends a JSON response with the provided data.

**Behavior:**
1. Sets `Content-Type` header to `application/json`
2. Encodes the data as JSON and writes to response

### jsonError

**Purpose:** Sends a JSON error response.

**Behavior:**
1. Sets `Content-Type` header to `application/json`
2. Sets the HTTP status code
3. Encodes an error object with the message

**Response Format:**

```json
{
    "error": "error message here"
}
```

---

## Error Handling

The handlers implement consistent error handling patterns:

### Page Handlers

- **Not Found:** Returns HTTP 404 with standard error page
- **Internal Error:** Logs error details and returns HTTP 500

### API Handlers

- **Not Found:** Returns HTTP 404 with JSON error body
- **Bad Request:** Returns HTTP 400 with JSON error body describing the issue
- **Internal Error:** Returns HTTP 500 with JSON error body

### Example Error Response

```json
{
    "error": "invalid request body"
}
```

---

## Future Improvements

### API Versioning

Currently, the API endpoints are not versioned. Future improvements should include:

1. **URL-based versioning:** `/api/v1/errors`, `/api/v2/errors`
2. **Header-based versioning:** `Accept: application/vnd.kube-sentinel.v1+json`
3. **Version discovery endpoint:** `GET /api/versions`

**Recommended Implementation:**

```go
// Version 1 routes
r.PathPrefix("/api/v1").Subrouter()

// Version 2 routes (future)
r.PathPrefix("/api/v2").Subrouter()

// Default to latest stable version
r.PathPrefix("/api").Handler(http.RedirectHandler("/api/v1", 301))
```

### OpenAPI Specification

Adding an OpenAPI (Swagger) specification would improve API documentation and enable client code generation.

**Benefits:**
- Auto-generated, interactive API documentation
- Client SDK generation for multiple languages
- Request/response validation
- API testing tools integration

**Implementation Steps:**
1. Add OpenAPI annotations to handlers using `swaggo/swag`
2. Generate specification at build time
3. Serve Swagger UI at `/api/docs`
4. Serve OpenAPI spec at `/api/openapi.json`

### Additional Improvements

1. **Rate Limiting:** Add rate limiting middleware for API endpoints
2. **Authentication:** Implement JWT or OAuth2 authentication
3. **Request Validation:** Add structured validation for request bodies
4. **Response Compression:** Enable gzip compression for large responses
5. **Caching:** Add ETag and conditional request support
6. **Metrics:** Expose Prometheus metrics for handler performance
7. **Audit Logging:** Log all API mutations for compliance
8. **CORS Configuration:** Add configurable CORS headers for cross-origin requests

### WebSocket Enhancements

1. **Subscription Filters:** Allow clients to subscribe to specific namespaces or error types
2. **Message Acknowledgment:** Implement message delivery confirmation
3. **Reconnection Protocol:** Define a standard reconnection flow with state recovery
4. **Heartbeat/Ping-Pong:** Add keep-alive mechanism for connection health
5. **Binary Protocol:** Consider Protocol Buffers for reduced message size

---

## Summary

The kube-sentinel web handlers provide a comprehensive interface for both human operators and automated systems to interact with the error monitoring platform. The separation of page handlers (HTML) and API handlers (JSON) allows for flexible integration while maintaining a user-friendly dashboard experience.

Key files:
- `/internal/web/handlers.go` - Handler implementations
- `/internal/web/server.go` - Server setup and routing
- `/internal/web/templates/` - HTML templates for page handlers
