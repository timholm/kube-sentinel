# Kube Sentinel Security Model & Threat Analysis

## Technical Whitepaper v1.0

---

## Executive Summary

Kube Sentinel operates as a privileged Kubernetes controller with the ability to delete pods, scale deployments, and trigger rollbacks. This document provides a comprehensive analysis of the security model, RBAC design, pod hardening, threat surface, and operational best practices to ensure secure deployment and operation.

Security is implemented through defense-in-depth: minimal RBAC permissions, hardened pod security contexts, namespace exclusion boundaries, rate limiting, and dry run capabilities for safe testing.

---

## 1. RBAC Design and Principle of Least Privilege

### 1.1 Design Philosophy

Kube Sentinel's RBAC configuration follows the principle of least privilege: the service account is granted only the minimum permissions required to perform its monitoring and remediation functions. No permissions are granted speculatively or for future features.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         RBAC Permission Model                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌────────────────────────────────────────────────────────────────────┐   │
│   │                     ServiceAccount                                   │   │
│   │                    kube-sentinel/kube-sentinel                       │   │
│   └────────────────────────┬───────────────────────────────────────────┘   │
│                            │                                                 │
│                            │ ClusterRoleBinding                             │
│                            ▼                                                 │
│   ┌────────────────────────────────────────────────────────────────────┐   │
│   │                      ClusterRole                                     │   │
│   │                     kube-sentinel                                    │   │
│   │  ┌─────────────────────────────────────────────────────────────┐   │   │
│   │  │  Pods:        get, list, watch, delete                       │   │   │
│   │  │  Deployments: get, list, watch, update, patch               │   │   │
│   │  │  ReplicaSets: get, list, watch                               │   │   │
│   │  │  Events:      get, list, watch                               │   │   │
│   │  │  Namespaces:  get, list, watch                               │   │   │
│   │  └─────────────────────────────────────────────────────────────┘   │   │
│   └────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 ServiceAccount Isolation

The kube-sentinel ServiceAccount is created in a dedicated namespace, isolating it from application workloads:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
```

**Security Benefits:**
- Namespace isolation prevents accidental permission inheritance
- Dedicated namespace simplifies audit and monitoring
- Clear ownership and accountability for the service account
- Easy to apply namespace-level policies (NetworkPolicy, ResourceQuota)

### 1.3 Why ClusterRole vs Role

Kube Sentinel requires cluster-wide visibility to monitor errors across all namespaces. A namespace-scoped `Role` would require:
- Creating a Role in every namespace
- Managing RoleBindings as namespaces are created/deleted
- Missing errors from newly created namespaces

The `ClusterRole` approach provides:
- Single configuration point
- Automatic coverage of new namespaces
- Simpler operational management
- Consistent permission model

---

## 2. ClusterRole Permissions Breakdown

### 2.1 Complete Permission Matrix

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
rules:
  # Pod operations (for restart-pod action)
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch", "delete"]

  # Deployment operations (for scale-up, scale-down, rollback actions)
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "update", "patch"]

  # ReplicaSet operations (for finding deployment from pod)
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["get", "list", "watch"]

  # Events (for additional context)
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]

  # Namespaces (for listing available namespaces)
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list", "watch"]
```

### 2.2 Permission Justification Table

| Resource | Verb | Required For | Risk Level |
|----------|------|--------------|------------|
| `pods` | `get` | Retrieve pod details for error context | Low |
| `pods` | `list` | Enumerate pods for stuck pod detection | Low |
| `pods` | `watch` | Monitor pod state changes | Low |
| `pods` | `delete` | **restart-pod**, **delete-stuck-pods** actions | **Medium** |
| `deployments` | `get` | Retrieve current replica count | Low |
| `deployments` | `list` | Find parent deployment from pod | Low |
| `deployments` | `watch` | Monitor deployment state | Low |
| `deployments` | `update` | **scale-up**, **scale-down** actions | **Medium** |
| `deployments` | `patch` | **rollback** action (update spec.template) | **Medium** |
| `replicasets` | `get/list/watch` | Discover deployment from pod owner chain | Low |
| `events` | `get/list/watch` | Additional error context for dashboard | Low |
| `namespaces` | `get/list/watch` | Enumerate namespaces for filtering | Low |

### 2.3 Permissions NOT Granted

The following permissions are explicitly NOT granted, limiting the attack surface:

| Resource | Verb | Why Not Granted |
|----------|------|-----------------|
| `secrets` | any | No need to read sensitive data |
| `configmaps` | any | Configuration is file-based, not from cluster |
| `pods` | `create` | Cannot create rogue workloads |
| `pods` | `update` | Cannot modify running pods |
| `deployments` | `create` | Cannot create new deployments |
| `deployments` | `delete` | Cannot delete deployments entirely |
| `nodes` | any | No node-level operations |
| `persistentvolumeclaims` | any | No storage operations |
| `services` | any | No network endpoint modifications |
| `ingresses` | any | No external traffic control |
| `roles/rolebindings` | any | Cannot escalate privileges |
| `clusterroles/clusterrolebindings` | any | Cannot escalate privileges |

### 2.4 Privilege Escalation Prevention

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Privilege Escalation Barriers                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────────────┐     ┌──────────────────────┐                      │
│  │  Cannot create pods  │     │  Cannot read secrets │                      │
│  │  with elevated       │     │  or configmaps       │                      │
│  │  privileges          │     │                      │                      │
│  └──────────────────────┘     └──────────────────────┘                      │
│                                                                              │
│  ┌──────────────────────┐     ┌──────────────────────┐                      │
│  │  Cannot modify RBAC  │     │  Cannot access       │                      │
│  │  roles or bindings   │     │  node resources      │                      │
│  └──────────────────────┘     └──────────────────────┘                      │
│                                                                              │
│  ┌──────────────────────┐     ┌──────────────────────┐                      │
│  │  Cannot create       │     │  Cannot modify       │                      │
│  │  ServiceAccounts     │     │  running pod specs   │                      │
│  └──────────────────────┘     └──────────────────────┘                      │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Pod Security Context

### 3.1 Complete Security Context Configuration

```yaml
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
  containers:
    - name: kube-sentinel
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: config
          mountPath: /etc/kube-sentinel
          readOnly: true
        - name: tmp
          mountPath: /tmp
  volumes:
    - name: config
      configMap:
        name: kube-sentinel-config
    - name: tmp
      emptyDir: {}
```

### 3.2 Security Context Breakdown

| Setting | Value | Security Benefit |
|---------|-------|------------------|
| `runAsNonRoot` | `true` | Prevents container from running as root user |
| `runAsUser` | `1000` | Explicit non-root UID, not a system user |
| `runAsGroup` | `1000` | Explicit non-root GID |
| `fsGroup` | `1000` | Volume mounts owned by non-root group |
| `allowPrivilegeEscalation` | `false` | Prevents setuid binaries, ptrace, etc. |
| `readOnlyRootFilesystem` | `true` | Prevents filesystem modifications |
| `capabilities.drop` | `ALL` | Removes all Linux capabilities |

### 3.3 Non-Root Execution

Running as non-root (UID 1000) provides critical security boundaries:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Non-Root Security Benefits                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Container Process (UID 1000)                                        │   │
│  │                                                                       │   │
│  │  Cannot:                                                              │   │
│  │    - Bind to privileged ports (< 1024)                               │   │
│  │    - Access /proc/sys, /sys filesystem                               │   │
│  │    - Load kernel modules                                              │   │
│  │    - Modify system files (/etc/passwd, /etc/shadow)                  │   │
│  │    - Access other users' processes                                    │   │
│  │    - Mount filesystems                                                │   │
│  │    - Use raw sockets                                                  │   │
│  │    - Change system time                                               │   │
│  │    - Reboot the system                                                │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.4 Read-Only Root Filesystem

With `readOnlyRootFilesystem: true`, the container cannot:
- Write malicious binaries to disk
- Modify configuration files
- Create persistence mechanisms
- Write temporary files outside /tmp

The only writable location is `/tmp` (emptyDir volume), which is:
- Ephemeral (lost on pod restart)
- Size-limited by kubelet
- Isolated per pod

### 3.5 Dropped Capabilities

Dropping ALL capabilities removes dangerous Linux permissions:

| Dropped Capability | Attack Prevention |
|--------------------|-------------------|
| `CAP_NET_RAW` | Cannot create raw sockets for packet sniffing |
| `CAP_NET_BIND_SERVICE` | Cannot bind privileged ports |
| `CAP_SYS_ADMIN` | Cannot perform mount, namespace, or cgroup operations |
| `CAP_SYS_PTRACE` | Cannot trace or inject into other processes |
| `CAP_DAC_OVERRIDE` | Cannot bypass file permission checks |
| `CAP_CHOWN` | Cannot change file ownership |
| `CAP_SETUID/SETGID` | Cannot change process UID/GID |
| `CAP_MKNOD` | Cannot create device nodes |
| `CAP_SYS_TIME` | Cannot change system clock |

### 3.6 Pod Security Standards Compliance

Kube Sentinel's pod configuration complies with Kubernetes Pod Security Standards at the **restricted** level:

```yaml
# These labels can be added to the kube-sentinel namespace
apiVersion: v1
kind: Namespace
metadata:
  name: kube-sentinel
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

---

## 4. Namespace Exclusions as Security Boundary

### 4.1 Default Exclusion Configuration

```go
// From config.go - Default excluded namespaces
ExcludedNamespaces: []string{
    "kube-system",
    "monitoring",
}
```

### 4.2 Security Rationale

Namespace exclusions protect critical cluster infrastructure from automated remediation:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Namespace Exclusion Boundary                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        PROTECTED ZONE                                 │   │
│  │                    (No Automated Remediation)                         │   │
│  │                                                                       │   │
│  │  kube-system                                                          │   │
│  │    - kube-apiserver                                                   │   │
│  │    - kube-controller-manager                                          │   │
│  │    - kube-scheduler                                                   │   │
│  │    - etcd                                                             │   │
│  │    - coredns                                                          │   │
│  │    - kube-proxy                                                       │   │
│  │                                                                       │   │
│  │  monitoring                                                           │   │
│  │    - prometheus                                                       │   │
│  │    - grafana                                                          │   │
│  │    - alertmanager                                                     │   │
│  │    - loki (kube-sentinel's data source)                              │   │
│  │                                                                       │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│                              ─────────────────                              │
│                              Security Boundary                               │
│                              ─────────────────                              │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      REMEDIATION ZONE                                 │   │
│  │                  (Auto-remediation Enabled)                           │   │
│  │                                                                       │   │
│  │  default, production, staging, development, ...                       │   │
│  │    - Application workloads                                            │   │
│  │    - Stateless services                                               │   │
│  │    - Worker pods                                                      │   │
│  │                                                                       │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.3 Implementation in Remediation Engine

```go
// From engine.go - Namespace exclusion check
func (e *Engine) Execute(ctx context.Context, err *rules.MatchedError, rule *rules.Rule) (*store.RemediationLog, error) {
    // ...

    // Check excluded namespaces - SECURITY BOUNDARY
    if e.excludedNamespaces[err.Namespace] {
        logEntry.Status = "skipped"
        logEntry.Message = fmt.Sprintf("namespace %s is excluded", err.Namespace)
        e.saveLog(logEntry)
        return logEntry, nil
    }

    // ...
}
```

### 4.4 Recommended Exclusions

| Namespace | Reason | Risk if Remediated |
|-----------|--------|-------------------|
| `kube-system` | Kubernetes control plane | Cluster instability/failure |
| `kube-public` | Cluster-wide public data | N/A (usually no workloads) |
| `kube-node-lease` | Node heartbeat | Node eviction issues |
| `monitoring` | Observability stack | Loss of visibility during incidents |
| `logging` | Log aggregation (Loki) | kube-sentinel loses data source |
| `kube-sentinel` | Self-protection | Remediation loop |
| `cert-manager` | Certificate management | TLS failures cluster-wide |
| `ingress-nginx` | Ingress controller | External traffic disruption |
| `istio-system` | Service mesh control plane | Mesh-wide failures |

### 4.5 Configuration Example

```yaml
remediation:
  enabled: true
  excluded_namespaces:
    - kube-system
    - kube-public
    - kube-node-lease
    - monitoring
    - logging
    - kube-sentinel
    - cert-manager
    - ingress-nginx
    - istio-system
```

---

## 5. Rate Limiting as DoS Protection

### 5.1 Rate Limiting Architecture

Rate limiting prevents runaway remediation during cascading failures:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Rate Limiting Controls                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                     Global Hourly Rate Limit                          │  │
│  │                                                                       │  │
│  │  max_actions_per_hour: 50 (default)                                  │  │
│  │                                                                       │  │
│  │  ┌─────────────────────────────────────────────────────────────┐    │  │
│  │  │  Sliding Window: Last 60 minutes                             │    │  │
│  │  │                                                               │    │  │
│  │  │  Time:  10:00  10:15  10:30  10:45  11:00  11:15  11:30      │    │  │
│  │  │         ──┬──  ──┬──  ──┬──  ──┬──  ──┬──  ──┬──  ──┬──      │    │  │
│  │  │           │      │      │      │      │      │      │        │    │  │
│  │  │           5      3      8      12     7      4      2        │    │  │
│  │  │                                                               │    │  │
│  │  │  At 11:30, window includes 10:30-11:30 = 33 actions          │    │  │
│  │  │  Capacity remaining: 50 - 33 = 17 actions                     │    │  │
│  │  └─────────────────────────────────────────────────────────────┘    │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                     Per-Rule+Target Cooldown                          │  │
│  │                                                                       │  │
│  │  Key Format: "ruleName:namespace/pod"                                 │  │
│  │                                                                       │  │
│  │  Example: "crashloop-backoff:production/api-server-7d8f9c"           │  │
│  │  Cooldown: 5 minutes after last action                                │  │
│  │                                                                       │  │
│  │  Same pod cannot be restarted by same rule for 5 minutes             │  │
│  │  Different pods can still be restarted immediately                    │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Implementation Details

```go
// From engine.go - Rate limiting implementation

// Track action timestamps in sliding window
hourlyLog []time.Time

// Cleanup old entries before checking
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

// Check rate limit before execution
e.cleanupHourlyLog()
if len(e.hourlyLog) >= e.maxActionsPerHour {
    logEntry.Status = "skipped"
    logEntry.Message = fmt.Sprintf("hourly limit reached (%d actions)", e.maxActionsPerHour)
    e.saveLog(logEntry)
    return logEntry, nil
}
```

### 5.3 DoS Attack Scenarios Mitigated

| Attack Scenario | Without Rate Limit | With Rate Limit |
|-----------------|-------------------|-----------------|
| Log injection attack flooding errors | Unlimited pod restarts | Max 50/hour |
| Cascading failure during outage | All pods restarted repeatedly | Controlled response |
| Malicious rule misconfiguration | Continuous scaling events | Capped at limit |
| Cooldown bypass via pod name changes | Each new pod name restartable | Per-deployment cooldown |

### 5.4 Recommended Rate Limit Settings

| Environment | max_actions_per_hour | Rationale |
|-------------|---------------------|-----------|
| Development | 10-20 | Low workload count |
| Staging | 30-50 | Medium workload count |
| Production (small) | 50-100 | Standard cluster |
| Production (large) | 100-200 | Many namespaces/workloads |

---

## 6. Dry Run Mode for Safe Testing

### 6.1 Dry Run Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Dry Run Mode Flow                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Error Detected                                                             │
│        │                                                                     │
│        ▼                                                                     │
│   ┌────────────────────┐                                                    │
│   │  All Safety Checks │                                                    │
│   │  (namespace, rate  │                                                    │
│   │   limit, cooldown) │                                                    │
│   └─────────┬──────────┘                                                    │
│             │                                                                │
│             ▼                                                                │
│   ┌─────────────────────────────────────────────────────────────────────┐  │
│   │                     DRY_RUN CHECK                                     │  │
│   │                                                                       │  │
│   │   if e.dryRun {                                                       │  │
│   │       logEntry.Status = "success"                                     │  │
│   │       logEntry.Message = "dry run - would execute action"            │  │
│   │       e.logger.Info("dry run remediation",                           │  │
│   │           "action", rule.Remediation.Action,                          │  │
│   │           "target", target.String(),                                  │  │
│   │           "rule", rule.Name,                                          │  │
│   │       )                                                               │  │
│   │       // NO KUBERNETES API CALL                                       │  │
│   │       return logEntry, nil                                            │  │
│   │   }                                                                   │  │
│   │                                                                       │  │
│   └─────────────────────────────────────────────────────────────────────┘  │
│             │                                                                │
│             │ dry_run: false                                                │
│             ▼                                                                │
│   ┌────────────────────┐                                                    │
│   │   Execute Action   │                                                    │
│   │  (Kubernetes API)  │                                                    │
│   └────────────────────┘                                                    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 Dry Run Benefits

| Benefit | Description |
|---------|-------------|
| Rule Validation | Verify rules match expected errors |
| Impact Assessment | See what would be remediated before enabling |
| Audit Trail | Build confidence through logged dry runs |
| Zero Risk Testing | No actual changes to cluster state |
| Production Validation | Run in production to validate before enabling |

### 6.3 Configuration

```yaml
# Safe testing configuration
remediation:
  enabled: true
  dry_run: true  # Log actions without executing
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
```

### 6.4 Dry Run Deployment Strategy

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Safe Deployment Progression                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Phase 1: Monitoring Only                                                    │
│  ─────────────────────────                                                   │
│  remediation:                                                                │
│    enabled: false      # Remediation completely disabled                     │
│    dry_run: true                                                             │
│                                                                              │
│  Duration: 1-2 weeks                                                         │
│  Purpose: Validate rule matching, review error patterns                      │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  Phase 2: Dry Run                                                            │
│  ─────────────────                                                           │
│  remediation:                                                                │
│    enabled: true                                                             │
│    dry_run: true       # Log what would happen                               │
│                                                                              │
│  Duration: 1-2 weeks                                                         │
│  Purpose: Validate remediation logic, review action logs                     │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  Phase 3: Limited Production                                                 │
│  ───────────────────────────                                                 │
│  remediation:                                                                │
│    enabled: true                                                             │
│    dry_run: false                                                            │
│    max_actions_per_hour: 10   # Start conservative                           │
│                                                                              │
│  Duration: 2-4 weeks                                                         │
│  Purpose: Validate real remediation, monitor outcomes                        │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  Phase 4: Full Production                                                    │
│  ────────────────────────                                                    │
│  remediation:                                                                │
│    enabled: true                                                             │
│    dry_run: false                                                            │
│    max_actions_per_hour: 50   # Increase based on experience                 │
│                                                                              │
│  Ongoing: Monitor, tune, adjust rules                                        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Network Policy Recommendations

### 7.1 Recommended NetworkPolicy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
  policyTypes:
    - Ingress
    - Egress

  ingress:
    # Allow Prometheus scraping
    - from:
        - namespaceSelector:
            matchLabels:
              name: monitoring
          podSelector:
            matchLabels:
              app.kubernetes.io/name: prometheus
      ports:
        - protocol: TCP
          port: 8080

    # Allow internal dashboard access (optional)
    - from:
        - namespaceSelector:
            matchLabels:
              name: ingress-nginx
      ports:
        - protocol: TCP
          port: 8080

  egress:
    # Allow DNS resolution
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53

    # Allow Loki queries
    - to:
        - namespaceSelector:
            matchLabels:
              name: monitoring
          podSelector:
            matchLabels:
              app.kubernetes.io/name: loki
      ports:
        - protocol: TCP
          port: 3100

    # Allow Kubernetes API access
    - to:
        - ipBlock:
            cidr: 10.96.0.1/32  # Typical ClusterIP for kubernetes.default
      ports:
        - protocol: TCP
          port: 443
```

### 7.2 Network Isolation Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Network Policy Boundaries                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                        INGRESS ALLOWED                                       │
│                                                                              │
│  ┌─────────────────┐         ┌─────────────────┐                            │
│  │   Prometheus    │─────────▶│  kube-sentinel  │                            │
│  │   (monitoring)  │  :8080   │    (port 8080)  │                            │
│  └─────────────────┘         └─────────────────┘                            │
│                                      ▲                                       │
│  ┌─────────────────┐                 │                                       │
│  │  Ingress NGINX  │─────────────────┘                                       │
│  │  (ingress-nginx)│  :8080 (dashboard)                                     │
│  └─────────────────┘                                                         │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│                        EGRESS ALLOWED                                        │
│                                                                              │
│                       ┌─────────────────┐                                    │
│                       │  kube-sentinel  │                                    │
│                       └───────┬─────────┘                                    │
│                               │                                              │
│             ┌─────────────────┼─────────────────┐                            │
│             │                 │                 │                            │
│             ▼                 ▼                 ▼                            │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐               │
│  │    CoreDNS      │ │      Loki       │ │  Kubernetes API │               │
│  │  (kube-system)  │ │  (monitoring)   │ │                 │               │
│  │    UDP:53       │ │   TCP:3100      │ │    TCP:443      │               │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘               │
│                                                                              │
│                        EGRESS DENIED                                         │
│                                                                              │
│  ╳ Internet access                                                           │
│  ╳ Other namespaces (except monitoring, kube-system)                        │
│  ╳ Other pods within kube-sentinel namespace                                │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.3 NetworkPolicy Security Benefits

| Restriction | Threat Mitigated |
|-------------|-----------------|
| Ingress limited to Prometheus | Unauthorized dashboard access |
| Egress limited to Loki, DNS, API | Data exfiltration |
| No internet egress | Callback to attacker C2 servers |
| No cross-namespace egress | Lateral movement |

---

## 8. Secret Management

### 8.1 Loki Credentials Configuration

Kube Sentinel supports optional authentication to Loki:

```go
// From config.go
type LokiConfig struct {
    URL          string        `yaml:"url"`
    Query        string        `yaml:"query"`
    PollInterval time.Duration `yaml:"poll_interval"`
    Lookback     time.Duration `yaml:"lookback"`
    TenantID     string        `yaml:"tenant_id,omitempty"`
    Username     string        `yaml:"username,omitempty"`
    Password     string        `yaml:"password,omitempty"`
}
```

### 8.2 Credential Handling Best Practices

**DO NOT** store credentials in ConfigMap:

```yaml
# BAD - Credentials in ConfigMap (visible to anyone with configmap read access)
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-sentinel-config
data:
  config.yaml: |
    loki:
      url: http://loki.monitoring:3100
      username: admin
      password: secretpassword  # EXPOSED!
```

**DO** use Kubernetes Secrets with environment variable injection:

```yaml
# GOOD - Credentials in Secret
apiVersion: v1
kind: Secret
metadata:
  name: kube-sentinel-loki-credentials
  namespace: kube-sentinel
type: Opaque
stringData:
  username: admin
  password: secretpassword

---
# Reference in Deployment
spec:
  containers:
    - name: kube-sentinel
      env:
        - name: LOKI_USERNAME
          valueFrom:
            secretKeyRef:
              name: kube-sentinel-loki-credentials
              key: username
        - name: LOKI_PASSWORD
          valueFrom:
            secretKeyRef:
              name: kube-sentinel-loki-credentials
              key: password
```

### 8.3 Secret Management Options

| Method | Security Level | Complexity | Use Case |
|--------|---------------|------------|----------|
| Environment from Secret | Medium | Low | Development, simple deployments |
| External Secrets Operator | High | Medium | Production with external vault |
| Vault Agent Sidecar | High | High | Enterprise with HashiCorp Vault |
| Workload Identity | Highest | High | Cloud-native (GKE, EKS, AKS) |

### 8.4 Secret Rotation Considerations

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Secret Rotation Strategy                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Option 1: Pod Restart on Secret Update                                      │
│  ────────────────────────────────────────                                    │
│  - Update Secret in Kubernetes                                               │
│  - Delete kube-sentinel pod                                                  │
│  - Deployment creates new pod with new credentials                           │
│  - Downtime: ~30 seconds                                                     │
│                                                                              │
│  Option 2: External Secrets Operator                                         │
│  ────────────────────────────────────                                        │
│  - ESO syncs from external vault                                             │
│  - Automatic rotation based on policy                                        │
│  - Pod restart still required for env vars                                   │
│                                                                              │
│  Option 3: Vault Agent Sidecar                                               │
│  ──────────────────────────────                                              │
│  - Vault Agent writes credentials to shared volume                           │
│  - Application reads from file                                               │
│  - Hot reload possible with file watching                                    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. Attack Surface Analysis

### 9.1 Attack Surface Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Attack Surface Map                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  EXTERNAL INPUTS                              INTERNAL COMPONENTS            │
│  ───────────────                              ───────────────────            │
│                                                                              │
│  ┌─────────────────┐                         ┌─────────────────────────┐   │
│  │  Loki Logs      │───────────────────────▶│  Loki Client            │   │
│  │  (Untrusted)    │    Log data may be     │  - URL parsing          │   │
│  └─────────────────┘    attacker-controlled │  - JSON parsing         │   │
│                                              │  - Auth headers         │   │
│                                              └───────────┬─────────────┘   │
│                                                          │                  │
│  ┌─────────────────┐                                     ▼                  │
│  │  HTTP Requests  │                         ┌─────────────────────────┐   │
│  │  (Dashboard)    │───────────────────────▶│  Web Server             │   │
│  └─────────────────┘    User input          │  - URL routing          │   │
│                                              │  - Query params         │   │
│                                              │  - Form handling        │   │
│                                              └───────────┬─────────────┘   │
│                                                          │                  │
│  ┌─────────────────┐                                     ▼                  │
│  │  Rules YAML     │                         ┌─────────────────────────┐   │
│  │  (ConfigMap)    │───────────────────────▶│  Rule Engine            │   │
│  └─────────────────┘    Regex patterns      │  - YAML parsing         │   │
│                                              │  - Regex compilation    │   │
│                                              │  - Pattern matching     │   │
│                                              └───────────┬─────────────┘   │
│                                                          │                  │
│                                                          ▼                  │
│                                              ┌─────────────────────────┐   │
│  KUBERNETES API                              │  Remediation Engine     │   │
│  ──────────────                              │  - Action execution     │   │
│                                              │  - Safety controls      │   │
│  ┌─────────────────┐                         └───────────┬─────────────┘   │
│  │  API Server     │◀───────────────────────────────────┘                  │
│  │                 │    Pod/Deployment operations                          │
│  └─────────────────┘                                                        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Input Vectors

| Input Vector | Source | Risk | Mitigation |
|--------------|--------|------|------------|
| Loki log data | External logs | Log injection, regex DoS | Input validation, regex timeout |
| HTTP requests | Dashboard users | XSS, CSRF, path traversal | Input sanitization, templating |
| Rules YAML | ConfigMap | Regex DoS, malicious patterns | Config validation, regex limits |
| Kubernetes API responses | API Server | Malformed data | Type-safe client |

### 9.3 Component Attack Surface

| Component | Exposed Ports | Input Types | Trust Level |
|-----------|--------------|-------------|-------------|
| Web Server | 8080 (HTTP) | URL paths, query params | Low (user input) |
| Loki Client | Outbound 3100 | JSON responses | Medium (internal service) |
| K8s Client | Outbound 443 | API responses | High (control plane) |
| Config Loader | Filesystem | YAML | High (admin-controlled) |

---

## 10. Threat Model

### 10.1 STRIDE Threat Analysis

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          STRIDE Threat Model                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  S - SPOOFING                                                                │
│  ─────────────                                                               │
│  Threat: Attacker impersonates kube-sentinel service account                 │
│  Impact: Unauthorized pod deletions, deployment modifications               │
│  Mitigation: ServiceAccount bound to specific namespace                      │
│              RBAC prevents token theft from other namespaces                │
│              Short-lived tokens (if using bound tokens)                      │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  T - TAMPERING                                                               │
│  ─────────────                                                               │
│  Threat: Attacker modifies rules to target specific workloads               │
│  Impact: Intentional disruption of critical services                        │
│  Mitigation: ConfigMap access requires RBAC permissions                      │
│              Audit logging of ConfigMap changes                              │
│              GitOps workflow for configuration changes                       │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  R - REPUDIATION                                                             │
│  ──────────────                                                              │
│  Threat: Attacker performs remediation with no audit trail                   │
│  Impact: Unable to investigate incidents or attribute actions               │
│  Mitigation: Remediation logging to store                                    │
│              Kubernetes audit logs capture API calls                         │
│              Structured logging with timestamps                              │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  I - INFORMATION DISCLOSURE                                                  │
│  ──────────────────────────                                                  │
│  Threat: Dashboard exposes sensitive error details                           │
│  Impact: Attackers learn about application internals                        │
│  Mitigation: Dashboard should not be publicly exposed                        │
│              NetworkPolicy restricts access                                  │
│              No secrets displayed in UI                                      │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  D - DENIAL OF SERVICE                                                       │
│  ─────────────────────                                                       │
│  Threat: Log flood triggers excessive remediation                            │
│  Impact: All pods constantly restarted, service outage                      │
│  Mitigation: Hourly rate limiting (max_actions_per_hour)                     │
│              Per-rule cooldowns                                              │
│              Namespace exclusions protect critical services                  │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  E - ELEVATION OF PRIVILEGE                                                  │
│  ────────────────────────                                                    │
│  Threat: Compromised kube-sentinel used to attack cluster                    │
│  Impact: Broader cluster compromise                                          │
│  Mitigation: No secret read access (cannot steal credentials)               │
│              No create permissions (cannot deploy malicious workloads)      │
│              No RBAC modification permissions                                │
│              Non-root container with dropped capabilities                    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.2 Attack Scenarios

#### Scenario 1: Log Injection Attack

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Attack: Log Injection                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Attacker Goal: Trigger remediation against target workload                  │
│                                                                              │
│  Attack Steps:                                                               │
│  1. Attacker gains access to a pod that can write logs                       │
│  2. Attacker crafts log messages matching high-priority rules               │
│  3. Log message includes target namespace/pod information                   │
│  4. kube-sentinel matches rule and triggers remediation                     │
│                                                                              │
│  Example Malicious Log:                                                      │
│  {"namespace":"production","pod":"critical-service-xyz",                    │
│   "message":"CrashLoopBackOff: Back-off restarting failed container"}       │
│                                                                              │
│  Mitigations:                                                                │
│  - Namespace exclusions protect critical namespaces                         │
│  - Rate limiting caps total remediation actions                             │
│  - Cooldowns prevent repeated actions on same target                        │
│  - Audit logs enable investigation                                           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Scenario 2: Rule Manipulation

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Attack: Rule Manipulation                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Attacker Goal: Modify rules to cause service disruption                     │
│                                                                              │
│  Attack Steps:                                                               │
│  1. Attacker gains ConfigMap edit access in kube-sentinel namespace         │
│  2. Attacker modifies rules.yaml to match broad patterns                    │
│  3. Rule triggers remediation on many workloads                             │
│                                                                              │
│  Example Malicious Rule:                                                     │
│  - name: match-everything                                                    │
│    match:                                                                    │
│      pattern: ".*"   # Matches all logs                                      │
│    priority: P1                                                              │
│    remediation:                                                              │
│      action: restart-pod                                                     │
│      cooldown: 1s   # Very short cooldown                                    │
│                                                                              │
│  Mitigations:                                                                │
│  - RBAC limits who can edit ConfigMaps                                      │
│  - GitOps requires PR review for config changes                             │
│  - Rate limiting caps damage even with bad rules                            │
│  - Audit logging captures ConfigMap changes                                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Scenario 3: Container Escape

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Attack: Container Escape                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Attacker Goal: Escape container to compromise node                          │
│                                                                              │
│  Attack Blocked By:                                                          │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Security Context Barriers                                           │   │
│  │                                                                       │   │
│  │  1. runAsNonRoot: true                                                │   │
│  │     - Cannot write to /etc or system directories                     │   │
│  │     - Cannot bind privileged ports                                    │   │
│  │                                                                       │   │
│  │  2. readOnlyRootFilesystem: true                                      │   │
│  │     - Cannot write malicious binaries                                 │   │
│  │     - Cannot modify application code                                  │   │
│  │                                                                       │   │
│  │  3. allowPrivilegeEscalation: false                                   │   │
│  │     - Cannot use setuid binaries                                      │   │
│  │     - Cannot gain additional privileges                               │   │
│  │                                                                       │   │
│  │  4. capabilities.drop: ALL                                            │   │
│  │     - No CAP_SYS_ADMIN for mount namespace escape                    │   │
│  │     - No CAP_NET_RAW for network attacks                             │   │
│  │     - No CAP_SYS_PTRACE for process injection                        │   │
│  │                                                                       │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.3 Risk Matrix

| Threat | Likelihood | Impact | Risk Level | Mitigation Status |
|--------|------------|--------|------------|-------------------|
| Log injection | Medium | Medium | Medium | Mitigated (rate limits, exclusions) |
| Rule manipulation | Low | High | Medium | Mitigated (RBAC, GitOps) |
| Container escape | Low | Critical | Medium | Mitigated (security context) |
| ServiceAccount theft | Low | High | Medium | Mitigated (namespace isolation) |
| DoS via remediation | Medium | High | High | Mitigated (rate limits) |
| Dashboard information leak | Medium | Low | Low | Requires network policy |

---

## 11. Security Best Practices Checklist

### 11.1 Deployment Checklist

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Security Deployment Checklist                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  RBAC & Service Account                                                      │
│  [ ] ServiceAccount created in dedicated namespace                           │
│  [ ] ClusterRole uses minimum required permissions                           │
│  [ ] No secret/configmap read permissions (unless required)                  │
│  [ ] No create permissions for pods/deployments                              │
│  [ ] ClusterRoleBinding references correct ServiceAccount                    │
│                                                                              │
│  Pod Security                                                                │
│  [ ] runAsNonRoot: true                                                      │
│  [ ] runAsUser/runAsGroup set to non-root UID (e.g., 1000)                  │
│  [ ] readOnlyRootFilesystem: true                                            │
│  [ ] allowPrivilegeEscalation: false                                         │
│  [ ] capabilities.drop: ALL                                                  │
│  [ ] /tmp mounted as emptyDir for writable space                            │
│  [ ] Config mounted as readOnly: true                                        │
│                                                                              │
│  Namespace Security                                                          │
│  [ ] kube-sentinel deployed in dedicated namespace                           │
│  [ ] Pod Security Standards enforced on namespace                            │
│  [ ] ResourceQuota prevents resource exhaustion                              │
│  [ ] LimitRange sets default resource limits                                 │
│                                                                              │
│  Remediation Safety                                                          │
│  [ ] excluded_namespaces includes kube-system                               │
│  [ ] excluded_namespaces includes monitoring namespace                       │
│  [ ] excluded_namespaces includes kube-sentinel namespace                   │
│  [ ] max_actions_per_hour set to reasonable limit                           │
│  [ ] dry_run enabled for initial deployment                                  │
│  [ ] Cooldowns configured on all remediation rules                           │
│                                                                              │
│  Network Security                                                            │
│  [ ] NetworkPolicy restricts ingress to Prometheus only                      │
│  [ ] NetworkPolicy restricts egress to Loki, DNS, API server                │
│  [ ] Dashboard NOT exposed to public internet                                │
│  [ ] TLS enabled for Loki connection (if external)                           │
│                                                                              │
│  Secret Management                                                           │
│  [ ] Loki credentials stored in Kubernetes Secret (not ConfigMap)           │
│  [ ] Secret referenced via secretKeyRef (not mounted file)                  │
│  [ ] External secrets manager for production (optional)                      │
│  [ ] Secret rotation procedure documented                                    │
│                                                                              │
│  Monitoring & Audit                                                          │
│  [ ] Prometheus scraping enabled for metrics                                 │
│  [ ] Alerts configured for remediation rate limits                          │
│  [ ] Kubernetes audit logging enabled for kube-sentinel SA                  │
│  [ ] Remediation logs retained for compliance period                        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 11.2 Operational Security Practices

| Practice | Frequency | Owner |
|----------|-----------|-------|
| Review remediation logs | Daily | SRE Team |
| Audit RBAC permissions | Monthly | Security Team |
| Rotate Loki credentials | Quarterly | Platform Team |
| Review namespace exclusions | Quarterly | Platform Team |
| Update kube-sentinel image | Monthly | Platform Team |
| Penetration testing | Annually | Security Team |
| Review rate limit settings | Quarterly | SRE Team |
| Validate dry run before changes | Each deployment | SRE Team |

### 11.3 Incident Response Procedures

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Incident Response Procedures                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Scenario: Suspected Unauthorized Remediation                                │
│  ────────────────────────────────────────────                                │
│                                                                              │
│  1. Immediate Actions:                                                       │
│     - Set dry_run: true in config                                            │
│     - Restart kube-sentinel to apply                                         │
│     - Preserve current remediation logs                                      │
│                                                                              │
│  2. Investigation:                                                           │
│     - Review remediation logs for anomalies                                  │
│     - Check Kubernetes audit logs for SA activity                           │
│     - Review ConfigMap change history                                        │
│     - Examine Loki logs for injection patterns                              │
│                                                                              │
│  3. Containment:                                                             │
│     - If SA compromised: delete and recreate                                 │
│     - If rules tampered: restore from GitOps                                 │
│     - If log injection: block source in Loki query                          │
│                                                                              │
│  4. Recovery:                                                                │
│     - Restore affected workloads if needed                                   │
│     - Re-enable remediation after root cause fixed                          │
│     - Start with dry_run: true, validate, then enable                       │
│                                                                              │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
│                                                                              │
│  Scenario: Rate Limit Exhaustion                                             │
│  ────────────────────────────────                                            │
│                                                                              │
│  1. Immediate Actions:                                                       │
│     - Check dashboard for error spike source                                 │
│     - Identify if legitimate incident or attack                             │
│                                                                              │
│  2. If Legitimate Incident:                                                  │
│     - Temporarily increase max_actions_per_hour                             │
│     - Focus on root cause of errors                                          │
│     - Return to normal limit after incident                                  │
│                                                                              │
│  3. If Attack:                                                               │
│     - Do NOT increase rate limit                                             │
│     - Add source to excluded_namespaces if identifiable                     │
│     - Block at Loki query level if possible                                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 12. Conclusion

Kube Sentinel's security model implements defense-in-depth through multiple layers:

1. **RBAC Least Privilege**: Minimal permissions for monitoring and remediation only
2. **Pod Hardening**: Non-root, read-only filesystem, dropped capabilities
3. **Namespace Boundaries**: Critical namespaces protected from automated actions
4. **Rate Limiting**: DoS protection through global and per-target limits
5. **Dry Run Mode**: Safe testing before production deployment
6. **Network Isolation**: Restricted ingress/egress through NetworkPolicy
7. **Secret Management**: Credentials stored securely, not in ConfigMaps

When properly configured following this security model, Kube Sentinel provides autonomous error remediation while maintaining the security posture required for production Kubernetes clusters.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
