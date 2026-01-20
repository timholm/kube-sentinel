# Rule Engine

The kube-sentinel rule engine is the core component responsible for classifying Kubernetes errors based on configurable matching rules. It evaluates incoming log entries against a set of rules to determine priority levels and trigger appropriate remediation actions.

## Overview

The rule engine (`internal/rules/engine.go`) provides:

- **Pattern Matching**: Flexible matching using regex patterns, keywords, labels, and namespace filters
- **Priority Assignment**: Automatic classification of errors into priority levels (P1-P4)
- **Thread-Safe Operations**: Concurrent access support with read-write mutex protection
- **Hot Reloading**: Dynamic rule updates without service restart
- **Batch Processing**: Efficient matching of multiple errors in a single operation

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Rule Engine                              │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │   Rules     │  │  Compiled   │  │      Sync Mutex         │  │
│  │   []Rule    │  │  Patterns   │  │  (Thread-Safe Access)   │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│                      Match Pipeline                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │Namespace │─▶│  Labels  │─▶│ Pattern  │─▶│ Keywords │        │
│  │  Filter  │  │  Match   │  │  (Regex) │  │  Match   │        │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

## Rule Evaluation Strategy

### First Match Wins

The rule engine uses a **first-match-wins** evaluation strategy. Rules are evaluated in the order they are defined, and the first rule that matches an error determines its priority and remediation action.

```go
// Try rules in order (first match wins)
for _, rule := range e.rules {
    if !rule.Enabled {
        continue
    }
    if e.matchRule(rule, err) {
        return &MatchedError{...}
    }
}
```

**Implications:**

1. **Rule ordering matters**: Place more specific rules before general ones
2. **Critical rules first**: Higher priority rules should appear earlier in the configuration
3. **Catch-all rules last**: Default or fallback rules should be at the end
4. **Disabled rules are skipped**: Rules with `enabled: false` are bypassed entirely

### Default Behavior

If no rule matches an error, the engine assigns a default low priority (P4) with the rule name "default". This ensures all errors are captured and classified, even if they do not match any specific rule.

## Pattern Matching

The engine supports four types of matching criteria, all of which must pass for a rule to match (AND logic). Within each criterion, the behavior varies.

### 1. Namespace Filtering

Namespace matching restricts rules to specific Kubernetes namespaces.

**Syntax:**
- Direct match: `production` matches only the "production" namespace
- Negation: `!kube-system` excludes the "kube-system" namespace

**Logic:**
```go
func (e *Engine) matchNamespace(allowed []string, namespace string) bool {
    for _, ns := range allowed {
        if strings.HasPrefix(ns, "!") {
            if namespace == ns[1:] {
                return false  // Explicit exclusion
            }
        } else if namespace == ns {
            return true  // Explicit inclusion
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

**Examples:**

| Configuration | Behavior |
|--------------|----------|
| `["production"]` | Matches only "production" namespace |
| `["production", "staging"]` | Matches "production" OR "staging" |
| `["!kube-system"]` | Matches any namespace except "kube-system" |
| `["!kube-system", "!monitoring"]` | Matches any namespace except "kube-system" and "monitoring" |

### 2. Label Matching

Label matching filters errors based on Kubernetes labels attached to the source pod or container.

**Syntax:**
- Exact match: `app: frontend` matches pods with label `app=frontend`
- Negation: `app: !debug` excludes pods with label `app=debug`
- Regex match: `app: ~frontend-.*` matches using regex pattern

**Logic:**
```go
func (e *Engine) matchLabels(matchers map[string]string, labels map[string]string) bool {
    for key, expected := range matchers {
        actual, exists := labels[key]

        // Negation check
        if strings.HasPrefix(expected, "!") {
            if actual == expected[1:] {
                return false
            }
            continue
        }

        // Label must exist for positive matches
        if !exists {
            return false
        }

        // Regex matching with ~ prefix
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

**Examples:**

| Configuration | Behavior |
|--------------|----------|
| `app: api-server` | Matches exact label value |
| `app: !test` | Matches if `app` label is not "test" |
| `version: ~v[0-9]+\.[0-9]+` | Matches version labels like "v1.0", "v2.3" |
| `tier: backend, env: prod` | Both labels must match (AND logic) |

### 3. Regex Pattern Matching

Pattern matching uses regular expressions to match against error messages and raw log content.

**Behavior:**
- Patterns are pre-compiled at engine initialization for performance
- Patterns are tested against both the parsed `Message` and the `Raw` log line
- If either matches, the pattern criterion passes

```go
if rule.Match.Pattern != "" {
    re := e.patterns[rule.Name]
    if re != nil {
        if !re.MatchString(err.Message) && !re.MatchString(err.Raw) {
            return false
        }
    }
}
```

**Examples:**

| Pattern | Matches |
|---------|---------|
| `OOMKilled` | Out of memory errors |
| `connection refused\|timeout` | Connection failures |
| `(?i)error.*database` | Case-insensitive database errors |
| `panic:.*runtime error` | Go runtime panics |
| `\[ERROR\].*MySQL` | MySQL errors with ERROR prefix |

### 4. Keyword Matching

Keyword matching provides simple substring search with case-insensitive comparison.

**Behavior:**
- Keywords are matched against a combined string of message and raw log
- Matching is case-insensitive
- Any keyword match is sufficient (OR logic)

```go
func (e *Engine) matchKeywords(keywords []string, message, raw string) bool {
    combined := strings.ToLower(message + " " + raw)
    for _, kw := range keywords {
        if strings.Contains(combined, strings.ToLower(kw)) {
            return true
        }
    }
    return false
}
```

**Examples:**

| Keywords | Matches |
|----------|---------|
| `["error", "failed"]` | Any message containing "error" OR "failed" |
| `["OOMKilled"]` | Out of memory kill events |
| `["connection", "refused"]` | Connection issues (either keyword) |

## Match Logic Summary

For a rule to match an error, all specified criteria must pass:

```
Rule Match = (Namespace Match) AND (Label Match) AND (Pattern Match) AND (Keyword Match)
```

Within each criterion:
- **Namespaces**: OR logic for inclusions, AND logic for exclusions
- **Labels**: AND logic (all label conditions must match)
- **Pattern**: Single regex, tested against message OR raw log
- **Keywords**: OR logic (any keyword match succeeds)

## TestPattern Functionality

The engine provides a `TestPattern` method for validating regex patterns before deployment:

```go
func (e *Engine) TestPattern(pattern, sample string) (bool, error) {
    re, err := regexp.Compile(pattern)
    if err != nil {
        return false, err
    }
    return re.MatchString(sample), nil
}
```

**Use Cases:**

1. **API Endpoint**: Expose pattern testing via REST API for rule validation
2. **Configuration Validation**: Verify patterns during configuration loading
3. **Development/Debugging**: Test patterns against sample log lines
4. **UI Integration**: Provide real-time feedback in rule configuration interfaces

**Example Usage:**

```go
engine, _ := NewEngine(rules, logger)

// Test a pattern
matched, err := engine.TestPattern(`connection refused`, "Error: connection refused to host")
// matched = true, err = nil

// Test an invalid pattern
matched, err = engine.TestPattern(`[invalid`, "sample text")
// matched = false, err = "error parsing regexp: missing closing ]"
```

## Thread Safety

The engine uses `sync.RWMutex` to ensure thread-safe operations:

| Operation | Lock Type | Description |
|-----------|-----------|-------------|
| `Match()` | Read Lock | Multiple concurrent matches allowed |
| `MatchBatch()` | Read Lock (per item) | Batch processing with read locks |
| `GetRules()` | Read Lock | Safe rule retrieval |
| `GetRuleByName()` | Read Lock | Single rule lookup |
| `UpdateRules()` | Write Lock | Exclusive access for updates |

## API Reference

### Engine Methods

| Method | Description |
|--------|-------------|
| `NewEngine(rules []Rule, logger *slog.Logger) (*Engine, error)` | Creates a new engine with pre-compiled patterns |
| `Match(err loki.ParsedError) *MatchedError` | Matches a single error against all rules |
| `MatchBatch(errors []loki.ParsedError) []*MatchedError` | Matches multiple errors in batch |
| `UpdateRules(rules []Rule) error` | Hot-reloads rules with new configuration |
| `GetRules() []Rule` | Returns a copy of current rules |
| `GetRuleByName(name string) *Rule` | Retrieves a specific rule by name |
| `TestPattern(pattern, sample string) (bool, error)` | Tests a regex pattern against sample text |

### MatchedError Structure

When a rule matches, a `MatchedError` is returned containing:

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique error identifier |
| `Fingerprint` | `string` | Error fingerprint for deduplication |
| `Timestamp` | `time.Time` | When the error occurred |
| `Namespace` | `string` | Kubernetes namespace |
| `Pod` | `string` | Pod name |
| `Container` | `string` | Container name |
| `Message` | `string` | Parsed error message |
| `Labels` | `map[string]string` | Kubernetes labels |
| `Raw` | `string` | Original log line |
| `Priority` | `Priority` | Assigned priority (P1-P4) |
| `RuleName` | `string` | Name of the matched rule |
| `Count` | `int` | Occurrence count |
| `FirstSeen` | `time.Time` | First occurrence timestamp |
| `LastSeen` | `time.Time` | Most recent occurrence |
| `Remediated` | `bool` | Whether remediation was applied |

## Future Improvements

### Performance Optimizations

1. **Pattern Indexing**: Build an inverted index of common terms to quickly eliminate non-matching rules without full regex evaluation.

2. **Bloom Filters**: Use bloom filters for keyword pre-filtering to reduce the number of rules that need full evaluation.

3. **Parallel Matching**: For large rule sets, evaluate rules in parallel using goroutine pools with early termination on first match.

4. **Compiled Keyword Sets**: Pre-process keywords into optimized data structures (e.g., Aho-Corasick automaton) for multi-pattern matching.

5. **Rule Caching**: Cache match results by error fingerprint to avoid re-evaluating identical errors.

### Alternative Matching Strategies

1. **Weighted Scoring**: Instead of first-match-wins, calculate a score for each matching rule and select the highest-scoring match. This allows for more nuanced prioritization.

2. **Rule Groups**: Organize rules into groups with different evaluation strategies (first-match within group, best-match across groups).

3. **Machine Learning Classification**: Train a classifier on historical error data to predict priority levels, using rules as features or fallback.

4. **Decision Trees**: Convert rule sets into optimized decision trees for O(log n) matching complexity.

5. **Probabilistic Matching**: Use probabilistic data structures for approximate matching in high-throughput scenarios.

### Feature Enhancements

1. **Time-Based Rules**: Match based on time of day, day of week, or specific time windows (e.g., higher priority during business hours).

2. **Rate-Based Matching**: Trigger rules based on error frequency (e.g., escalate priority if error count exceeds threshold).

3. **Correlation Rules**: Match patterns across multiple related errors (e.g., cascade failures).

4. **Dynamic Priority**: Adjust priority based on contextual factors (deployment in progress, incident declared, etc.).

5. **Rule Dependencies**: Define rules that only activate after other rules have matched.

6. **Pattern Extraction**: Extract and store matched groups from regex patterns for use in remediation actions.

### Operational Improvements

1. **Rule Metrics**: Track match counts, latency, and false positive rates per rule for optimization.

2. **A/B Testing**: Support for testing new rules against a subset of traffic before full deployment.

3. **Rule Versioning**: Maintain rule history and support rollback to previous configurations.

4. **Conflict Detection**: Identify overlapping or contradictory rules during configuration validation.

5. **Rule Simulation**: Replay historical errors against new rule sets to predict behavior changes.
