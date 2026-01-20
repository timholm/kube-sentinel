# Deployment Guide

This document provides comprehensive documentation for deploying Kube Sentinel to Kubernetes clusters. It covers the Docker build process, Makefile automation, Kubernetes manifests, RBAC requirements, runtime configuration, and future improvement opportunities.

## Table of Contents

1. [Docker Image](#docker-image)
2. [Makefile Automation](#makefile-automation)
3. [Kubernetes Manifests](#kubernetes-manifests)
4. [RBAC Requirements](#rbac-requirements)
5. [Runtime Configuration](#runtime-configuration)
6. [Future Improvements](#future-improvements)

---

## Docker Image

The Kube Sentinel container image is built using a multi-stage Dockerfile that prioritizes security, minimal image size, and reproducible builds.

### Multi-Stage Build Architecture

**File:** `/Dockerfile`

The Dockerfile uses a two-stage build process:

#### Stage 1: Builder

```dockerfile
FROM golang:1.22-alpine AS builder
```

The builder stage:

1. **Base Image**: Uses `golang:1.22-alpine` for a minimal Go build environment
2. **Build Dependencies**: Installs `git` (for version info), `ca-certificates` (for HTTPS), and `tzdata` (for timezone support)
3. **Dependency Caching**: Copies `go.mod` and `go.sum` first, then runs `go mod download` to leverage Docker layer caching
4. **Static Binary**: Compiles with `CGO_ENABLED=0` to produce a fully static binary that requires no C libraries
5. **Build Arguments**: Accepts `VERSION`, `COMMIT`, and `BUILD_DATE` for embedding version information via `-ldflags`
6. **Binary Optimization**: Uses `-s -w` flags to strip debug symbols and reduce binary size

#### Stage 2: Runtime

```dockerfile
FROM alpine:3.19
```

The runtime stage:

1. **Minimal Base**: Uses `alpine:3.19` (approximately 5MB) as the runtime base
2. **Runtime Dependencies**: Only includes `ca-certificates` and `tzdata`
3. **Non-Root User**: Creates and uses a dedicated `sentinel` user (UID/GID 1000)
4. **Binary Copy**: Copies only the compiled binary from the builder stage
5. **Default Configuration**: Includes default `config.yaml` and `rules.yaml` files

### Security Practices

The Dockerfile implements several security best practices:

| Practice | Implementation |
|----------|----------------|
| Non-root execution | Dedicated `sentinel` user with UID 1000 |
| Minimal attack surface | Alpine base with only required packages |
| No build tools in runtime | Multi-stage build separates build and runtime |
| Static binary | CGO disabled, no dynamic library dependencies |
| Health checking | Built-in `HEALTHCHECK` instruction |
| Read-only config | Configuration mounted read-only at runtime |

### Health Check Configuration

The Dockerfile includes a built-in health check:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1
```

- **Interval**: Checks every 30 seconds
- **Timeout**: Fails if check takes longer than 5 seconds
- **Start Period**: Allows 10 seconds for startup before checking
- **Retries**: Marks unhealthy after 3 consecutive failures

### Build Arguments

| Argument | Default | Purpose |
|----------|---------|---------|
| `VERSION` | `dev` | Semantic version for the build |
| `COMMIT` | `none` | Git commit SHA |
| `BUILD_DATE` | `unknown` | ISO 8601 build timestamp |

---

## Makefile Automation

The Makefile provides comprehensive automation for building, testing, and deploying Kube Sentinel.

**File:** `/Makefile`

### Variable Configuration

The Makefile automatically derives version information from Git:

```makefile
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT ?= $(shell git rev-parse --short HEAD)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
```

Configurable variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `VERSION` | Git tag/commit | Application version |
| `DOCKER_REGISTRY` | `ghcr.io/kube-sentinel` | Container registry |
| `DOCKER_TAG` | `$(VERSION)` | Image tag |

### Available Targets

#### Build Targets

| Target | Command | Description |
|--------|---------|-------------|
| `build` | `make build` | Build binary for current platform |
| `build-linux` | `make build-linux` | Build for Linux (amd64 + arm64) |
| `build-darwin` | `make build-darwin` | Build for macOS (amd64 + arm64) |
| `build-windows` | `make build-windows` | Build for Windows (amd64) |
| `build-all` | `make build-all` | Build for all platforms |

All binaries are placed in `./build/bin/` with platform-specific suffixes.

#### Development Targets

| Target | Command | Description |
|--------|---------|-------------|
| `run` | `make run` | Build and run locally with debug logging |
| `dev` | `make dev` | Run with hot reload using `air` |

#### Testing Targets

| Target | Command | Description |
|--------|---------|-------------|
| `test` | `make test` | Run tests with race detection and coverage |
| `test-coverage` | `make test-coverage` | Generate HTML coverage report |

#### Code Quality Targets

| Target | Command | Description |
|--------|---------|-------------|
| `lint` | `make lint` | Run golangci-lint |
| `fmt` | `make fmt` | Format code with gofmt and goimports |
| `vet` | `make vet` | Run go vet |
| `check` | `make check` | Run all quality checks (fmt, vet, lint, test) |

#### Docker Targets

| Target | Command | Description |
|--------|---------|-------------|
| `docker` | `make docker` | Build Docker image |
| `docker-push` | `make docker-push` | Build and push to registry |
| `docker-buildx` | `make docker-buildx` | Build multi-arch image (amd64 + arm64) |

#### Kubernetes Targets

| Target | Command | Description |
|--------|---------|-------------|
| `deploy` | `make deploy` | Deploy to Kubernetes using Kustomize |
| `undeploy` | `make undeploy` | Remove from Kubernetes |
| `logs` | `make logs` | Stream pod logs |
| `port-forward` | `make port-forward` | Forward port 8080 to access dashboard |

#### Utility Targets

| Target | Command | Description |
|--------|---------|-------------|
| `deps` | `make deps` | Download Go dependencies |
| `deps-update` | `make deps-update` | Update dependencies to latest versions |
| `clean` | `make clean` | Remove build artifacts |
| `help` | `make help` | Display all available targets |

### Typical Workflows

**Local Development:**

```bash
make dev          # Start with hot reload
```

**Pre-commit Checks:**

```bash
make check        # Run all quality checks
```

**Release Build:**

```bash
make docker-buildx  # Build and push multi-arch image
make deploy         # Deploy to Kubernetes
```

---

## Kubernetes Manifests

Kube Sentinel uses Kustomize for Kubernetes deployment, providing a declarative and composable approach to manifest management.

**Directory:** `/deploy/kubernetes/`

### Manifest Overview

| File | Resource Type | Purpose |
|------|---------------|---------|
| `namespace.yaml` | Namespace | Isolated namespace for Kube Sentinel |
| `rbac.yaml` | ServiceAccount, ClusterRole, ClusterRoleBinding | Service identity and permissions |
| `configmap.yaml` | ConfigMap | Application and rules configuration |
| `deployment.yaml` | Deployment | Pod specification and lifecycle |
| `service.yaml` | Service, (Ingress) | Network access to the dashboard |
| `kustomization.yaml` | Kustomization | Manifest composition and customization |

### Namespace

**File:** `namespace.yaml`

Creates an isolated namespace for all Kube Sentinel resources:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
```

Using a dedicated namespace provides:
- Resource isolation from other workloads
- Simplified RBAC management
- Easy cleanup via namespace deletion
- Clear resource organization

### Deployment

**File:** `deployment.yaml`

The Deployment manages the Kube Sentinel pod lifecycle with the following features:

#### Pod Security Context

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  fsGroup: 1000
```

- Enforces non-root execution at the pod level
- Matches the container user (UID 1000) created in the Dockerfile

#### Container Security Context

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

- Prevents privilege escalation attacks
- Enforces immutable container filesystem
- Drops all Linux capabilities

#### Resource Management

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

- Guarantees minimum resources (100m CPU, 128Mi memory)
- Prevents resource exhaustion with limits

#### Health Probes

**Liveness Probe:**
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 10
  periodSeconds: 30
```

Kubernetes restarts the container if it becomes unresponsive.

**Readiness Probe:**
```yaml
readinessProbe:
  httpGet:
    path: /ready
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10
```

Traffic is only routed when the application is ready to serve requests.

#### Prometheus Integration

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

Enables automatic metric collection by Prometheus using standard annotations.

#### Scheduling

```yaml
nodeSelector:
  kubernetes.io/os: linux
tolerations:
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
```

- Ensures scheduling only on Linux nodes
- Allows scheduling on control-plane nodes if needed

### Service

**File:** `service.yaml`

Provides internal cluster access to the Kube Sentinel dashboard:

```yaml
apiVersion: v1
kind: Service
spec:
  type: ClusterIP
  ports:
    - port: 80
      targetPort: http
```

- **ClusterIP**: Internal-only access (default)
- Maps external port 80 to container port 8080

The file also includes a commented Ingress template for external access via an ingress controller.

### Kustomization

**File:** `kustomization.yaml`

Orchestrates all manifests and provides customization:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: kube-sentinel

resources:
  - namespace.yaml
  - rbac.yaml
  - configmap.yaml
  - deployment.yaml
  - service.yaml

commonLabels:
  app.kubernetes.io/name: kube-sentinel
  app.kubernetes.io/version: "1.0.0"
  app.kubernetes.io/managed-by: kustomize

images:
  - name: ghcr.io/kube-sentinel/kube-sentinel
    newTag: latest
```

Features:
- **Resource ordering**: Lists manifests in dependency order
- **Common labels**: Applies standard Kubernetes labels to all resources
- **Image management**: Centralizes image tag configuration

---

## RBAC Requirements

Kube Sentinel requires cluster-wide permissions to monitor and remediate workloads across all namespaces.

**File:** `rbac.yaml`

### ServiceAccount

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
```

The ServiceAccount provides the pod with a Kubernetes identity for API authentication.

### ClusterRole Permissions

The ClusterRole grants specific permissions required for Kube Sentinel's functionality:

#### Pod Operations

```yaml
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch", "delete"]
```

| Verb | Purpose |
|------|---------|
| `get` | Retrieve pod details for context enrichment |
| `list` | Enumerate pods across namespaces |
| `watch` | Receive real-time pod state changes |
| `delete` | Execute `restart-pod` remediation action |

**Why delete?** The `restart-pod` remediation action works by deleting a pod, allowing its controller (Deployment, StatefulSet) to recreate it with a fresh state.

#### Deployment Operations

```yaml
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "update", "patch"]
```

| Verb | Purpose |
|------|---------|
| `get` | Retrieve deployment configuration |
| `list` | Enumerate deployments across namespaces |
| `watch` | Receive real-time deployment changes |
| `update` | Execute `scale-up`/`scale-down` actions |
| `patch` | Execute `rollback` action via patch operations |

**Why update/patch?** Remediation actions like `scale-up`, `scale-down`, and `rollback-deployment` modify deployment specifications.

#### ReplicaSet Operations

```yaml
- apiGroups: ["apps"]
  resources: ["replicasets"]
  verbs: ["get", "list", "watch"]
```

| Verb | Purpose |
|------|---------|
| `get` | Retrieve ReplicaSet details |
| `list` | Enumerate ReplicaSets |
| `watch` | Track ReplicaSet changes |

**Why read-only?** ReplicaSets are used to trace pod ownership back to their parent Deployment. Kube Sentinel extracts the pod's owner reference chain to identify which Deployment to remediate.

#### Event Operations

```yaml
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "list", "watch"]
```

| Verb | Purpose |
|------|---------|
| `get` | Retrieve specific events |
| `list` | Query events for context |
| `watch` | Monitor cluster events |

**Why needed?** Kubernetes events provide additional context about pod failures, scheduling issues, and other cluster activities that complement log-based error detection.

#### Namespace Operations

```yaml
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list", "watch"]
```

| Verb | Purpose |
|------|---------|
| `get` | Retrieve namespace details |
| `list` | Enumerate available namespaces |
| `watch` | Track namespace changes |

**Why needed?** Namespace enumeration is required to display namespace filters in the dashboard and to apply exclusion rules.

### ClusterRoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-sentinel
subjects:
  - kind: ServiceAccount
    name: kube-sentinel
    namespace: kube-sentinel
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-sentinel
```

Binds the ClusterRole to the ServiceAccount, granting cluster-wide permissions.

### Security Considerations

1. **Principle of Least Privilege**: Only the minimum required permissions are granted
2. **No secrets access**: The role cannot read Secrets or ConfigMaps in other namespaces
3. **No cluster admin**: Cannot modify RBAC, nodes, or other cluster-level resources
4. **Auditable actions**: All API calls are logged in the Kubernetes audit log

---

## Runtime Configuration

Kube Sentinel receives its configuration at runtime through Kubernetes ConfigMaps mounted as files.

**File:** `configmap.yaml`

### ConfigMap Structure

The ConfigMap contains two configuration files:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-sentinel-config
data:
  config.yaml: |
    # Application configuration
  rules.yaml: |
    # Rule definitions
```

### Application Configuration (`config.yaml`)

#### Loki Integration

```yaml
loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'
  poll_interval: 30s
  lookback: 5m
```

| Parameter | Description |
|-----------|-------------|
| `url` | Loki server endpoint (internal cluster DNS) |
| `query` | LogQL query for error detection |
| `poll_interval` | How often to query Loki |
| `lookback` | Time window for each query |

#### Kubernetes Client

```yaml
kubernetes:
  in_cluster: true
```

Uses in-cluster configuration with the mounted ServiceAccount token.

#### Web Dashboard

```yaml
web:
  listen: ":8080"
```

Binds the HTTP server to port 8080 on all interfaces.

#### Remediation Settings

```yaml
remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
    - logging
    - kube-sentinel
```

| Parameter | Description |
|-----------|-------------|
| `enabled` | Master switch for remediation |
| `dry_run` | Log actions without executing |
| `max_actions_per_hour` | Rate limiting for safety |
| `excluded_namespaces` | Namespaces exempt from remediation |

#### Storage Backend

```yaml
store:
  type: memory
```

Uses in-memory storage for error state (resets on restart).

### Rules Configuration (`rules.yaml`)

Rules define error patterns and their remediation actions:

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
```

Each rule specifies:

| Field | Description |
|-------|-------------|
| `name` | Unique identifier for the rule |
| `match.pattern` | Regex pattern to match in logs |
| `priority` | Severity level (P1-P4) |
| `remediation.action` | Action to take (`restart-pod`, `scale-up`, `scale-down`, `rollback-deployment`, `none`) |
| `remediation.cooldown` | Minimum time between repeated actions |
| `enabled` | Whether the rule is active |

### Volume Mounting

The Deployment mounts the ConfigMap as a volume:

```yaml
volumeMounts:
  - name: config
    mountPath: /etc/kube-sentinel
    readOnly: true
volumes:
  - name: config
    configMap:
      name: kube-sentinel-config
```

Configuration files appear at:
- `/etc/kube-sentinel/config.yaml`
- `/etc/kube-sentinel/rules.yaml`

### Updating Configuration

To update configuration:

1. Edit the ConfigMap or apply a new one:
   ```bash
   kubectl apply -f configmap.yaml
   ```

2. Restart the pod to pick up changes:
   ```bash
   kubectl rollout restart deployment/kube-sentinel -n kube-sentinel
   ```

---

## Future Improvements

This section outlines potential enhancements for production deployments and enterprise use cases.

### Helm Chart

A Helm chart would provide:

- **Templated values**: Parameterize all configuration options
- **Environment overlays**: Development, staging, production presets
- **Dependency management**: Optional Loki/Prometheus sub-charts
- **Release management**: Versioned releases with rollback support
- **Repository hosting**: Distribute via Helm repository

Example `values.yaml` structure:

```yaml
replicaCount: 1

image:
  repository: ghcr.io/kube-sentinel/kube-sentinel
  tag: latest
  pullPolicy: IfNotPresent

loki:
  url: http://loki:3100
  pollInterval: 30s

remediation:
  enabled: true
  dryRun: false
  maxActionsPerHour: 50

resources:
  requests:
    cpu: 100m
    memory: 128Mi

ingress:
  enabled: false
  className: nginx
  hosts:
    - host: sentinel.example.com
```

### GitOps Workflows

Integration with GitOps tools like Flux or Argo CD:

**Flux Integration:**

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: kube-sentinel
  namespace: flux-system
spec:
  interval: 10m
  sourceRef:
    kind: GitRepository
    name: infrastructure
  path: ./clusters/production/kube-sentinel
  prune: true
  healthChecks:
    - apiVersion: apps/v1
      kind: Deployment
      name: kube-sentinel
      namespace: kube-sentinel
```

**Argo CD Integration:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kube-sentinel
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/org/kube-sentinel
    targetRevision: HEAD
    path: deploy/kubernetes
  destination:
    server: https://kubernetes.default.svc
    namespace: kube-sentinel
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### Multi-Cluster Support

Enhancements for managing multiple Kubernetes clusters:

1. **Centralized Dashboard**: Single pane of glass across clusters
2. **Federated Configuration**: Shared rules with per-cluster overrides
3. **Cross-Cluster Correlation**: Identify related errors across environments
4. **Cluster-Specific RBAC**: Separate credentials per cluster

Architecture options:

| Pattern | Description |
|---------|-------------|
| Hub-and-Spoke | Central controller with remote agents |
| Federated | Independent instances with shared backend |
| Multi-Context | Single instance with multiple kubeconfigs |

### High Availability

For production deployments requiring high availability:

1. **Multiple Replicas**: Run 2-3 replicas with leader election
2. **Persistent Storage**: Redis or PostgreSQL backend for state
3. **Pod Disruption Budget**: Ensure minimum availability during updates

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: kube-sentinel
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
```

### Additional Features

| Feature | Description |
|---------|-------------|
| **Alertmanager Integration** | Send alerts for high-priority errors |
| **Slack/PagerDuty Notifications** | Real-time incident notifications |
| **Custom Remediation Scripts** | Webhook-based custom actions |
| **Audit Logging** | Detailed remediation action audit trail |
| **OIDC Authentication** | Secure dashboard with SSO |
| **Network Policies** | Restrict pod communication |
| **Service Mesh Integration** | Istio/Linkerd observability |

### Security Hardening

Additional security measures for enterprise deployments:

1. **Pod Security Standards**: Apply `restricted` policy
2. **Image Signing**: Sign and verify container images with Sigstore
3. **Runtime Security**: Falco rules for anomaly detection
4. **Secret Management**: External secrets operator for credentials
5. **Audit Policies**: Kubernetes audit logging for compliance

```yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: kube-sentinel-restricted
spec:
  privileged: false
  runAsUser:
    rule: MustRunAsNonRoot
  seLinux:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
  volumes:
    - configMap
    - emptyDir
    - secret
```

---

## Quick Reference

### Deployment Commands

```bash
# Deploy to Kubernetes
make deploy

# View logs
make logs

# Access dashboard locally
make port-forward

# Remove from cluster
make undeploy
```

### Docker Commands

```bash
# Build image
make docker

# Build and push
make docker-push

# Build multi-arch
make docker-buildx
```

### Verification

```bash
# Check deployment status
kubectl get all -n kube-sentinel

# Check pod logs
kubectl logs -n kube-sentinel -l app.kubernetes.io/name=kube-sentinel

# Test health endpoint
kubectl port-forward -n kube-sentinel svc/kube-sentinel 8080:80 &
curl http://localhost:8080/health
```
