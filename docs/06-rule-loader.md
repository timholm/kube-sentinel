# Rule Loader

The Rule Loader is a core component of Kube Sentinel responsible for loading, parsing, validating, and monitoring rule configurations from YAML files. This document provides a comprehensive overview of how rules are managed within the system.

## Overview

The rule loader (`internal/rules/loader.go`) provides the following capabilities:

- Loading rules from YAML configuration files
- Parsing and validating rule definitions
- Applying sensible defaults for missing fields
- Providing built-in default rules for common Kubernetes error patterns
- File watching for configuration changes

## Loading Rules from YAML

### Basic Usage

Rules are loaded using the `Loader` type, which is initialized with a path to the rules configuration file:

```go
loader := rules.NewLoader("/path/to/rules.yaml")
rules, err := loader.Load()
if err != nil {
    log.Fatalf("Failed to load rules: %v", err)
}
```

### Loading Process

The loading process follows these steps:

1. **File Reading**: The loader reads the entire contents of the specified YAML file
2. **YAML Parsing**: The raw bytes are unmarshaled into a `RulesConfig` struct
3. **Default Application**: Missing optional fields receive sensible defaults
4. **Validation**: Each rule is validated to ensure required fields are present and valid
5. **Return**: The validated slice of rules is returned to the caller

### Default Values

When rules are parsed, the loader automatically applies the following defaults:

| Field | Default Value | Notes |
|-------|---------------|-------|
| `enabled` | `true` | Rules are enabled by default |
| `remediation.cooldown` | `5m` | Prevents action spam for recurring errors |
| `remediation.action` | `none` | If no remediation is specified, defaults to alert-only |

## Rule File Structure

### Top-Level Structure

The rules configuration file follows this structure:

```yaml
rules:
  - name: rule-name
    match:
      pattern: "regex-pattern"
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m
    enabled: true
```

### Rule Fields

Each rule consists of the following fields:

#### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique identifier for the rule |
| `match.pattern` or `match.keywords` | string or string[] | At least one matching criterion is required |
| `priority` | string | Severity level (P1-P4) |

#### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Whether the rule is active |
| `match.labels` | map[string]string | - | Label selectors for pod matching |
| `match.namespaces` | string[] | - | Namespace whitelist |
| `remediation.action` | string | `none` | Action to take on match |
| `remediation.cooldown` | duration | `5m` | Minimum time between actions |
| `remediation.params` | map[string]string | - | Action-specific parameters |

### Priority Levels

Priority levels indicate the severity of matched errors:

| Priority | Label | Weight | Description |
|----------|-------|--------|-------------|
| `P1` | Critical | 1 | Immediate attention required |
| `P2` | High | 2 | Important issues that need prompt resolution |
| `P3` | Medium | 3 | Issues that should be addressed soon |
| `P4` | Low | 4 | Minor issues or informational |

Priority can be specified using either the code (`P1`, `P2`, etc.) or the label (`critical`, `high`, etc.).

### Remediation Actions

The following remediation actions are available:

| Action | Description |
|--------|-------------|
| `none` | Alert only, no automatic action |
| `restart-pod` | Delete the pod to trigger a restart |
| `scale-up` | Increase replica count |
| `scale-down` | Decrease replica count |
| `rollback` | Rollback to previous deployment revision |
| `delete-stuck-pods` | Remove pods stuck in terminating state |
| `exec-script` | Execute a custom script |

### Match Patterns

Match patterns use Go regular expression syntax. Some useful examples:

```yaml
# Case-insensitive match
pattern: "(?i)error"

# Multiple alternatives
pattern: "OOMKilled|Out of memory|memory cgroup out of memory"

# Word boundary match
pattern: "\\berror\\b"

# Complex pattern with capture groups
pattern: "goroutine .* \\[running\\]"
```

### Example Rule Configuration

```yaml
rules:
  # Match CrashLoopBackOff events and restart the pod
  - name: crashloop-backoff
    match:
      pattern: "CrashLoopBackOff|Back-off restarting failed container"
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m
    enabled: true

  # Match errors in production namespaces only
  - name: production-critical
    match:
      pattern: "(?i)error"
      labels:
        environment: "production"
      namespaces:
        - prod
        - production
    priority: P1
    remediation:
      action: none
      cooldown: 2m
    enabled: true
```

## Default Rules

Kube Sentinel provides a comprehensive set of built-in default rules that cover common Kubernetes error patterns. These rules are available via the `DefaultRules()` function and are used when no custom rules file is specified.

### Built-in Rules

| Rule Name | Priority | Pattern | Action | Cooldown |
|-----------|----------|---------|--------|----------|
| `crashloop-backoff` | P1 | CrashLoopBackOff, Back-off restarting | `restart-pod` | 5m |
| `oom-killed` | P1 | OOMKilled, Out of memory | `none` | 10m |
| `panic` | P1 | panic:, runtime error:, fatal error: | `none` | 5m |
| `image-pull-error` | P2 | ImagePullBackOff, ErrImagePull | `none` | 5m |
| `probe-failed` | P2 | Readiness/Liveness probe failed | `restart-pod` | 3m |
| `node-pressure` | P1 | NodeNotReady, DiskPressure, MemoryPressure | `none` | 10m |
| `connection-refused` | P3 | connection refused, ECONNREFUSED | `none` | 2m |
| `timeout` | P3 | context deadline exceeded, timeout | `none` | 2m |
| `permission-denied` | P2 | permission denied, forbidden, unauthorized | `none` | 5m |
| `database-error` | P2 | database error, sql error | `none` | 2m |
| `tls-error` | P2 | certificate error, x509, TLS error | `none` | 5m |
| `dns-error` | P2 | no such host, DNS error, NXDOMAIN | `none` | 2m |
| `quota-exceeded` | P2 | quota exceeded, resource quota | `none` | 10m |
| `pod-evicted` | P2 | evicted, preempted, node drain | `none` | 5m |
| `generic-error` | P4 | error (word boundary) | `none` | 5m |

### Using Default Rules

Default rules can be used as a starting point and extended with custom rules:

```go
// Get default rules
defaults := rules.DefaultRules()

// Load custom rules from file
loader := rules.NewLoader("/path/to/custom-rules.yaml")
custom, err := loader.Load()

// Combine both (custom rules take precedence)
allRules := append(custom, defaults...)
```

## File Watching Capability

The rule loader includes a file watching mechanism that monitors the rules file for changes and automatically reloads rules when modifications are detected.

### How It Works

The current implementation uses a polling-based approach:

1. A background goroutine checks the file's modification time every 30 seconds
2. When a change is detected, the rules are reloaded and parsed
3. If loading succeeds, the new rules are sent to a channel
4. If loading fails (invalid YAML, validation errors), the change is silently ignored

### Usage

```go
loader := rules.NewLoader("/path/to/rules.yaml")

// Start watching for changes
rulesChan, err := loader.Watch()
if err != nil {
    log.Fatalf("Failed to start watching: %v", err)
}

// Handle rule updates
go func() {
    for newRules := range rulesChan {
        log.Printf("Rules updated: %d rules loaded", len(newRules))
        // Apply new rules to the engine
        engine.UpdateRules(newRules)
    }
}()
```

### Current Limitations

- **Polling-based**: Uses 30-second polling intervals rather than filesystem events
- **Silent failures**: Parse errors during reload are silently ignored
- **No graceful shutdown**: The watcher goroutine runs indefinitely

## Validation

Rules are validated during the loading process. The following validations are performed:

### Required Fields

1. **Rule name**: Every rule must have a non-empty `name` field
2. **Match criteria**: Either `pattern` or `keywords` must be specified
3. **Valid priority**: Priority must be a recognized value (P1-P4 or equivalent labels)

### Validation Errors

When validation fails, the entire load operation fails with a descriptive error:

```
rule name is required
rule crashloop-backoff: either pattern or keywords is required
rule my-rule: unknown priority: P5
```

### Pattern Validation

Note that regex pattern syntax is not validated at load time. Invalid patterns will fail at runtime when the rule engine attempts to compile them.

## Future Improvements

The following enhancements are planned or suggested for the rule loader:

### Schema Enforcement

- **JSON Schema validation**: Add a JSON Schema definition for rules to enable IDE support and pre-commit validation
- **Pattern syntax validation**: Validate regex patterns at load time to catch syntax errors early
- **Type coercion**: Improve handling of duration strings and other typed fields

### Hot Reloading Improvements

- **fsnotify integration**: Replace polling with proper filesystem event watching using the `fsnotify` package for immediate reload on changes
- **Error reporting**: Surface parse errors through a dedicated error channel or logging
- **Graceful reload**: Implement atomic rule updates to prevent race conditions
- **Validation mode**: Add a dry-run mode to validate rules without applying them

### Additional Features

- **Rule inheritance**: Allow rules to extend or override other rules
- **Environment variables**: Support variable substitution in rule patterns and parameters
- **Remote rule sources**: Load rules from ConfigMaps, remote URLs, or Git repositories
- **Rule versioning**: Track rule configuration versions for auditing and rollback
- **Conditional rules**: Support time-based or condition-based rule activation
- **Rule testing**: Provide a test framework for validating rules against sample log data

### Configuration Improvements

- **Multiple rule files**: Support loading rules from multiple files or directories
- **Rule ordering**: Explicit priority ordering for rules with the same match criteria
- **Rule groups**: Organize rules into logical groups for easier management
- **Include/exclude patterns**: Global patterns to pre-filter logs before rule matching

## API Reference

### Types

```go
// Loader handles loading and reloading rules from YAML files
type Loader struct {
    path string
}

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
    Pattern    string            `yaml:"pattern"`
    Keywords   []string          `yaml:"keywords,omitempty"`
    Labels     map[string]string `yaml:"labels,omitempty"`
    Namespaces []string          `yaml:"namespaces,omitempty"`
}

// Remediation defines the action to take when a rule matches
type Remediation struct {
    Action   ActionType        `yaml:"action"`
    Params   map[string]string `yaml:"params,omitempty"`
    Cooldown time.Duration     `yaml:"cooldown"`
}
```

### Functions

```go
// NewLoader creates a new rule loader
func NewLoader(path string) *Loader

// Load reads rules from the configured file path
func (l *Loader) Load() ([]Rule, error)

// ParseRules parses rules from YAML bytes
func ParseRules(data []byte) ([]Rule, error)

// DefaultRules returns a set of sensible default rules
func DefaultRules() []Rule

// Watch starts watching the rules file for changes
func (l *Loader) Watch() (<-chan []Rule, error)

// Validate checks if a rule is valid
func (r *Rule) Validate() error
```
