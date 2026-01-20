# Remediation Actions

This document provides a comprehensive reference for the remediation actions available in kube-sentinel. Remediation actions are automated responses that kube-sentinel can execute when specific conditions or anomalies are detected in your Kubernetes cluster.

## Table of Contents

- [Overview](#overview)
- [Action Interface](#action-interface)
- [Available Actions](#available-actions)
  - [RestartPodAction](#restartpodaction)
  - [ScaleUpAction](#scaleupaction)
  - [ScaleDownAction](#scaledownaction)
  - [RollbackAction](#rollbackaction)
  - [DeleteStuckPodsAction](#deletestuckpodsaction)
  - [NoneAction](#noneaction)
- [How Actions Are Triggered](#how-actions-are-triggered)
- [Target Specification](#target-specification)
- [Best Practices](#best-practices)
- [Future Actions and Integrations](#future-actions-and-integrations)

---

## Overview

Remediation actions in kube-sentinel provide automated self-healing capabilities for your Kubernetes workloads. When the system detects issues such as resource exhaustion, unhealthy pods, or deployment problems, it can automatically execute predefined actions to restore normal operation without manual intervention.

Each action is designed to be:

- **Atomic**: Actions perform a single, well-defined operation
- **Idempotent**: Running the same action multiple times produces consistent results
- **Validated**: Parameters are validated before execution
- **Auditable**: All actions can be logged and tracked

---

## Action Interface

All remediation actions implement the `Action` interface, which defines three core methods:

```go
type Action interface {
    Name() string
    Execute(ctx context.Context, target Target, params map[string]string) error
    Validate(params map[string]string) error
}
```

| Method | Description |
|--------|-------------|
| `Name()` | Returns the unique identifier for the action (e.g., `restart-pod`, `scale-up`) |
| `Execute()` | Performs the remediation action against the specified target |
| `Validate()` | Validates the provided parameters before execution |

---

## Available Actions

### RestartPodAction

**Action Name:** `restart-pod`

#### Purpose

Restarts a pod by deleting it, allowing the controller (Deployment, ReplicaSet, DaemonSet, etc.) to create a replacement. This is useful for recovering from transient issues such as memory leaks, deadlocks, or corrupted state.

#### How It Works

1. The action receives a target specifying the pod namespace and name
2. The pod is deleted with a grace period of 0 seconds (immediate termination)
3. The deletion uses `DeletePropagationForeground` to ensure dependent resources are cleaned up
4. The owning controller automatically schedules a new pod to replace the deleted one

#### Parameters

This action does not require any parameters.

#### Target Requirements

| Field | Required | Description |
|-------|----------|-------------|
| `Namespace` | Yes | The namespace containing the pod |
| `Pod` | Yes | The name of the pod to restart |

#### Example Use Cases

- Pod experiencing memory leaks that cannot be resolved without restart
- Application stuck in a deadlock or unresponsive state
- Pod with corrupted local state that needs reinitialization
- Recovery from network partition or connection pool exhaustion

#### Behavior Notes

- The action performs a **forced deletion** (grace period = 0), which immediately terminates the pod
- The owning controller must be configured to restart pods; standalone pods will not be recreated
- Running containers will be terminated without graceful shutdown

---

### ScaleUpAction

**Action Name:** `scale-up`

#### Purpose

Increases the replica count of a Deployment to add capacity. This is useful for handling increased load, replacing unhealthy pods, or improving availability during incidents.

#### How It Works

1. The action identifies the target Deployment (either directly or by finding the Deployment that owns a specified pod)
2. The current replica count is retrieved from the Deployment spec
3. The new replica count is calculated based on the provided parameters
4. A JSON merge patch is applied to update the Deployment's replica count
5. Kubernetes handles scheduling and starting the new pod replicas

#### Parameters

| Parameter | Required | Format | Description |
|-----------|----------|--------|-------------|
| `replicas` | No | `+N` or `N` | Number of replicas to add (`+N`) or absolute target count (`N`). Default: `+1` |
| `max_replicas` | No | `N` | Maximum allowed replica count. Action fails if the new count would exceed this limit |

#### Parameter Examples

| `replicas` Value | Current Replicas | Result |
|------------------|------------------|--------|
| (not specified) | 3 | 4 (default +1) |
| `+2` | 3 | 5 |
| `5` | 3 | 5 |
| `+3` with `max_replicas=4` | 2 | Error: would exceed max |

#### Target Requirements

| Field | Required | Description |
|-------|----------|-------------|
| `Namespace` | Yes | The namespace containing the Deployment |
| `Deployment` | Conditional | The Deployment name (required if Pod not specified) |
| `Pod` | Conditional | A pod owned by the Deployment (used to discover the Deployment) |

#### Deployment Discovery

If only a pod name is provided, the action traces the ownership chain:
1. Get the pod's owner references
2. Find the ReplicaSet that owns the pod
3. Find the Deployment that owns the ReplicaSet

#### Behavior Notes

- The replica count will never be reduced below 1
- The `max_replicas` parameter prevents runaway scaling
- The action uses `MergePatchType` for atomic updates

---

### ScaleDownAction

**Action Name:** `scale-down`

#### Purpose

Decreases the replica count of a Deployment to reduce capacity. This is useful for cost optimization, reducing resource contention, or responding to decreased load.

#### How It Works

1. The action identifies the target Deployment (same discovery logic as ScaleUpAction)
2. The current replica count is retrieved from the Deployment spec
3. The new replica count is calculated based on the provided parameters
4. A minimum replica count is enforced to prevent scaling to zero
5. A JSON merge patch is applied to update the Deployment's replica count

#### Parameters

| Parameter | Required | Format | Description |
|-----------|----------|--------|-------------|
| `replicas` | No | `-N` or `N` | Number of replicas to remove (`-N`) or absolute target count (`N`). Default: `-1` |
| `min_replicas` | No | `N` | Minimum allowed replica count. Default: `1` |

#### Parameter Examples

| `replicas` Value | Current Replicas | `min_replicas` | Result |
|------------------|------------------|----------------|--------|
| (not specified) | 5 | (default 1) | 4 |
| `-2` | 5 | 1 | 3 |
| `2` | 5 | 1 | 2 |
| `-3` | 3 | 2 | 2 (clamped to min) |

#### Target Requirements

Same as ScaleUpAction - either `Deployment` or `Pod` must be specified along with `Namespace`.

#### Behavior Notes

- The replica count will never be reduced below `min_replicas` (default: 1)
- This prevents accidentally scaling a deployment to zero replicas
- Kubernetes handles graceful termination of excess pods according to the Deployment's termination policy

---

### RollbackAction

**Action Name:** `rollback`

#### Purpose

Rolls back a Deployment to its previous revision. This is useful for recovering from failed deployments, reverting problematic changes, or restoring a known-good configuration.

#### How It Works

1. The action identifies the target Deployment
2. All ReplicaSets associated with the Deployment are listed using the Deployment's label selector
3. The ReplicaSets are sorted by creation timestamp to identify the current and previous revisions
4. The previous ReplicaSet's pod template is extracted
5. The Deployment is patched with the previous pod template, triggering a rollout

#### Parameters

This action does not currently accept parameters. It always rolls back to the immediately previous revision.

#### Target Requirements

| Field | Required | Description |
|-------|----------|-------------|
| `Namespace` | Yes | The namespace containing the Deployment |
| `Deployment` | Conditional | The Deployment name (required if Pod not specified) |
| `Pod` | Conditional | A pod owned by the Deployment |

#### Rollback Process

The rollback is performed by:
1. Identifying ReplicaSets with the `deployment.kubernetes.io/revision` annotation
2. Finding the two most recent ReplicaSets by creation timestamp
3. Applying the older ReplicaSet's pod template to the Deployment
4. Kubernetes then performs a standard rolling update to the previous configuration

#### Prerequisites

- At least two ReplicaSets must exist for the Deployment (current and previous)
- ReplicaSets must have the revision annotation set
- The action will fail if there is no previous revision available

#### Behavior Notes

- The rollback creates a new revision (it does not simply restore an old revision number)
- Standard Deployment rollout strategies (RollingUpdate, Recreate) are respected
- The action only affects the pod template; other Deployment settings remain unchanged

---

### DeleteStuckPodsAction

**Action Name:** `delete-stuck-pods`

#### Purpose

Force-deletes pods that are stuck in a Terminating state. This resolves situations where pods cannot be gracefully terminated due to issues such as finalizers, node problems, or hung processes.

#### How It Works

1. The action lists pods in the target namespace (optionally filtered by pod name)
2. Each pod is checked for the stuck terminating condition:
   - Has a `DeletionTimestamp` set (marked for deletion)
   - Is still in `Running` phase (not terminating properly)
3. Matching pods are force-deleted with a grace period of 0 seconds

#### Parameters

This action does not require any parameters.

#### Target Requirements

| Field | Required | Description |
|-------|----------|-------------|
| `Namespace` | Yes | The namespace to search for stuck pods |
| `Pod` | No | Specific pod name to check (if not specified, all pods in namespace are checked) |

#### Detection Criteria

A pod is considered "stuck in terminating" when:
- `pod.DeletionTimestamp != nil` - The pod has been marked for deletion
- `pod.Status.Phase == Running` - The pod is still running instead of terminating

#### Use Cases

- Pods with finalizers that cannot complete
- Pods on unreachable nodes
- Pods with containers that do not respond to SIGTERM
- Cleanup after failed node drains

#### Behavior Notes

- This is a forceful operation that bypasses graceful shutdown
- Data loss may occur if containers have not flushed buffers
- Finalizers are not executed when using force deletion
- Use with caution in production environments

---

### NoneAction

**Action Name:** `none`

#### Purpose

A no-operation action used for alert-only rules. This allows kube-sentinel to detect and report issues without taking any automated remediation action.

#### How It Works

The Execute method returns immediately without performing any operations.

#### Use Cases

- Alert-only monitoring rules
- Testing detection logic without side effects
- Situations requiring manual intervention
- Audit and compliance logging

---

## How Actions Are Triggered

Remediation actions in kube-sentinel are triggered through a rule-based system. The general flow is:

1. **Detection**: Monitors and analyzers continuously observe cluster state, metrics, and events
2. **Evaluation**: Detected conditions are evaluated against configured rules
3. **Matching**: When conditions match a rule's criteria, the rule's associated action is selected
4. **Validation**: Action parameters are validated before execution
5. **Execution**: The action is executed against the identified target
6. **Recording**: Results are logged and metrics are updated

### Conceptual Trigger Flow

```
[Cluster Events/Metrics]
        |
        v
[Detection Engine] --> [Rule Matcher] --> [Action Selector]
                                                  |
                                                  v
                                          [Parameter Validator]
                                                  |
                                                  v
                                          [Action Executor]
                                                  |
                                                  v
                                          [Audit Logger]
```

### Rule Configuration

Rules typically specify:
- **Conditions**: What triggers the action (e.g., CPU > 90%, pod in CrashLoopBackOff)
- **Target Selector**: How to identify affected resources
- **Action**: Which remediation action to execute
- **Parameters**: Configuration for the action
- **Cooldown**: Minimum time between repeated executions

---

## Target Specification

The `Target` struct identifies the Kubernetes resource that an action should operate on:

```go
type Target struct {
    Namespace  string  // Required: The Kubernetes namespace
    Pod        string  // The pod name (for pod-level actions)
    Deployment string  // The deployment name (for deployment-level actions)
    Container  string  // The container name within a pod
}
```

### Target Resolution

- For pod-level actions (`restart-pod`, `delete-stuck-pods`): Specify `Namespace` and `Pod`
- For deployment-level actions (`scale-up`, `scale-down`, `rollback`): Specify `Namespace` and either `Deployment` or `Pod`
- When `Pod` is specified for deployment actions, the system automatically discovers the owning Deployment

---

## Best Practices

### General Guidelines

1. **Start with alerts**: Use `NoneAction` initially to validate detection logic before enabling automated remediation
2. **Set appropriate limits**: Always configure `max_replicas` and `min_replicas` to prevent unbounded scaling
3. **Use cooldowns**: Prevent action storms by configuring appropriate cooldown periods
4. **Test in staging**: Validate remediation rules in non-production environments first
5. **Monitor action outcomes**: Track action execution metrics and success rates

### Action-Specific Recommendations

| Action | Recommendation |
|--------|----------------|
| `restart-pod` | Ensure pods have proper readiness probes to avoid restarting pods that are slow to initialize |
| `scale-up` | Set `max_replicas` based on cluster capacity and cost constraints |
| `scale-down` | Set `min_replicas` to maintain required availability |
| `rollback` | Ensure Deployment revision history limit is sufficient (default is 10) |
| `delete-stuck-pods` | Use selectively; investigate root cause of stuck pods |

---

## Future Actions and Integrations

The remediation action framework is designed to be extensible. The following actions and integrations are planned or under consideration for future releases:

### Planned Actions

| Action | Description | Status |
|--------|-------------|--------|
| `cordon-node` | Mark a node as unschedulable to prevent new pods | Planned |
| `drain-node` | Safely evict all pods from a problematic node | Planned |
| `restart-container` | Restart a specific container without restarting the entire pod | Planned |
| `update-resource-limits` | Dynamically adjust container resource requests/limits | Under consideration |
| `trigger-job` | Execute a Kubernetes Job for custom remediation logic | Under consideration |
| `patch-resource` | Apply arbitrary patches to Kubernetes resources | Under consideration |

### External System Integrations

| Integration | Description | Status |
|-------------|-------------|--------|
| **PagerDuty** | Create incidents and trigger on-call escalations | Planned |
| **Slack/Teams** | Send notifications to chat channels | Planned |
| **Webhook** | Generic HTTP webhook for custom integrations | Planned |
| **OpsGenie** | Alert management and incident response | Under consideration |
| **JIRA** | Automatic ticket creation for tracked issues | Under consideration |
| **Prometheus Alertmanager** | Integration with existing alerting pipelines | Under consideration |

### Custom Actions

Organizations can implement custom actions by:

1. Implementing the `Action` interface
2. Registering the action with the remediation engine
3. Referencing the action in rule configurations

This allows for domain-specific remediation logic tailored to your applications and infrastructure.

---

## See Also

- [Rule Configuration Guide](./11-rule-configuration.md) - How to configure remediation rules
- [Monitoring and Metrics](./12-monitoring.md) - Tracking action execution and outcomes
- [Security Considerations](./13-security.md) - RBAC and security best practices for remediation
