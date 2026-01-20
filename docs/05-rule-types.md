# Rule Types Reference

This document provides a comprehensive reference for the rule-related types used in kube-sentinel. Understanding these types is essential for creating effective rules that detect, classify, and remediate Kubernetes errors.

## Overview

The kube-sentinel rule system is built around several interconnected types that work together to provide a flexible and powerful error detection and remediation framework:

```
┌─────────────────────────────────────────────────────────────┐
│                         Rule                                │
│  ┌─────────────┐  ┌──────────┐  ┌─────────────────────┐    │
│  │    Match    │  │ Priority │  │    Remediation      │    │
│  │  (when)     │  │  (how    │  │    (what to do)     │    │
│  │             │  │  urgent) │  │                     │    │
│  │ - Pattern   │  │          │  │ - Action            │    │
│  │ - Keywords  │  │  P1-P4   │  │ - Params            │    │
│  │ - Labels    │  │          │  │ - Cooldown          │    │
│  │ - Namespace │  │          │  │                     │    │
│  └─────────────┘  └──────────┘  └─────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

## Type Definitions

### Rule

The `Rule` type is the primary configuration unit in kube-sentinel. Each rule defines what to look for, how important it is, and optionally what action to take.

```go
type Rule struct {
    Name        string       `yaml:"name"`
    Match       Match        `yaml:"match"`
    Priority    Priority     `yaml:"priority"`
    Remediation *Remediation `yaml:"remediation,omitempty"`
    Enabled     bool         `yaml:"enabled"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `Name` | string | Yes | Unique identifier for the rule. Used in logs, alerts, and the dashboard. |
| `Match` | Match | Yes | Conditions that determine when this rule applies to an error. |
| `Priority` | Priority | Yes | Severity level assigned to matching errors (P1-P4). |
| `Remediation` | *Remediation | No | Optional automated action to take when the rule matches. |
| `Enabled` | bool | Yes | Whether the rule is active. Disabled rules are ignored during matching. |

#### Validation Requirements

A rule must satisfy the following validation criteria:

1. The `Name` field must not be empty
2. The `Match` field must have either a `Pattern` or at least one entry in `Keywords`
3. The `Priority` must be a valid priority level (P1, P2, P3, or P4)

### Match

The `Match` type defines the conditions for matching an error. Multiple conditions can be combined for more precise targeting.

```go
type Match struct {
    Pattern    string            `yaml:"pattern"`
    Keywords   []string          `yaml:"keywords,omitempty"`
    Labels     map[string]string `yaml:"labels,omitempty"`
    Namespaces []string          `yaml:"namespaces,omitempty"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `Pattern` | string | Conditional | Regular expression pattern to match against error messages. Required if `Keywords` is empty. |
| `Keywords` | []string | Conditional | Simple substring matches. Required if `Pattern` is empty. |
| `Labels` | map[string]string | No | Kubernetes label selectors that must match the pod/resource. |
| `Namespaces` | []string | No | Whitelist of namespaces. If specified, only errors from these namespaces will match. |

#### Matching Logic

When evaluating a match:

1. **Pattern or Keywords**: At least one of `Pattern` or `Keywords` must be specified. If both are present, either can trigger a match.
2. **Labels**: If specified, all label key-value pairs must match.
3. **Namespaces**: If specified, the error must originate from one of the listed namespaces.

All specified conditions must be satisfied for the rule to match (AND logic between different condition types).

### Priority

The `Priority` type represents the severity level of an error. It is implemented as a string type with four predefined constants.

```go
type Priority string

const (
    PriorityCritical Priority = "P1"
    PriorityHigh     Priority = "P2"
    PriorityMedium   Priority = "P3"
    PriorityLow      Priority = "P4"
)
```

#### Priority Methods

| Method | Return Type | Description |
|--------|-------------|-------------|
| `Weight()` | int | Returns numeric weight (1-4, lower is more important). Useful for sorting. |
| `String()` | string | Returns the priority code (P1, P2, P3, P4). |
| `Label()` | string | Returns human-readable label (Critical, High, Medium, Low). |
| `Color()` | string | Returns CSS color class for UI rendering. |

#### Parsing Priorities

The `ParsePriority` function accepts multiple input formats:

| Input Values | Resulting Priority |
|--------------|-------------------|
| `P1`, `p1`, `critical`, `CRITICAL` | PriorityCritical |
| `P2`, `p2`, `high`, `HIGH` | PriorityHigh |
| `P3`, `p3`, `medium`, `MEDIUM` | PriorityMedium |
| `P4`, `p4`, `low`, `LOW` | PriorityLow |

### ActionType

The `ActionType` type defines the available automated remediation actions.

```go
type ActionType string

const (
    ActionNone             ActionType = "none"
    ActionRestartPod       ActionType = "restart-pod"
    ActionScaleUp          ActionType = "scale-up"
    ActionScaleDown        ActionType = "scale-down"
    ActionRollback         ActionType = "rollback"
    ActionDeleteStuckPods  ActionType = "delete-stuck-pods"
    ActionExecScript       ActionType = "exec-script"
)
```

### Remediation

The `Remediation` type defines the automated action to take when a rule matches.

```go
type Remediation struct {
    Action   ActionType        `yaml:"action"`
    Params   map[string]string `yaml:"params,omitempty"`
    Cooldown time.Duration     `yaml:"cooldown"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `Action` | ActionType | Yes | The type of remediation action to perform. |
| `Params` | map[string]string | No | Action-specific parameters (see Action Types section below). |
| `Cooldown` | time.Duration | Yes | Minimum time between repeated remediation attempts for the same error. |

### MatchedError

The `MatchedError` type represents an error that has been matched by a rule. This is the result of the matching process and contains all relevant context.

```go
type MatchedError struct {
    ID          string
    Fingerprint string
    Timestamp   time.Time
    Namespace   string
    Pod         string
    Container   string
    Message     string
    Labels      map[string]string
    Raw         string
    Priority    Priority
    RuleName    string
    Count       int
    FirstSeen   time.Time
    LastSeen    time.Time
    Remediated  bool
}
```

| Field | Type | Description |
|-------|------|-------------|
| `ID` | string | Unique identifier for this error instance. |
| `Fingerprint` | string | Hash used for deduplication of similar errors. |
| `Timestamp` | time.Time | When the error was detected. |
| `Namespace` | string | Kubernetes namespace where the error occurred. |
| `Pod` | string | Pod name that generated the error. |
| `Container` | string | Container name within the pod. |
| `Message` | string | The parsed error message. |
| `Labels` | map[string]string | Labels from the source pod/resource. |
| `Raw` | string | Original, unprocessed log line. |
| `Priority` | Priority | Priority level assigned by the matched rule. |
| `RuleName` | string | Name of the rule that matched this error. |
| `Count` | int | Number of occurrences of this error (based on fingerprint). |
| `FirstSeen` | time.Time | When this error was first detected. |
| `LastSeen` | time.Time | Most recent occurrence of this error. |
| `Remediated` | bool | Whether remediation has been attempted. |

### RulesConfig

The `RulesConfig` type represents the top-level structure of a rules configuration file.

```go
type RulesConfig struct {
    Rules []Rule `yaml:"rules"`
}
```

## Priority Levels

kube-sentinel uses a four-tier priority system to classify error severity. This system helps operators focus on the most critical issues first.

### P1 - Critical

| Attribute | Value |
|-----------|-------|
| Code | `P1` |
| Label | Critical |
| Weight | 1 |
| Color | Red |

**Description**: Critical errors that require immediate attention. These typically indicate:

- Complete service outages
- Data corruption risks
- Security breaches
- Cascading failures affecting multiple services

**Expected Response Time**: Immediate (within minutes)

**Example Scenarios**:
- OOMKilled events on critical services
- Database connection pool exhaustion
- Certificate expiration
- Persistent volume mount failures

### P2 - High

| Attribute | Value |
|-----------|-------|
| Code | `P2` |
| Label | High |
| Weight | 2 |
| Color | Orange |

**Description**: High-priority errors that significantly impact service quality but do not constitute a complete outage.

**Expected Response Time**: Within 1 hour

**Example Scenarios**:
- High error rates on API endpoints
- Memory usage approaching limits
- Pod restart loops (CrashLoopBackOff)
- Degraded replica counts

### P3 - Medium

| Attribute | Value |
|-----------|-------|
| Code | `P3` |
| Label | Medium |
| Weight | 3 |
| Color | Yellow |

**Description**: Medium-priority errors that should be addressed during normal working hours.

**Expected Response Time**: Within 24 hours

**Example Scenarios**:
- Deprecation warnings
- Non-critical configuration issues
- Intermittent connectivity problems
- Resource quota warnings

### P4 - Low

| Attribute | Value |
|-----------|-------|
| Code | `P4` |
| Label | Low |
| Weight | 4 |
| Color | Blue |

**Description**: Low-priority errors that represent minor issues or informational alerts.

**Expected Response Time**: As time permits

**Example Scenarios**:
- Debug-level messages
- Performance optimization opportunities
- Non-impacting warnings
- Informational notices

## Action Types

The following remediation actions are available:

### none

Takes no automated action. Use this when you want to detect and alert on errors without automated remediation.

```yaml
remediation:
  action: none
  cooldown: 0s
```

### restart-pod

Deletes the affected pod, allowing the deployment/replicaset to create a replacement.

```yaml
remediation:
  action: restart-pod
  cooldown: 5m
  params:
    grace-period: "30"  # Seconds for graceful termination
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `grace-period` | string | "30" | Seconds to wait for graceful pod termination. |

### scale-up

Increases the replica count of the affected deployment.

```yaml
remediation:
  action: scale-up
  cooldown: 10m
  params:
    increment: "1"      # Number of replicas to add
    max-replicas: "10"  # Maximum replica count
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `increment` | string | "1" | Number of replicas to add. |
| `max-replicas` | string | - | Maximum replica count (prevents runaway scaling). |

### scale-down

Decreases the replica count of the affected deployment.

```yaml
remediation:
  action: scale-down
  cooldown: 10m
  params:
    decrement: "1"      # Number of replicas to remove
    min-replicas: "1"   # Minimum replica count
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `decrement` | string | "1" | Number of replicas to remove. |
| `min-replicas` | string | "1" | Minimum replica count (prevents scaling to zero). |

### rollback

Reverts the deployment to a previous revision.

```yaml
remediation:
  action: rollback
  cooldown: 30m
  params:
    to-revision: "0"  # 0 = previous revision
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `to-revision` | string | "0" | Target revision number. Use "0" for the previous revision. |

### delete-stuck-pods

Removes pods that are stuck in terminating or unknown states.

```yaml
remediation:
  action: delete-stuck-pods
  cooldown: 5m
  params:
    force: "false"           # Force delete (bypass graceful deletion)
    stuck-threshold: "300"   # Seconds before considering a pod stuck
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `force` | string | "false" | Whether to force delete (sets grace period to 0). |
| `stuck-threshold` | string | "300" | Seconds a pod must be in terminating state to be considered stuck. |

### exec-script

Executes a custom script for advanced remediation scenarios.

```yaml
remediation:
  action: exec-script
  cooldown: 15m
  params:
    script: "/scripts/custom-remediation.sh"
    timeout: "60"
    args: "--verbose --namespace={{namespace}}"
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `script` | string | Required | Path to the script to execute. |
| `timeout` | string | "30" | Maximum execution time in seconds. |
| `args` | string | "" | Arguments to pass to the script. Supports template variables. |

**Template Variables for Scripts**:
- `{{namespace}}` - Namespace of the affected resource
- `{{pod}}` - Pod name
- `{{container}}` - Container name
- `{{message}}` - Error message

## How the Types Work Together

The rule system follows this logical flow:

```
Log Entry
    │
    ▼
┌─────────────────────────┐
│   Match Evaluation      │
│                         │
│   1. Check Pattern/     │
│      Keywords           │
│   2. Check Labels       │
│   3. Check Namespaces   │
└───────────┬─────────────┘
            │
            │ Match found
            ▼
┌─────────────────────────┐
│   Create MatchedError   │
│                         │
│   - Assign Priority     │
│   - Record Rule Name    │
│   - Generate Fingerprint│
│   - Track Timestamps    │
└───────────┬─────────────┘
            │
            │ Has Remediation?
            ▼
┌─────────────────────────┐
│   Execute Remediation   │
│                         │
│   1. Check Cooldown     │
│   2. Execute Action     │
│   3. Update Remediated  │
│      Flag               │
└─────────────────────────┘
```

### Rule Configuration Example

Here is a complete example showing how all types work together:

```yaml
rules:
  - name: oom-killed-critical-services
    enabled: true
    match:
      pattern: "OOMKilled|Out of memory"
      keywords:
        - "oom"
        - "memory"
      labels:
        tier: "critical"
        app.kubernetes.io/part-of: "core-platform"
      namespaces:
        - production
        - staging
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m
      params:
        grace-period: "10"
```

This rule:
1. **Matches** errors containing "OOMKilled", "Out of memory", "oom", or "memory"
2. **Filters** to only pods with the specified labels
3. **Restricts** to production and staging namespaces
4. **Assigns** P1 (Critical) priority
5. **Remediates** by restarting the pod with a 5-minute cooldown

## Future Extensibility

The current type system is designed with extensibility in mind. Here are potential enhancements for future versions:

### Custom Priority Levels

The current P1-P4 system could be extended to support custom priority definitions:

```yaml
# Potential future syntax
priorities:
  - code: P0
    label: Emergency
    weight: 0
    color: purple
    sla: 5m
  - code: P5
    label: Informational
    weight: 5
    color: gray
    sla: null
```

This would allow organizations to:
- Add emergency-level priorities for catastrophic failures
- Include informational priorities for audit logging
- Define custom SLA expectations per priority

### New Action Categories

Future action types could include:

| Category | Potential Actions | Use Cases |
|----------|------------------|-----------|
| **Networking** | `update-network-policy`, `refresh-dns`, `reset-service-mesh` | Connectivity issues, DNS problems |
| **Storage** | `expand-pvc`, `cleanup-temp-files`, `snapshot-volume` | Storage pressure, disk space issues |
| **Configuration** | `reload-configmap`, `rotate-secret`, `update-env` | Configuration drift, secret expiration |
| **Observability** | `increase-log-level`, `enable-tracing`, `capture-heap-dump` | Debugging, performance analysis |
| **External** | `trigger-webhook`, `send-slack`, `create-jira-ticket` | Integration with external systems |

### Enhanced Match Conditions

Future match enhancements could include:

```yaml
# Potential future syntax
match:
  # Time-based conditions
  schedule:
    days: ["monday", "tuesday", "wednesday", "thursday", "friday"]
    hours: "09:00-17:00"
    timezone: "America/New_York"

  # Rate-based conditions
  rate:
    threshold: 10
    window: 5m

  # Resource conditions
  resources:
    cpu: "> 80%"
    memory: "> 90%"

  # Dependency conditions
  dependencies:
    requires:
      - rule: "database-connection-pool"
        state: "active"
```

### Rule Chaining and Workflows

Complex remediation scenarios could be supported through rule chaining:

```yaml
# Potential future syntax
workflows:
  - name: gradual-recovery
    trigger: oom-killed-critical
    steps:
      - action: restart-pod
        wait: 2m
        success_condition: "pod.status == Running"
      - action: scale-up
        if: "restart.failed"
        params:
          increment: "1"
      - action: notify
        always: true
        params:
          channel: "#ops-alerts"
```

### Conditional Remediation

Enhanced decision-making for remediation:

```yaml
# Potential future syntax
remediation:
  conditions:
    - if: "error.count < 3"
      action: none
    - if: "error.count >= 3 && error.count < 10"
      action: restart-pod
    - if: "error.count >= 10"
      action: scale-up
      notify:
        - channel: slack
          severity: critical
```

### Metrics and Feedback Loop

Integration with metrics for adaptive remediation:

```yaml
# Potential future syntax
remediation:
  action: scale-up
  feedback:
    success_metric: "http_requests_total{status='200'}"
    failure_metric: "http_requests_total{status='500'}"
    evaluation_window: 5m
    rollback_on_failure: true
```

## Best Practices

1. **Start with Detection**: Begin by creating rules with `action: none` to understand error patterns before enabling automated remediation.

2. **Use Appropriate Cooldowns**: Set cooldown periods that allow time to verify if remediation was successful before attempting again.

3. **Be Specific with Matches**: Use labels and namespaces to avoid false positives. Overly broad patterns can trigger unnecessary remediations.

4. **Layer Your Rules**: Create rules at different priority levels to handle the same error type with escalating severity based on frequency or duration.

5. **Test in Non-Production**: Always test new rules in staging or development environments before deploying to production.

6. **Monitor Remediation**: Track remediation success rates and adjust rules based on observed outcomes.

## See Also

- [Getting Started Guide](01-getting-started.md)
- [Configuration Reference](02-configuration.md)
- [Writing Custom Rules](03-custom-rules.md)
- [API Reference](04-api-reference.md)
