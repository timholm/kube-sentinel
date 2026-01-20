# Store Interface

This document describes the storage abstraction layer used by kube-sentinel for persisting error records and remediation logs.

## Overview

The `Store` interface defines a contract for persistent storage operations in kube-sentinel. By abstracting storage behind an interface, the system achieves:

- **Backend Flexibility**: Switch between in-memory, SQLite, PostgreSQL, or distributed stores without modifying business logic
- **Testability**: Use mock implementations for unit testing without external dependencies
- **Separation of Concerns**: Storage implementation details remain isolated from detection and remediation logic
- **Future Extensibility**: Add new storage backends (e.g., cloud-native databases, time-series stores) by implementing the interface

The interface is defined in `internal/store/store.go` and serves as the foundation for all persistence operations.

## Data Structures

### Error

The `Error` struct represents a detected error that has been captured and stored by kube-sentinel.

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique identifier for the error record |
| `Fingerprint` | `string` | Hash-based identifier for deduplication; errors with the same fingerprint are considered duplicates |
| `Timestamp` | `time.Time` | When the error was initially recorded |
| `Namespace` | `string` | Kubernetes namespace where the error originated |
| `Pod` | `string` | Name of the pod that generated the error |
| `Container` | `string` | Container name within the pod |
| `Message` | `string` | The error message content |
| `Priority` | `rules.Priority` | Severity level assigned by matching rules |
| `Count` | `int` | Number of times this error (by fingerprint) has occurred |
| `FirstSeen` | `time.Time` | Timestamp of the first occurrence |
| `LastSeen` | `time.Time` | Timestamp of the most recent occurrence |
| `RuleMatched` | `string` | Name of the rule that matched this error |
| `Remediated` | `bool` | Whether remediation has been attempted |
| `RemediatedAt` | `*time.Time` | When remediation occurred (nil if not remediated) |
| `Labels` | `map[string]string` | Additional metadata labels for categorization |

### RemediationLog

The `RemediationLog` struct records actions taken in response to detected errors.

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique identifier for the log entry |
| `ErrorID` | `string` | Reference to the associated `Error.ID` |
| `Action` | `string` | Type of remediation action (e.g., `restart`, `scale`, `delete`) |
| `Target` | `string` | Resource targeted by the action in `namespace/resource` format |
| `Status` | `string` | Outcome of the action: `success`, `failed`, or `skipped` |
| `Message` | `string` | Human-readable description of the result |
| `Timestamp` | `time.Time` | When the remediation action was executed |
| `DryRun` | `bool` | Whether the action was simulated without actual execution |

### ErrorFilter

The `ErrorFilter` struct provides query parameters for filtering error results.

| Field | Type | Description |
|-------|------|-------------|
| `Namespace` | `string` | Filter by Kubernetes namespace (empty matches all) |
| `Pod` | `string` | Filter by pod name (empty matches all) |
| `Priority` | `rules.Priority` | Filter by minimum priority level |
| `Remediated` | `*bool` | Filter by remediation status (nil matches all) |
| `Since` | `time.Time` | Return only errors after this timestamp |
| `Search` | `string` | Free-text search across error messages |

### PaginationOptions

The `PaginationOptions` struct controls result set pagination.

| Field | Type | Description |
|-------|------|-------------|
| `Offset` | `int` | Number of records to skip |
| `Limit` | `int` | Maximum number of records to return |

### Stats

The `Stats` struct provides aggregate statistics about stored data.

| Field | Type | Description |
|-------|------|-------------|
| `TotalErrors` | `int` | Total count of stored error records |
| `ErrorsByPriority` | `map[rules.Priority]int` | Error counts grouped by priority level |
| `ErrorsByNamespace` | `map[string]int` | Error counts grouped by namespace |
| `RemediationCount` | `int` | Total number of remediation actions logged |
| `SuccessfulActions` | `int` | Count of remediations with `success` status |
| `FailedActions` | `int` | Count of remediations with `failed` status |
| `LastError` | `*time.Time` | Timestamp of the most recent error |
| `LastRemediation` | `*time.Time` | Timestamp of the most recent remediation |

## Store Interface

The `Store` interface defines the following method groups:

### Error Operations

#### SaveError

```go
SaveError(err *Error) error
```

Persists a new error record. Implementations should:
- Generate a unique `ID` if not provided
- Set `Timestamp`, `FirstSeen`, and `LastSeen` to the current time for new records
- Initialize `Count` to 1 for new records

#### GetError

```go
GetError(id string) (*Error, error)
```

Retrieves an error by its unique identifier. Returns `nil` with no error if the record does not exist, or an error if the lookup fails.

#### GetErrorByFingerprint

```go
GetErrorByFingerprint(fingerprint string) (*Error, error)
```

Retrieves an error by its fingerprint hash. This method supports deduplication by allowing callers to check for existing errors before creating new records. Returns `nil` with no error if no matching record exists.

#### ListErrors

```go
ListErrors(filter ErrorFilter, opts PaginationOptions) ([]*Error, int, error)
```

Returns a paginated list of errors matching the filter criteria. The second return value is the total count of matching records (before pagination), enabling UI pagination controls.

Implementations should:
- Apply all non-zero filter fields as AND conditions
- Sort results by `LastSeen` descending (most recent first)
- Return an empty slice (not nil) when no results match

#### UpdateError

```go
UpdateError(err *Error) error
```

Updates an existing error record. Typically used to:
- Increment `Count` for duplicate errors
- Update `LastSeen` timestamp
- Set `Remediated` and `RemediatedAt` after remediation

#### DeleteError

```go
DeleteError(id string) error
```

Removes a single error record by ID. Returns an error if the deletion fails; deleting a non-existent record may or may not return an error depending on the implementation.

#### DeleteOldErrors

```go
DeleteOldErrors(before time.Time) (int, error)
```

Bulk deletes errors with `LastSeen` before the specified time. Returns the count of deleted records. Used for data retention and cleanup operations.

### Remediation Log Operations

#### SaveRemediationLog

```go
SaveRemediationLog(log *RemediationLog) error
```

Persists a new remediation log entry. Implementations should generate a unique `ID` if not provided.

#### GetRemediationLog

```go
GetRemediationLog(id string) (*RemediationLog, error)
```

Retrieves a remediation log entry by ID.

#### ListRemediationLogs

```go
ListRemediationLogs(opts PaginationOptions) ([]*RemediationLog, int, error)
```

Returns a paginated list of all remediation logs, sorted by `Timestamp` descending.

#### ListRemediationLogsForError

```go
ListRemediationLogsForError(errorID string) ([]*RemediationLog, error)
```

Returns all remediation logs associated with a specific error ID, sorted by `Timestamp` descending.

#### DeleteOldRemediationLogs

```go
DeleteOldRemediationLogs(before time.Time) (int, error)
```

Bulk deletes remediation logs with `Timestamp` before the specified time.

### Statistics

#### GetStats

```go
GetStats() (*Stats, error)
```

Computes and returns aggregate statistics. Implementations may cache these values for performance, but should ensure reasonable freshness.

### Lifecycle

#### Close

```go
Close() error
```

Releases any resources held by the store (database connections, file handles, etc.). Should be called during application shutdown.

## Implementation Requirements

All implementations of the `Store` interface must satisfy these requirements:

### Thread Safety

All methods must be safe for concurrent access from multiple goroutines. Implementations should use appropriate synchronization mechanisms (mutexes, connection pooling, etc.).

### Error Handling

- Return descriptive errors that include context about the operation
- Distinguish between "not found" conditions (return nil, nil) and actual errors (return nil, error)
- Wrap underlying errors with additional context when appropriate

### ID Generation

- Generate UUIDs or similarly unique identifiers for `ID` fields
- Ensure IDs are URL-safe for use in API endpoints

### Filtering Behavior

- Empty/zero-value filter fields should match all records
- Multiple non-empty filter fields combine with AND logic
- The `Search` field should perform case-insensitive substring matching

### Pagination Behavior

- `Limit` of 0 should return all matching records
- `Offset` beyond the result set should return an empty slice
- Always return the total count regardless of pagination

## Future Backend Considerations

### SQLite

An SQLite backend would provide:
- Single-file persistence for simple deployments
- ACID transactions for data integrity
- SQL query capabilities for complex filtering
- Suitable for single-node or small-scale deployments

Implementation notes:
- Use WAL mode for better concurrent read performance
- Implement connection pooling or use a single connection with mutex
- Create indexes on `Fingerprint`, `Namespace`, `LastSeen`, and `Priority`

### PostgreSQL

A PostgreSQL backend would provide:
- Enterprise-grade durability and reliability
- Advanced indexing (GIN indexes for label queries)
- Connection pooling for high concurrency
- Suitable for production multi-node deployments

Implementation notes:
- Use prepared statements for frequently executed queries
- Implement connection pool management
- Consider partitioning the errors table by time for large datasets
- Use JSONB columns for flexible label storage

### Distributed Stores

For large-scale or multi-cluster deployments, distributed backends could include:

**etcd**: Suitable for storing configuration alongside error data in Kubernetes-native environments, though limited query capabilities make it less ideal for complex filtering.

**CockroachDB**: Provides PostgreSQL compatibility with distributed storage, enabling horizontal scaling while maintaining SQL query capabilities.

**Redis (with persistence)**: Offers high-performance caching with optional persistence, suitable for high-throughput environments where some data loss is acceptable.

**Time-Series Databases (InfluxDB, TimescaleDB)**: Optimized for time-based error data with built-in retention policies and efficient time-range queries.

### Implementation Checklist for New Backends

When implementing a new storage backend:

1. Implement all methods defined in the `Store` interface
2. Ensure thread-safe operation for concurrent access
3. Write comprehensive unit tests covering all CRUD operations
4. Test pagination edge cases (empty results, offset beyond data)
5. Verify filter combinations work correctly
6. Benchmark performance for expected workloads
7. Document any backend-specific configuration options
8. Implement proper connection lifecycle management
