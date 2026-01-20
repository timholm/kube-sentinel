# Kube Sentinel

Kubernetes error prioritization and auto-remediation agent. Continuously monitors Loki logs, prioritizes errors by severity/impact, displays them in a web dashboard, and automatically remediates known issues.

## Features

- **Real-time Log Monitoring**: Polls Loki for error logs with configurable queries
- **Intelligent Prioritization**: Rule-based error classification (P1-Critical to P4-Low)
- **Auto-Remediation**: Automatically fix common issues like CrashLoopBackOff
- **Web Dashboard**: Real-time error feed, priority queue, remediation history
- **Safety Controls**: Cooldowns, rate limits, dry-run mode, namespace exclusions
- **Deduplication**: Smart fingerprinting to group similar errors

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        kube-sentinel                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Loki Poller  │  │ Rule Engine  │  │ Remediation Engine   │  │
│  │              │──▶│              │──▶│                      │  │
│  │ - Query logs │  │ - Classify   │  │ - Pod restart        │  │
│  │ - Parse      │  │ - Prioritize │  │ - Scale deployment   │  │
│  │ - Dedupe     │  │ - Group      │  │ - Rollback           │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    Web Dashboard                          │  │
│  │  - Real-time error feed        - Remediation history     │  │
│  │  - Priority queue              - Rule configuration      │  │
│  │  - Namespace/pod filtering     - Health overview         │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
         │                                        │
         ▼                                        ▼
    ┌─────────┐                           ┌─────────────┐
    │  Loki   │                           │ Kubernetes  │
    │  API    │                           │ API Server  │
    └─────────┘                           └─────────────┘
```

## Quick Start

### Prerequisites

- Kubernetes cluster with Loki deployed
- `kubectl` configured to access your cluster

### Deploy to Kubernetes

```bash
# Deploy using kustomize
kubectl apply -k deploy/kubernetes/

# Or apply individual manifests
kubectl apply -f deploy/kubernetes/namespace.yaml
kubectl apply -f deploy/kubernetes/rbac.yaml
kubectl apply -f deploy/kubernetes/configmap.yaml
kubectl apply -f deploy/kubernetes/deployment.yaml
kubectl apply -f deploy/kubernetes/service.yaml

# Access the dashboard
kubectl port-forward -n kube-sentinel svc/kube-sentinel 8080:80
```

Open http://localhost:8080 in your browser.

### Run Locally

```bash
# Build
make build

# Run with local config
./build/bin/kube-sentinel --config config.yaml --rules rules.yaml

# Or use make
make run
```

## Configuration

### config.yaml

```yaml
loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'
  poll_interval: 30s
  lookback: 5m

kubernetes:
  in_cluster: true

web:
  listen: ":8080"

remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
    - monitoring

rules_file: /etc/kube-sentinel/rules.yaml

store:
  type: memory
```

### rules.yaml

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
      pattern: "OOMKilled|Out of memory"
    priority: P1
    remediation:
      action: none  # Alert only, can't auto-fix
      cooldown: 10m
    enabled: true

  - name: image-pull-error
    match:
      pattern: "ImagePullBackOff|ErrImagePull"
    priority: P2
    remediation:
      action: none
      cooldown: 5m
    enabled: true
```

## Remediation Actions

| Action | Description |
|--------|-------------|
| `restart-pod` | Delete pod to trigger restart via controller |
| `scale-up` | Increase deployment replicas |
| `scale-down` | Decrease deployment replicas |
| `rollback` | Rollback deployment to previous revision |
| `delete-stuck-pods` | Force delete pods stuck in Terminating |
| `none` | Alert only, no action |

## Priority Levels

| Priority | Label | Description |
|----------|-------|-------------|
| P1 | Critical | Immediate attention required |
| P2 | High | Important, should be addressed soon |
| P3 | Medium | Should be investigated |
| P4 | Low | Informational, low urgency |

## Safety Features

- **Cooldown Periods**: Prevent action spam on the same target
- **Hourly Rate Limit**: Maximum actions per hour (default: 50)
- **Namespace Exclusions**: Protect critical namespaces (kube-system, etc.)
- **Dry Run Mode**: Test without executing actions
- **Audit Log**: Full history of all remediation attempts

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Dashboard |
| `/errors` | GET | Error list with filtering |
| `/errors/{id}` | GET | Error detail |
| `/rules` | GET | Rule configuration |
| `/history` | GET | Remediation history |
| `/settings` | GET | Settings page |
| `/api/errors` | GET | JSON error list |
| `/api/stats` | GET | Statistics |
| `/api/settings` | GET/POST | Get/update settings |
| `/ws` | WS | WebSocket for real-time updates |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |

## Development

```bash
# Install dependencies
make deps

# Run tests
make test

# Run linter
make lint

# Build for all platforms
make build-all

# Build Docker image
make docker

# Deploy to Kubernetes
make deploy
```

## RBAC Requirements

Kube Sentinel requires the following Kubernetes permissions:

```yaml
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: [""]
    resources: ["events", "namespaces"]
    verbs: ["get", "list", "watch"]
```

## License

MIT License - see [LICENSE](LICENSE) for details.
