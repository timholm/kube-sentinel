# Remediation Engine

This document describes the remediation engine in kube-sentinel, which coordinates automated responses to detected errors with comprehensive safety controls.

## Overview

The remediation engine is the component responsible for executing corrective actions when errors are detected and matched against rules. It provides a controlled, auditable framework for automated incident response while preventing runaway automation through multiple safety mechanisms.

The engine is implemented in `internal/remediation/engine.go` and works in conjunction with the rules engine to determine when and how to respond to errors.

### Key Capabilities

- **Action Coordination**: Routes matched errors to appropriate remediation actions based on rule configuration
- **Safety Controls**: Implements multiple layers of protection including cooldowns, rate limits, and namespace exclusions
- **Dry-Run Mode**: Allows testing remediation logic without executing actual changes
- **Audit Logging**: Records all remediation attempts for compliance and debugging
- **Extensibility**: Supports registration of custom remediation actions

## Architecture

### Engine Structure

The `Engine` struct maintains state for coordinating remediation actions:

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | `bool` | Master switch for all remediation actions |
| `dryRun` | `bool` | When true, simulates actions without execution |
| `maxActionsPerHour` | `int` | Global rate limit for remediation actions |
| `excludedNamespaces` | `map[string]bool` | Namespaces protected from remediation |
| `actions` | `map[string]Action` | Registry of available remediation actions |
| `cooldowns` | `map[string]time.Time` | Tracks cooldown expiration per rule-target pair |
| `hourlyLog` | `[]time.Time` | Rolling window of action timestamps for rate limiting |
| `store` | `store.Store` | Persistence layer for audit logs |
| `logger` | `*slog.Logger` | Structured logging output |

### Configuration

The engine is configured via the `EngineConfig` struct:

```go
type EngineConfig struct {
    Enabled            bool
    DryRun             bool
    MaxActionsPerHour  int
    ExcludedNamespaces []string
}
```

Example configuration:

```yaml
remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
```

## Safety Controls

The remediation engine implements multiple layers of safety controls to prevent unintended consequences from automated actions.

### Master Enable Switch

The `enabled` flag provides a global kill switch for all remediation. When disabled, the engine logs all potential actions with status `skipped` and message `remediation disabled`, but takes no actual action. This allows operators to:

- Temporarily pause all automation during maintenance windows
- Disable remediation in staging environments while keeping detection active
- Quickly halt automation if unexpected behavior is observed

### Namespace Exclusions

Critical infrastructure namespaces can be protected from automated remediation by adding them to the exclusion list. When an error originates from an excluded namespace, remediation is skipped with the message `namespace {name} is excluded`.

Common exclusions include:

- `kube-system`: Core Kubernetes components
- `kube-public`: Cluster-wide public resources
- `kube-node-lease`: Node heartbeat data
- Monitoring and observability namespaces
- Security and policy enforcement namespaces

### Per-Rule Cooldowns

Each rule can specify a cooldown period that prevents the same action from being executed repeatedly on the same target. The cooldown is tracked using a composite key of `{rule_name}:{target}`, where target is typically `{namespace}/{pod}`.

When a cooldown is active, the action is skipped with a message indicating when the cooldown expires:

```
cooldown active until 2024-01-15T10:30:00Z
```

Cooldown behavior:

- Default cooldown is 5 minutes if not specified in the rule
- Cooldown is set after successful execution or dry-run simulation
- Cooldowns persist in memory only; they reset on engine restart
- Individual cooldowns can be cleared via `ClearCooldown(ruleName, target)`
- All cooldowns can be cleared via `ClearAllCooldowns()`

### Global Rate Limiting

The engine enforces a global rate limit on the number of actions executed per hour. This prevents cascading failures from triggering a flood of remediation actions that could destabilize the cluster.

The rate limiter uses a sliding window approach:

1. Before each action, the engine cleans up timestamps older than one hour
2. If the count of recent actions meets or exceeds `maxActionsPerHour`, the action is skipped
3. After successful execution, the current timestamp is added to the hourly log

When the limit is reached, actions are skipped with the message:

```
hourly limit reached ({limit} actions)
```

The current action count can be queried via `GetActionsThisHour()`.

## Dry-Run Mode

Dry-run mode allows operators to test remediation logic without making actual changes to the cluster. When enabled:

1. All safety checks (namespace exclusions, cooldowns, rate limits) are evaluated normally
2. Action parameters are validated
3. Instead of executing, the engine logs what would have happened
4. Cooldowns are still set (to accurately simulate repeated execution behavior)
5. The action is recorded in the hourly log
6. The audit log entry is marked with `DryRun: true` and status `success`

Dry-run mode is valuable for:

- Testing new rules in production before enabling actual remediation
- Validating remediation configuration during initial deployment
- Debugging why certain remediations are or are not triggering
- Training and demonstrations

Toggle dry-run mode at runtime via `SetDryRun(bool)`.

## Decision Flow

When the engine receives an error for remediation via `Execute()`, it follows this decision flow:

```
1. Create audit log entry
2. Check: Is remediation enabled?
   └─ No  → Log "remediation disabled", return
3. Check: Is action type "none"?
   └─ Yes → Log "no remediation action configured", return
4. Check: Is namespace excluded?
   └─ Yes → Log "namespace {ns} is excluded", return
5. Check: Is cooldown active for rule+target?
   └─ Yes → Log "cooldown active until {time}", return
6. Check: Has hourly rate limit been reached?
   └─ Yes → Log "hourly limit reached", return
7. Look up action by name
   └─ Not found → Log "unknown action", return error
8. Validate action parameters
   └─ Invalid → Log "invalid params", return error
9. Execute action (or simulate if dry-run)
   └─ Failure → Log error message, return error
10. Set cooldown for rule+target
11. Record timestamp in hourly log
12. Save audit log with status "success"
```

### Status Values

Remediation attempts result in one of three statuses:

| Status | Meaning |
|--------|---------|
| `success` | Action executed (or would execute in dry-run) |
| `skipped` | Action blocked by a safety control |
| `failed` | Action attempted but encountered an error |

## Built-in Actions

The engine registers these actions by default when a Kubernetes client is provided:

### restart-pod

Deletes a pod to trigger a restart by its controller (Deployment, StatefulSet, etc.).

- **Target**: Requires `namespace` and `pod`
- **Parameters**: None
- **Behavior**: Uses foreground deletion with zero grace period for immediate termination

### scale-up

Increases the replica count of a deployment.

- **Target**: Requires `namespace` and either `pod` or `deployment`
- **Parameters**:
  - `replicas`: Target count or increment (e.g., `5` or `+2`)
  - `max_replicas`: Upper bound (optional, prevents exceeding limit)
- **Behavior**: If only pod is specified, traces owner references to find the deployment

### scale-down

Decreases the replica count of a deployment.

- **Target**: Requires `namespace` and either `pod` or `deployment`
- **Parameters**:
  - `replicas`: Target count or decrement (e.g., `2` or `-1`)
  - `min_replicas`: Lower bound (default: 1)
- **Behavior**: Prevents scaling below `min_replicas`

### rollback

Reverts a deployment to its previous revision.

- **Target**: Requires `namespace` and either `pod` or `deployment`
- **Parameters**: None
- **Behavior**: Finds the second-most-recent ReplicaSet and applies its pod template to the deployment

### delete-stuck-pods

Force-deletes pods stuck in Terminating state.

- **Target**: Requires `namespace`, optionally `pod` (if not specified, checks all pods in namespace)
- **Parameters**: None
- **Behavior**: Identifies pods with `DeletionTimestamp` set but still in `Running` phase

### none

A no-op action for rules that should only alert without automated remediation.

- **Target**: Any
- **Parameters**: None
- **Behavior**: Does nothing; used for alert-only rules

## Registering Custom Actions

Custom remediation actions can be registered by implementing the `Action` interface:

```go
type Action interface {
    Name() string
    Execute(ctx context.Context, target Target, params map[string]string) error
    Validate(params map[string]string) error
}
```

Register custom actions after engine creation:

```go
engine.RegisterAction(myCustomAction)
```

### Target Structure

Actions receive a `Target` struct identifying the Kubernetes resource:

```go
type Target struct {
    Namespace  string
    Pod        string
    Deployment string
    Container  string
}
```

The `String()` method returns a human-readable representation:

- Pod target: `{namespace}/{pod}`
- Deployment target: `{namespace}/deployment/{deployment}`
- Namespace only: `{namespace}`

## Audit Logging

Every remediation attempt is recorded as a `RemediationLog` entry:

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique identifier (SHA256-based) |
| `ErrorID` | `string` | Reference to the triggering error |
| `Action` | `string` | Name of the attempted action |
| `Target` | `string` | String representation of the target resource |
| `Status` | `string` | Outcome: `success`, `skipped`, or `failed` |
| `Message` | `string` | Human-readable explanation of the result |
| `Timestamp` | `time.Time` | When the attempt was made |
| `DryRun` | `bool` | Whether this was a simulation |

Audit logs enable:

- **Compliance**: Demonstrate what automated actions were taken and why
- **Debugging**: Understand why remediations succeeded, failed, or were skipped
- **Metrics**: Analyze remediation patterns and effectiveness
- **Incident Response**: Correlate cluster changes with detected errors

Logs are persisted via the `Store` interface and can be queried through the API.

## Runtime Control

The engine exposes methods for runtime configuration changes:

| Method | Description |
|--------|-------------|
| `SetEnabled(bool)` | Enable or disable all remediation |
| `SetDryRun(bool)` | Toggle dry-run mode |
| `IsEnabled()` | Query current enabled state |
| `IsDryRun()` | Query current dry-run state |
| `GetActionsThisHour()` | Get count of actions in the sliding window |
| `ClearCooldown(rule, target)` | Remove cooldown for specific rule-target pair |
| `ClearAllCooldowns()` | Remove all active cooldowns |

These methods are thread-safe and can be called from API handlers or operators.

## Integration with Rules Engine

The `ProcessError` method provides a convenient integration point:

```go
func (e *Engine) ProcessError(ctx context.Context, err *rules.MatchedError, ruleEngine *rules.Engine) (*store.RemediationLog, error)
```

This method:

1. Looks up the full rule definition by name from the matched error
2. Checks if the rule has remediation configured
3. Delegates to `Execute()` with the error and rule

Typical usage in the main processing loop:

```go
matchedError := ruleEngine.Match(logEntry)
if matchedError != nil {
    log, err := remediationEngine.ProcessError(ctx, matchedError, ruleEngine)
    if err != nil {
        // Handle execution error
    }
    // log contains the audit record
}
```

## Future Enhancements

The following enhancements are planned or under consideration for future releases:

### Retry Logic

Add configurable retry behavior for failed remediation actions:

- Maximum retry count per action
- Exponential backoff between retries
- Distinction between retryable and non-retryable errors
- Circuit breaker pattern to prevent repeated failures

### Approval Gates

Implement human-in-the-loop approval for high-impact actions:

- Configurable approval requirements per action type or priority level
- Integration with Slack, PagerDuty, or other notification systems
- Time-limited approval windows with automatic expiration
- Audit trail of approvals and approvers

### Webhook Notifications

Send notifications on remediation events:

- Pre-execution webhooks for external approval systems
- Post-execution webhooks for alerting and metrics
- Configurable payload templates
- Retry and timeout handling for webhook delivery

### Action Chaining

Support multi-step remediation workflows:

- Define sequences of actions to execute in order
- Conditional logic based on action results
- Rollback on failure of any step
- Parallel execution of independent actions

### Enhanced Rate Limiting

More sophisticated rate limiting options:

- Per-namespace rate limits
- Per-action-type rate limits
- Configurable time windows (not just hourly)
- Token bucket algorithm for burst handling

### Metrics and Observability

Expose remediation metrics for monitoring:

- Prometheus metrics for action counts, durations, and outcomes
- OpenTelemetry tracing for action execution
- Dashboard templates for common monitoring systems
- SLO/SLI definitions for remediation effectiveness

### Persistent Cooldowns

Store cooldown state in the persistent store:

- Survive engine restarts without losing cooldown state
- Distributed cooldown tracking for multi-replica deployments
- Configurable cooldown persistence strategy

### Action Dry-Run Preview

Enhanced dry-run capabilities:

- Preview the exact Kubernetes API calls that would be made
- Diff view showing resource state before and after
- Integration with kubectl diff for familiar output format
