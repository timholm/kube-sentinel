# Kube Sentinel Rule Engine Pattern Matching System

## Technical Whitepaper v1.0

---

## Executive Summary

The Kube Sentinel Rule Engine provides a sophisticated pattern matching system for classifying and prioritizing Kubernetes errors. This document details the rule structure, pattern compilation, multi-criteria matching logic, and dynamic rule update capabilities that power the error classification pipeline.

The engine supports regex patterns, keyword matching, label selectors, and namespace filtering with both inclusion and exclusion semantics. Rules are evaluated using a first-match-wins algorithm, enabling precise control over error classification through rule ordering.

---

## 1. Rule Structure and YAML Schema

### 1.1 Rule Type Definition

Each rule is defined as a Go struct with corresponding YAML tags:

```go
// Rule defines a matching rule for errors
type Rule struct {
    Name        string       `yaml:"name"`
    Match       Match        `yaml:"match"`
    Priority    Priority     `yaml:"priority"`
    Remediation *Remediation `yaml:"remediation,omitempty"`
    Enabled     bool         `yaml:"enabled"`
}

// Match defines the conditions for matching an error
type Match struct {
    Pattern    string            `yaml:"pattern"`              // Regex pattern
    Keywords   []string          `yaml:"keywords,omitempty"`   // Simple keyword match
    Labels     map[string]string `yaml:"labels,omitempty"`     // Label matchers
    Namespaces []string          `yaml:"namespaces,omitempty"` // Namespace whitelist
}
```

### 1.2 Complete YAML Schema

```yaml
rules:
  - name: string              # Required: Unique rule identifier
    match:
      pattern: string         # Optional: Regex pattern for message matching
      keywords:               # Optional: Simple keyword matching (OR logic)
        - string
        - string
      labels:                 # Optional: Kubernetes label matchers
        key: value            # Exact match
        key: "!value"         # Negation (not equal)
        key: "~regex"         # Regex match
      namespaces:             # Optional: Namespace filter
        - string              # Include namespace
        - "!string"           # Exclude namespace
    priority: P1|P2|P3|P4     # Required: Priority level
    remediation:              # Optional: Auto-remediation config
      action: string          # Action type
      params:                 # Optional: Action parameters
        key: value
      cooldown: duration      # Cooldown between actions
    enabled: bool             # Required: Enable/disable rule
```

### 1.3 Priority Levels

The system defines four priority levels with associated weights:

```go
type Priority string

const (
    PriorityCritical Priority = "P1"  // Weight: 1 - Immediate attention
    PriorityHigh     Priority = "P2"  // Weight: 2 - Urgent
    PriorityMedium   Priority = "P3"  // Weight: 3 - Notable
    PriorityLow      Priority = "P4"  // Weight: 4 - Informational
)
```

Priority parsing supports multiple formats:

```go
func ParsePriority(s string) (Priority, error) {
    switch s {
    case "P1", "p1", "critical", "CRITICAL":
        return PriorityCritical, nil
    case "P2", "p2", "high", "HIGH":
        return PriorityHigh, nil
    case "P3", "p3", "medium", "MEDIUM":
        return PriorityMedium, nil
    case "P4", "p4", "low", "LOW":
        return PriorityLow, nil
    default:
        return "", fmt.Errorf("unknown priority: %s", s)
    }
}
```

### 1.4 Example Rule Configuration

```yaml
rules:
  - name: crashloop-backoff
    match:
      pattern: "CrashLoopBackOff|Back-off restarting failed container"
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m
    enabled: true

  - name: oom-killed
    match:
      pattern: "OOMKilled|Out of memory|memory cgroup out of memory"
    priority: P1
    remediation:
      action: none
      cooldown: 10m
    enabled: true

  - name: panic
    match:
      pattern: "(?i)panic:|runtime error:|fatal error:"
    priority: P1
    remediation:
      action: none
      cooldown: 5m
    enabled: true
```

---

## 2. Regex Pattern Compilation and Caching

### 2.1 Engine Architecture

The rule engine maintains a cache of pre-compiled regex patterns to avoid repeated compilation overhead:

```go
// Engine handles rule matching and prioritization
type Engine struct {
    mu     sync.RWMutex
    rules  []Rule
    logger *slog.Logger

    // Compiled regex patterns
    patterns map[string]*regexp.Regexp
}
```

### 2.2 Pattern Pre-Compilation

Patterns are compiled once during engine initialization:

```go
func NewEngine(rules []Rule, logger *slog.Logger) (*Engine, error) {
    e := &Engine{
        rules:    rules,
        logger:   logger,
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

### 2.3 Pattern Caching Strategy

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Pattern Compilation Flow                         │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │   Load Rules YAML    │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │  For Each Rule       │
    │  with Pattern != ""  │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │  regexp.Compile()    │─Err─▶│ Return Error        │
    └──────────┬───────────┘     │ Engine Not Created  │
               │ OK              └─────────────────────┘
               ▼
    ┌──────────────────────┐
    │ Store in patterns    │
    │ map[ruleName]*regexp │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │  Engine Ready        │
    │  O(1) Pattern Lookup │
    └──────────────────────┘
```

**Key Benefits:**
- **O(1) pattern lookup**: Compiled patterns stored by rule name
- **Fail-fast validation**: Invalid patterns caught at startup
- **Memory efficiency**: Each pattern compiled exactly once
- **Thread-safe access**: Protected by RWMutex

### 2.4 Pattern Matching at Runtime

During error matching, patterns are retrieved from the cache:

```go
// Check pattern
if rule.Match.Pattern != "" {
    re := e.patterns[rule.Name]
    if re != nil {
        // Match against both message and raw log line
        if !re.MatchString(err.Message) && !re.MatchString(err.Raw) {
            return false
        }
    }
}
```

**Dual-Target Matching**: Patterns are tested against both the parsed error message and the raw log line, ensuring matches even when parsing extracts only part of the relevant content.

---

## 3. Multi-Criteria Matching

### 3.1 Criteria Evaluation Order

The engine evaluates match criteria in a specific order, optimized for early termination:

```go
func (e *Engine) matchRule(rule Rule, err loki.ParsedError) bool {
    // 1. Check namespace filter (fast, exact match)
    if len(rule.Match.Namespaces) > 0 {
        if !e.matchNamespace(rule.Match.Namespaces, err.Namespace) {
            return false
        }
    }

    // 2. Check label matchers (moderate complexity)
    if len(rule.Match.Labels) > 0 {
        if !e.matchLabels(rule.Match.Labels, err.Labels) {
            return false
        }
    }

    // 3. Check regex pattern (most expensive)
    if rule.Match.Pattern != "" {
        re := e.patterns[rule.Name]
        if re != nil {
            if !re.MatchString(err.Message) && !re.MatchString(err.Raw) {
                return false
            }
        }
    }

    // 4. Check keywords (string contains)
    if len(rule.Match.Keywords) > 0 {
        if !e.matchKeywords(rule.Match.Keywords, err.Message, err.Raw) {
            return false
        }
    }

    return true  // All specified criteria passed
}
```

### 3.2 Criteria Evaluation Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Multi-Criteria Match Flow                         │
│                        (AND Logic)                                   │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │   Incoming Error     │
    │ namespace, labels,   │
    │ message, raw         │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ 1. Namespace Filter  │─No──▶│  MATCH FAILED      │
    │    (if specified)    │     │  Try next rule     │
    └──────────┬───────────┘     └─────────────────────┘
               │ Pass
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ 2. Label Matchers    │─No──▶│  MATCH FAILED      │
    │    (if specified)    │     │  Try next rule     │
    └──────────┬───────────┘     └─────────────────────┘
               │ Pass
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ 3. Regex Pattern     │─No──▶│  MATCH FAILED      │
    │    (if specified)    │     │  Try next rule     │
    └──────────┬───────────┘     └─────────────────────┘
               │ Pass
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ 4. Keyword Search    │─No──▶│  MATCH FAILED      │
    │    (if specified)    │     │  Try next rule     │
    └──────────┬───────────┘     └─────────────────────┘
               │ Pass
               ▼
    ┌──────────────────────┐
    │    MATCH SUCCESS     │
    │ Assign rule priority │
    └──────────────────────┘
```

### 3.3 Example: Complex Multi-Criteria Rule

```yaml
- name: production-database-errors
  match:
    pattern: "connection refused|timeout|deadlock"
    keywords:
      - "postgres"
      - "mysql"
      - "redis"
    labels:
      app: "~.*-db$"           # Regex: ends with "-db"
      environment: "production"
      tier: "!frontend"        # Negation: not frontend
    namespaces:
      - "production"
      - "!kube-system"         # Exclude kube-system
  priority: P1
  enabled: true
```

This rule matches errors that:
1. **AND** are in the `production` namespace (not `kube-system`)
2. **AND** have `environment=production`, `app` matching `*-db`, and `tier` not equal to `frontend`
3. **AND** match the regex pattern for connection issues
4. **AND** contain at least one database keyword

---

## 4. AND Logic for Criteria, OR Logic for Keywords

### 4.1 Criteria-Level AND Logic

All specified criteria must pass for a rule to match. This is implicit in the early-return pattern:

```go
// Each criteria check returns false immediately on failure
if len(rule.Match.Namespaces) > 0 {
    if !e.matchNamespace(...) {
        return false  // Fail fast
    }
}
// Only reaches here if namespace check passed

if len(rule.Match.Labels) > 0 {
    if !e.matchLabels(...) {
        return false  // Fail fast
    }
}
// Only reaches here if BOTH namespace AND labels passed
```

### 4.2 Keyword-Level OR Logic

Keywords use OR logic - any single keyword match is sufficient:

```go
func (e *Engine) matchKeywords(keywords []string, message, raw string) bool {
    combined := strings.ToLower(message + " " + raw)
    for _, kw := range keywords {
        if strings.Contains(combined, strings.ToLower(kw)) {
            return true  // First match wins
        }
    }
    return false  // No keywords matched
}
```

**Key Characteristics:**
- **Case-insensitive**: Both keywords and text are lowercased
- **Combined search**: Searches both message and raw log line
- **Short-circuit evaluation**: Returns immediately on first match

### 4.3 Logic Comparison Table

| Component | Logic | Behavior |
|-----------|-------|----------|
| Namespaces | Whitelist OR Blacklist | Match any inclusion, fail any exclusion |
| Labels | AND | All label matchers must pass |
| Pattern | Single | Regex must match message OR raw |
| Keywords | OR | Any keyword match is sufficient |
| **Criteria** | **AND** | All specified criteria must pass |

### 4.4 Example: OR Logic with Keywords

```yaml
- name: database-errors
  match:
    keywords:
      - "postgres"
      - "mysql"
      - "mongodb"
      - "redis"
  priority: P2
  enabled: true
```

This rule matches if the error message contains ANY of:
- "postgres" OR
- "mysql" OR
- "mongodb" OR
- "redis"

---

## 5. Namespace Negation Syntax

### 5.1 Negation with `!` Prefix

Namespaces support both inclusion (whitelist) and exclusion (blacklist) using the `!` prefix:

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

    // If all rules are negations, and none matched, allow
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

### 5.2 Namespace Matching Decision Tree

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Namespace Matching Algorithm                        │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │   Input: namespace   │
    │   Filter: []string   │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │  For each filter     │
    │  entry               │
    └──────────┬───────────┘
               │
         ┌─────┴─────┐
         │           │
         ▼           ▼
    ┌─────────┐  ┌─────────┐
    │ !prefix │  │No prefix│
    │(exclude)│  │(include)│
    └────┬────┘  └────┬────┘
         │            │
         ▼            ▼
    ┌─────────┐  ┌─────────┐
    │ Match?  │  │ Match?  │
    └────┬────┘  └────┬────┘
         │            │
    Yes: │       Yes: │
    Return│       Return
    FALSE │       TRUE
         │            │
    No:  │       No:  │
    Continue     Continue
         │            │
         └─────┬──────┘
               │
               ▼
    ┌──────────────────────┐
    │ After all entries:   │
    │ If ALL were negations│
    │ → Return TRUE        │
    │ Otherwise            │
    │ → Return FALSE       │
    └──────────────────────┘
```

### 5.3 Namespace Filter Examples

| Filter Configuration | Matches | Does Not Match |
|---------------------|---------|----------------|
| `["production"]` | `production` | `staging`, `dev`, `kube-system` |
| `["!kube-system"]` | `production`, `staging`, `dev` | `kube-system` |
| `["production", "staging"]` | `production`, `staging` | `dev`, `kube-system` |
| `["!kube-system", "!monitoring"]` | `production`, `dev` | `kube-system`, `monitoring` |
| `["production", "!kube-system"]` | `production` | `staging`, `kube-system` |

### 5.4 YAML Configuration Examples

```yaml
# Match only production namespace
- name: production-only
  match:
    namespaces:
      - "production"
    pattern: "error"
  priority: P1

# Match all namespaces except system ones
- name: exclude-system
  match:
    namespaces:
      - "!kube-system"
      - "!kube-public"
      - "!monitoring"
    pattern: "error"
  priority: P2

# Match specific environments, exclude system
- name: app-namespaces
  match:
    namespaces:
      - "production"
      - "staging"
      - "dev"
      - "!kube-system"
    pattern: "error"
  priority: P3
```

---

## 6. Label Matching with Regex Support

### 6.1 Label Matcher Syntax

Labels support three matching modes:
- **Exact match**: `key: "value"`
- **Negation**: `key: "!value"` (not equal)
- **Regex match**: `key: "~pattern"` (regex)

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

### 6.2 Label Matching Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Label Matching Algorithm                         │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │  For each matcher    │
    │  (key: expected)     │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │ Starts with "!" ?    │
    └──────────┬───────────┘
               │
         ┌─────┴─────┐
         │ Yes       │ No
         ▼           ▼
    ┌─────────┐  ┌─────────────┐
    │Negation │  │Label exists?│
    │actual == │  └──────┬──────┘
    │expected? │         │
    └────┬────┘    No:   │ Yes
         │    Return     │
    Yes: │    FALSE      │
    Return               ▼
    FALSE           ┌─────────┐
         │         │Starts   │
    No:  │         │with "~"?│
    Continue       └────┬────┘
                        │
                  ┌─────┴─────┐
                  │ Yes       │ No
                  ▼           ▼
             ┌─────────┐  ┌─────────┐
             │ Compile │  │ Exact   │
             │ regex   │  │ match?  │
             └────┬────┘  └────┬────┘
                  │            │
             ┌────┴────┐  ┌────┴────┐
             │ Match?  │  │         │
             └────┬────┘  │         │
                  │       │         │
             No:  │  No:  │    Yes: │
             Return  Return   Continue
             FALSE   FALSE
```

### 6.3 Label Matcher Examples

```yaml
labels:
  # Exact match - app must equal "nginx"
  app: "nginx"

  # Negation - environment must NOT equal "dev"
  environment: "!dev"

  # Regex - version must match pattern v followed by digits
  version: "~v[0-9]+\\.[0-9]+\\.[0-9]+"

  # Regex - app name must end with "-api"
  service: "~.*-api$"

  # Negation with regex not supported - use exact negation
  tier: "!frontend"
```

### 6.4 Complete Label Matching Example

```yaml
- name: production-api-errors
  match:
    labels:
      app: "~.*-api$"              # Any API service
      environment: "production"    # In production
      team: "!infrastructure"      # Not infrastructure team
      version: "~v2\\.[0-9]+"      # Version 2.x
    pattern: "5[0-9]{2}|error"
  priority: P1
  enabled: true
```

**Matches errors from pods with:**
- `app` label ending in `-api` (e.g., `user-api`, `order-api`)
- `environment=production`
- `team` label NOT equal to `infrastructure`
- `version` label matching `v2.0`, `v2.1`, etc.

---

## 7. First-Match-Wins Algorithm

### 7.1 Algorithm Implementation

The rule engine processes rules in definition order. The first matching rule determines the error's priority:

```go
func (e *Engine) Match(err loki.ParsedError) *MatchedError {
    e.mu.RLock()
    defer e.mu.RUnlock()

    // Try rules in order (first match wins)
    for _, rule := range e.rules {
        if !rule.Enabled {
            continue
        }

        if e.matchRule(rule, err) {
            return &MatchedError{
                ID:          err.ID,
                Fingerprint: err.Fingerprint,
                Timestamp:   err.Timestamp,
                Namespace:   err.Namespace,
                Pod:         err.Pod,
                Container:   err.Container,
                Message:     err.Message,
                Labels:      err.Labels,
                Raw:         err.Raw,
                Priority:    rule.Priority,
                RuleName:    rule.Name,
                Count:       1,
                FirstSeen:   err.Timestamp,
                LastSeen:    err.Timestamp,
            }
        }
    }

    // No rule matched - assign default low priority
    return &MatchedError{
        ID:          err.ID,
        Fingerprint: err.Fingerprint,
        Timestamp:   err.Timestamp,
        Namespace:   err.Namespace,
        Pod:         err.Pod,
        Container:   err.Container,
        Message:     err.Message,
        Labels:      err.Labels,
        Raw:         err.Raw,
        Priority:    PriorityLow,
        RuleName:    "default",
        Count:       1,
        FirstSeen:   err.Timestamp,
        LastSeen:    err.Timestamp,
    }
}
```

### 7.2 First-Match-Wins Visualization

```
┌─────────────────────────────────────────────────────────────────────┐
│                   First-Match-Wins Algorithm                         │
└─────────────────────────────────────────────────────────────────────┘

    Error: "CrashLoopBackOff: container failed"

    ┌─────────────────────────────────────────────────────────────────┐
    │ Rule 1: crashloop-backoff (P1)                                  │
    │ Pattern: "CrashLoopBackOff|Back-off"                            │
    │ Result: MATCH ──────────────────────────────────▶ Return P1     │
    └─────────────────────────────────────────────────────────────────┘
                                                          │
                                                          │ (stops here)
                                                          │
    ┌─────────────────────────────────────────────────────────────────┐
    │ Rule 2: container-error (P2)                      (not checked) │
    │ Pattern: "container.*failed"                                    │
    └─────────────────────────────────────────────────────────────────┘

    ┌─────────────────────────────────────────────────────────────────┐
    │ Rule 3: generic-error (P4)                        (not checked) │
    │ Pattern: "(?i)\\berror\\b"                                      │
    └─────────────────────────────────────────────────────────────────┘
```

### 7.3 Rule Ordering Strategy

**Recommended rule ordering (most specific to least specific):**

1. **Critical patterns first** (P1): CrashLoopBackOff, OOMKilled, panic
2. **High-priority patterns** (P2): Image pull errors, probe failures
3. **Namespace-specific rules**: Production-specific error handling
4. **Label-filtered rules**: Team or service-specific patterns
5. **Medium priority** (P3): Connection errors, timeouts
6. **Catch-all last** (P4): Generic error patterns

```yaml
rules:
  # 1. Most critical - service outages
  - name: crashloop-backoff
    match:
      pattern: "CrashLoopBackOff"
    priority: P1
    enabled: true

  # 2. Critical - memory issues
  - name: oom-killed
    match:
      pattern: "OOMKilled"
    priority: P1
    enabled: true

  # 3. High - deployment issues
  - name: image-pull-error
    match:
      pattern: "ImagePullBackOff|ErrImagePull"
    priority: P2
    enabled: true

  # 4. Medium - connectivity
  - name: connection-refused
    match:
      pattern: "connection refused|ECONNREFUSED"
    priority: P3
    enabled: true

  # 5. LAST - catch-all (must be last)
  - name: generic-error
    match:
      pattern: "(?i)\\berror\\b"
    priority: P4
    enabled: true
```

### 7.4 Default Rule Behavior

When no rules match, errors receive default classification:

```go
// No rule matched - assign default low priority
return &MatchedError{
    Priority: PriorityLow,
    RuleName: "default",
    // ... error details preserved
}
```

---

## 8. Rule Validation and Error Handling

### 8.1 Rule Validation

Each rule is validated during loading:

```go
func (r *Rule) Validate() error {
    if r.Name == "" {
        return fmt.Errorf("rule name is required")
    }

    if r.Match.Pattern == "" && len(r.Match.Keywords) == 0 {
        return fmt.Errorf("rule %s: either pattern or keywords is required", r.Name)
    }

    if _, err := ParsePriority(string(r.Priority)); err != nil {
        return fmt.Errorf("rule %s: %w", r.Name, err)
    }

    return nil
}
```

### 8.2 Validation Rules

| Check | Error Message | Resolution |
|-------|--------------|------------|
| Empty name | "rule name is required" | Add unique `name` field |
| No pattern or keywords | "either pattern or keywords is required" | Add `pattern` or `keywords` |
| Invalid priority | "unknown priority: X" | Use P1, P2, P3, or P4 |
| Invalid regex | "error parsing regexp" | Fix regex syntax |

### 8.3 Loading with Defaults

The loader applies sensible defaults during parsing:

```go
func ParseRules(data []byte) ([]Rule, error) {
    var config RulesConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("parsing rules YAML: %w", err)
    }

    // Set defaults and validate
    for i := range config.Rules {
        rule := &config.Rules[i]

        // Default enabled to true
        if !rule.Enabled {
            rule.Enabled = true
        }

        // Default cooldown
        if rule.Remediation != nil && rule.Remediation.Cooldown == 0 {
            rule.Remediation.Cooldown = 5 * time.Minute
        }

        // Default action to none
        if rule.Remediation == nil {
            rule.Remediation = &Remediation{
                Action:   ActionNone,
                Cooldown: 5 * time.Minute,
            }
        }

        if err := rule.Validate(); err != nil {
            return nil, err
        }
    }

    return config.Rules, nil
}
```

### 8.4 Default Values Applied

| Field | Default Value | Notes |
|-------|--------------|-------|
| `enabled` | `true` | Rules are active by default |
| `remediation.cooldown` | `5m` | Five minute cooldown |
| `remediation.action` | `none` | No auto-remediation |

### 8.5 Error Handling During Engine Creation

Pattern compilation errors during engine creation cause immediate failure:

```go
func NewEngine(rules []Rule, logger *slog.Logger) (*Engine, error) {
    // ...
    for _, rule := range rules {
        if rule.Match.Pattern != "" {
            re, err := regexp.Compile(rule.Match.Pattern)
            if err != nil {
                return nil, err  // Fail-fast on invalid regex
            }
            e.patterns[rule.Name] = re
        }
    }
    return e, nil
}
```

This fail-fast approach ensures configuration errors are caught at startup rather than runtime.

---

## 9. Dynamic Rule Updates

### 9.1 Update Rules Method

The engine supports hot-reloading rules without restart:

```go
func (e *Engine) UpdateRules(rules []Rule) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    patterns := make(map[string]*regexp.Regexp)
    for _, rule := range rules {
        if rule.Match.Pattern != "" {
            re, err := regexp.Compile(rule.Match.Pattern)
            if err != nil {
                return err
            }
            patterns[rule.Name] = re
        }
    }

    e.rules = rules
    e.patterns = patterns
    return nil
}
```

### 9.2 Atomic Update Process

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Atomic Rule Update                               │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │  New Rules Received  │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │ Acquire Write Lock   │  (blocks all reads)
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │ Compile New Patterns │
    │ (into temp map)      │
    └──────────┬───────────┘
               │
         ┌─────┴─────┐
         │           │
    Error│           │Success
         ▼           ▼
    ┌─────────┐  ┌─────────────┐
    │ Unlock  │  │Replace rules│
    │ Return  │  │Replace pats │
    │ error   │  └──────┬──────┘
    └─────────┘         │
                        ▼
                   ┌─────────┐
                   │ Unlock  │
                   │ Success │
                   └─────────┘
```

**Key Safety Properties:**
- **Write lock held**: No reads during update
- **Temp pattern map**: Old patterns preserved if compilation fails
- **Atomic swap**: Rules and patterns replaced together
- **Error rollback**: Failed updates leave engine unchanged

### 9.3 File Watching for Auto-Reload

The loader supports watching the rules file for changes:

```go
func (l *Loader) Watch() (<-chan []Rule, error) {
    ch := make(chan []Rule, 1)

    // Poll the file every 30 seconds
    go func() {
        var lastModTime time.Time
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for range ticker.C {
            info, err := os.Stat(l.path)
            if err != nil {
                continue
            }

            if info.ModTime().After(lastModTime) {
                lastModTime = info.ModTime()
                rules, err := l.Load()
                if err != nil {
                    continue
                }
                ch <- rules
            }
        }
    }()

    return ch, nil
}
```

### 9.4 Integration Pattern

```go
// Start watching for rule changes
rulesChan, _ := loader.Watch()

go func() {
    for newRules := range rulesChan {
        if err := engine.UpdateRules(newRules); err != nil {
            logger.Error("failed to update rules", "error", err)
            continue
        }
        logger.Info("rules reloaded", "count", len(newRules))
    }
}()
```

### 9.5 Thread-Safe Rule Access

The engine provides safe access to current rules:

```go
func (e *Engine) GetRules() []Rule {
    e.mu.RLock()
    defer e.mu.RUnlock()

    result := make([]Rule, len(e.rules))
    copy(result, e.rules)
    return result
}

func (e *Engine) GetRuleByName(name string) *Rule {
    e.mu.RLock()
    defer e.mu.RUnlock()

    for _, rule := range e.rules {
        if rule.Name == name {
            return &rule
        }
    }
    return nil
}
```

---

## 10. Testing Patterns with TestPattern

### 10.1 TestPattern Method

The engine provides a utility method for testing patterns against sample text:

```go
func (e *Engine) TestPattern(pattern, sample string) (bool, error) {
    re, err := regexp.Compile(pattern)
    if err != nil {
        return false, err
    }
    return re.MatchString(sample), nil
}
```

### 10.2 Use Cases

**1. Rule Development**: Test patterns before adding to configuration

```go
engine, _ := rules.NewEngine(existingRules, logger)

// Test new pattern
matched, err := engine.TestPattern(
    `CrashLoopBackOff|Back-off.*container`,
    "Back-off restarting failed container",
)
// matched = true, err = nil
```

**2. Pattern Validation**: Validate regex syntax

```go
_, err := engine.TestPattern(
    `[invalid(regex`,  // Unclosed bracket
    "any sample",
)
// err = "error parsing regexp: missing closing ]"
```

**3. Debugging**: Verify why a rule does or does not match

```go
// Test against actual log line
matched, _ := engine.TestPattern(
    `(?i)\\berror\\b`,
    "ERROR: Connection failed",
)
// matched = true (case insensitive word boundary match)
```

### 10.3 Common Pattern Testing Scenarios

| Pattern | Sample | Result | Notes |
|---------|--------|--------|-------|
| `CrashLoopBackOff` | "CrashLoopBackOff: restarting" | true | Exact substring |
| `(?i)error` | "ERROR occurred" | true | Case insensitive |
| `\\berror\\b` | "myerror occurred" | false | Word boundary fails |
| `\\berror\\b` | "error occurred" | true | Word boundary matches |
| `[0-9]{3}` | "HTTP 500" | true | Digit pattern |
| `OOM\|oom` | "OOMKilled" | true | Alternation |

### 10.4 Pattern Testing Best Practices

1. **Test with real log samples**: Use actual error messages from your cluster
2. **Test edge cases**: Empty strings, special characters, case variations
3. **Test negatives**: Verify patterns do NOT match unintended content
4. **Validate before deployment**: Run TestPattern on all new patterns

```go
// Comprehensive pattern test
testCases := []struct {
    pattern string
    sample  string
    want    bool
}{
    // Should match
    {"CrashLoopBackOff", "CrashLoopBackOff", true},
    {"CrashLoopBackOff", "Back-off restarting failed container", false},

    // Should not match
    {"(?i)\\berror\\b", "errors", false},  // Word boundary
    {"(?i)\\berror\\b", "error:", true},   // Punctuation OK
}

for _, tc := range testCases {
    got, _ := engine.TestPattern(tc.pattern, tc.sample)
    if got != tc.want {
        log.Printf("FAIL: pattern=%q sample=%q got=%v want=%v",
            tc.pattern, tc.sample, got, tc.want)
    }
}
```

---

## 11. Default Rules Reference

### 11.1 Built-in Default Rules

The loader provides sensible default rules when no configuration is provided:

```go
func DefaultRules() []Rule {
    return []Rule{
        {
            Name: "crashloop-backoff",
            Match: Match{
                Pattern: `CrashLoopBackOff|Back-off restarting failed container`,
            },
            Priority: PriorityCritical,
            Remediation: &Remediation{
                Action:   ActionRestartPod,
                Cooldown: 5 * time.Minute,
            },
            Enabled: true,
        },
        {
            Name: "oom-killed",
            Match: Match{
                Pattern: `OOMKilled|Out of memory|memory cgroup out of memory`,
            },
            Priority: PriorityCritical,
            Remediation: &Remediation{
                Action:   ActionNone,
                Cooldown: 10 * time.Minute,
            },
            Enabled: true,
        },
        // ... additional defaults
    }
}
```

### 11.2 Default Rules Table

| Rule Name | Pattern | Priority | Action | Cooldown |
|-----------|---------|----------|--------|----------|
| `crashloop-backoff` | `CrashLoopBackOff\|Back-off restarting failed container` | P1 | restart-pod | 5m |
| `oom-killed` | `OOMKilled\|Out of memory\|memory cgroup out of memory` | P1 | none | 10m |
| `image-pull-error` | `ImagePullBackOff\|ErrImagePull\|failed to pull image` | P2 | none | 5m |
| `readiness-probe-failed` | `Readiness probe failed\|Liveness probe failed` | P2 | restart-pod | 3m |
| `connection-refused` | `connection refused\|ECONNREFUSED\|dial tcp.*refused` | P3 | none | 2m |
| `context-deadline-exceeded` | `context deadline exceeded\|context canceled\|timeout` | P3 | none | 2m |
| `panic` | `(?i)panic:\|runtime error:\|fatal error:` | P1 | none | 5m |
| `permission-denied` | `permission denied\|access denied\|forbidden\|unauthorized` | P3 | none | 5m |
| `generic-error` | `(?i)\\berror\\b` | P4 | none | 5m |

---

## 12. Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                        YAML Configuration                            │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  rules:                                                      │   │
│  │    - name: crashloop                                         │   │
│  │      match:                                                  │   │
│  │        pattern: "CrashLoopBackOff"                           │   │
│  │        namespaces: ["production", "!kube-system"]           │   │
│  │        labels: {app: "~.*-api$"}                            │   │
│  │      priority: P1                                           │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Rule Loader                                  │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  1. Parse YAML                                               │   │
│  │  2. Apply defaults (enabled, cooldown, action)               │   │
│  │  3. Validate each rule                                       │   │
│  │  4. Watch for file changes (every 30s)                       │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Rule Engine                                  │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  rules []Rule           ◄─────── Ordered list of rules       │   │
│  │  patterns map[string]*regexp ◄── Pre-compiled regex cache    │   │
│  │  mu sync.RWMutex        ◄─────── Thread-safe access          │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Match Algorithm                                 │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  For each rule (in order):                                   │   │
│  │    1. Skip if disabled                                       │   │
│  │    2. Check namespace (whitelist/blacklist)                  │   │
│  │    3. Check labels (exact/negation/regex)                    │   │
│  │    4. Check pattern (pre-compiled regex)                     │   │
│  │    5. Check keywords (case-insensitive OR)                   │   │
│  │    6. First match wins → return priority                     │   │
│  │  No match → return P4 (default)                              │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Matched Error                                   │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  ID:          "err-12345"                                    │   │
│  │  Fingerprint: "abc123..."                                    │   │
│  │  Namespace:   "production"                                   │   │
│  │  Pod:         "api-server-5d4f6"                            │   │
│  │  Message:     "CrashLoopBackOff: container failed"          │   │
│  │  Priority:    P1 (Critical)                                 │   │
│  │  RuleName:    "crashloop-backoff"                           │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 13. Batch Processing

### 13.1 MatchBatch Method

For efficiency, the engine supports batch matching:

```go
func (e *Engine) MatchBatch(errors []loki.ParsedError) []*MatchedError {
    result := make([]*MatchedError, 0, len(errors))
    for _, err := range errors {
        if matched := e.Match(err); matched != nil {
            result = append(result, matched)
        }
    }
    return result
}
```

### 13.2 Batch Processing Characteristics

- **Pre-allocated slice**: Capacity set to input length for efficiency
- **Single lock acquisition**: Each Match() call acquires read lock
- **No parallelism**: Sequential processing maintains rule order semantics
- **All errors matched**: Every error gets at least default priority

---

## 14. Conclusion

The Kube Sentinel Rule Engine provides a powerful and flexible pattern matching system with:

1. **Multi-Criteria Matching**: Combine namespaces, labels, patterns, and keywords with AND logic
2. **Flexible Syntax**: Support for regex patterns, negation (`!`), and label regex (`~`)
3. **Performance Optimization**: Pre-compiled regex patterns with O(1) lookup
4. **First-Match-Wins**: Deterministic priority assignment through rule ordering
5. **Dynamic Updates**: Hot-reload rules without service restart
6. **Comprehensive Validation**: Fail-fast on invalid configuration
7. **Thread-Safe Design**: RWMutex protects concurrent access
8. **Testing Support**: TestPattern utility for development and debugging

This architecture enables operations teams to create sophisticated error classification rules that accurately prioritize Kubernetes errors based on multiple criteria while maintaining high performance through pattern caching and early-exit evaluation.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
