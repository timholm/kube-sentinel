# Kube Sentinel Deployment and Scaling Guide

## Technical Whitepaper v1.0

---

## Executive Summary

This document provides comprehensive guidance for deploying Kube Sentinel in Kubernetes environments, from single-cluster installations to multi-cluster enterprise deployments. It covers manifest configurations, container image architecture, resource optimization, high availability patterns, and upgrade strategies.

Kube Sentinel is designed for production-grade deployments with security-first defaults, including non-root execution, read-only filesystems, and minimal RBAC permissions.

---

## 1. Kubernetes Manifest Walkthrough

### 1.1 Resource Overview

Kube Sentinel deployment consists of six core Kubernetes resources:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     Kube Sentinel Resource Stack                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────────────┐  │
│  │  Namespace  │  │    RBAC     │  │          ConfigMap               │  │
│  │             │  │             │  │                                  │  │
│  │ kube-       │  │ Service     │  │ - config.yaml                   │  │
│  │ sentinel    │  │ Account +   │  │ - rules.yaml                    │  │
│  │             │  │ ClusterRole │  │                                  │  │
│  │             │  │ + Binding   │  │                                  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────────────┘  │
│                                                                          │
│  ┌─────────────────────────────────┐  ┌─────────────────────────────┐  │
│  │           Deployment            │  │          Service             │  │
│  │                                 │  │                              │  │
│  │  - Pod spec                     │  │  - ClusterIP                 │  │
│  │  - Security context             │  │  - Port 80 → 8080            │  │
│  │  - Health probes                │  │                              │  │
│  │  - Resource limits              │  │                              │  │
│  └─────────────────────────────────┘  └─────────────────────────────┘  │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │                         HTTPRoute                                    ││
│  │                                                                      ││
│  │  Gateway API routing for external access                            ││
│  └─────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Namespace Resource

The namespace provides isolation and organization for all Kube Sentinel resources:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
```

**Key Points:**
- Dedicated namespace prevents resource conflicts
- Standard Kubernetes labeling for identification
- Enables namespace-scoped resource quotas and network policies

### 1.3 RBAC Configuration

Kube Sentinel requires specific permissions to monitor and remediate cluster resources. The RBAC configuration follows the principle of least privilege:

#### ServiceAccount

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
```

#### ClusterRole

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

**Permission Breakdown:**

| Resource | Verbs | Purpose |
|----------|-------|---------|
| `pods` | get, list, watch, delete | Monitor pods, execute restart-pod action |
| `deployments` | get, list, watch, update, patch | Scale operations, rollback actions |
| `replicasets` | get, list, watch | Discover deployment ownership chain |
| `events` | get, list, watch | Provide additional error context |
| `namespaces` | get, list, watch | List available namespaces for filtering |

**Security Notes:**
- No `create` permissions: Cannot create new resources
- No `secrets` access: Cannot read sensitive data
- Scoped to workload resources only
- ClusterRole required for cross-namespace monitoring

#### ClusterRoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
subjects:
  - kind: ServiceAccount
    name: kube-sentinel
    namespace: kube-sentinel
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-sentinel
```

### 1.4 ConfigMap

The ConfigMap contains both the main configuration and remediation rules:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-sentinel-config
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
data:
  config.yaml: |
    loki:
      url: http://loki.monitoring:3100
      query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'
      poll_interval: 30s
      lookback: 5m

    kubernetes:
      in_cluster: true

    web:
      listen: ":8080"
      base_path: /kube-sentinel

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

    rules_file: /etc/kube-sentinel/rules.yaml

    store:
      type: memory

  rules.yaml: |
    rules:
      - name: crashloop-backoff
        match:
          pattern: "CrashLoopBackOff|Back-off restarting failed container"
        priority: P1
        remediation:
          action: restart-pod
          cooldown: 5m
        enabled: true
      # ... additional rules
```

**Configuration Sections:**

| Section | Description |
|---------|-------------|
| `loki` | Loki server connection and query parameters |
| `kubernetes` | Kubernetes client configuration |
| `web` | Web server settings including base path |
| `remediation` | Auto-remediation engine settings |
| `store` | Error storage backend configuration |

### 1.5 Deployment

The Deployment resource defines the pod specification with security and operational best practices:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
    app.kubernetes.io/component: server
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kube-sentinel
        app.kubernetes.io/component: server
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: kube-sentinel
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
      containers:
        - name: kube-sentinel
          image: ghcr.io/timholm/kube-sentinel:latest
          imagePullPolicy: Always
          args:
            - --config=/etc/kube-sentinel/config.yaml
            - --log-level=info
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 10
            periodSeconds: 30
            timeoutSeconds: 5
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /ready
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
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
      nodeSelector:
        kubernetes.io/os: linux
      tolerations:
        - key: node-role.kubernetes.io/control-plane
          operator: Exists
          effect: NoSchedule
        - key: node.cilium.io/agent-not-ready
          operator: Exists
          effect: NoSchedule
```

### 1.6 Service

The Service provides stable network access to the Kube Sentinel pods:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
spec:
  type: ClusterIP
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app.kubernetes.io/name: kube-sentinel
```

**Service Characteristics:**
- ClusterIP type for internal cluster access
- Port 80 maps to container port 8080
- Named port (`http`) for HTTPRoute compatibility

### 1.7 HTTPRoute

The HTTPRoute resource provides Gateway API-based external access:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  parentRefs:
    - name: main-gateway
      namespace: envoy-gateway-system
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /kube-sentinel
      filters:
        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /
      backendRefs:
        - name: kube-sentinel
          port: 80
```

**HTTPRoute Features:**
- Path-based routing under `/kube-sentinel` prefix
- URL rewriting strips prefix before forwarding to backend
- Gateway API v1 specification compliant
- Compatible with Envoy Gateway, Istio, Contour, and other Gateway API implementations

---

## 2. Kustomize Overlay Structure

### 2.1 Base Kustomization

The base `kustomization.yaml` provides the foundation for all deployments:

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
  - httproute.yaml

commonLabels:
  app.kubernetes.io/name: kube-sentinel
  app.kubernetes.io/version: "1.0.0"
  app.kubernetes.io/managed-by: kustomize

images:
  - name: ghcr.io/timholm/kube-sentinel
    newTag: latest
```

### 2.2 Recommended Overlay Structure

For production deployments, implement environment-specific overlays:

```
deploy/
├── kubernetes/
│   ├── base/
│   │   ├── kustomization.yaml
│   │   ├── namespace.yaml
│   │   ├── rbac.yaml
│   │   ├── configmap.yaml
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── httproute.yaml
│   └── overlays/
│       ├── development/
│       │   ├── kustomization.yaml
│       │   ├── configmap-patch.yaml
│       │   └── deployment-patch.yaml
│       ├── staging/
│       │   ├── kustomization.yaml
│       │   ├── configmap-patch.yaml
│       │   └── deployment-patch.yaml
│       └── production/
│           ├── kustomization.yaml
│           ├── configmap-patch.yaml
│           ├── deployment-patch.yaml
│           └── hpa.yaml
```

### 2.3 Development Overlay Example

```yaml
# overlays/development/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: kube-sentinel-dev

resources:
  - ../../base

nameSuffix: -dev

patches:
  - path: configmap-patch.yaml
  - path: deployment-patch.yaml

images:
  - name: ghcr.io/timholm/kube-sentinel
    newTag: dev-latest
```

```yaml
# overlays/development/configmap-patch.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-sentinel-config
data:
  config.yaml: |
    loki:
      url: http://loki.monitoring:3100
      poll_interval: 10s  # Faster polling for development
      lookback: 2m
    remediation:
      enabled: true
      dry_run: true  # Dry run in development
      max_actions_per_hour: 100
```

```yaml
# overlays/development/deployment-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel
spec:
  template:
    spec:
      containers:
        - name: kube-sentinel
          args:
            - --config=/etc/kube-sentinel/config.yaml
            - --log-level=debug  # Debug logging
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 256Mi
```

### 2.4 Production Overlay Example

```yaml
# overlays/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: kube-sentinel

resources:
  - ../../base
  - hpa.yaml
  - pdb.yaml

patches:
  - path: configmap-patch.yaml
  - path: deployment-patch.yaml

images:
  - name: ghcr.io/timholm/kube-sentinel
    newTag: v1.0.0  # Pinned version

replicas:
  - name: kube-sentinel
    count: 3  # High availability
```

```yaml
# overlays/production/hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: kube-sentinel
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

```yaml
# overlays/production/pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
```

### 2.5 Deployment Commands

```bash
# Deploy base configuration
kubectl apply -k deploy/kubernetes/

# Deploy specific overlay
kubectl apply -k deploy/kubernetes/overlays/production/

# Preview rendered manifests
kubectl kustomize deploy/kubernetes/overlays/production/

# Diff against current state
kubectl diff -k deploy/kubernetes/overlays/production/
```

---

## 3. Container Image Details

### 3.1 Multi-Stage Dockerfile

The Kube Sentinel container image uses a multi-stage build for security and size optimization:

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o kube-sentinel ./cmd/kube-sentinel

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS and tzdata for timezone support
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 sentinel && \
    adduser -u 1000 -G sentinel -s /bin/sh -D sentinel

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/kube-sentinel /app/kube-sentinel

# Copy default config and rules
COPY config.yaml /etc/kube-sentinel/config.yaml
COPY rules.yaml /etc/kube-sentinel/rules.yaml

# Set ownership
RUN chown -R sentinel:sentinel /app /etc/kube-sentinel

# Switch to non-root user
USER sentinel

# Expose web dashboard port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["/app/kube-sentinel"]
CMD ["--config", "/etc/kube-sentinel/config.yaml"]
```

### 3.2 Image Security Features

| Feature | Implementation | Benefit |
|---------|----------------|---------|
| Multi-stage build | Separate builder and runtime stages | Minimal attack surface, smaller image |
| Alpine base | `alpine:3.19` runtime | Small footprint (~5MB base) |
| Non-root user | UID/GID 1000 (`sentinel`) | Reduced privilege escalation risk |
| Static binary | `CGO_ENABLED=0` | No runtime dependencies |
| Stripped binary | `-ldflags "-s -w"` | Smaller binary size |
| Built-in health check | Docker HEALTHCHECK | Container orchestration compatibility |

### 3.3 Multi-Architecture Builds

For multi-architecture support, use Docker Buildx:

```bash
# Create builder instance
docker buildx create --name multiarch --use

# Build and push multi-arch image
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=v1.0.0 \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --tag ghcr.io/timholm/kube-sentinel:v1.0.0 \
  --tag ghcr.io/timholm/kube-sentinel:latest \
  --push \
  .
```

**Supported Architectures:**
- `linux/amd64` - Standard x86-64 servers
- `linux/arm64` - AWS Graviton, Apple Silicon, ARM servers

### 3.4 Image Versioning Strategy

| Tag Pattern | Description | Use Case |
|-------------|-------------|----------|
| `latest` | Most recent build | Development only |
| `v1.0.0` | Semantic version | Production releases |
| `v1.0` | Minor version | Auto-update within minor |
| `sha-abc123` | Git commit SHA | Precise version tracking |
| `dev-latest` | Development branch | Testing pre-release |

---

## 4. Resource Sizing Recommendations

### 4.1 Default Resource Configuration

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

### 4.2 Sizing Guidelines

The resource requirements scale primarily with:
1. **Number of namespaces monitored**
2. **Log volume from Loki**
3. **Error rate and deduplication complexity**
4. **Remediation action frequency**

**Recommended Sizing Tiers:**

| Cluster Size | Namespaces | Errors/Hour | CPU Request | Memory Request | CPU Limit | Memory Limit |
|--------------|------------|-------------|-------------|----------------|-----------|--------------|
| Small | 1-10 | < 100 | 50m | 64Mi | 200m | 256Mi |
| Medium | 10-50 | 100-500 | 100m | 128Mi | 500m | 512Mi |
| Large | 50-100 | 500-2000 | 200m | 256Mi | 1000m | 1Gi |
| Enterprise | 100+ | 2000+ | 500m | 512Mi | 2000m | 2Gi |

### 4.3 Memory Considerations

Memory usage is dominated by:

```
┌────────────────────────────────────────────────────────────────────┐
│                     Memory Allocation                               │
├────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Error Store (MemoryStore)                                   │   │
│  │  - Deduplicated errors (30-day retention)                   │   │
│  │  - Fingerprint index                                        │   │
│  │  - Estimated: ~1KB per unique error                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Remediation Logs                                           │   │
│  │  - Action audit trail (30-day retention)                    │   │
│  │  - Estimated: ~500B per log entry                           │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Rule Engine                                                 │   │
│  │  - Compiled regex patterns                                  │   │
│  │  - Cooldown state                                           │   │
│  │  - Estimated: ~10KB + 1KB per rule                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Go Runtime Overhead                                        │   │
│  │  - GC, goroutines, HTTP server                              │   │
│  │  - Estimated: ~30-50MB baseline                             │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└────────────────────────────────────────────────────────────────────┘
```

**Memory Estimation Formula:**
```
Required Memory = 50MB (baseline) + (unique_errors * 1KB) + (remediation_logs * 0.5KB) + (rules * 1KB)
```

### 4.4 CPU Considerations

CPU usage spikes during:
- Loki query execution
- Regex pattern matching
- Template rendering for dashboard
- Kubernetes API calls during remediation

**CPU Estimation:**
- Idle: < 10m (polling interval sleep)
- Query processing: 50-100m (depends on result size)
- Remediation execution: 20-50m per action

---

## 5. Health Checks

### 5.1 Liveness Probe

The liveness probe determines if the application is running and should be restarted if unhealthy:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 10
  periodSeconds: 30
  timeoutSeconds: 5
  failureThreshold: 3
```

**Probe Behavior:**
| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `initialDelaySeconds` | 10 | Allow startup initialization |
| `periodSeconds` | 30 | Balance responsiveness vs overhead |
| `timeoutSeconds` | 5 | Generous timeout for loaded systems |
| `failureThreshold` | 3 | Avoid restart on transient issues |

**Health Endpoint Implementation:**
```go
// /health returns 200 if the application is running
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}
```

### 5.2 Readiness Probe

The readiness probe determines if the pod should receive traffic:

```yaml
readinessProbe:
  httpGet:
    path: /ready
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

**Ready Endpoint Implementation:**
```go
// /ready returns 200 only when all dependencies are available
func readyHandler(w http.ResponseWriter, r *http.Request) {
    // Check Loki connectivity
    if !lokiClient.IsConnected() {
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte("Loki unavailable"))
        return
    }

    // Check Kubernetes API connectivity
    if !k8sClient.IsConnected() {
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte("Kubernetes API unavailable"))
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Ready"))
}
```

### 5.3 Startup Probe (Optional)

For environments with slow startup, add a startup probe:

```yaml
startupProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 0
  periodSeconds: 5
  timeoutSeconds: 5
  failureThreshold: 30  # Allow up to 150 seconds for startup
```

---

## 6. Security Context Configuration

### 6.1 Pod Security Context

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  fsGroup: 1000
```

| Setting | Value | Purpose |
|---------|-------|---------|
| `runAsNonRoot` | true | Enforce non-root execution |
| `runAsUser` | 1000 | Run as `sentinel` user (UID 1000) |
| `runAsGroup` | 1000 | Primary group for process |
| `fsGroup` | 1000 | Group ownership for mounted volumes |

### 6.2 Container Security Context

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

| Setting | Value | Purpose |
|---------|-------|---------|
| `allowPrivilegeEscalation` | false | Prevent privilege escalation attacks |
| `readOnlyRootFilesystem` | true | Immutable container filesystem |
| `capabilities.drop` | ALL | Remove all Linux capabilities |

### 6.3 Volume Mounts for Read-Only Filesystem

With `readOnlyRootFilesystem: true`, writable paths require explicit volumes:

```yaml
volumeMounts:
  - name: config
    mountPath: /etc/kube-sentinel
    readOnly: true  # Configuration is read-only
  - name: tmp
    mountPath: /tmp  # Writable temp directory

volumes:
  - name: config
    configMap:
      name: kube-sentinel-config
  - name: tmp
    emptyDir: {}  # Ephemeral writable storage
```

### 6.4 Pod Security Standards Compliance

Kube Sentinel is compatible with Kubernetes Pod Security Standards:

| Standard | Level | Compatible |
|----------|-------|------------|
| Pod Security Standards | Restricted | Yes |
| Pod Security Admission | enforce: restricted | Yes |
| OPA Gatekeeper | Container security policies | Yes |

**Pod Security Admission Label:**
```yaml
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

## 7. HTTPRoute/Ingress Configuration

### 7.1 Gateway API HTTPRoute

Modern Kubernetes deployments should use the Gateway API for external access:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  parentRefs:
    - name: main-gateway
      namespace: envoy-gateway-system
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /kube-sentinel
      filters:
        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /
      backendRefs:
        - name: kube-sentinel
          port: 80
```

**HTTPRoute Configuration Details:**

| Field | Value | Description |
|-------|-------|-------------|
| `parentRefs.name` | main-gateway | Reference to Gateway resource |
| `parentRefs.namespace` | envoy-gateway-system | Gateway's namespace |
| `matches.path.type` | PathPrefix | Match path prefix |
| `matches.path.value` | /kube-sentinel | URL path prefix |
| `filters.urlRewrite` | ReplacePrefixMatch | Strip prefix before backend |
| `backendRefs.name` | kube-sentinel | Target Service name |

### 7.2 Traditional Ingress (Alternative)

For clusters without Gateway API, use Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/rewrite-target: /$2
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts:
        - sentinel.example.com
      secretName: kube-sentinel-tls
  rules:
    - host: sentinel.example.com
      http:
        paths:
          - path: /kube-sentinel(/|$)(.*)
            pathType: ImplementationSpecific
            backend:
              service:
                name: kube-sentinel
                port:
                  number: 80
```

### 7.3 Path-Based vs Host-Based Routing

**Path-Based Routing (Recommended for shared domains):**
```
https://ops.example.com/kube-sentinel/
https://ops.example.com/grafana/
https://ops.example.com/prometheus/
```

**Host-Based Routing (For dedicated subdomain):**
```
https://sentinel.example.com/
```

### 7.4 TLS Configuration

For production deployments, always enable TLS:

```yaml
# With cert-manager
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kube-sentinel
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  parentRefs:
    - name: main-gateway
      namespace: envoy-gateway-system
      sectionName: https  # Reference HTTPS listener
```

---

## 8. High Availability Considerations

### 8.1 HA Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     High Availability Deployment                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                        Load Balancer                              │  │
│  │                    (Service / Ingress)                            │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                              │                                          │
│            ┌─────────────────┼─────────────────┐                       │
│            │                 │                 │                        │
│            ▼                 ▼                 ▼                        │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐          │
│  │   Pod (Zone A)   │ │   Pod (Zone B)   │ │   Pod (Zone C)   │          │
│  │                  │ │                  │ │                  │          │
│  │ kube-sentinel    │ │ kube-sentinel    │ │ kube-sentinel    │          │
│  │ replica-0        │ │ replica-1        │ │ replica-2        │          │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘          │
│            │                 │                 │                        │
│            └─────────────────┼─────────────────┘                       │
│                              │                                          │
│                              ▼                                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                     Shared Resources                              │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │  │
│  │  │    Loki         │  │  Kubernetes API  │  │   ConfigMap     │  │  │
│  │  │  (external)     │  │                  │  │                 │  │  │
│  │  └─────────────────┘  └─────────────────┘  └─────────────────┘  │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 8.2 Replica Configuration

For production HA, deploy multiple replicas:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

### 8.3 Pod Anti-Affinity

Ensure pods are distributed across nodes and zones:

```yaml
spec:
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app.kubernetes.io/name
                      operator: In
                      values:
                        - kube-sentinel
                topologyKey: kubernetes.io/hostname
            - weight: 50
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app.kubernetes.io/name
                      operator: In
                      values:
                        - kube-sentinel
                topologyKey: topology.kubernetes.io/zone
```

### 8.4 Pod Disruption Budget

Maintain minimum availability during disruptions:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  minAvailable: 2  # Or use maxUnavailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
```

### 8.5 Horizontal Pod Autoscaler

Scale based on resource utilization:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: kube-sentinel
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Pods
          value: 1
          periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
        - type: Pods
          value: 2
          periodSeconds: 60
```

### 8.6 HA Considerations for Stateful Components

**Current Design (Memory Store):**
- Each replica maintains independent error state
- No cross-replica synchronization
- Suitable for stateless dashboard viewing

**For Shared State (Future Enhancement):**
- Use Redis or PostgreSQL backend for `store.type`
- Enables consistent view across replicas
- Required for leader election in remediation

**Leader Election for Remediation:**

When running multiple replicas with remediation enabled, implement leader election to prevent duplicate actions:

```yaml
# Future configuration option
remediation:
  leader_election:
    enabled: true
    lease_name: kube-sentinel-leader
    lease_namespace: kube-sentinel
```

---

## 9. Multi-Cluster Deployment Patterns

### 9.1 Deployment Models

```
┌─────────────────────────────────────────────────────────────────────────┐
│                  Multi-Cluster Deployment Models                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Model A: Per-Cluster Deployment                                         │
│  ─────────────────────────────                                          │
│                                                                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐         │
│  │  Cluster: Prod  │  │  Cluster: Stage │  │  Cluster: Dev   │         │
│  │  ┌───────────┐  │  │  ┌───────────┐  │  │  ┌───────────┐  │         │
│  │  │ Sentinel  │  │  │  │ Sentinel  │  │  │  │ Sentinel  │  │         │
│  │  │ + Loki    │  │  │  │ + Loki    │  │  │  │ + Loki    │  │         │
│  │  └───────────┘  │  │  └───────────┘  │  │  └───────────┘  │         │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘         │
│                                                                          │
│  Model B: Centralized Dashboard                                          │
│  ──────────────────────────────                                         │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                    Central Management Cluster                    │   │
│  │  ┌────────────────────────────────────────────────────────────┐ │   │
│  │  │               Kube Sentinel (Multi-Cluster View)            │ │   │
│  │  └────────────────────────────────────────────────────────────┘ │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                              │                                          │
│            ┌─────────────────┼─────────────────┐                       │
│            ▼                 ▼                 ▼                        │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐          │
│  │  Cluster: Prod  │ │  Cluster: Stage │ │  Cluster: Dev   │          │
│  │  ┌───────────┐  │ │  ┌───────────┐  │ │  ┌───────────┐  │          │
│  │  │   Loki    │  │ │  │   Loki    │  │ │  │   Loki    │  │          │
│  │  └───────────┘  │ │  └───────────┘  │ │  └───────────┘  │          │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘          │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Per-Cluster Deployment (Recommended)

Deploy Kube Sentinel to each cluster independently:

**Advantages:**
- Simple operational model
- No cross-cluster dependencies
- Remediation works without network connectivity to central cluster
- Scales naturally with cluster count

**Implementation:**
```bash
# For each cluster
kubectl config use-context cluster-prod
kubectl apply -k deploy/kubernetes/overlays/production/

kubectl config use-context cluster-staging
kubectl apply -k deploy/kubernetes/overlays/staging/
```

### 9.3 Centralized Dashboard Pattern

For unified visibility across clusters:

**Architecture Components:**
1. **Central Kube Sentinel**: Aggregates data from multiple clusters
2. **Loki Federation**: Multi-tenant or federated Loki deployment
3. **Cross-Cluster RBAC**: ServiceAccount tokens for remote clusters

**Configuration for Multi-Cluster Loki:**
```yaml
# config.yaml for centralized deployment
loki:
  url: http://loki-gateway.monitoring:3100
  query: '{cluster=~"prod|staging"} |~ "(?i)(error|fatal|panic)"'
  headers:
    X-Scope-OrgID: multi-tenant
```

### 9.4 GitOps Multi-Cluster Deployment

Using ArgoCD or Flux for multi-cluster deployment:

```yaml
# ArgoCD ApplicationSet
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: kube-sentinel
  namespace: argocd
spec:
  generators:
    - clusters:
        selector:
          matchLabels:
            kube-sentinel.io/enabled: "true"
  template:
    metadata:
      name: 'kube-sentinel-{{name}}'
    spec:
      project: platform
      source:
        repoURL: https://github.com/org/kube-sentinel-config
        targetRevision: main
        path: 'overlays/{{metadata.labels.environment}}'
      destination:
        server: '{{server}}'
        namespace: kube-sentinel
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
```

### 9.5 Cross-Cluster Remediation Considerations

When remediating across clusters from a central deployment:

1. **Kubeconfig Management**: Use separate kubeconfig files per cluster
2. **ServiceAccount Tokens**: Long-lived tokens for remote API access
3. **Network Connectivity**: Ensure API server reachability
4. **Latency**: Account for cross-cluster API call latency

```yaml
# Future multi-cluster configuration
kubernetes:
  clusters:
    - name: production
      kubeconfig: /etc/kube-sentinel/kubeconfig-prod
      context: prod-admin
    - name: staging
      kubeconfig: /etc/kube-sentinel/kubeconfig-staging
      context: staging-admin
```

---

## 10. Upgrade Strategies

### 10.1 Rolling Update (Default)

The default rolling update strategy ensures zero downtime:

```yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

**Upgrade Process:**
1. New pods are created with updated image
2. Readiness probe passes on new pods
3. Old pods receive termination signal
4. Traffic shifts to new pods
5. Old pods terminate after grace period

### 10.2 Blue-Green Deployment

For critical environments, use blue-green deployment:

```yaml
# Blue deployment (current)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel-blue
  labels:
    app.kubernetes.io/name: kube-sentinel
    app.kubernetes.io/version: v1.0.0
    deployment: blue

---
# Green deployment (new version)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel-green
  labels:
    app.kubernetes.io/name: kube-sentinel
    app.kubernetes.io/version: v1.1.0
    deployment: green
```

**Traffic Switch:**
```yaml
# Update Service selector to switch traffic
apiVersion: v1
kind: Service
metadata:
  name: kube-sentinel
spec:
  selector:
    app.kubernetes.io/name: kube-sentinel
    deployment: green  # Switch from blue to green
```

### 10.3 Canary Deployment

Gradually shift traffic to new version:

```yaml
# Using Flagger or Argo Rollouts
apiVersion: flagger.app/v1beta1
kind: Canary
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: kube-sentinel
  progressDeadlineSeconds: 60
  service:
    port: 80
    targetPort: 8080
  analysis:
    interval: 30s
    threshold: 5
    maxWeight: 50
    stepWeight: 10
    metrics:
      - name: request-success-rate
        thresholdRange:
          min: 99
        interval: 1m
      - name: request-duration
        thresholdRange:
          max: 500
        interval: 1m
```

### 10.4 Version Pinning

Always pin versions in production:

```yaml
# kustomization.yaml
images:
  - name: ghcr.io/timholm/kube-sentinel
    newTag: v1.0.0  # Pinned version, not 'latest'
    digest: sha256:abc123...  # Optional: pin by digest for immutability
```

### 10.5 Rollback Procedures

**Immediate Rollback:**
```bash
# Rollback to previous revision
kubectl rollout undo deployment/kube-sentinel -n kube-sentinel

# Rollback to specific revision
kubectl rollout undo deployment/kube-sentinel -n kube-sentinel --to-revision=3

# Check rollout history
kubectl rollout history deployment/kube-sentinel -n kube-sentinel
```

**Kustomize Rollback:**
```bash
# Revert to previous version in kustomization.yaml
cd deploy/kubernetes/overlays/production
git checkout HEAD~1 -- kustomization.yaml
kubectl apply -k .
```

### 10.6 Pre-Upgrade Checklist

Before upgrading Kube Sentinel:

| Step | Action | Command |
|------|--------|---------|
| 1 | Check current version | `kubectl get deployment kube-sentinel -n kube-sentinel -o jsonpath='{.spec.template.spec.containers[0].image}'` |
| 2 | Review changelog | Check release notes for breaking changes |
| 3 | Backup ConfigMap | `kubectl get configmap kube-sentinel-config -n kube-sentinel -o yaml > backup-config.yaml` |
| 4 | Test in staging | Deploy to staging environment first |
| 5 | Verify health | Check `/health` and `/ready` endpoints |
| 6 | Monitor metrics | Watch Prometheus metrics during rollout |

### 10.7 Post-Upgrade Verification

```bash
# Verify deployment status
kubectl rollout status deployment/kube-sentinel -n kube-sentinel

# Check pod health
kubectl get pods -n kube-sentinel -l app.kubernetes.io/name=kube-sentinel

# Verify new version
kubectl get deployment kube-sentinel -n kube-sentinel -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check logs for errors
kubectl logs -n kube-sentinel -l app.kubernetes.io/name=kube-sentinel --tail=100

# Test health endpoints
kubectl port-forward -n kube-sentinel svc/kube-sentinel 8080:80 &
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

---

## 11. Troubleshooting Guide

### 11.1 Common Issues

| Issue | Symptom | Solution |
|-------|---------|----------|
| Pod CrashLoopBackOff | Pod restarts repeatedly | Check logs: `kubectl logs -n kube-sentinel deployment/kube-sentinel` |
| Loki connection failed | "connection refused" in logs | Verify Loki URL and network policies |
| RBAC errors | "forbidden" errors in logs | Apply RBAC manifests: `kubectl apply -f rbac.yaml` |
| OOMKilled | Pod killed due to memory | Increase memory limits |
| Readiness probe failing | Pod not receiving traffic | Check `/ready` endpoint and dependencies |

### 11.2 Diagnostic Commands

```bash
# Pod status and events
kubectl describe pod -n kube-sentinel -l app.kubernetes.io/name=kube-sentinel

# Recent logs
kubectl logs -n kube-sentinel deployment/kube-sentinel --tail=200

# Check RBAC permissions
kubectl auth can-i list pods --as=system:serviceaccount:kube-sentinel:kube-sentinel

# Network connectivity to Loki
kubectl run -n kube-sentinel debug --rm -i --tty --image=busybox -- wget -O- http://loki.monitoring:3100/ready

# Resource usage
kubectl top pods -n kube-sentinel
```

---

## 12. Conclusion

This deployment guide covers the complete lifecycle of Kube Sentinel deployments, from initial installation to production-grade high availability configurations. Key takeaways:

1. **Security First**: Non-root execution, read-only filesystem, minimal RBAC
2. **Kustomize Overlays**: Environment-specific configurations with shared base
3. **Right-Sizing**: Resource allocation based on cluster scale
4. **High Availability**: Multi-replica deployment with anti-affinity and PDB
5. **Gateway API**: Modern HTTPRoute for external access
6. **Safe Upgrades**: Rolling updates with rollback capability

For production deployments, start with the recommended configuration and tune based on observed resource utilization and error volumes.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
