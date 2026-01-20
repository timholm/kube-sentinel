# Kube Sentinel Priority Classification System

## Technical Whitepaper v1.0

---

## Executive Summary

Kube Sentinel implements a sophisticated four-tier priority classification system designed to triage Kubernetes errors based on operational impact and urgency. This document details the technical architecture, classification logic, and sorting algorithms that power the priority queue system.

---

## 1. Priority Level Definitions

### 1.1 Priority Tier Architecture

Kube Sentinel defines four distinct priority levels, each represented as a typed constant in Go:

```go
type Priority string

const (
    PriorityCritical Priority = "P1"  // Critical - Immediate attention required
    PriorityHigh     Priority = "P2"  // High - Urgent, service degradation
    PriorityMedium   Priority = "P3"  // Medium - Notable but not urgent
    PriorityLow      Priority = "P4"  // Low - Informational
)
```

### 1.2 Priority Weight System

Each priority level is assigned a numeric weight used for sorting operations. **Lower weight values indicate higher priority**, ensuring critical errors surface first:

| Priority | Label    | Weight | Color  | Use Case |
|----------|----------|--------|--------|----------|
| P1       | Critical | 1      | Red    | Service outages, crashes, OOM kills |
| P2       | High     | 2      | Orange | Degraded service, image pull failures |
| P3       | Medium   | 3      | Yellow | Connectivity issues, transient errors |
| P4       | Low      | 4      | Blue   | Generic errors, informational |

The weight function implementation:

```go
func (p Priority) Weight() int {
    switch p {
    case PriorityCritical:
        return 1
    case PriorityHigh:
        return 2
    case PriorityMedium:
        return 3
    case PriorityLow:
        return 4
    default:
        return 5  // Unknown priorities sink to bottom
    }
}
```

---

## 2. Rule-Based Classification Engine

### 2.1 Rule Structure

Errors are classified by matching against a configurable set of rules. Each rule contains:

```yaml
rules:
  - name: string           # Unique identifier
    match:
      pattern: string      # Regex pattern for message matching
      keywords: []string   # Simple keyword matching (OR logic)
      labels: map          # Kubernetes label matchers
      namespaces: []string # Namespace whitelist/blacklist
    priority: P1|P2|P3|P4  # Assigned priority level
    remediation:           # Optional auto-remediation config
      action: string
      cooldown: duration
    enabled: bool
```

### 2.2 First-Match-Wins Algorithm

The rule engine processes rules in **definition order**, and the first matching rule determines the error's priority:

```go
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
                // ... error details
            }
        }
    }
    // No rule matched - assign default low priority
    return &MatchedError{
        Priority: PriorityLow,
        RuleName: "default",
    }
}
```

**Key Design Decision**: Rules are evaluated sequentially, not by priority. This allows operators to define specific high-priority rules first, with broader catch-all rules at the end. Unmatched errors default to P4 (Low).

### 2.3 Multi-Criteria Matching

Each rule can specify multiple match criteria that are evaluated with AND logic:

```go
func (e *Engine) matchRule(rule Rule, err loki.ParsedError) bool {
    // 1. Check namespace filter (if specified)
    if len(rule.Match.Namespaces) > 0 {
        if !e.matchNamespace(rule.Match.Namespaces, err.Namespace) {
            return false
        }
    }

    // 2. Check label matchers (if specified)
    if len(rule.Match.Labels) > 0 {
        if !e.matchLabels(rule.Match.Labels, err.Labels) {
            return false
        }
    }

    // 3. Check regex pattern (if specified)
    if rule.Match.Pattern != "" {
        re := e.patterns[rule.Name]
        if re != nil {
            if !re.MatchString(err.Message) && !re.MatchString(err.Raw) {
                return false
            }
        }
    }

    // 4. Check keywords (if specified) - OR logic within keywords
    if len(rule.Match.Keywords) > 0 {
        if !e.matchKeywords(rule.Match.Keywords, err.Message, err.Raw) {
            return false
        }
    }

    return true  // All specified criteria passed
}
```

---

## 3. Default Priority Classifications

### 3.1 P1 - Critical Errors

These errors indicate immediate service impact requiring urgent attention:

| Rule Name | Pattern | Rationale |
|-----------|---------|-----------|
| `crashloop-backoff` | `CrashLoopBackOff\|Back-off restarting failed container` | Pod repeatedly crashing, service unavailable |
| `oom-killed` | `OOMKilled\|Out of memory\|memory cgroup out of memory` | Memory exhaustion, process killed by kernel |
| `panic` | `(?i)panic:\|runtime error:\|fatal error:` | Application crash, unrecoverable state |

### 3.2 P2 - High Priority Errors

Service degradation or deployment issues requiring prompt attention:

| Rule Name | Pattern | Rationale |
|-----------|---------|-----------|
| `image-pull-error` | `ImagePullBackOff\|ErrImagePull\|failed to pull image` | Deployment blocked, new pods cannot start |
| `probe-failed` | `Readiness probe failed\|Liveness probe failed` | Health check failures, traffic routing affected |

### 3.3 P3 - Medium Priority Errors

Notable issues that may indicate developing problems:

| Rule Name | Pattern | Rationale |
|-----------|---------|-----------|
| `connection-refused` | `connection refused\|ECONNREFUSED` | Network connectivity issues, dependency unavailable |

### 3.4 P4 - Low Priority Errors

Informational errors and catch-all patterns:

| Rule Name | Pattern | Rationale |
|-----------|---------|-----------|
| `generic-error` | `(?i)\berror\b` | Word boundary match for "error", lowest priority catch-all |

---

## 4. Priority Queue Sorting Algorithm

### 4.1 Multi-Factor Sort

The error queue is sorted using a two-factor comparison that ensures:
1. **Higher priority errors appear first** (lower weight)
2. **Within same priority, newer errors appear first** (recent timestamp)

```go
sort.Slice(filtered, func(i, j int) bool {
    wi := filtered[i].Priority.Weight()
    wj := filtered[j].Priority.Weight()

    // Primary sort: by priority weight (ascending)
    if wi != wj {
        return wi < wj
    }

    // Secondary sort: by last seen time (descending - newest first)
    return filtered[i].LastSeen.After(filtered[j].LastSeen)
})
```

### 4.2 Sort Order Example

Given these errors:
```
Error A: P2, LastSeen: 10:05
Error B: P1, LastSeen: 10:00
Error C: P2, LastSeen: 10:10
Error D: P1, LastSeen: 10:02
```

Resulting sort order:
```
1. Error D (P1, Weight=1, 10:02) - P1, more recent
2. Error B (P1, Weight=1, 10:00) - P1, older
3. Error C (P2, Weight=2, 10:10) - P2, more recent
4. Error A (P2, Weight=2, 10:05) - P2, older
```

---

## 5. Pattern Matching Capabilities

### 5.1 Regex Pattern Compilation

For performance, regex patterns are pre-compiled at engine initialization:

```go
func NewEngine(rules []Rule, logger *slog.Logger) (*Engine, error) {
    e := &Engine{
        rules:    rules,
        patterns: make(map[string]*regexp.Regexp),
    }

    // Pre-compile regex patterns
    for _, rule := range rules {
        if rule.Match.Pattern != "" {
            re, err := regexp.Compile(rule.Match.Pattern)
            if err != nil {
                return nil, err
            }
            e.patterns[rule.Name] = re
        }
    }
    return e, nil
}
```

### 5.2 Namespace Filtering with Negation

Namespaces support both whitelist and blacklist patterns using the `!` prefix:

```go
func (e *Engine) matchNamespace(allowed []string, namespace string) bool {
    for _, ns := range allowed {
        // Support negation with !
        if strings.HasPrefix(ns, "!") {
            if namespace == ns[1:] {
                return false  // Explicitly excluded
            }
        } else if namespace == ns {
            return true  // Explicitly included
        }
    }

    // If all rules are negations and none matched, allow
    allNegations := true
    for _, ns := range allowed {
        if !strings.HasPrefix(ns, "!") {
            allNegations = false
            break
        }
    }
    return allNegations
}
```

**Example configurations**:
```yaml
# Only match production namespace
namespaces: ["production"]

# Match everything except kube-system
namespaces: ["!kube-system"]

# Match prod and staging, exclude kube-system
namespaces: ["production", "staging", "!kube-system"]
```

### 5.3 Label Matching with Regex Support

Labels support exact match, negation, and regex patterns:

```go
func (e *Engine) matchLabels(matchers map[string]string, labels map[string]string) bool {
    for key, expected := range matchers {
        actual, exists := labels[key]

        // Negation: !value
        if strings.HasPrefix(expected, "!") {
            if actual == expected[1:] {
                return false
            }
            continue
        }

        if !exists {
            return false
        }

        // Regex: ~pattern
        if strings.HasPrefix(expected, "~") {
            re, err := regexp.Compile(expected[1:])
            if err != nil {
                return false
            }
            if !re.MatchString(actual) {
                return false
            }
            continue
        }

        // Exact match
        if actual != expected {
            return false
        }
    }
    return true
}
```

**Example configurations**:
```yaml
labels:
  app: "nginx"              # Exact match
  environment: "!dev"       # Not equal to "dev"
  version: "~v[0-9]+\\."    # Regex match for versioned apps
```

---

## 6. Error Deduplication

### 6.1 Fingerprint-Based Deduplication

Errors are deduplicated using a fingerprint derived from key attributes. When a duplicate is detected, the existing error's count is incremented:

```go
func (s *MemoryStore) SaveError(err *Error) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Check if we already have this error by fingerprint
    if existing, ok := s.errorsByFP[err.Fingerprint]; ok {
        // Update existing error
        existing.Count++
        existing.LastSeen = err.Timestamp
        if err.Timestamp.Before(existing.FirstSeen) {
            existing.FirstSeen = err.Timestamp
        }
        return nil
    }

    // Store new error
    s.errors[err.ID] = err
    s.errorsByFP[err.Fingerprint] = err
    return nil
}
```

### 6.2 Fingerprint Generation

Fingerprints are generated from:
- Namespace
- Pod name (with replica suffix stripped)
- Container name
- Normalized error message

This ensures that the same error from different pod replicas or at different times is correctly grouped.

---

## 7. Statistics and Aggregation

### 7.1 Priority Distribution Tracking

The system maintains real-time statistics on error distribution:

```go
func (s *MemoryStore) GetStats() (*Stats, error) {
    stats := &Stats{
        TotalErrors:       len(s.errors),
        ErrorsByPriority:  make(map[rules.Priority]int),
        ErrorsByNamespace: make(map[string]int),
        RemediationCount:  len(s.remediationLogs),
    }

    for _, err := range s.errors {
        stats.ErrorsByPriority[err.Priority]++
        stats.ErrorsByNamespace[err.Namespace]++
    }

    return stats, nil
}
```

### 7.2 Dashboard Metrics

The web dashboard displays:
- **Total Errors**: Count of unique error fingerprints
- **P1 (Critical)**: Count of P1 priority errors
- **Remediations**: Total auto-remediation actions taken
- **Success/Failed**: Remediation action outcomes

---

## 8. Configuration Best Practices

### 8.1 Rule Ordering Strategy

1. **Specific rules first**: Place highly specific patterns before general ones
2. **Critical patterns early**: P1 rules should be evaluated before P4 catch-alls
3. **Namespace exclusions**: Use `!kube-system` to avoid system noise
4. **Catch-all last**: Generic error patterns should be the final rule

### 8.2 Recommended Rule Template

```yaml
rules:
  # 1. Critical - Service outages
  - name: crashloop-backoff
    match:
      pattern: "CrashLoopBackOff"
    priority: P1
    enabled: true

  # 2. High - Deployment issues
  - name: image-pull-error
    match:
      pattern: "ImagePullBackOff|ErrImagePull"
    priority: P2
    enabled: true

  # 3. Medium - Connectivity
  - name: connection-refused
    match:
      pattern: "connection refused"
    priority: P3
    enabled: true

  # 4. Low - Catch-all (must be last)
  - name: generic-error
    match:
      pattern: "(?i)\\berror\\b"
    priority: P4
    enabled: true
```

---

## 9. Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Log Ingestion                                │
│                     (Loki Query Results)                             │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Rule Engine                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  For each error:                                             │   │
│  │    1. Iterate rules in definition order                      │   │
│  │    2. Check: namespace → labels → pattern → keywords         │   │
│  │    3. First match wins → assign priority                     │   │
│  │    4. No match → default to P4                               │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Priority Assignment                             │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐               │
│  │   P1    │  │   P2    │  │   P3    │  │   P4    │               │
│  │Critical │  │  High   │  │ Medium  │  │   Low   │               │
│  │Weight=1 │  │Weight=2 │  │Weight=3 │  │Weight=4 │               │
│  └─────────┘  └─────────┘  └─────────┘  └─────────┘               │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Deduplication                                   │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Fingerprint = hash(namespace, pod, container, message)      │   │
│  │  If exists: increment count, update LastSeen                 │   │
│  │  If new: store with Count=1                                  │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Priority Queue                                  │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Sort by:                                                    │   │
│  │    1. Priority Weight (ascending) - P1 before P4            │   │
│  │    2. LastSeen (descending) - newest first within priority  │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 10. Conclusion

The Kube Sentinel priority classification system provides:

1. **Deterministic Classification**: First-match-wins algorithm ensures predictable behavior
2. **Flexible Matching**: Regex, keywords, labels, and namespace filters support complex rules
3. **Efficient Sorting**: Weight-based priority with timestamp secondary sort
4. **Smart Deduplication**: Fingerprint-based grouping reduces noise
5. **Extensible Rules**: YAML configuration allows runtime customization

This architecture enables operations teams to focus on the most critical issues first while maintaining visibility into lower-priority errors for trend analysis.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
