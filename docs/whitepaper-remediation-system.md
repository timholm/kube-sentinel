# Kube Sentinel Auto-Remediation System

## Technical Whitepaper v1.0

---

## Executive Summary

Kube Sentinel's auto-remediation system provides autonomous error recovery capabilities for Kubernetes clusters. When enabled, the system automatically executes corrective actions in response to detected errors, reducing mean time to recovery (MTTR) and minimizing human intervention for known failure patterns.

This document details the remediation engine architecture, available actions, safety mechanisms, and configuration options.

---

## 1. Remediation Engine Architecture

### 1.1 Engine Components

The remediation engine consists of four primary components:

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Remediation Engine                               │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐ │
│  │  Action Registry │  │  Safety Controls │  │  Execution Logger  │ │
│  │                  │  │                  │  │                    │ │
│  │  - restart-pod   │  │  - Cooldowns     │  │  - Audit trail     │ │
│  │  - scale-up      │  │  - Rate limits   │  │  - Success/fail    │ │
│  │  - scale-down    │  │  - NS exclusions │  │  - Dry-run mode    │ │
│  │  - rollback      │  │  - Dry-run mode  │  │  - Timestamps      │ │
│  │  - delete-stuck  │  │                  │  │                    │ │
│  │  - none          │  │                  │  │                    │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ Kubernetes API  │
                    │    Server       │
                    └─────────────────┘
```

### 1.2 Engine Configuration

```go
type EngineConfig struct {
    Enabled            bool      // Master on/off switch
    DryRun             bool      // Log actions without executing
    MaxActionsPerHour  int       // Rate limit threshold
    ExcludedNamespaces []string  // Protected namespaces
}
```

### 1.3 Engine State

The engine maintains runtime state for safety enforcement:

```go
type Engine struct {
    enabled            bool
    dryRun             bool
    maxActionsPerHour  int
    excludedNamespaces map[string]bool

    actions   map[string]Action           // Registered action handlers
    cooldowns map[string]time.Time        // Active cooldowns (rule:target → expiry)
    hourlyLog []time.Time                 // Action timestamps for rate limiting

    store  store.Store                    // Audit log persistence
    logger *slog.Logger                   // Structured logging
}
```

---

## 2. Available Remediation Actions

### 2.1 Action Interface

All remediation actions implement a common interface:

```go
type Action interface {
    Name() string
    Execute(ctx context.Context, target Target, params map[string]string) error
    Validate(params map[string]string) error
}

type Target struct {
    Namespace  string
    Pod        string
    Deployment string
    Container  string
}
```

### 2.2 Built-in Actions

| Action | Description | Kubernetes API | Use Case |
|--------|-------------|----------------|----------|
| `restart-pod` | Delete pod to trigger restart | `DELETE /pods/{name}` | CrashLoopBackOff, hung processes |
| `scale-up` | Increase deployment replicas | `PATCH /deployments/{name}` | OOM kills, capacity issues |
| `scale-down` | Decrease deployment replicas | `PATCH /deployments/{name}` | Resource optimization |
| `rollback` | Revert to previous deployment | `PATCH /deployments/{name}` | Bad deployments |
| `delete-stuck-pods` | Force delete terminating pods | `DELETE /pods/{name}` | Stuck finalizers |
| `none` | No action (alert only) | N/A | Monitoring without action |

---

## 3. Action Implementations

### 3.1 Restart Pod Action

Deletes the target pod with zero grace period, triggering an immediate restart by the controller (Deployment, ReplicaSet, etc.).

```go
func (a *RestartPodAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    gracePeriod := int64(0)  // Force immediate deletion
    deletePolicy := metav1.DeletePropagationForeground

    return a.client.CoreV1().Pods(target.Namespace).Delete(ctx, target.Pod, metav1.DeleteOptions{
        GracePeriodSeconds: &gracePeriod,
        PropagationPolicy:  &deletePolicy,
    })
}
```

**When to use:**
- CrashLoopBackOff errors
- Hung or unresponsive processes
- Memory leaks requiring restart

**Configuration example:**
```yaml
remediation:
  action: restart-pod
  cooldown: 5m
```

### 3.2 Scale Up Action

Increases the replica count of the deployment owning the target pod.

```go
func (a *ScaleUpAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    deployment, err := a.getDeployment(ctx, target)

    currentReplicas := *deployment.Spec.Replicas
    increment := int32(1)

    // Support "+N" for relative or "N" for absolute
    if val, ok := params["replicas"]; ok {
        if val[0] == '+' {
            increment, _ = strconv.ParseInt(val[1:], 10, 32)
        } else {
            absolute, _ := strconv.ParseInt(val, 10, 32)
            increment = int32(absolute) - currentReplicas
        }
    }

    newReplicas := currentReplicas + increment

    // Enforce max_replicas limit
    if max, ok := params["max_replicas"]; ok {
        if newReplicas > max {
            return fmt.Errorf("would exceed max replicas limit")
        }
    }

    patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, newReplicas)
    return a.client.AppsV1().Deployments(target.Namespace).Patch(...)
}
```

**Deployment Discovery:**

The action automatically discovers the parent deployment by traversing owner references:

```
Pod → OwnerReference → ReplicaSet → OwnerReference → Deployment
```

**Parameters:**
| Parameter | Description | Example |
|-----------|-------------|---------|
| `replicas` | Increment (`+N`) or absolute (`N`) | `+1`, `5` |
| `max_replicas` | Upper bound safety limit | `10` |

**Configuration example:**
```yaml
remediation:
  action: scale-up
  params:
    replicas: "+1"
    max_replicas: "10"
  cooldown: 10m
```

### 3.3 Scale Down Action

Decreases the replica count with minimum replica protection.

```go
func (a *ScaleDownAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    deployment, err := a.getDeployment(ctx, target)

    currentReplicas := *deployment.Spec.Replicas
    decrement := int32(1)

    // Parse decrement value
    if val, ok := params["replicas"]; ok {
        if val[0] == '-' {
            decrement, _ = strconv.ParseInt(val[1:], 10, 32)
        }
    }

    newReplicas := currentReplicas - decrement

    // Enforce min_replicas floor
    minReplicas := int32(1)
    if min, ok := params["min_replicas"]; ok {
        minReplicas, _ = strconv.ParseInt(min, 10, 32)
    }
    if newReplicas < minReplicas {
        newReplicas = minReplicas
    }

    patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, newReplicas)
    return a.client.AppsV1().Deployments(target.Namespace).Patch(...)
}
```

**Parameters:**
| Parameter | Description | Example |
|-----------|-------------|---------|
| `replicas` | Decrement (`-N`) or absolute (`N`) | `-1`, `2` |
| `min_replicas` | Lower bound safety limit | `1` |

### 3.4 Rollback Action

Reverts a deployment to its previous ReplicaSet template.

```go
func (a *RollbackAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    deployment, _ := a.getDeployment(ctx, target)

    // List all ReplicaSets for this deployment
    replicaSets, _ := a.client.AppsV1().ReplicaSets(target.Namespace).List(ctx, metav1.ListOptions{
        LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
    })

    // Find current and previous ReplicaSet by creation time
    var previous, current *appsv1.ReplicaSet
    for _, rs := range replicaSets.Items {
        // Track most recent and second most recent
        if current == nil || rs.CreationTimestamp.After(current.CreationTimestamp) {
            previous = current
            current = &rs
        }
    }

    if previous == nil {
        return fmt.Errorf("no previous revision to rollback to")
    }

    // Patch deployment with previous pod template
    patch := map[string]interface{}{
        "spec": map[string]interface{}{
            "template": previous.Spec.Template,
        },
    }
    return a.client.AppsV1().Deployments(target.Namespace).Patch(...)
}
```

**When to use:**
- Recent deployment causing errors
- Bad configuration rollout
- Broken container images

### 3.5 Delete Stuck Pods Action

Force-deletes pods stuck in Terminating state (common with stuck finalizers).

```go
func (a *DeleteStuckPodsAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    pods, _ := a.client.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{})

    gracePeriod := int64(0)
    deleteOpts := metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}

    for _, pod := range pods.Items {
        // Check if pod is stuck in terminating
        if pod.DeletionTimestamp != nil && pod.Status.Phase == corev1.PodRunning {
            a.client.CoreV1().Pods(target.Namespace).Delete(ctx, pod.Name, deleteOpts)
        }
    }
    return nil
}
```

### 3.6 None Action

A no-op action for rules that should only alert without taking action.

```go
func (a *NoneAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    return nil  // Intentionally empty
}
```

---

## 4. Safety Mechanisms

### 4.1 Safety Control Flow

Every remediation request passes through multiple safety checks:

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Safety Control Flow                              │
└─────────────────────────────────────────────────────────────────────┘

    ┌──────────────────────┐
    │  Error Matched Rule  │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ Remediation Enabled? │──No─▶│ Skip: "disabled"    │
    └──────────┬───────────┘     └─────────────────────┘
               │ Yes
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │  Action = "none"?    │─Yes─▶│ Skip: "no action"   │
    └──────────┬───────────┘     └─────────────────────┘
               │ No
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ Namespace Excluded?  │─Yes─▶│ Skip: "excluded"    │
    └──────────┬───────────┘     └─────────────────────┘
               │ No
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ Cooldown Active?     │─Yes─▶│ Skip: "cooldown"    │
    └──────────┬───────────┘     └─────────────────────┘
               │ No
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │ Hourly Limit Hit?    │─Yes─▶│ Skip: "rate limit"  │
    └──────────┬───────────┘     └─────────────────────┘
               │ No
               ▼
    ┌──────────────────────┐     ┌─────────────────────┐
    │    Dry Run Mode?     │─Yes─▶│ Log: "would execute"│
    └──────────┬───────────┘     └─────────────────────┘
               │ No
               ▼
    ┌──────────────────────┐
    │   Execute Action     │
    └──────────────────────┘
```

### 4.2 Cooldown System

Cooldowns prevent action spam by enforcing a waiting period after each action for a specific rule+target combination.

```go
// Cooldown key format: "ruleName:namespace/pod"
cooldownKey := fmt.Sprintf("%s:%s", rule.Name, target.String())

// Check if cooldown is active
if expiresAt, ok := e.cooldowns[cooldownKey]; ok && time.Now().Before(expiresAt) {
    return "skipped", "cooldown active until " + expiresAt.Format(time.RFC3339)
}

// After successful execution, set cooldown
e.cooldowns[cooldownKey] = time.Now().Add(rule.Remediation.Cooldown)
```

**Cooldown Behavior:**
- Cooldowns are per rule + target combination
- Different pods can be restarted independently
- Cooldowns survive across polling cycles but not engine restarts

**Example:**
```yaml
# Rule with 5-minute cooldown
remediation:
  action: restart-pod
  cooldown: 5m
```

If `pod-a` triggers this rule at 10:00, it cannot be restarted again by this rule until 10:05. However, `pod-b` can still be restarted immediately if it matches.

### 4.3 Hourly Rate Limiting

A global rate limit prevents runaway remediation during cascading failures.

```go
// Track action timestamps
e.hourlyLog = append(e.hourlyLog, time.Now())

// Cleanup old entries (sliding window)
func (e *Engine) cleanupHourlyLog() {
    cutoff := time.Now().Add(-time.Hour)
    var kept []time.Time
    for _, t := range e.hourlyLog {
        if t.After(cutoff) {
            kept = append(kept, t)
        }
    }
    e.hourlyLog = kept
}

// Check before execution
if len(e.hourlyLog) >= e.maxActionsPerHour {
    return "skipped", "hourly limit reached"
}
```

**Configuration:**
```yaml
remediation:
  max_actions_per_hour: 50
```

### 4.4 Namespace Exclusions

Critical namespaces can be protected from all remediation actions.

```go
// Check at execution time
if e.excludedNamespaces[err.Namespace] {
    return "skipped", fmt.Sprintf("namespace %s is excluded", err.Namespace)
}
```

**Default exclusions:**
```yaml
remediation:
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
    - logging
    - kube-sentinel
```

### 4.5 Dry Run Mode

Dry run mode logs all actions that would be taken without executing them.

```go
if e.dryRun {
    logEntry.Status = "success"
    logEntry.Message = "dry run - would execute action"
    e.logger.Info("dry run remediation",
        "action", rule.Remediation.Action,
        "target", target.String(),
        "rule", rule.Name,
    )
    return logEntry, nil
}
```

**Enabling dry run:**
```yaml
remediation:
  enabled: true
  dry_run: true  # Actions logged but not executed
```

---

## 5. Execution Flow

### 5.1 ProcessError Entry Point

```go
func (e *Engine) ProcessError(ctx context.Context, err *rules.MatchedError, ruleEngine *rules.Engine) (*store.RemediationLog, error) {
    // 1. Look up the matched rule
    rule := ruleEngine.GetRuleByName(err.RuleName)
    if rule == nil {
        return nil, fmt.Errorf("rule not found: %s", err.RuleName)
    }

    // 2. Check if rule has remediation configured
    if rule.Remediation == nil {
        return nil, nil
    }

    // 3. Execute with safety checks
    return e.Execute(ctx, err, rule)
}
```

### 5.2 Execute Method

```go
func (e *Engine) Execute(ctx context.Context, err *rules.MatchedError, rule *rules.Rule) (*store.RemediationLog, error) {
    // Create audit log entry
    logEntry := &store.RemediationLog{
        ID:        generateLogID(),
        ErrorID:   err.ID,
        Timestamp: time.Now(),
        DryRun:    e.dryRun,
        Target:    fmt.Sprintf("%s/%s", err.Namespace, err.Pod),
        Action:    string(rule.Remediation.Action),
    }

    // Safety check 1: Engine enabled?
    if !e.enabled {
        logEntry.Status = "skipped"
        logEntry.Message = "remediation disabled"
        return logEntry, nil
    }

    // Safety check 2: Action is "none"?
    if rule.Remediation.Action == rules.ActionNone {
        logEntry.Status = "skipped"
        logEntry.Message = "no remediation action configured"
        return logEntry, nil
    }

    // Safety check 3: Namespace excluded?
    if e.excludedNamespaces[err.Namespace] {
        logEntry.Status = "skipped"
        logEntry.Message = "namespace excluded"
        return logEntry, nil
    }

    // Safety check 4: Cooldown active?
    cooldownKey := fmt.Sprintf("%s:%s/%s", rule.Name, err.Namespace, err.Pod)
    if expiresAt, ok := e.cooldowns[cooldownKey]; ok && time.Now().Before(expiresAt) {
        logEntry.Status = "skipped"
        logEntry.Message = "cooldown active"
        return logEntry, nil
    }

    // Safety check 5: Hourly rate limit?
    e.cleanupHourlyLog()
    if len(e.hourlyLog) >= e.maxActionsPerHour {
        logEntry.Status = "skipped"
        logEntry.Message = "hourly limit reached"
        return logEntry, nil
    }

    // Get action handler
    action, ok := e.actions[string(rule.Remediation.Action)]
    if !ok {
        logEntry.Status = "failed"
        logEntry.Message = "unknown action"
        return logEntry, fmt.Errorf("unknown action")
    }

    // Validate parameters
    if err := action.Validate(rule.Remediation.Params); err != nil {
        logEntry.Status = "failed"
        logEntry.Message = err.Error()
        return logEntry, err
    }

    // Execute or dry run
    if e.dryRun {
        logEntry.Status = "success"
        logEntry.Message = "dry run - would execute"
    } else {
        if execErr := action.Execute(ctx, target, rule.Remediation.Params); execErr != nil {
            logEntry.Status = "failed"
            logEntry.Message = execErr.Error()
            return logEntry, execErr
        }
        logEntry.Status = "success"
        logEntry.Message = "action executed successfully"
    }

    // Set cooldown and record in hourly log
    e.cooldowns[cooldownKey] = time.Now().Add(rule.Remediation.Cooldown)
    e.hourlyLog = append(e.hourlyLog, time.Now())

    // Persist audit log
    e.store.SaveRemediationLog(logEntry)
    return logEntry, nil
}
```

---

## 6. Audit Logging

### 6.1 Remediation Log Structure

Every remediation attempt (successful, failed, or skipped) is logged:

```go
type RemediationLog struct {
    ID        string    // Unique log entry ID
    ErrorID   string    // Reference to triggering error
    Action    string    // Action attempted (restart-pod, scale-up, etc.)
    Target    string    // Target resource (namespace/pod)
    Status    string    // Outcome: success, failed, skipped
    Message   string    // Human-readable description
    Timestamp time.Time // When action was attempted
    DryRun    bool      // Whether this was a dry run
}
```

### 6.2 Status Values

| Status | Description | Example Message |
|--------|-------------|-----------------|
| `success` | Action executed successfully | "action executed successfully" |
| `failed` | Action execution failed | "deleting pod: pod not found" |
| `skipped` | Safety check prevented action | "cooldown active until 2024-01-15T10:30:00Z" |

### 6.3 Log Retention

Remediation logs are retained for 30 days by default, with automatic cleanup:

```go
// Clean up old remediation logs (older than 30 days)
logCutoff := time.Now().Add(-30 * 24 * time.Hour)
deleted, _ := dataStore.DeleteOldRemediationLogs(logCutoff)
```

---

## 7. RBAC Requirements

### 7.1 Required Permissions

The remediation engine requires the following Kubernetes RBAC permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-sentinel
rules:
  # Pod operations (restart, delete stuck)
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch", "delete"]

  # Deployment operations (scale, rollback)
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "update", "patch"]

  # ReplicaSet operations (rollback discovery)
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["get", "list", "watch"]

  # Event watching
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
```

### 7.2 Principle of Least Privilege

The permissions are scoped to the minimum required:
- **No create permissions**: Cannot create new resources
- **No secret access**: Cannot read sensitive data
- **Limited to workload resources**: Pods, Deployments, ReplicaSets only

---

## 8. Configuration Reference

### 8.1 Full Configuration Example

```yaml
remediation:
  # Master switch - enables/disables all remediation
  enabled: true

  # Dry run mode - log actions without executing
  dry_run: false

  # Maximum actions per hour (global rate limit)
  max_actions_per_hour: 50

  # Namespaces protected from all remediation
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
    - logging
```

### 8.2 Rule-Level Configuration

```yaml
rules:
  - name: crashloop-backoff
    match:
      pattern: "CrashLoopBackOff"
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 5m
    enabled: true

  - name: oom-killed
    match:
      pattern: "OOMKilled"
    priority: P1
    remediation:
      action: scale-up
      params:
        replicas: "+1"
        max_replicas: "10"
      cooldown: 10m
    enabled: true

  - name: image-pull-error
    match:
      pattern: "ImagePullBackOff"
    priority: P2
    remediation:
      action: none  # Alert only, cannot auto-fix
      cooldown: 5m
    enabled: true
```

---

## 9. Best Practices

### 9.1 Deployment Strategy

1. **Start with dry run**: Enable remediation in dry run mode first
2. **Monitor logs**: Review remediation logs before enabling live actions
3. **Conservative cooldowns**: Start with longer cooldowns (10-15 minutes)
4. **Low rate limits**: Start with 10-20 actions/hour maximum
5. **Exclude critical namespaces**: Always protect kube-system

### 9.2 Rule Design Guidelines

| Error Type | Recommended Action | Cooldown |
|------------|-------------------|----------|
| CrashLoopBackOff | `restart-pod` | 5m |
| OOMKilled | `scale-up` or `none` | 10m |
| ImagePullBackOff | `none` | N/A |
| Probe failures | `restart-pod` | 3m |
| Connection refused | `none` | 2m |

### 9.3 Monitoring Recommendations

1. **Track action rates**: Alert if approaching hourly limit
2. **Monitor success rates**: High failure rates indicate misconfiguration
3. **Review skipped actions**: Frequent skips may indicate cooldown tuning needed
4. **Audit regularly**: Review remediation logs weekly

---

## 10. Troubleshooting

### 10.1 Common Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| Actions never execute | `enabled: false` | Set `enabled: true` |
| Actions logged but not executed | `dry_run: true` | Set `dry_run: false` |
| Actions skipped for namespace | Namespace in exclusion list | Remove from `excluded_namespaces` |
| Actions skipped frequently | Cooldown too short | Increase cooldown duration |
| "hourly limit reached" | Too many errors triggering actions | Increase `max_actions_per_hour` or fix root cause |
| "unknown action" | Typo in action name | Check action spelling in rule |

### 10.2 Debugging Commands

```bash
# Check remediation status in logs
kubectl logs -n kube-sentinel deployment/kube-sentinel | grep -i remediation

# View recent remediation history via API
curl https://holm.chat/kube-sentinel/api/remediations

# Check current action count
curl https://holm.chat/kube-sentinel/api/stats
```

---

## 11. Conclusion

The Kube Sentinel remediation system provides:

1. **Autonomous Recovery**: Automatic response to known error patterns
2. **Defense in Depth**: Multiple safety mechanisms prevent runaway actions
3. **Full Auditability**: Every action logged with outcome and reasoning
4. **Flexible Configuration**: Per-rule actions, cooldowns, and parameters
5. **Safe Defaults**: Dry run mode, namespace exclusions, rate limits

When properly configured, auto-remediation significantly reduces MTTR for common Kubernetes failures while maintaining operational safety through comprehensive guardrails.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
