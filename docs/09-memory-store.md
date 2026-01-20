# Memory Store

This document describes the in-memory storage implementation for kube-sentinel, which provides fast, concurrent access to error and remediation data.

## Overview

The `MemoryStore` is the default storage backend for kube-sentinel. It implements the `Store` interface and maintains all error and remediation log data in memory using Go maps protected by a read-write mutex for thread safety.

### Key Characteristics

- **Fast access**: O(1) lookups by ID or fingerprint
- **Thread-safe**: Uses `sync.RWMutex` for concurrent read/write access
- **Automatic deduplication**: Errors with identical fingerprints are merged
- **Priority-based sorting**: Errors are sorted by priority and timestamp
- **Bounded memory**: Configurable limits with automatic cleanup

## Architecture

### Data Structures

The `MemoryStore` maintains four primary data structures:

```go
type MemoryStore struct {
    mu sync.RWMutex

    errors            map[string]*Error            // indexed by ID
    errorsByFP        map[string]*Error            // indexed by fingerprint
    remediationLogs   map[string]*RemediationLog   // indexed by ID
    remediationsByErr map[string][]*RemediationLog // indexed by error ID
}
```

| Map | Purpose |
|-----|---------|
| `errors` | Primary error storage, keyed by unique ID |
| `errorsByFP` | Secondary index for fingerprint-based lookups |
| `remediationLogs` | Primary remediation log storage |
| `remediationsByErr` | Index for retrieving all remediation attempts for a specific error |

### Configuration Options

The store supports functional options for configuration:

```go
store := NewMemoryStore(
    WithMaxErrors(10000),           // Maximum error entries (default: 10000)
    WithMaxRemediationLogs(5000),   // Maximum remediation logs (default: 5000)
)
```

## Error Deduplication by Fingerprint

One of the most important features of the memory store is error deduplication. When multiple occurrences of the same error are detected, they are consolidated into a single entry rather than creating duplicates.

### How Fingerprinting Works

Each error has a `Fingerprint` field that uniquely identifies the error type. The fingerprint is typically generated from:

- Namespace
- Pod name pattern
- Error message pattern
- Container name

### Deduplication Logic

When `SaveError` is called:

1. The store checks if an error with the same fingerprint already exists
2. If found, the existing error is updated:
   - `Count` is incremented
   - `LastSeen` is updated to the new timestamp
   - `FirstSeen` is preserved (or updated if the new timestamp is earlier)
3. If not found, a new error entry is created

```go
if existing, ok := s.errorsByFP[err.Fingerprint]; ok {
    existing.Count++
    existing.LastSeen = err.Timestamp
    if err.Timestamp.Before(existing.FirstSeen) {
        existing.FirstSeen = err.Timestamp
    }
    return nil
}
```

### Benefits of Deduplication

- **Reduced memory usage**: Thousands of identical errors become one entry with a count
- **Clearer error visibility**: High-frequency errors are surfaced with occurrence counts
- **Better trend analysis**: FirstSeen/LastSeen timestamps show error duration

## Sorting by Priority and Timestamp

When listing errors, the store applies a two-level sort order:

1. **Primary sort: Priority weight** (ascending, so critical errors appear first)
2. **Secondary sort: Last seen timestamp** (descending, so newest errors appear first)

### Priority Levels

| Priority | Code | Weight |
|----------|------|--------|
| Critical | P1 | 1 |
| High | P2 | 2 |
| Medium | P3 | 3 |
| Low | P4 | 4 |

### Sorting Implementation

```go
sort.Slice(filtered, func(i, j int) bool {
    wi := filtered[i].Priority.Weight()
    wj := filtered[j].Priority.Weight()
    if wi != wj {
        return wi < wj  // Lower weight = higher priority
    }
    return filtered[i].LastSeen.After(filtered[j].LastSeen)
})
```

This ensures that:
- Critical (P1) errors always appear before High (P2) errors
- Within the same priority level, the most recently seen errors appear first

## Automatic Cleanup of Old Entries

The memory store implements automatic cleanup to prevent unbounded memory growth.

### Threshold-Based Cleanup

When the number of stored entries exceeds the configured maximum, cleanup is triggered automatically:

```go
if len(s.errors) > s.maxErrors {
    s.cleanupOldErrors()
}
```

### Cleanup Strategy

The cleanup algorithm removes the **oldest 10%** of entries based on `LastSeen` timestamp:

1. Collect all entries into a slice
2. Sort by `LastSeen` timestamp (oldest first)
3. Remove the oldest 10% of entries
4. Update both primary and secondary indexes

```go
func (s *MemoryStore) cleanupOldErrors() {
    var errors []*Error
    for _, err := range s.errors {
        errors = append(errors, err)
    }

    sort.Slice(errors, func(i, j int) bool {
        return errors[i].LastSeen.Before(errors[j].LastSeen)
    })

    toRemove := len(errors) / 10
    for i := 0; i < toRemove; i++ {
        delete(s.errors, errors[i].ID)
        delete(s.errorsByFP, errors[i].Fingerprint)
    }
}
```

### Time-Based Cleanup

The store also provides explicit time-based cleanup methods:

```go
// Delete errors not seen since a specific time
count, err := store.DeleteOldErrors(time.Now().Add(-24 * time.Hour))

// Delete old remediation logs
count, err := store.DeleteOldRemediationLogs(time.Now().Add(-7 * 24 * time.Hour))
```

These methods are typically called by a background cleanup goroutine on a schedule.

## Filtering and Pagination

### Available Filters

The `ListErrors` method supports comprehensive filtering:

| Filter | Description |
|--------|-------------|
| `Namespace` | Exact match on namespace |
| `Pod` | Substring match on pod name |
| `Priority` | Exact match on priority level |
| `Remediated` | Filter by remediation status (true/false) |
| `Since` | Only errors seen after this time |
| `Search` | Full-text search across message, pod, and namespace |

### Pagination

Both error and remediation log listing support offset-based pagination:

```go
errors, total, err := store.ListErrors(filter, PaginationOptions{
    Offset: 0,
    Limit:  50,
})
```

The `total` return value indicates the total number of matching records before pagination, enabling proper pagination UI.

## Limitations

### Data Loss on Restart

The most significant limitation of the memory store is that **all data is lost when the process restarts**. This includes:

- All stored errors and their occurrence counts
- All remediation logs
- Aggregate statistics

This makes the memory store unsuitable for:

- Production environments requiring data persistence
- Compliance scenarios requiring audit trails
- Long-term trend analysis

### Memory Constraints

- All data must fit in available memory
- Large clusters with many errors may require significant memory allocation
- No disk overflow or swapping mechanism

### No Distributed Support

- Single-instance only; no replication or clustering
- Not suitable for high-availability deployments with multiple kube-sentinel replicas

### Index Maintenance

- The `remediationsByErr` index is not fully maintained during time-based cleanup
- Orphaned entries may remain until the next cleanup cycle

## Thread Safety

The store uses a read-write mutex (`sync.RWMutex`) for thread safety:

- **Read operations** (`GetError`, `ListErrors`, `GetStats`): Acquire read lock
- **Write operations** (`SaveError`, `UpdateError`, `DeleteError`): Acquire write lock

This allows multiple concurrent readers while ensuring exclusive access for writers.

## Future Improvements

The following enhancements are planned or under consideration for the memory store:

### Eviction Policies

Currently, only LRU (Least Recently Used) eviction based on `LastSeen` is implemented. Future versions may support:

- **LFU (Least Frequently Used)**: Evict errors with lowest occurrence counts
- **Priority-aware eviction**: Preserve high-priority errors longer
- **Weighted eviction**: Combine multiple factors (priority, age, count)

### Persistence Options

To address data loss concerns:

- **Snapshot persistence**: Periodic serialization to disk
- **Write-ahead logging (WAL)**: Durability for recent entries
- **Hybrid storage**: Keep hot data in memory, cold data on disk

### Alternative Storage Backends

The `Store` interface allows for alternative implementations:

- **SQLite**: Lightweight embedded database for single-node persistence
- **PostgreSQL/MySQL**: Full relational database for production use
- **Redis**: Distributed caching with optional persistence
- **etcd**: Kubernetes-native distributed storage

### Performance Optimizations

- **Sharding**: Partition data by namespace for reduced lock contention
- **Bloom filters**: Fast negative lookups for fingerprint deduplication
- **Index optimization**: Additional indexes for common query patterns
- **Batch operations**: Bulk insert/update for high-volume scenarios

### Metrics and Observability

- Prometheus metrics for store operations (insert rate, query latency, memory usage)
- Alerts for approaching capacity limits
- Query performance logging

## API Reference

### Store Interface

```go
type Store interface {
    // Error operations
    SaveError(err *Error) error
    GetError(id string) (*Error, error)
    GetErrorByFingerprint(fingerprint string) (*Error, error)
    ListErrors(filter ErrorFilter, opts PaginationOptions) ([]*Error, int, error)
    UpdateError(err *Error) error
    DeleteError(id string) error
    DeleteOldErrors(before time.Time) (int, error)

    // Remediation log operations
    SaveRemediationLog(log *RemediationLog) error
    GetRemediationLog(id string) (*RemediationLog, error)
    ListRemediationLogs(opts PaginationOptions) ([]*RemediationLog, int, error)
    ListRemediationLogsForError(errorID string) ([]*RemediationLog, error)
    DeleteOldRemediationLogs(before time.Time) (int, error)

    // Statistics
    GetStats() (*Stats, error)

    // Lifecycle
    Close() error
}
```

### Usage Example

```go
package main

import (
    "time"
    "github.com/kube-sentinel/kube-sentinel/internal/store"
    "github.com/kube-sentinel/kube-sentinel/internal/rules"
)

func main() {
    // Create store with custom limits
    s := store.NewMemoryStore(
        store.WithMaxErrors(5000),
        store.WithMaxRemediationLogs(2500),
    )
    defer s.Close()

    // Save an error
    err := s.SaveError(&store.Error{
        ID:          "err-001",
        Fingerprint: "fp-oom-nginx",
        Namespace:   "production",
        Pod:         "nginx-abc123",
        Message:     "OOMKilled",
        Priority:    rules.PriorityCritical,
        Timestamp:   time.Now(),
        FirstSeen:   time.Now(),
        LastSeen:    time.Now(),
        Count:       1,
    })

    // List critical errors
    errors, total, err := s.ListErrors(
        store.ErrorFilter{Priority: rules.PriorityCritical},
        store.PaginationOptions{Limit: 10},
    )

    // Get statistics
    stats, err := s.GetStats()
}
```

## Related Documentation

- Store Interface (`internal/store/store.go`)
- Rules and Priority Types (`internal/rules/types.go`)
