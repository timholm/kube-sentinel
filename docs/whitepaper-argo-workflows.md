# Argo Workflows Integration

Kube Sentinel integrates with Argo Workflows to enable complex, multi-step remediation workflows that go beyond simple pod restarts or scaling operations.

## Overview

While Kube Sentinel's built-in actions handle common remediation scenarios, some situations require more sophisticated responses:

- **Multi-step diagnostics**: Collect logs, check metrics, analyze patterns before taking action
- **Coordinated remediation**: Update multiple resources in sequence with rollback capability
- **External integrations**: Trigger CI/CD pipelines, notify on-call teams, create tickets
- **Custom logic**: Organization-specific remediation procedures

Argo Workflows provides a powerful Kubernetes-native workflow engine that complements Kube Sentinel's error detection capabilities.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        kube-sentinel                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Loki Poller  │  │ Rule Engine  │  │ Remediation Engine   │  │
│  │              │──▶│              │──▶│                      │  │
│  │ - Query logs │  │ - Classify   │  │ - trigger-argo-      │  │
│  │ - Parse      │  │ - Prioritize │  │   workflow           │  │
│  └──────────────┘  └──────────────┘  └──────────┬───────────┘  │
└───────────────────────────────────────────────────┼─────────────┘
                                                    │
                                                    ▼
                                     ┌──────────────────────────┐
                                     │   Argo Workflows         │
                                     │   ┌──────────────────┐   │
                                     │   │ WorkflowTemplate │   │
                                     │   │ - diagnose-pod   │   │
                                     │   │ - restart-backup │   │
                                     │   │ - scale-monitor  │   │
                                     │   └──────────────────┘   │
                                     │           │              │
                                     │           ▼              │
                                     │   ┌──────────────────┐   │
                                     │   │ Workflow         │   │
                                     │   │ (running)        │   │
                                     │   └──────────────────┘   │
                                     └──────────────────────────┘
```

## Configuration

### Rule Configuration

To trigger an Argo Workflow as a remediation action:

```yaml
rules:
  - name: complex-failure-recovery
    match:
      pattern: "database connection failed|connection pool exhausted"
    priority: P1
    remediation:
      action: trigger-argo-workflow
      params:
        workflow_template: diagnose-and-recover
        namespace: argo
        service_account: argo-workflow
      cooldown: 10m
    enabled: true
```

### Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `workflow_template` | Yes* | Name of WorkflowTemplate to use |
| `workflow_name` | Yes* | Name for inline workflow (alternative to template) |
| `namespace` | No | Namespace for workflow (default: argo) |
| `service_account` | No | ServiceAccount for workflow |
| `arguments` | No | JSON array of additional workflow arguments |
| `image` | No | Container image for inline workflows |
| `script` | No | Custom script for inline workflows |
| `inline_action` | No | Action type for default script: restart, describe, logs, events, diagnose |

*Either `workflow_template` or `workflow_name` is required.

## Built-in WorkflowTemplates

Kube Sentinel provides several built-in WorkflowTemplates for common scenarios:

### 1. diagnose-pod

Collects comprehensive diagnostic information for a failing pod.

**Steps:**
1. Describe pod (resource status, conditions, events)
2. Collect container logs
3. Gather related events

**Usage:**
```yaml
remediation:
  action: trigger-argo-workflow
  params:
    workflow_template: diagnose-pod
```

### 2. restart-with-backup

Safely restarts a pod after backing up its logs for analysis.

**Steps:**
1. Backup container logs to artifact storage
2. Delete pod to trigger restart
3. Verify new pod is healthy

**Usage:**
```yaml
remediation:
  action: trigger-argo-workflow
  params:
    workflow_template: restart-with-backup
```

### 3. scale-and-monitor

Scales a deployment and monitors for successful rollout.

**Steps:**
1. Scale deployment to target replicas
2. Wait for rollout to complete
3. Verify health checks pass

**Usage:**
```yaml
remediation:
  action: trigger-argo-workflow
  params:
    workflow_template: scale-and-monitor
    arguments: '[{"name": "replicas", "value": "5"}]'
```

## Creating Custom WorkflowTemplates

### Basic Template Structure

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: my-remediation
  namespace: argo
  labels:
    app.kubernetes.io/managed-by: kube-sentinel
spec:
  entrypoint: main
  arguments:
    parameters:
      - name: namespace
      - name: pod
      - name: container
  templates:
    - name: main
      inputs:
        parameters:
          - name: namespace
          - name: pod
          - name: container
      steps:
        - - name: step1
            template: do-something
            arguments:
              parameters:
                - name: target
                  value: "{{inputs.parameters.namespace}}/{{inputs.parameters.pod}}"
    - name: do-something
      inputs:
        parameters:
          - name: target
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args:
          - echo "Processing {{inputs.parameters.target}}"
```

### Best Practices

1. **Always accept standard parameters**: `namespace`, `pod`, `container`
2. **Use labels**: Tag workflows for easy identification and cleanup
3. **Set TTL strategies**: Auto-cleanup completed workflows
4. **Handle failures gracefully**: Use `onExit` handlers for cleanup
5. **Limit resource usage**: Set resource requests/limits on containers

### Advanced Patterns

#### DAG-based Parallel Execution

```yaml
templates:
  - name: parallel-diagnostics
    dag:
      tasks:
        - name: logs
          template: get-logs
        - name: events
          template: get-events
        - name: metrics
          template: get-metrics
        - name: analyze
          template: analyze-results
          dependencies: [logs, events, metrics]
```

#### Conditional Remediation

```yaml
templates:
  - name: smart-remediation
    steps:
      - - name: diagnose
          template: check-health
      - - name: restart
          template: restart-pod
          when: "{{steps.diagnose.outputs.result}} == 'unhealthy'"
      - - name: escalate
          template: notify-oncall
          when: "{{steps.diagnose.outputs.result}} == 'critical'"
```

#### Integration with External Systems

```yaml
templates:
  - name: create-incident
    container:
      image: curlimages/curl:latest
      command: ["/bin/sh", "-c"]
      args:
        - |
          curl -X POST https://api.pagerduty.com/incidents \
            -H "Authorization: Token token=$PD_TOKEN" \
            -H "Content-Type: application/json" \
            -d '{
              "incident": {
                "type": "incident",
                "title": "Kube Sentinel: Pod failure in {{inputs.parameters.namespace}}",
                "service": {"id": "PXXXXXX", "type": "service_reference"},
                "body": {"type": "incident_body", "details": "Pod {{inputs.parameters.pod}} is failing"}
              }
            }'
      env:
        - name: PD_TOKEN
          valueFrom:
            secretKeyRef:
              name: pagerduty-token
              key: token
```

## Error-to-Workflow Mapping

Map specific error patterns to appropriate remediation workflows:

```yaml
rules:
  # OOM errors - diagnose memory usage
  - name: oom-investigation
    match:
      pattern: "OOMKilled|Out of memory"
    priority: P1
    remediation:
      action: trigger-argo-workflow
      params:
        workflow_template: diagnose-memory
    enabled: true

  # Connection errors - check network and dependencies
  - name: network-diagnostics
    match:
      pattern: "connection refused|ECONNREFUSED|no route to host"
    priority: P2
    remediation:
      action: trigger-argo-workflow
      params:
        workflow_template: network-diagnostics
    enabled: true

  # Image pull errors - check registry and credentials
  - name: image-pull-investigation
    match:
      pattern: "ImagePullBackOff|ErrImagePull"
    priority: P2
    remediation:
      action: trigger-argo-workflow
      params:
        workflow_template: check-image-registry
    enabled: true

  # Generic failures - comprehensive diagnostics
  - name: general-diagnostics
    match:
      pattern: "CrashLoopBackOff"
    priority: P1
    remediation:
      action: trigger-argo-workflow
      params:
        workflow_template: diagnose-pod
    enabled: true
```

## RBAC Requirements

The Argo Workflow ServiceAccount needs permissions to perform remediation actions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-sentinel-workflow
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log", "events"]
    verbs: ["get", "list", "watch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: [""]
    resources: ["configmaps", "secrets"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-sentinel-workflow
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-sentinel-workflow
subjects:
  - kind: ServiceAccount
    name: argo-workflow
    namespace: argo
```

## Monitoring Workflow Execution

### View Running Workflows

```bash
# List all kube-sentinel triggered workflows
kubectl get workflows -n argo -l app.kubernetes.io/managed-by=kube-sentinel

# Get workflow logs
argo logs -n argo kube-sentinel-remediation-xxxxx
```

### Workflow Cleanup

Workflows are automatically cleaned up based on TTL strategy:
- Success: 10 minutes (600 seconds)
- Failure: 24 hours (86400 seconds)
- Completed: 1 hour (3600 seconds)

### Prometheus Metrics

Monitor workflow execution with Prometheus:

```promql
# Workflow success rate
sum(rate(argo_workflow_status_phase{phase="Succeeded"}[5m])) /
sum(rate(argo_workflow_status_phase[5m]))

# Average workflow duration
histogram_quantile(0.95,
  rate(argo_workflow_duration_seconds_bucket[5m])
)
```

## Troubleshooting

### Workflow Not Triggering

1. Check Kube Sentinel logs for errors:
   ```bash
   kubectl logs -n kube-sentinel deploy/kube-sentinel | grep "argo-workflow"
   ```

2. Verify WorkflowTemplate exists:
   ```bash
   kubectl get workflowtemplates -n argo
   ```

3. Check RBAC permissions:
   ```bash
   kubectl auth can-i create workflows -n argo --as=system:serviceaccount:kube-sentinel:kube-sentinel
   ```

### Workflow Fails

1. Check workflow status:
   ```bash
   argo get -n argo <workflow-name>
   ```

2. View step logs:
   ```bash
   argo logs -n argo <workflow-name>
   ```

3. Check events:
   ```bash
   kubectl get events -n argo --field-selector involvedObject.name=<workflow-name>
   ```

## Best Practices

1. **Start with dry-run**: Test workflows manually before enabling in rules
2. **Use templates**: Prefer WorkflowTemplates over inline workflows for reusability
3. **Set appropriate cooldowns**: Prevent workflow spam with generous cooldowns
4. **Monitor costs**: Workflow pods consume resources; set limits appropriately
5. **Clean up artifacts**: Configure artifact GC to prevent storage bloat
6. **Test thoroughly**: Validate workflows in non-production first
7. **Document runbooks**: Create documentation for each workflow's purpose and expected outcome
