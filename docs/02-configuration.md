# Configuration Reference

This document provides a comprehensive reference for configuring Kube Sentinel. The configuration system is designed to be flexible, supporting both in-cluster Kubernetes deployments and local development environments.

## Table of Contents

- [Overview](#overview)
- [Configuration Loading](#configuration-loading)
- [Configuration Types](#configuration-types)
  - [Root Configuration](#root-configuration)
  - [Loki Configuration](#loki-configuration)
  - [Kubernetes Configuration](#kubernetes-configuration)
  - [Web Configuration](#web-configuration)
  - [Remediation Configuration](#remediation-configuration)
  - [Store Configuration](#store-configuration)
- [Validation Rules](#validation-rules)
- [Default Values](#default-values)
- [Example Configuration](#example-configuration)
- [Future Extensions](#future-extensions)

---

## Overview

Kube Sentinel uses a YAML-based configuration system that provides:

- **Sensible defaults**: The application works out of the box with minimal configuration
- **Layered configuration**: Defaults are applied first, then overridden by user-provided values
- **Validation**: All configuration values are validated at startup to catch errors early
- **Type safety**: Configuration is strongly typed using Go structs with YAML tags

The configuration is defined in `/Users/tim/kube-sentinel/internal/config/config.go` and controls all aspects of the application including log collection, Kubernetes integration, web dashboard, and remediation behavior.

---

## Configuration Loading

### Load Functions

The configuration system provides two primary functions for loading configuration:

#### `Load(path string) (*Config, error)`

Reads configuration from the specified YAML file path. This function:

1. Initializes a configuration with default values
2. Reads the YAML file from disk
3. Unmarshals the YAML content, overlaying it on the defaults
4. Validates the resulting configuration
5. Returns the validated configuration or an error

```go
cfg, err := config.Load("/etc/kube-sentinel/config.yaml")
if err != nil {
    log.Fatalf("Failed to load configuration: %v", err)
}
```

#### `LoadOrDefault(path string) (*Config, error)`

A convenience function that attempts to load configuration from the specified path but gracefully falls back to defaults if:

- The path is empty
- The file does not exist

This is useful for applications that want to support optional configuration files.

```go
cfg, err := config.LoadOrDefault(configPath)
if err != nil {
    log.Fatalf("Failed to load configuration: %v", err)
}
```

### Configuration File Locations

By default, Kube Sentinel looks for configuration in the following locations:

| Location | Use Case |
|----------|----------|
| `/etc/kube-sentinel/config.yaml` | Production deployment |
| `--config` flag | Custom path specified at runtime |

---

## Configuration Types

### Root Configuration

The root `Config` struct aggregates all configuration sections:

| Field | Type | YAML Key | Description |
|-------|------|----------|-------------|
| `Loki` | `LokiConfig` | `loki` | Loki connection and query settings |
| `Kubernetes` | `KubernetesConfig` | `kubernetes` | Kubernetes cluster connection settings |
| `Web` | `WebConfig` | `web` | Web dashboard server settings |
| `Remediation` | `RemediationConfig` | `remediation` | Automated remediation behavior |
| `RulesFile` | `string` | `rules_file` | Path to the remediation rules file |
| `Store` | `StoreConfig` | `store` | Data persistence configuration |

---

### Loki Configuration

The `LokiConfig` struct defines how Kube Sentinel connects to and queries Grafana Loki for log data.

#### Fields

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `URL` | `string` | `url` | Yes | Loki server URL (e.g., `http://loki.monitoring:3100`) |
| `Query` | `string` | `query` | Yes | LogQL query for fetching error logs |
| `PollInterval` | `time.Duration` | `poll_interval` | Yes | Interval between log polling cycles |
| `Lookback` | `time.Duration` | `lookback` | Yes | Time window to query on each poll |
| `TenantID` | `string` | `tenant_id` | No | Tenant ID for multi-tenant Loki (sets `X-Scope-OrgID` header) |
| `Username` | `string` | `username` | No | Basic authentication username |
| `Password` | `string` | `password` | No | Basic authentication password |

#### LogQL Query Design

The default query is designed to capture error-related log entries across all namespaces:

```logql
{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"
```

This query:
- Selects logs from all namespaces (`namespace=~".+"`)
- Filters for common error patterns using case-insensitive matching
- Captures: `error`, `fatal`, `panic`, `exception`, and `fail`

#### Timing Considerations

The `poll_interval` and `lookback` parameters work together:

- `poll_interval`: How frequently the poller queries Loki
- `lookback`: The time window for each query

**Important**: `lookback` must be greater than or equal to `poll_interval` to ensure no logs are missed between polls. A small overlap is acceptable and helps handle timing edge cases.

```yaml
loki:
  poll_interval: 30s
  lookback: 5m  # Ensures coverage even with processing delays
```

---

### Kubernetes Configuration

The `KubernetesConfig` struct controls how Kube Sentinel connects to the Kubernetes API.

#### Fields

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `InCluster` | `bool` | `in_cluster` | No | Use in-cluster service account credentials |
| `Kubeconfig` | `string` | `kubeconfig` | No | Path to kubeconfig file for out-of-cluster access |

#### Connection Modes

**In-Cluster Mode** (default for production):
```yaml
kubernetes:
  in_cluster: true
```

When running inside a Kubernetes cluster, Kube Sentinel uses the mounted service account token and CA certificate to authenticate with the API server.

**Out-of-Cluster Mode** (for development):
```yaml
kubernetes:
  in_cluster: false
  kubeconfig: ~/.kube/config
```

When running outside the cluster (e.g., during development), specify the path to a kubeconfig file.

---

### Web Configuration

The `WebConfig` struct defines the web dashboard server settings.

#### Fields

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `Listen` | `string` | `listen` | Yes | Address and port for the web server |

#### Listen Address Formats

| Format | Description |
|--------|-------------|
| `:8080` | Listen on all interfaces, port 8080 |
| `127.0.0.1:8080` | Listen only on localhost |
| `0.0.0.0:8080` | Explicitly listen on all interfaces |

```yaml
web:
  listen: ":8080"
```

---

### Remediation Configuration

The `RemediationConfig` struct controls the automated remediation engine behavior and safety limits.

#### Fields

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `Enabled` | `bool` | `enabled` | No | Enable or disable automatic remediation |
| `DryRun` | `bool` | `dry_run` | No | Log actions without executing them |
| `MaxActionsPerHour` | `int` | `max_actions_per_hour` | No | Rate limit for remediation actions |
| `ExcludedNamespaces` | `[]string` | `excluded_namespaces` | No | Namespaces protected from remediation |

#### Safety Features

**Dry Run Mode**: When enabled, Kube Sentinel logs what actions it would take without executing them. This is essential for:
- Initial deployment and testing
- Validating rules before enabling live remediation
- Auditing potential impact

```yaml
remediation:
  dry_run: true  # Test mode - no actual changes
```

**Rate Limiting**: The `max_actions_per_hour` setting prevents runaway remediation cascades:

```yaml
remediation:
  max_actions_per_hour: 50  # Safety limit
```

**Namespace Exclusions**: Critical namespaces should be protected:

```yaml
remediation:
  excluded_namespaces:
    - kube-system      # Core Kubernetes components
    - kube-public      # Public cluster information
    - monitoring       # Monitoring stack
    - logging          # Logging infrastructure
```

---

### Store Configuration

The `StoreConfig` struct defines data persistence settings for alerts and remediation history.

#### Fields

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `Type` | `string` | `type` | Yes | Storage backend: `memory` or `sqlite` |
| `Path` | `string` | `path` | Conditional | Database file path (required for `sqlite`) |

#### Storage Backends

**Memory Store** (default):
```yaml
store:
  type: memory
```
- No persistence across restarts
- Suitable for development and testing
- Zero configuration required

**SQLite Store**:
```yaml
store:
  type: sqlite
  path: /data/sentinel.db
```
- Persistent storage
- Survives application restarts
- Suitable for production deployments

---

## Validation Rules

The configuration system enforces the following validation rules at load time:

| Rule | Error Message |
|------|---------------|
| Loki URL must be provided | `loki.url is required` |
| Loki query must be provided | `loki.query is required` |
| Poll interval must be at least 1 second | `loki.poll_interval must be at least 1s` |
| Lookback must be >= poll interval | `loki.lookback must be >= poll_interval` |
| Web listen address must be provided | `web.listen is required` |
| Max actions per hour must be non-negative | `remediation.max_actions_per_hour must be >= 0` |
| Store type must be valid | `store.type must be 'memory' or 'sqlite'` |

---

## Default Values

When no configuration file is provided, or when fields are omitted, the following defaults apply:

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

---

## Example Configuration

Below is a complete example configuration for a production deployment:

```yaml
# Kube Sentinel Configuration
# /etc/kube-sentinel/config.yaml

loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"'
  poll_interval: 30s
  lookback: 5m
  # tenant_id: "my-tenant"  # For multi-tenant Loki
  # username: "loki-user"   # For basic auth
  # password: "secret"      # For basic auth

kubernetes:
  in_cluster: true
  # kubeconfig: ~/.kube/config  # For out-of-cluster

web:
  listen: ":8080"

remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 50
  excluded_namespaces:
    - kube-system
    - kube-public
    - monitoring
    - logging

rules_file: /etc/kube-sentinel/rules.yaml

store:
  type: sqlite
  path: /data/sentinel.db
```

---

## Future Extensions

The configuration system is designed to be extensible. The following enhancements are planned or under consideration:

### Environment Variable Overrides

Support for environment variables to override configuration values, following the pattern:

```
SENTINEL_LOKI_URL=http://loki:3100
SENTINEL_WEB_LISTEN=:9090
SENTINEL_REMEDIATION_DRY_RUN=true
```

This enables:
- Container-native configuration via Kubernetes ConfigMaps and Secrets
- 12-factor app compliance
- Sensitive data (passwords) kept out of configuration files

### JSON Schema Validation

A JSON Schema definition for configuration validation providing:
- IDE autocompletion and inline documentation
- Pre-deployment validation in CI/CD pipelines
- Self-documenting configuration format

### Configuration Hot Reload

Support for reloading configuration without restarting the application:
- Watch configuration file for changes
- Gracefully apply non-disruptive changes
- Signal handling (SIGHUP) for manual reload triggers

### Secrets Management Integration

Integration with external secrets management systems:
- Kubernetes Secrets references
- HashiCorp Vault integration
- AWS Secrets Manager / Azure Key Vault support

### Configuration Profiles

Support for named configuration profiles:

```yaml
profiles:
  development:
    remediation:
      dry_run: true
  production:
    remediation:
      dry_run: false
```

### Metrics and Observability Configuration

Extended configuration for metrics export and tracing:

```yaml
observability:
  metrics:
    enabled: true
    path: /metrics
  tracing:
    enabled: true
    endpoint: jaeger:14268
```

---

## See Also

- [Rules Configuration](./03-rules.md) - Defining remediation rules
- [Deployment Guide](./04-deployment.md) - Kubernetes deployment instructions
- [API Reference](./05-api.md) - Web API documentation
