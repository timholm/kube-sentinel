# Main Entry Point Documentation

This document provides comprehensive documentation for the kube-sentinel main entry point, covering the application startup flow, command-line interface, component initialization, error handling pipeline, and graceful shutdown mechanisms.

## Table of Contents

1. [Overview](#overview)
2. [Application Startup Flow](#application-startup-flow)
3. [Command-Line Flags](#command-line-flags)
4. [Component Initialization and Wiring](#component-initialization-and-wiring)
5. [Error Handling Pipeline](#error-handling-pipeline)
6. [Graceful Shutdown Handling](#graceful-shutdown-handling)
7. [Future Improvements](#future-improvements)

---

## Overview

The main entry point for kube-sentinel is located at `cmd/kube-sentinel/main.go`. This file orchestrates the entire application lifecycle, from parsing command-line arguments and loading configuration to initializing all components and managing graceful shutdown.

The application follows a modular architecture where components are initialized in a specific order based on their dependencies, then wired together through function callbacks and shared interfaces.

### Build Information

The application embeds build-time metadata through linker flags:

```go
var (
    version   = "dev"
    commit    = "none"
    buildDate = "unknown"
)
```

These values are typically set during the build process via `-ldflags`:

```bash
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

---

## Application Startup Flow

The startup sequence follows a carefully ordered initialization process:

```
┌─────────────────────────────────────────────────────────────────┐
│                    APPLICATION STARTUP FLOW                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Parse Command-Line Flags                                     │
│         │                                                        │
│         ▼                                                        │
│  2. Handle Version Flag (exit if requested)                      │
│         │                                                        │
│         ▼                                                        │
│  3. Initialize Structured Logger (slog)                          │
│         │                                                        │
│         ▼                                                        │
│  4. Load Configuration File                                      │
│         │                                                        │
│         ▼                                                        │
│  5. Load Rules (file or defaults)                                │
│         │                                                        │
│         ▼                                                        │
│  6. Initialize Rule Engine                                       │
│         │                                                        │
│         ▼                                                        │
│  7. Initialize Memory Store                                      │
│         │                                                        │
│         ▼                                                        │
│  8. Create Kubernetes Client (if remediation enabled)            │
│         │                                                        │
│         ▼                                                        │
│  9. Initialize Remediation Engine                                │
│         │                                                        │
│         ▼                                                        │
│ 10. Initialize Web Server                                        │
│         │                                                        │
│         ▼                                                        │
│ 11. Setup Signal Handlers (SIGINT, SIGTERM)                      │
│         │                                                        │
│         ▼                                                        │
│ 12. Initialize Loki Client                                       │
│         │                                                        │
│         ▼                                                        │
│ 13. Create Error Handler Callback                                │
│         │                                                        │
│         ▼                                                        │
│ 14. Create Loki Poller                                           │
│         │                                                        │
│         ▼                                                        │
│ 15. Start Background Components:                                 │
│     ├── Loki Poller (goroutine)                                  │
│     ├── Web Server (goroutine)                                   │
│     └── Periodic Cleanup (goroutine)                             │
│         │                                                        │
│         ▼                                                        │
│ 16. Wait for Shutdown Signal or Error                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Detailed Startup Steps

1. **Flag Parsing**: Command-line arguments are parsed using the standard `flag` package.

2. **Version Check**: If `--version` is specified, version information is printed and the application exits.

3. **Logger Setup**: A structured JSON logger is configured based on the specified log level.

4. **Configuration Loading**: Configuration is loaded from the specified file path, or defaults are used.

5. **Rules Loading**: Rules are loaded from a file if specified, otherwise default rules are applied.

6. **Component Initialization**: Each component is initialized in dependency order.

7. **Signal Handler Setup**: OS signal handlers are registered for graceful shutdown.

8. **Background Services**: Long-running services are started in separate goroutines.

---

## Command-Line Flags

The application supports the following command-line flags:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `""` | Path to the YAML configuration file |
| `-rules` | string | `""` | Path to rules file (overrides config file setting) |
| `-version` | bool | `false` | Display version information and exit |
| `-log-level` | string | `"info"` | Logging verbosity level |

### Flag Details

#### `-config`

Specifies the path to the main configuration file. If not provided, the application uses default configuration values.

```bash
kube-sentinel -config /etc/kube-sentinel/config.yaml
```

#### `-rules`

Allows overriding the rules file path specified in the configuration. This is useful for testing different rule sets without modifying the main configuration.

```bash
kube-sentinel -config /etc/kube-sentinel/config.yaml -rules /etc/kube-sentinel/rules-test.yaml
```

#### `-version`

Displays version information including the version string, git commit hash, and build date.

```bash
$ kube-sentinel -version
kube-sentinel 1.2.0 (commit: a1b2c3d, built: 2024-01-15T10:30:00Z)
```

#### `-log-level`

Controls the verbosity of log output. Supported values:

| Level | Description |
|-------|-------------|
| `debug` | Verbose debugging information |
| `info` | General operational information (default) |
| `warn` | Warning messages for potentially problematic situations |
| `error` | Error messages for serious problems |

```bash
kube-sentinel -config /etc/kube-sentinel/config.yaml -log-level debug
```

---

## Component Initialization and Wiring

Components are initialized in a specific order to satisfy their dependencies. The following diagram illustrates the component dependencies:

```
┌─────────────────────────────────────────────────────────────────┐
│                   COMPONENT DEPENDENCY GRAPH                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│                      ┌──────────────┐                            │
│                      │    Config    │                            │
│                      └──────────────┘                            │
│                             │                                    │
│           ┌─────────────────┼─────────────────┐                  │
│           │                 │                 │                  │
│           ▼                 ▼                 ▼                  │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│   │    Rules     │  │  K8s Client  │  │ Loki Client  │           │
│   │   Loader     │  │  (optional)  │  │              │           │
│   └──────────────┘  └──────────────┘  └──────────────┘           │
│           │                 │                 │                  │
│           ▼                 │                 │                  │
│   ┌──────────────┐          │                 │                  │
│   │ Rule Engine  │          │                 │                  │
│   └──────────────┘          │                 │                  │
│           │                 │                 │                  │
│           │         ┌──────────────┐          │                  │
│           │         │ Memory Store │          │                  │
│           │         └──────────────┘          │                  │
│           │                 │                 │                  │
│           │    ┌────────────┼────────────┐    │                  │
│           │    │            │            │    │                  │
│           ▼    ▼            ▼            │    │                  │
│   ┌──────────────┐  ┌──────────────┐     │    │                  │
│   │ Remediation  │  │  Web Server  │     │    │                  │
│   │   Engine     │  │              │     │    │                  │
│   └──────────────┘  └──────────────┘     │    │                  │
│           │                 │            │    │                  │
│           └─────────────────┼────────────┘    │                  │
│                             │                 │                  │
│                             ▼                 │                  │
│                   ┌──────────────────┐        │                  │
│                   │  Error Handler   │◄───────┘                  │
│                   │   (callback)     │                           │
│                   └──────────────────┘                           │
│                             │                                    │
│                             ▼                                    │
│                   ┌──────────────────┐                           │
│                   │   Loki Poller    │                           │
│                   └──────────────────┘                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Component Details

#### Configuration (`config.LoadOrDefault`)

The configuration module loads settings from a YAML file or applies sensible defaults. It provides configuration for all other components.

```go
cfg, err := config.LoadOrDefault(*configPath)
```

#### Rules Loader and Engine

Rules are loaded from a YAML file or default rules are used. The rule engine is initialized with the loaded rules and provides pattern matching capabilities.

```go
loader := rules.NewLoader(cfg.RulesFile)
rulesList, err = loader.Load()
ruleEngine, err := rules.NewEngine(rulesList, logger)
```

#### Memory Store

The in-memory store provides persistence for errors and remediation logs. It implements the `store.Store` interface.

```go
dataStore := store.NewMemoryStore()
```

#### Kubernetes Client

The Kubernetes client is conditionally created when remediation is enabled. It supports both in-cluster and out-of-cluster configurations.

```go
func createK8sClient(cfg config.KubernetesConfig) (kubernetes.Interface, error) {
    var restConfig *rest.Config

    if cfg.InCluster {
        restConfig, err = rest.InClusterConfig()
    } else {
        restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
    }

    return kubernetes.NewForConfig(restConfig)
}
```

#### Remediation Engine

The remediation engine handles automated responses to detected errors. It is configured with rate limits, dry-run mode, and namespace exclusions.

```go
remEngine := remediation.NewEngine(k8sClient, dataStore, remediation.EngineConfig{
    Enabled:            cfg.Remediation.Enabled && k8sClient != nil,
    DryRun:             cfg.Remediation.DryRun,
    MaxActionsPerHour:  cfg.Remediation.MaxActionsPerHour,
    ExcludedNamespaces: cfg.Remediation.ExcludedNamespaces,
}, logger)
```

#### Web Server

The web server provides HTTP endpoints and WebSocket support for real-time updates. It integrates with the store, rule engine, and remediation engine.

```go
webServer, err := web.NewServer(cfg.Web.Listen, dataStore, ruleEngine, remEngine, logger)
```

#### Loki Client and Poller

The Loki client connects to Grafana Loki for log queries. The poller periodically fetches new log entries and passes them to the error handler.

```go
lokiClient := loki.NewClient(cfg.Loki.URL, lokiOpts...)
poller := loki.NewPoller(lokiClient, cfg.Loki.Query, cfg.Loki.PollInterval,
                          cfg.Loki.Lookback, errorHandler, loki.WithLogger(logger))
```

---

## Error Handling Pipeline

The error handling pipeline is the core processing flow of kube-sentinel. It processes log entries from Loki, matches them against rules, stores them, and optionally triggers remediation actions.

```
┌─────────────────────────────────────────────────────────────────┐
│                    ERROR HANDLING PIPELINE                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐                                                │
│  │ Loki Poller  │  Periodically polls Loki for new log entries  │
│  └──────┬───────┘                                                │
│         │                                                        │
│         │ []loki.ParsedError                                     │
│         ▼                                                        │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                    Error Handler                          │    │
│  │  (Closure defined in main.go)                            │    │
│  └──────────────────────────────────────────────────────────┘    │
│         │                                                        │
│         │ For each error:                                        │
│         ▼                                                        │
│  ┌──────────────┐                                                │
│  │ Rule Engine  │  Matches error against configured rules        │
│  │   .Match()   │                                                │
│  └──────┬───────┘                                                │
│         │                                                        │
│         │ *rules.MatchedError (nil if no match)                  │
│         ▼                                                        │
│  ┌────────────────────┐                                          │
│  │ Skip if no match   │───────────────────────┐                  │
│  └────────┬───────────┘                       │                  │
│           │                                   │                  │
│           │ Matched                           │ No Match         │
│           ▼                                   ▼                  │
│  ┌──────────────┐                      (continue loop)           │
│  │ Memory Store │  Persists error with metadata                  │
│  │ .SaveError() │                                                │
│  └──────┬───────┘                                                │
│         │                                                        │
│         │ *store.Error                                           │
│         ▼                                                        │
│  ┌──────────────────┐                                            │
│  │   Web Server     │  Broadcasts to WebSocket clients           │
│  │ .BroadcastError()│                                            │
│  └──────┬───────────┘                                            │
│         │                                                        │
│         │ Check if remediation enabled                           │
│         ▼                                                        │
│  ┌─────────────────────────────────────────────────────────┐     │
│  │                Is Remediation Enabled?                   │     │
│  └─────────────────────────────────────────────────────────┘     │
│         │                           │                            │
│         │ Yes                       │ No                         │
│         ▼                           ▼                            │
│  ┌──────────────────┐        (skip remediation)                  │
│  │ Remediation      │                                            │
│  │ Engine           │                                            │
│  │ .ProcessError()  │                                            │
│  └──────┬───────────┘                                            │
│         │                                                        │
│         │ *remediation.Log, error                                │
│         ▼                                                        │
│  ┌──────────────────┐                                            │
│  │   Web Server     │  Broadcasts remediation result             │
│  │.BroadcastRemed() │                                            │
│  └──────┬───────────┘                                            │
│         │                                                        │
│         │ If remediation succeeded                               │
│         ▼                                                        │
│  ┌──────────────┐                                                │
│  │ Memory Store │  Updates error as remediated                   │
│  │.UpdateError()│                                                │
│  └──────────────┘                                                │
│         │                                                        │
│         │ After processing all errors                            │
│         ▼                                                        │
│  ┌──────────────────┐                                            │
│  │   Web Server     │  Broadcasts updated statistics             │
│  │ .BroadcastStats()│                                            │
│  └──────────────────┘                                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Pipeline Implementation

The error handler is implemented as a closure that captures references to all required components:

```go
errorHandler := func(errors []loki.ParsedError) {
    for _, e := range errors {
        // 1. Match against rules
        matched := ruleEngine.Match(e)
        if matched == nil {
            continue
        }

        // 2. Convert to store format and persist
        storeErr := &store.Error{
            ID:          matched.ID,
            Fingerprint: matched.Fingerprint,
            // ... additional fields
        }

        if err := dataStore.SaveError(storeErr); err != nil {
            logger.Error("failed to save error", "error", err)
            continue
        }

        // 3. Broadcast to WebSocket clients
        webServer.BroadcastError(storeErr)

        // 4. Execute remediation if enabled
        if remEngine.IsEnabled() {
            log, err := remEngine.ProcessError(ctx, matched, ruleEngine)
            if err != nil {
                logger.Error("remediation failed", "error", err)
            }
            if log != nil {
                webServer.BroadcastRemediation(log)

                // 5. Update error status if remediation succeeded
                if log.Status == "success" {
                    storeErr.Remediated = true
                    now := time.Now()
                    storeErr.RemediatedAt = &now
                    dataStore.UpdateError(storeErr)
                }
            }
        }
    }

    // 6. Broadcast updated stats after processing batch
    webServer.BroadcastStats()
}
```

### Error Data Transformation

The pipeline transforms data through several stages:

| Stage | Type | Description |
|-------|------|-------------|
| Input | `loki.ParsedError` | Raw parsed error from Loki |
| Rule Matching | `*rules.MatchedError` | Error enriched with rule metadata |
| Storage | `*store.Error` | Persistent error record |
| Remediation | `*remediation.Log` | Record of remediation action |

---

## Graceful Shutdown Handling

The application implements graceful shutdown to ensure clean termination of all components and prevent data loss.

### Shutdown Sequence

```
┌─────────────────────────────────────────────────────────────────┐
│                    GRACEFUL SHUTDOWN FLOW                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │            SIGINT or SIGTERM Received                     │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │         Log "received shutdown signal"                    │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │           Cancel main context                             │    │
│  │           (stops poller and cleanup goroutines)           │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │           Log "shutting down"                             │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │      Create shutdown context with 30-second timeout       │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │           Shutdown Web Server                             │    │
│  │           (gracefully closes HTTP connections)            │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │           Close Data Store                                │    │
│  │           (cleanup any resources)                         │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │           Log "shutdown complete"                         │    │
│  └──────────────────────────────────────────────────────────┘    │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                   Process Exit                            │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation Details

#### Signal Handling

```go
// Create context for graceful shutdown
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Handle shutdown signals
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
    sig := <-sigCh
    logger.Info("received shutdown signal", "signal", sig)
    cancel()
}()
```

#### Shutdown Timeout

The application uses a 30-second timeout for graceful shutdown to prevent hanging:

```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer shutdownCancel()

if err := webServer.Shutdown(shutdownCtx); err != nil {
    logger.Error("web server shutdown error", "error", err)
}
```

#### Component Error Handling

The application monitors for component errors through a dedicated error channel:

```go
errCh := make(chan error, 2)

// Component goroutines report errors to errCh
go func() {
    if err := poller.Start(ctx); err != nil && err != context.Canceled {
        errCh <- fmt.Errorf("poller error: %w", err)
    }
}()

// Wait for shutdown or error
select {
case <-ctx.Done():
    logger.Info("shutting down")
case err := <-errCh:
    logger.Error("component error", "error", err)
    cancel()
}
```

### Periodic Cleanup

The application runs a background goroutine for periodic cleanup of old data:

```go
go func() {
    ticker := time.NewTicker(time.Hour)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Clean up errors older than 7 days
            cutoff := time.Now().Add(-7 * 24 * time.Hour)
            deleted, _ := dataStore.DeleteOldErrors(cutoff)

            // Clean up remediation logs older than 30 days
            logCutoff := time.Now().Add(-30 * 24 * time.Hour)
            logDeleted, _ := dataStore.DeleteOldRemediationLogs(logCutoff)
        }
    }
}()
```

---

## Future Improvements

The following improvements would enhance the main entry point and overall application architecture:

### 1. Better Lifecycle Management

**Component Interface**

Introduce a unified component interface for consistent lifecycle management:

```go
type Component interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health() HealthStatus
}
```

**Component Manager**

Implement a component manager to handle ordered startup and shutdown:

```go
type ComponentManager struct {
    components []Component
    logger     *slog.Logger
}

func (m *ComponentManager) StartAll(ctx context.Context) error {
    for _, c := range m.components {
        if err := c.Start(ctx); err != nil {
            return fmt.Errorf("failed to start %s: %w", c.Name(), err)
        }
    }
    return nil
}

func (m *ComponentManager) StopAll(ctx context.Context) error {
    // Stop in reverse order
    for i := len(m.components) - 1; i >= 0; i-- {
        if err := m.components[i].Stop(ctx); err != nil {
            m.logger.Error("component stop error",
                "component", m.components[i].Name(),
                "error", err)
        }
    }
    return nil
}
```

### 2. Plugin Support

**Plugin Architecture**

Add support for dynamically loaded plugins to extend functionality:

```go
type Plugin interface {
    Name() string
    Version() string
    Initialize(ctx context.Context, config map[string]interface{}) error
    Shutdown(ctx context.Context) error
}

type RulePlugin interface {
    Plugin
    RegisterRuleTypes() []rules.RuleType
}

type RemediationPlugin interface {
    Plugin
    RegisterActions() []remediation.Action
}

type NotificationPlugin interface {
    Plugin
    Notify(ctx context.Context, event Event) error
}
```

**Plugin Loader**

```go
type PluginLoader struct {
    pluginDir string
    plugins   map[string]Plugin
}

func (l *PluginLoader) LoadPlugins(ctx context.Context) error {
    // Discover and load plugins from pluginDir
    // Initialize each plugin with configuration
}
```

### 3. Health Checks and Readiness Probes

**Health Endpoint**

Add comprehensive health checking for Kubernetes deployments:

```go
type HealthChecker struct {
    components []Component
}

func (h *HealthChecker) LivenessCheck() bool {
    // Return true if application is running
    return true
}

func (h *HealthChecker) ReadinessCheck() bool {
    // Check all components are ready
    for _, c := range h.components {
        if c.Health() != HealthStatusReady {
            return false
        }
    }
    return true
}
```

### 4. Configuration Hot Reloading

**File Watcher**

Implement configuration reloading without restart:

```go
type ConfigWatcher struct {
    configPath string
    onChange   func(newConfig *Config)
}

func (w *ConfigWatcher) Watch(ctx context.Context) error {
    // Watch for file changes and trigger reload
}
```

### 5. Metrics and Observability

**Prometheus Metrics**

Add comprehensive metrics for monitoring:

```go
var (
    errorsProcessed = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "kube_sentinel_errors_processed_total",
            Help: "Total number of errors processed",
        },
        []string{"namespace", "priority"},
    )

    remediationsExecuted = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "kube_sentinel_remediations_executed_total",
            Help: "Total number of remediations executed",
        },
        []string{"action", "status"},
    )
)
```

### 6. Distributed Mode

**Cluster Coordination**

Support for running multiple instances with leader election:

```go
type ClusterCoordinator struct {
    leaderElection *leaderelection.LeaderElector
    isLeader       atomic.Bool
}

func (c *ClusterCoordinator) Run(ctx context.Context) error {
    // Only the leader performs remediation actions
    // All instances can serve the web UI
}
```

### 7. Enhanced Error Recovery

**Circuit Breaker Pattern**

Add circuit breakers for external dependencies:

```go
type CircuitBreaker struct {
    failures    int
    threshold   int
    timeout     time.Duration
    lastFailure time.Time
    state       CircuitState
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
    if cb.state == StateOpen {
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.state = StateHalfOpen
        } else {
            return ErrCircuitOpen
        }
    }
    // Execute and track failures
}
```

### 8. Structured Dependency Injection

**Wire or Manual DI**

Replace manual wiring with structured dependency injection:

```go
// Using Google Wire or similar
func InitializeApplication(cfg *Config) (*Application, error) {
    wire.Build(
        NewLogger,
        rules.NewEngine,
        store.NewMemoryStore,
        remediation.NewEngine,
        web.NewServer,
        loki.NewPoller,
        NewApplication,
    )
    return nil, nil
}
```

---

## Summary

The kube-sentinel main entry point provides a well-structured application startup flow with clear component initialization order, comprehensive error handling, and graceful shutdown support. The modular architecture allows for easy testing and future enhancements, while the error handling pipeline efficiently processes log entries through the rule engine, storage, and remediation systems.
