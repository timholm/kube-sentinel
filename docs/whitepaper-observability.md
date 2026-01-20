# Kube Sentinel Observability & Monitoring

## Technical Whitepaper v1.0

---

## Executive Summary

Effective observability is critical for operating Kube Sentinel in production environments. This document details the comprehensive observability stack built into Kube Sentinel, including structured logging with slog, Prometheus metrics exposition, health check endpoints, and the Stats API. Additionally, it provides actionable recommendations for alerting strategies, Grafana dashboards, log aggregation, and SLI/SLO definitions.

This whitepaper serves as both a technical reference and an operational guide for teams deploying Kube Sentinel.

---

## 1. Structured JSON Logging with slog

### 1.1 Logger Architecture

Kube Sentinel uses Go's standard library `log/slog` package for structured, JSON-formatted logging. This provides machine-parseable output ideal for log aggregation systems.

```go
import "log/slog"

// Logger initialization in main.go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: level,
}))
slog.SetDefault(logger)
```

### 1.2 JSON Output Format

All logs are emitted as single-line JSON objects with consistent field structure:

```json
{
  "time": "2026-01-19T10:30:45.123456789Z",
  "level": "INFO",
  "msg": "starting kube-sentinel",
  "version": "1.0.0",
  "commit": "abc123",
  "build_date": "2026-01-15"
}
```

### 1.3 Standard Log Fields

Every log entry includes these base fields:

| Field | Type | Description |
|-------|------|-------------|
| `time` | ISO 8601 timestamp | When the log was emitted |
| `level` | string | Log level (DEBUG, INFO, WARN, ERROR) |
| `msg` | string | Human-readable message |

### 1.4 Contextual Fields

Log entries include contextual fields based on the operation:

**Startup logs:**
```json
{
  "time": "2026-01-19T10:30:45Z",
  "level": "INFO",
  "msg": "starting kube-sentinel",
  "version": "1.0.0",
  "commit": "abc123",
  "build_date": "2026-01-15"
}
```

**Rule loading:**
```json
{
  "time": "2026-01-19T10:30:45Z",
  "level": "INFO",
  "msg": "loaded rules",
  "count": 7
}
```

**Error processing:**
```json
{
  "time": "2026-01-19T10:35:12Z",
  "level": "ERROR",
  "msg": "failed to save error",
  "error": "database connection timeout"
}
```

**Remediation actions:**
```json
{
  "time": "2026-01-19T10:36:00Z",
  "level": "INFO",
  "msg": "dry run remediation",
  "action": "restart-pod",
  "target": "production/api-server-7b8d9",
  "rule": "crashloop-backoff"
}
```

**Cleanup operations:**
```json
{
  "time": "2026-01-19T11:00:00Z",
  "level": "INFO",
  "msg": "cleaned up old errors",
  "count": 42
}
```

---

## 2. Log Levels and What Each Captures

### 2.1 Log Level Configuration

Log level is configured via command-line flag:

```bash
kube-sentinel --log-level=info
```

Supported levels: `debug`, `info`, `warn`, `error`

### 2.2 Level Hierarchy

```go
switch *logLevel {
case "debug":
    level = slog.LevelDebug
case "warn":
    level = slog.LevelWarn
case "error":
    level = slog.LevelError
default:
    level = slog.LevelInfo
}
```

### 2.3 What Each Level Captures

#### DEBUG Level
Verbose diagnostic information for troubleshooting:

| Category | Example Messages |
|----------|-----------------|
| WebSocket | `websocket closed`, `websocket upgrade failed` |
| Loki polling | Query timing, response parsing details |
| Rule matching | Individual rule evaluation steps |
| Template rendering | Template execution details |

**Example:**
```json
{"time":"2026-01-19T10:30:45Z","level":"DEBUG","msg":"websocket closed","error":"connection reset by peer"}
```

**When to use:** Development, troubleshooting specific issues, investigating intermittent failures.

#### INFO Level (Default)
Normal operational events:

| Category | Example Messages |
|----------|-----------------|
| Startup | `starting kube-sentinel`, `starting web server`, `starting loki poller` |
| Configuration | `loaded rules`, `loaded config` |
| Shutdown | `received shutdown signal`, `shutting down`, `shutdown complete` |
| Periodic tasks | `cleaned up old errors`, `cleaned up old remediation logs` |
| Remediation | `dry run remediation` (successful actions) |

**Example:**
```json
{"time":"2026-01-19T10:30:45Z","level":"INFO","msg":"starting web server","addr":":8080"}
```

**When to use:** Production environments, normal operations monitoring.

#### WARN Level
Potentially problematic situations that don't prevent operation:

| Category | Example Messages |
|----------|-----------------|
| Configuration | `failed to load rules file, using defaults` |
| Kubernetes | `failed to create kubernetes client, remediation will be disabled` |
| Degraded operation | Non-critical component failures |

**Example:**
```json
{"time":"2026-01-19T10:30:45Z","level":"WARN","msg":"failed to load rules file, using defaults","error":"file not found","path":"/etc/kube-sentinel/rules.yaml"}
```

**When to use:** When you want to minimize noise but still catch configuration issues.

#### ERROR Level
Failures that require attention:

| Category | Example Messages |
|----------|-----------------|
| Critical failures | `failed to load config`, `failed to create rule engine`, `failed to create web server` |
| Runtime errors | `failed to save error`, `remediation failed`, `template render failed` |
| Component errors | `component error`, `web server shutdown error`, `store close error` |

**Example:**
```json
{"time":"2026-01-19T10:30:45Z","level":"ERROR","msg":"remediation failed","error":"pod not found"}
```

**When to use:** When you only want to see actionable failures.

### 2.4 Log Level Selection Guide

| Environment | Recommended Level | Rationale |
|-------------|------------------|-----------|
| Development | DEBUG | Full visibility for debugging |
| Staging | DEBUG or INFO | Catch issues before production |
| Production | INFO | Balance of visibility and volume |
| Production (quiet) | WARN | Minimal noise, only issues |
| Incident response | DEBUG | Temporary increase for investigation |

---

## 3. Prometheus Metrics Exposition

### 3.1 Deployment Annotations

Kube Sentinel exposes metrics via standard Prometheus annotations in the Kubernetes deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
spec:
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
```

### 3.2 Annotation Reference

| Annotation | Value | Description |
|------------|-------|-------------|
| `prometheus.io/scrape` | `"true"` | Enables Prometheus service discovery |
| `prometheus.io/port` | `"8080"` | Port where metrics are exposed |
| `prometheus.io/path` | `"/metrics"` | HTTP path for metrics endpoint |

### 3.3 ServiceMonitor Configuration (Prometheus Operator)

For environments using the Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kube-sentinel
  namespace: kube-sentinel
  labels:
    app.kubernetes.io/name: kube-sentinel
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-sentinel
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - kube-sentinel
```

### 3.4 Recommended Metrics to Export

While Kube Sentinel exposes application state via the Stats API, operators should consider instrumenting additional Prometheus metrics:

| Metric Name | Type | Description |
|-------------|------|-------------|
| `kube_sentinel_errors_total` | Counter | Total errors detected by priority |
| `kube_sentinel_remediations_total` | Counter | Total remediation actions by status |
| `kube_sentinel_remediation_duration_seconds` | Histogram | Time to execute remediation |
| `kube_sentinel_loki_poll_duration_seconds` | Histogram | Loki query latency |
| `kube_sentinel_websocket_connections` | Gauge | Active WebSocket clients |
| `kube_sentinel_store_errors_count` | Gauge | Current errors in store |

### 3.5 Example Prometheus Queries

**Error rate by priority:**
```promql
sum(rate(kube_sentinel_errors_total[5m])) by (priority)
```

**Remediation success rate:**
```promql
sum(rate(kube_sentinel_remediations_total{status="success"}[1h])) /
sum(rate(kube_sentinel_remediations_total[1h]))
```

**P99 remediation latency:**
```promql
histogram_quantile(0.99, sum(rate(kube_sentinel_remediation_duration_seconds_bucket[5m])) by (le))
```

**Loki polling health:**
```promql
histogram_quantile(0.95, sum(rate(kube_sentinel_loki_poll_duration_seconds_bucket[5m])) by (le))
```

---

## 4. Health Check Endpoints

### 4.1 Endpoint Overview

Kube Sentinel exposes two health check endpoints for Kubernetes probes:

| Endpoint | Purpose | Probe Type |
|----------|---------|------------|
| `/health` | Liveness check | livenessProbe |
| `/ready` | Readiness check | readinessProbe |

### 4.2 Health Endpoint Implementation

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}
```

**Response:**
- Status: `200 OK`
- Body: `OK`

The health endpoint indicates the process is running. A failure here triggers pod restart.

### 4.3 Ready Endpoint Implementation

```go
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Ready"))
}
```

**Response:**
- Status: `200 OK`
- Body: `Ready`

The ready endpoint indicates the service can accept traffic. A failure removes the pod from service endpoints.

### 4.4 Kubernetes Probe Configuration

```yaml
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
```

### 4.5 Probe Timing Rationale

| Parameter | Liveness | Readiness | Rationale |
|-----------|----------|-----------|-----------|
| `initialDelaySeconds` | 10 | 5 | Readiness checked sooner for traffic routing |
| `periodSeconds` | 30 | 10 | Readiness checked more frequently |
| `timeoutSeconds` | 5 | 5 | Same timeout for both |
| `failureThreshold` | 3 | 3 | Three consecutive failures trigger action |

**Liveness timing:** 10s initial + (3 failures x 30s) = ~100s before restart
**Readiness timing:** 5s initial + (3 failures x 10s) = ~35s before traffic removal

### 4.6 Health Check Best Practices

1. **Keep endpoints lightweight**: No database queries in health checks
2. **Separate concerns**: Liveness = "am I alive?", Readiness = "can I serve?"
3. **Fail fast on startup**: Use short `initialDelaySeconds` for readiness
4. **Be tolerant of transient failures**: Use `failureThreshold > 1`

---

## 5. Stats API Endpoint for Monitoring

### 5.1 Endpoint Specification

```
GET /api/stats
Content-Type: application/json
```

### 5.2 Stats Structure

```go
type Stats struct {
    TotalErrors       int                       // Unique error count
    ErrorsByPriority  map[rules.Priority]int    // P1, P2, P3, P4 breakdown
    ErrorsByNamespace map[string]int            // Namespace distribution
    RemediationCount  int                       // Total remediation attempts
    SuccessfulActions int                       // Successful remediations
    FailedActions     int                       // Failed remediations
    LastError         *time.Time                // Most recent error timestamp
    LastRemediation   *time.Time                // Most recent remediation timestamp
}
```

### 5.3 Example Response

```json
{
  "TotalErrors": 156,
  "ErrorsByPriority": {
    "P1": 3,
    "P2": 12,
    "P3": 45,
    "P4": 96
  },
  "ErrorsByNamespace": {
    "production": 78,
    "staging": 45,
    "development": 33
  },
  "RemediationCount": 47,
  "SuccessfulActions": 42,
  "FailedActions": 5,
  "LastError": "2026-01-19T10:35:12Z",
  "LastRemediation": "2026-01-19T10:36:00Z"
}
```

### 5.4 Handler Implementation

```go
func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
    stats, err := s.store.GetStats()
    if err != nil {
        s.jsonError(w, err.Error(), http.StatusInternalServerError)
        return
    }
    s.jsonResponse(w, stats)
}
```

### 5.5 Polling the Stats API

**Using curl:**
```bash
curl -s http://localhost:8080/api/stats | jq
```

**Using watch for continuous monitoring:**
```bash
watch -n 5 'curl -s http://localhost:8080/api/stats | jq'
```

**Prometheus metrics exporter script:**
```bash
#!/bin/bash
# Export stats as Prometheus metrics
STATS=$(curl -s http://localhost:8080/api/stats)

echo "# HELP kube_sentinel_errors_total Total errors by priority"
echo "# TYPE kube_sentinel_errors_total gauge"
echo "kube_sentinel_errors_total{priority=\"P1\"} $(echo $STATS | jq '.ErrorsByPriority.P1 // 0')"
echo "kube_sentinel_errors_total{priority=\"P2\"} $(echo $STATS | jq '.ErrorsByPriority.P2 // 0')"
echo "kube_sentinel_errors_total{priority=\"P3\"} $(echo $STATS | jq '.ErrorsByPriority.P3 // 0')"
echo "kube_sentinel_errors_total{priority=\"P4\"} $(echo $STATS | jq '.ErrorsByPriority.P4 // 0')"

echo "# HELP kube_sentinel_remediations_total Total remediation actions"
echo "# TYPE kube_sentinel_remediations_total gauge"
echo "kube_sentinel_remediations_total{status=\"success\"} $(echo $STATS | jq '.SuccessfulActions')"
echo "kube_sentinel_remediations_total{status=\"failed\"} $(echo $STATS | jq '.FailedActions')"
```

---

## 6. Alerting Strategies

### 6.1 Critical Alerts (P1 - Page Immediately)

These alerts require immediate human attention:

**Alert: High P1 Error Rate**
```yaml
- alert: KubeSentinelCriticalErrors
  expr: |
    sum(kube_sentinel_errors_total{priority="P1"}) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Critical P1 errors detected in Kubernetes cluster"
    description: "{{ $value }} critical errors requiring immediate attention"
    runbook_url: "https://wiki.example.com/kube-sentinel/p1-errors"
```

**Alert: Kube Sentinel Down**
```yaml
- alert: KubeSentinelDown
  expr: |
    up{job="kube-sentinel"} == 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Kube Sentinel is not responding"
    description: "Kube Sentinel has been down for more than 2 minutes"
```

**Alert: Remediation Failure Spike**
```yaml
- alert: KubeSentinelRemediationFailures
  expr: |
    sum(rate(kube_sentinel_remediations_total{status="failed"}[5m])) /
    sum(rate(kube_sentinel_remediations_total[5m])) > 0.5
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "More than 50% of remediation actions are failing"
    description: "Remediation failure rate is {{ $value | humanizePercentage }}"
```

### 6.2 Warning Alerts (P2 - Notify)

Alerts that require attention but not immediate response:

**Alert: High Error Volume**
```yaml
- alert: KubeSentinelHighErrorVolume
  expr: |
    sum(rate(kube_sentinel_errors_total[5m])) > 10
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "High volume of errors detected"
    description: "Error rate is {{ $value }} errors/second"
```

**Alert: Approaching Rate Limit**
```yaml
- alert: KubeSentinelApproachingRateLimit
  expr: |
    kube_sentinel_actions_this_hour / kube_sentinel_max_actions_per_hour > 0.8
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Kube Sentinel approaching hourly action limit"
    description: "{{ $value | humanizePercentage }} of hourly limit used"
```

**Alert: Loki Query Latency**
```yaml
- alert: KubeSentinelLokiLatency
  expr: |
    histogram_quantile(0.95, sum(rate(kube_sentinel_loki_poll_duration_seconds_bucket[5m])) by (le)) > 5
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Loki queries taking too long"
    description: "P95 Loki query latency is {{ $value }}s"
```

### 6.3 Informational Alerts (P3/P4 - Log Only)

Alerts for trend monitoring:

**Alert: Error Count Increasing**
```yaml
- alert: KubeSentinelErrorTrend
  expr: |
    delta(kube_sentinel_errors_total[1h]) > 100
  for: 30m
  labels:
    severity: info
  annotations:
    summary: "Significant increase in error count"
    description: "{{ $value }} new errors in the last hour"
```

### 6.4 Complete AlertManager Rules File

```yaml
groups:
  - name: kube-sentinel
    rules:
      # Critical
      - alert: KubeSentinelDown
        expr: up{job="kube-sentinel"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Kube Sentinel is down"

      - alert: KubeSentinelCriticalErrors
        expr: sum(kube_sentinel_errors_total{priority="P1"}) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Critical P1 errors detected"

      - alert: KubeSentinelRemediationFailures
        expr: |
          (sum(rate(kube_sentinel_remediations_total{status="failed"}[5m])) /
          sum(rate(kube_sentinel_remediations_total[5m]))) > 0.5
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High remediation failure rate"

      # Warning
      - alert: KubeSentinelHighP2Errors
        expr: sum(kube_sentinel_errors_total{priority="P2"}) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High number of P2 errors"

      - alert: KubeSentinelApproachingRateLimit
        expr: |
          kube_sentinel_actions_this_hour / kube_sentinel_max_actions_per_hour > 0.8
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Approaching hourly action limit"

      # Info
      - alert: KubeSentinelRemediationDisabled
        expr: kube_sentinel_remediation_enabled == 0
        for: 1h
        labels:
          severity: info
        annotations:
          summary: "Remediation is disabled"
```

---

## 7. Dashboard Recommendations

### 7.1 Grafana Dashboard Overview

The recommended dashboard layout consists of four rows:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Row 1: Overview Stats (Single Stats)                                         │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│ │  Total   │ │    P1    │ │    P2    │ │  Remed.  │ │ Success  │           │
│ │  Errors  │ │ Critical │ │   High   │ │  Count   │ │   Rate   │           │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
├─────────────────────────────────────────────────────────────────────────────┤
│ Row 2: Time Series Graphs                                                    │
│ ┌────────────────────────────────┐ ┌────────────────────────────────┐      │
│ │    Error Rate by Priority      │ │    Remediation Activity        │      │
│ │         (stacked area)         │ │       (line graph)             │      │
│ └────────────────────────────────┘ └────────────────────────────────┘      │
├─────────────────────────────────────────────────────────────────────────────┤
│ Row 3: Distribution Views                                                    │
│ ┌────────────────────────────────┐ ┌────────────────────────────────┐      │
│ │  Errors by Namespace (pie)     │ │  Errors by Priority (bar)      │      │
│ └────────────────────────────────┘ └────────────────────────────────┘      │
├─────────────────────────────────────────────────────────────────────────────┤
│ Row 4: Health & Performance                                                  │
│ ┌────────────────────────────────┐ ┌────────────────────────────────┐      │
│ │    Loki Query Latency          │ │    WebSocket Connections       │      │
│ └────────────────────────────────┘ └────────────────────────────────┘      │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.2 Grafana Dashboard JSON

```json
{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 1,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "yellow", "value": 50},
              {"color": "red", "value": 100}
            ]
          }
        },
        "overrides": []
      },
      "gridPos": {"h": 4, "w": 4, "x": 0, "y": 0},
      "id": 1,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "title": "Total Errors",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(kube_sentinel_errors_total)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "red", "value": 1}
            ]
          }
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 4, "y": 0},
      "id": 2,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "title": "P1 Critical",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(kube_sentinel_errors_total{priority=\"P1\"}) or vector(0)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "orange", "value": 5}
            ]
          }
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 8, "y": 0},
      "id": 3,
      "title": "P2 High",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(kube_sentinel_errors_total{priority=\"P2\"}) or vector(0)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "blue", "value": null}
            ]
          }
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 12, "y": 0},
      "id": 4,
      "title": "Remediations",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(kube_sentinel_remediations_total)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "unit": "percentunit",
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "red", "value": null},
              {"color": "yellow", "value": 0.8},
              {"color": "green", "value": 0.95}
            ]
          }
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 16, "y": 0},
      "id": 5,
      "title": "Success Rate",
      "type": "stat",
      "targets": [
        {
          "expr": "sum(kube_sentinel_remediations_total{status=\"success\"}) / sum(kube_sentinel_remediations_total)",
          "refId": "A"
        }
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "fillOpacity": 30,
            "stacking": {
              "mode": "normal"
            }
          },
          "color": {
            "mode": "palette-classic"
          }
        },
        "overrides": [
          {"matcher": {"id": "byName", "options": "P1"}, "properties": [{"id": "color", "value": {"fixedColor": "red", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P2"}, "properties": [{"id": "color", "value": {"fixedColor": "orange", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P3"}, "properties": [{"id": "color", "value": {"fixedColor": "yellow", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P4"}, "properties": [{"id": "color", "value": {"fixedColor": "blue", "mode": "fixed"}}]}
        ]
      },
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 4},
      "id": 6,
      "options": {
        "legend": {"displayMode": "list", "placement": "bottom"},
        "tooltip": {"mode": "multi"}
      },
      "title": "Error Rate by Priority",
      "type": "timeseries",
      "targets": [
        {"expr": "sum(rate(kube_sentinel_errors_total{priority=\"P1\"}[5m]))", "legendFormat": "P1", "refId": "A"},
        {"expr": "sum(rate(kube_sentinel_errors_total{priority=\"P2\"}[5m]))", "legendFormat": "P2", "refId": "B"},
        {"expr": "sum(rate(kube_sentinel_errors_total{priority=\"P3\"}[5m]))", "legendFormat": "P3", "refId": "C"},
        {"expr": "sum(rate(kube_sentinel_errors_total{priority=\"P4\"}[5m]))", "legendFormat": "P4", "refId": "D"}
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth"
          }
        },
        "overrides": [
          {"matcher": {"id": "byName", "options": "Success"}, "properties": [{"id": "color", "value": {"fixedColor": "green", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "Failed"}, "properties": [{"id": "color", "value": {"fixedColor": "red", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "Skipped"}, "properties": [{"id": "color", "value": {"fixedColor": "yellow", "mode": "fixed"}}]}
        ]
      },
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 4},
      "id": 7,
      "title": "Remediation Activity",
      "type": "timeseries",
      "targets": [
        {"expr": "sum(rate(kube_sentinel_remediations_total{status=\"success\"}[5m]))", "legendFormat": "Success", "refId": "A"},
        {"expr": "sum(rate(kube_sentinel_remediations_total{status=\"failed\"}[5m]))", "legendFormat": "Failed", "refId": "B"},
        {"expr": "sum(rate(kube_sentinel_remediations_total{status=\"skipped\"}[5m]))", "legendFormat": "Skipped", "refId": "C"}
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"}
        }
      },
      "gridPos": {"h": 8, "w": 8, "x": 0, "y": 12},
      "id": 8,
      "options": {
        "legend": {"displayMode": "table", "placement": "right"},
        "pieType": "pie",
        "reduceOptions": {"calcs": ["lastNotNull"]}
      },
      "title": "Errors by Namespace",
      "type": "piechart",
      "targets": [
        {"expr": "sum(kube_sentinel_errors_total) by (namespace)", "legendFormat": "{{namespace}}", "refId": "A"}
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"}
        },
        "overrides": [
          {"matcher": {"id": "byName", "options": "P1"}, "properties": [{"id": "color", "value": {"fixedColor": "red", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P2"}, "properties": [{"id": "color", "value": {"fixedColor": "orange", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P3"}, "properties": [{"id": "color", "value": {"fixedColor": "yellow", "mode": "fixed"}}]},
          {"matcher": {"id": "byName", "options": "P4"}, "properties": [{"id": "color", "value": {"fixedColor": "blue", "mode": "fixed"}}]}
        ]
      },
      "gridPos": {"h": 8, "w": 8, "x": 8, "y": 12},
      "id": 9,
      "options": {
        "orientation": "horizontal"
      },
      "title": "Errors by Priority",
      "type": "barchart",
      "targets": [
        {"expr": "sum(kube_sentinel_errors_total) by (priority)", "legendFormat": "{{priority}}", "refId": "A"}
      ]
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${DS_PROMETHEUS}"
      },
      "fieldConfig": {
        "defaults": {
          "custom": {"drawStyle": "line"},
          "unit": "s"
        }
      },
      "gridPos": {"h": 8, "w": 8, "x": 16, "y": 12},
      "id": 10,
      "title": "Loki Query Latency",
      "type": "timeseries",
      "targets": [
        {"expr": "histogram_quantile(0.50, sum(rate(kube_sentinel_loki_poll_duration_seconds_bucket[5m])) by (le))", "legendFormat": "p50", "refId": "A"},
        {"expr": "histogram_quantile(0.95, sum(rate(kube_sentinel_loki_poll_duration_seconds_bucket[5m])) by (le))", "legendFormat": "p95", "refId": "B"},
        {"expr": "histogram_quantile(0.99, sum(rate(kube_sentinel_loki_poll_duration_seconds_bucket[5m])) by (le))", "legendFormat": "p99", "refId": "C"}
      ]
    }
  ],
  "refresh": "30s",
  "schemaVersion": 38,
  "style": "dark",
  "tags": ["kube-sentinel", "kubernetes", "observability"],
  "templating": {
    "list": [
      {
        "current": {},
        "datasource": {"type": "prometheus", "uid": "${DS_PROMETHEUS}"},
        "definition": "label_values(kube_sentinel_errors_total, namespace)",
        "hide": 0,
        "includeAll": true,
        "multi": true,
        "name": "namespace",
        "options": [],
        "query": {"query": "label_values(kube_sentinel_errors_total, namespace)"},
        "refresh": 2,
        "type": "query"
      }
    ]
  },
  "time": {"from": "now-1h", "to": "now"},
  "title": "Kube Sentinel Overview",
  "uid": "kube-sentinel-overview",
  "version": 1
}
```

### 7.3 Dashboard Import Instructions

1. Navigate to Grafana > Dashboards > Import
2. Paste the JSON above or upload as file
3. Select your Prometheus data source
4. Click Import

---

## 8. Log Aggregation for Kube Sentinel Logs

### 8.1 Collection Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  Kube Sentinel  │────▶│   Fluent Bit    │────▶│      Loki       │
│     (stdout)    │     │   (DaemonSet)   │     │   (storage)     │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                                        │
                                                        ▼
                                                ┌─────────────────┐
                                                │     Grafana     │
                                                │   (explore)     │
                                                └─────────────────┘
```

### 8.2 Fluent Bit Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluent-bit-config
  namespace: logging
data:
  fluent-bit.conf: |
    [SERVICE]
        Flush         1
        Log_Level     info
        Parsers_File  parsers.conf

    [INPUT]
        Name              tail
        Tag               kube.*
        Path              /var/log/containers/kube-sentinel-*.log
        Parser            docker
        Refresh_Interval  5
        Mem_Buf_Limit     5MB

    [FILTER]
        Name         kubernetes
        Match        kube.*
        Kube_URL     https://kubernetes.default.svc:443
        Merge_Log    On
        K8S-Logging.Parser  On

    [FILTER]
        Name    modify
        Match   kube.*
        Add     app kube-sentinel

    [OUTPUT]
        Name        loki
        Match       kube.*
        Host        loki.logging.svc
        Port        3100
        Labels      job=kube-sentinel, namespace=$kubernetes['namespace_name'], pod=$kubernetes['pod_name']
        Line_Format json

  parsers.conf: |
    [PARSER]
        Name        docker
        Format      json
        Time_Key    time
        Time_Format %Y-%m-%dT%H:%M:%S.%L
```

### 8.3 Loki Query Examples

**All Kube Sentinel logs:**
```logql
{job="kube-sentinel"}
```

**Error level only:**
```logql
{job="kube-sentinel"} |= "ERROR"
```

**JSON parsing and filtering:**
```logql
{job="kube-sentinel"} | json | level="ERROR"
```

**Remediation actions:**
```logql
{job="kube-sentinel"} | json | msg=~".*remediation.*"
```

**Errors by rule name:**
```logql
{job="kube-sentinel"} | json | rule!="" | line_format "{{.rule}}: {{.msg}}"
```

**Count errors per minute:**
```logql
count_over_time({job="kube-sentinel"} | json | level="ERROR" [1m])
```

### 8.4 Grafana Explore Queries

**Recent errors with context:**
```logql
{job="kube-sentinel"} | json | level="ERROR" | line_format "{{.time}} [{{.level}}] {{.msg}} error={{.error}}"
```

**Startup timeline:**
```logql
{job="kube-sentinel"} | json | msg=~"starting.*" | line_format "{{.time}} {{.msg}}"
```

### 8.5 Log Retention Recommendations

| Environment | Retention | Rationale |
|-------------|-----------|-----------|
| Development | 7 days | Short-term debugging |
| Staging | 14 days | Pre-production investigation |
| Production | 30 days | Incident response, compliance |
| Audit/Compliance | 90+ days | Regulatory requirements |

---

## 9. SLI/SLO Recommendations

### 9.1 Service Level Indicators (SLIs)

| SLI | Definition | Measurement |
|-----|------------|-------------|
| **Availability** | Kube Sentinel responds to health checks | `up{job="kube-sentinel"} == 1` |
| **Error Detection Latency** | Time from log emission to error detection | Loki poll interval + processing time |
| **Remediation Success Rate** | Percentage of successful remediation actions | `success / (success + failed)` |
| **Dashboard Availability** | Web UI responds within timeout | HTTP 200 on `/` within 5s |
| **P1 Detection Time** | Time to detect and classify P1 errors | < 2 minutes from occurrence |

### 9.2 Service Level Objectives (SLOs)

#### SLO 1: Availability
```
Target: 99.9% monthly uptime
Budget: 43.2 minutes/month downtime
Window: 30 days rolling

Measurement:
  success = sum(up{job="kube-sentinel"})
  total = count(up{job="kube-sentinel"})
  SLI = success / total
```

**Alert on budget burn:**
```yaml
- alert: KubeSentinelSLOBudgetBurn
  expr: |
    (
      1 - avg_over_time(up{job="kube-sentinel"}[1h])
    ) > (1 - 0.999) * 10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Burning through availability SLO budget"
```

#### SLO 2: Remediation Success Rate
```
Target: 95% success rate
Window: 7 days rolling

Measurement:
  success = sum(kube_sentinel_remediations_total{status="success"})
  total = sum(kube_sentinel_remediations_total{status=~"success|failed"})
  SLI = success / total
```

**Prometheus recording rule:**
```yaml
- record: kube_sentinel:remediation_success_rate:7d
  expr: |
    sum(increase(kube_sentinel_remediations_total{status="success"}[7d])) /
    sum(increase(kube_sentinel_remediations_total{status=~"success|failed"}[7d]))
```

#### SLO 3: Error Detection Freshness
```
Target: 99% of errors detected within 2 minutes
Window: 24 hours

Measurement:
  fresh_detections = errors detected within 2 min of occurrence
  total_detections = all errors detected
  SLI = fresh_detections / total_detections
```

#### SLO 4: P1 Response Time
```
Target: 100% of P1 errors trigger remediation within 5 minutes
Window: 30 days

Measurement:
  Audit log timestamp - Error first seen timestamp < 5 minutes
```

### 9.3 Error Budget Policy

| Budget Remaining | Action |
|-----------------|--------|
| > 50% | Normal operations |
| 25-50% | Increased monitoring, no risky changes |
| 10-25% | Change freeze, focus on reliability |
| < 10% | Incident mode, all hands on reliability |

### 9.4 SLO Dashboard Queries

**Availability SLO:**
```promql
# Current availability (30d)
avg_over_time(up{job="kube-sentinel"}[30d])

# Error budget remaining
(0.999 - (1 - avg_over_time(up{job="kube-sentinel"}[30d]))) / 0.001
```

**Remediation SLO:**
```promql
# Current success rate (7d)
sum(increase(kube_sentinel_remediations_total{status="success"}[7d])) /
sum(increase(kube_sentinel_remediations_total{status=~"success|failed"}[7d]))

# Budget burn rate (last hour vs 7d average)
(
  sum(rate(kube_sentinel_remediations_total{status="failed"}[1h])) /
  sum(rate(kube_sentinel_remediations_total[1h]))
) / (
  sum(rate(kube_sentinel_remediations_total{status="failed"}[7d])) /
  sum(rate(kube_sentinel_remediations_total[7d]))
)
```

---

## 10. Debugging and Troubleshooting with Logs

### 10.1 Common Issues and Log Patterns

#### Issue: Kube Sentinel Not Starting

**Symptoms:** Pod in CrashLoopBackOff

**Log pattern:**
```json
{"level":"ERROR","msg":"failed to load config","error":"invalid yaml: ..."}
```

**Resolution:**
1. Check config syntax: `kubectl get configmap kube-sentinel-config -o yaml`
2. Validate YAML: `yq eval '.' config.yaml`

#### Issue: No Errors Being Detected

**Symptoms:** Dashboard shows zero errors despite known issues

**Debug steps:**
1. Check Loki connectivity:
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel | grep -i loki
```

**Expected log:**
```json
{"level":"INFO","msg":"starting loki poller"}
```

**Error log:**
```json
{"level":"ERROR","msg":"loki query failed","error":"connection refused"}
```

2. Verify Loki query:
```bash
# Test query manually
curl -G "http://loki:3100/loki/api/v1/query_range" \
  --data-urlencode 'query={namespace=~".+"}' \
  --data-urlencode 'start=1705660800000000000' \
  --data-urlencode 'end=1705664400000000000'
```

#### Issue: Remediation Not Executing

**Symptoms:** Errors detected but no remediation actions

**Log pattern to search:**
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel | grep -i "remediation\|skipped"
```

**Possible causes:**

1. **Disabled:**
```json
{"level":"INFO","msg":"remediation skipped","reason":"disabled"}
```
Fix: Set `remediation.enabled: true`

2. **Dry run mode:**
```json
{"level":"INFO","msg":"dry run remediation","action":"restart-pod"}
```
Fix: Set `remediation.dry_run: false`

3. **Namespace excluded:**
```json
{"level":"INFO","msg":"remediation skipped","reason":"namespace excluded","namespace":"kube-system"}
```
Fix: Remove from `excluded_namespaces` or accept exclusion

4. **Cooldown active:**
```json
{"level":"INFO","msg":"remediation skipped","reason":"cooldown active"}
```
Fix: Wait for cooldown or increase cooldown duration

5. **Rate limit:**
```json
{"level":"INFO","msg":"remediation skipped","reason":"hourly limit reached"}
```
Fix: Increase `max_actions_per_hour` or wait

#### Issue: High Memory Usage

**Debug steps:**
```bash
# Check store size
curl http://localhost:8080/api/stats | jq '.TotalErrors'
```

**Log pattern:**
```json
{"level":"INFO","msg":"cleaned up old errors","count":1000}
```

If cleanup is not running, check for logs showing cleanup:
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel | grep "cleaned up"
```

### 10.2 Debug Log Level Deep Dive

Enable debug logging temporarily:

```bash
kubectl set env deployment/kube-sentinel -n kube-sentinel LOG_LEVEL=debug
# Or edit deployment args: --log-level=debug
```

**Debug log examples:**

WebSocket activity:
```json
{"level":"DEBUG","msg":"websocket closed","error":"connection reset"}
```

Rule matching:
```json
{"level":"DEBUG","msg":"rule matched","rule":"crashloop-backoff","pod":"api-server-abc123"}
```

### 10.3 Log Analysis One-Liners

**Count errors by level:**
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel --since=1h | \
  jq -r '.level' | sort | uniq -c | sort -rn
```

**Find most common error messages:**
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel --since=1h | \
  jq -r 'select(.level=="ERROR") | .msg' | sort | uniq -c | sort -rn | head -10
```

**Timeline of remediation actions:**
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel --since=24h | \
  jq -r 'select(.msg | contains("remediation")) | "\(.time) \(.action) \(.target) \(.status)"'
```

**Extract all unique rule matches:**
```bash
kubectl logs -n kube-sentinel deployment/kube-sentinel --since=1h | \
  jq -r 'select(.rule != null) | .rule' | sort -u
```

### 10.4 Troubleshooting Checklist

```
[ ] 1. Pod status: kubectl get pods -n kube-sentinel
[ ] 2. Pod logs: kubectl logs -n kube-sentinel deployment/kube-sentinel
[ ] 3. Pod events: kubectl describe pod -n kube-sentinel -l app=kube-sentinel
[ ] 4. Config validity: kubectl get configmap kube-sentinel-config -o yaml
[ ] 5. Loki connectivity: curl http://loki:3100/ready
[ ] 6. RBAC permissions: kubectl auth can-i --list --as=system:serviceaccount:kube-sentinel:kube-sentinel
[ ] 7. Stats endpoint: curl http://localhost:8080/api/stats
[ ] 8. Health endpoint: curl http://localhost:8080/health
[ ] 9. Ready endpoint: curl http://localhost:8080/ready
[ ] 10. Recent remediations: curl http://localhost:8080/api/remediations
```

### 10.5 Port-Forward for Local Debugging

```bash
# Forward Kube Sentinel web UI
kubectl port-forward -n kube-sentinel deployment/kube-sentinel 8080:8080

# Access endpoints
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/api/stats | jq
curl http://localhost:8080/api/errors | jq
curl http://localhost:8080/api/remediations | jq
```

---

## 11. Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Kube Sentinel Observability Stack                    │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                              Data Sources                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         Kube Sentinel                                │   │
│  │                                                                      │   │
│  │   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐           │   │
│  │   │  JSON Logs   │   │   /metrics   │   │  /api/stats  │           │   │
│  │   │   (stdout)   │   │  (Prometheus)│   │    (JSON)    │           │   │
│  │   └──────┬───────┘   └──────┬───────┘   └──────┬───────┘           │   │
│  │          │                   │                   │                   │   │
│  │   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐           │   │
│  │   │   /health    │   │    /ready    │   │     /ws      │           │   │
│  │   │  (liveness)  │   │ (readiness)  │   │ (WebSocket)  │           │   │
│  │   └──────────────┘   └──────────────┘   └──────────────┘           │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
           │                       │                       │
           ▼                       ▼                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Collection Layer                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐          │
│   │   Fluent Bit    │   │   Prometheus    │   │   Dashboard     │          │
│   │   (DaemonSet)   │   │    (scrape)     │   │   (poll API)    │          │
│   └────────┬────────┘   └────────┬────────┘   └────────┬────────┘          │
│            │                      │                      │                   │
└────────────┼──────────────────────┼──────────────────────┼───────────────────┘
             │                      │                      │
             ▼                      ▼                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Storage Layer                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────┐   ┌─────────────────┐                                │
│   │      Loki       │   │   Prometheus    │                                │
│   │   (log store)   │   │   (TSDB)        │                                │
│   └────────┬────────┘   └────────┬────────┘                                │
│            │                      │                                         │
└────────────┼──────────────────────┼─────────────────────────────────────────┘
             │                      │
             └──────────┬───────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Visualization Layer                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                          Grafana                                     │   │
│   │                                                                      │   │
│   │   ┌───────────────┐   ┌───────────────┐   ┌───────────────┐        │   │
│   │   │  Dashboards   │   │    Explore    │   │    Alerts     │        │   │
│   │   │  (metrics)    │   │   (logs)      │   │  (rules)      │        │   │
│   │   └───────────────┘   └───────────────┘   └───────────────┘        │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Alerting Layer                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐          │
│   │  AlertManager   │──▶│     Slack       │   │   PagerDuty    │          │
│   │   (routing)     │   │   (channel)     │   │   (incidents)  │          │
│   └─────────────────┘   └─────────────────┘   └─────────────────┘          │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 12. Quick Reference

### 12.1 Endpoints Summary

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/health` | GET | Liveness probe |
| `/ready` | GET | Readiness probe |
| `/metrics` | GET | Prometheus metrics |
| `/api/stats` | GET | Application statistics |
| `/api/errors` | GET | List detected errors |
| `/api/remediations` | GET | List remediation logs |
| `/ws` | WebSocket | Real-time updates |

### 12.2 Log Level Quick Reference

| Level | Flag | Use Case |
|-------|------|----------|
| DEBUG | `--log-level=debug` | Development, debugging |
| INFO | `--log-level=info` | Production (default) |
| WARN | `--log-level=warn` | Quiet production |
| ERROR | `--log-level=error` | Minimal output |

### 12.3 Key Prometheus Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `kube_sentinel_errors_total` | Counter | priority, namespace |
| `kube_sentinel_remediations_total` | Counter | status, action |
| `kube_sentinel_loki_poll_duration_seconds` | Histogram | - |
| `up{job="kube-sentinel"}` | Gauge | - |

### 12.4 SLO Targets

| SLO | Target | Window |
|-----|--------|--------|
| Availability | 99.9% | 30 days |
| Remediation Success | 95% | 7 days |
| P1 Detection Time | < 2 min | Always |

---

## 13. Conclusion

Effective observability of Kube Sentinel requires a multi-layered approach:

1. **Structured Logging**: JSON-formatted slog output enables machine parsing and sophisticated querying
2. **Metrics Exposition**: Prometheus annotations enable automatic scraping and alerting
3. **Health Endpoints**: Kubernetes-native probes ensure reliable pod lifecycle management
4. **Stats API**: Real-time application state for dashboards and external monitoring
5. **Alerting**: Tiered alert severity ensures appropriate response to issues
6. **Dashboards**: Pre-built Grafana panels provide immediate visibility
7. **SLIs/SLOs**: Quantified reliability targets guide operational decisions

By implementing the recommendations in this whitepaper, operations teams gain comprehensive visibility into Kube Sentinel's behavior, enabling proactive issue detection and rapid incident response.

---

*Document Version: 1.0*
*Last Updated: January 2026*
*Kube Sentinel Project*
