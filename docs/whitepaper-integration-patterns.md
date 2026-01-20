# Kube-Sentinel Integration Patterns

## Technical Whitepaper

**Version:** 1.0
**Last Updated:** January 2026

---

## Table of Contents

1. [Introduction](#introduction)
2. [Architecture Overview](#architecture-overview)
3. [Notification Integrations](#notification-integrations)
   - [Slack Webhook Integration](#slack-webhook-integration)
   - [PagerDuty Integration](#pagerduty-integration)
   - [Email Notification Pattern](#email-notification-pattern)
   - [Generic Webhook Pattern](#generic-webhook-pattern)
4. [Custom Remediation Actions](#custom-remediation-actions)
   - [The Action Interface](#the-action-interface)
   - [Registering Custom Actions](#registering-custom-actions)
   - [exec-script Action](#exec-script-action)
   - [Custom Scaling Logic Example](#custom-scaling-logic-example)
5. [Webhook Extensions](#webhook-extensions)
   - [Outbound Webhooks on Error Detection](#outbound-webhooks-on-error-detection)
   - [Outbound Webhooks on Remediation](#outbound-webhooks-on-remediation)
   - [Payload Format Examples](#payload-format-examples)
6. [API Integration](#api-integration)
   - [REST API Endpoint Reference](#rest-api-endpoint-reference)
   - [Authentication Patterns](#authentication-patterns)
   - [Example curl Commands](#example-curl-commands)
   - [Building Dashboards with the API](#building-dashboards-with-the-api)
7. [Multi-tenant Configurations](#multi-tenant-configurations)
   - [Namespace-based Isolation](#namespace-based-isolation)
   - [Rule Sets per Tenant](#rule-sets-per-tenant)
   - [RBAC Considerations](#rbac-considerations)
8. [CI/CD Integration](#cicd-integration)
   - [Deploying Rule Changes via GitOps](#deploying-rule-changes-via-gitops)
   - [Testing Rules in CI](#testing-rules-in-ci)
   - [Canary Rule Deployments](#canary-rule-deployments)
9. [Appendix](#appendix)

---

## Introduction

Kube-Sentinel is an extensible error detection and automated remediation platform for Kubernetes clusters. This whitepaper provides detailed integration patterns for extending Kube-Sentinel's capabilities, integrating with external notification systems, and embedding it within your operational workflows.

The extensibility model is built around three core concepts:

1. **Actions** - Pluggable remediation behaviors
2. **Webhooks** - Event-driven integrations with external systems
3. **API** - Programmatic access to errors, rules, and remediation history

---

## Architecture Overview

```
+------------------+     +------------------+     +------------------+
|                  |     |                  |     |                  |
|   Loki/Logs      +---->+  Kube-Sentinel   +---->+   Kubernetes     |
|                  |     |                  |     |   API Server     |
+------------------+     +--------+---------+     +------------------+
                                 |
                    +------------+------------+
                    |            |            |
              +-----v----+ +-----v----+ +-----v----+
              |          | |          | |          |
              | Webhooks | |   API    | |  WebUI   |
              |          | |          | |          |
              +----------+ +----------+ +----------+
                    |            |
              +-----v----+ +-----v----+
              |  Slack   | | Grafana  |
              | PagerDuty| | Custom   |
              |  Email   | | Dashboards|
              +----------+ +----------+
```

**Core Components:**

- **Rules Engine** - Matches log patterns and assigns priorities
- **Remediation Engine** - Executes actions with safety controls
- **Store** - Persists errors and remediation history
- **Web Server** - Provides UI and REST API

---

## Notification Integrations

### Slack Webhook Integration

Integrate Kube-Sentinel with Slack to receive real-time error notifications in your team channels.

#### Implementation Pattern

Create a custom notification action that sends formatted messages to Slack:

```go
package notifications

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/remediation"
)

// SlackNotifyAction sends error notifications to Slack
type SlackNotifyAction struct {
    webhookURL string
    client     *http.Client
}

func NewSlackNotifyAction(webhookURL string) *SlackNotifyAction {
    return &SlackNotifyAction{
        webhookURL: webhookURL,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}

func (a *SlackNotifyAction) Name() string {
    return "slack-notify"
}

func (a *SlackNotifyAction) Execute(ctx context.Context, target remediation.Target, params map[string]string) error {
    message := params["message"]
    if message == "" {
        message = fmt.Sprintf("Alert: Error detected in %s", target.String())
    }

    priority := params["priority"]
    color := a.priorityToColor(priority)

    payload := map[string]interface{}{
        "attachments": []map[string]interface{}{
            {
                "color":  color,
                "title":  "Kube-Sentinel Alert",
                "text":   message,
                "fields": []map[string]string{
                    {"title": "Namespace", "value": target.Namespace, "short": "true"},
                    {"title": "Pod", "value": target.Pod, "short": "true"},
                },
                "footer": "Kube-Sentinel",
                "ts":     time.Now().Unix(),
            },
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshaling slack payload: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", a.webhookURL, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := a.client.Do(req)
    if err != nil {
        return fmt.Errorf("sending to slack: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("slack returned status %d", resp.StatusCode)
    }

    return nil
}

func (a *SlackNotifyAction) Validate(params map[string]string) error {
    // Optional validation
    return nil
}

func (a *SlackNotifyAction) priorityToColor(priority string) string {
    switch priority {
    case "P1":
        return "danger"  // Red
    case "P2":
        return "warning" // Orange
    case "P3":
        return "#439FE0" // Blue
    default:
        return "good"    // Green
    }
}
```

#### Configuration

Add a rule that uses the Slack notification action:

```yaml
rules:
  - name: critical-error-notify
    match:
      pattern: "(?i)(panic|fatal|OOMKilled)"
    priority: P1
    remediation:
      action: slack-notify
      params:
        message: "Critical error detected - immediate attention required"
        channel: "#alerts-critical"
      cooldown: 5m
    enabled: true
```

#### Sequence Diagram: Slack Notification Flow

```
+--------+     +---------------+     +--------+     +-------+
| Loki   |     | Kube-Sentinel |     | Action |     | Slack |
+---+----+     +-------+-------+     +----+---+     +---+---+
    |                  |                  |             |
    | Log Event        |                  |             |
    +----------------->|                  |             |
    |                  |                  |             |
    |                  | Match Rule       |             |
    |                  +-----+            |             |
    |                  |     |            |             |
    |                  |<----+            |             |
    |                  |                  |             |
    |                  | Execute Action   |             |
    |                  +----------------->|             |
    |                  |                  |             |
    |                  |                  | POST /webhook|
    |                  |                  +------------>|
    |                  |                  |             |
    |                  |                  |   200 OK    |
    |                  |                  |<------------+
    |                  |                  |             |
    |                  |  Action Complete |             |
    |                  |<-----------------+             |
    |                  |                  |             |
```

---

### PagerDuty Integration

For critical production incidents, integrate with PagerDuty to trigger on-call alerting.

#### Implementation Pattern

```go
package notifications

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/remediation"
)

// PagerDutyAction triggers PagerDuty incidents
type PagerDutyAction struct {
    routingKey string
    client     *http.Client
}

func NewPagerDutyAction(routingKey string) *PagerDutyAction {
    return &PagerDutyAction{
        routingKey: routingKey,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}

func (a *PagerDutyAction) Name() string {
    return "pagerduty-alert"
}

func (a *PagerDutyAction) Execute(ctx context.Context, target remediation.Target, params map[string]string) error {
    severity := params["severity"]
    if severity == "" {
        severity = "error"
    }

    summary := params["summary"]
    if summary == "" {
        summary = fmt.Sprintf("Kubernetes error in %s", target.String())
    }

    payload := map[string]interface{}{
        "routing_key":  a.routingKey,
        "event_action": "trigger",
        "dedup_key":    fmt.Sprintf("kube-sentinel-%s-%s", target.Namespace, target.Pod),
        "payload": map[string]interface{}{
            "summary":   summary,
            "severity":  severity,
            "source":    "kube-sentinel",
            "component": target.Pod,
            "group":     target.Namespace,
            "class":     params["error_type"],
            "custom_details": map[string]string{
                "namespace": target.Namespace,
                "pod":       target.Pod,
                "container": target.Container,
                "rule":      params["rule_name"],
            },
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshaling pagerduty payload: %w", err)
    }

    req, err := http.NewRequestWithContext(
        ctx,
        "POST",
        "https://events.pagerduty.com/v2/enqueue",
        bytes.NewReader(body),
    )
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := a.client.Do(req)
    if err != nil {
        return fmt.Errorf("sending to pagerduty: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusAccepted {
        return fmt.Errorf("pagerduty returned status %d", resp.StatusCode)
    }

    return nil
}

func (a *PagerDutyAction) Validate(params map[string]string) error {
    return nil
}
```

#### Configuration

```yaml
rules:
  - name: production-critical-pagerduty
    match:
      pattern: "(?i)(OOMKilled|panic|fatal)"
      namespaces:
        - production
    priority: P1
    remediation:
      action: pagerduty-alert
      params:
        severity: critical
        summary: "Production critical error requires immediate attention"
        error_type: "application_crash"
      cooldown: 10m
    enabled: true
```

---

### Email Notification Pattern

For organizations requiring email-based alerting, implement an SMTP-based notification action.

#### Implementation Pattern

```go
package notifications

import (
    "context"
    "crypto/tls"
    "fmt"
    "net/smtp"
    "strings"

    "github.com/kube-sentinel/kube-sentinel/internal/remediation"
)

// EmailNotifyAction sends email notifications
type EmailNotifyAction struct {
    smtpHost     string
    smtpPort     int
    username     string
    password     string
    fromAddress  string
    toAddresses  []string
    useTLS       bool
}

type EmailConfig struct {
    SMTPHost    string
    SMTPPort    int
    Username    string
    Password    string
    FromAddress string
    ToAddresses []string
    UseTLS      bool
}

func NewEmailNotifyAction(cfg EmailConfig) *EmailNotifyAction {
    return &EmailNotifyAction{
        smtpHost:    cfg.SMTPHost,
        smtpPort:    cfg.SMTPPort,
        username:    cfg.Username,
        password:    cfg.Password,
        fromAddress: cfg.FromAddress,
        toAddresses: cfg.ToAddresses,
        useTLS:      cfg.UseTLS,
    }
}

func (a *EmailNotifyAction) Name() string {
    return "email-notify"
}

func (a *EmailNotifyAction) Execute(ctx context.Context, target remediation.Target, params map[string]string) error {
    subject := params["subject"]
    if subject == "" {
        subject = fmt.Sprintf("[Kube-Sentinel] Alert: %s/%s", target.Namespace, target.Pod)
    }

    priority := params["priority"]
    errorMessage := params["error_message"]

    body := a.buildEmailBody(target, priority, errorMessage)

    // Build recipients list
    recipients := a.toAddresses
    if override := params["recipients"]; override != "" {
        recipients = strings.Split(override, ",")
    }

    message := fmt.Sprintf(
        "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
        a.fromAddress,
        strings.Join(recipients, ","),
        subject,
        body,
    )

    addr := fmt.Sprintf("%s:%d", a.smtpHost, a.smtpPort)
    auth := smtp.PlainAuth("", a.username, a.password, a.smtpHost)

    if a.useTLS {
        return a.sendWithTLS(addr, auth, recipients, []byte(message))
    }

    return smtp.SendMail(addr, auth, a.fromAddress, recipients, []byte(message))
}

func (a *EmailNotifyAction) sendWithTLS(addr string, auth smtp.Auth, recipients []string, message []byte) error {
    tlsConfig := &tls.Config{
        ServerName: a.smtpHost,
    }

    conn, err := tls.Dial("tcp", addr, tlsConfig)
    if err != nil {
        return fmt.Errorf("TLS dial failed: %w", err)
    }
    defer conn.Close()

    client, err := smtp.NewClient(conn, a.smtpHost)
    if err != nil {
        return fmt.Errorf("SMTP client creation failed: %w", err)
    }
    defer client.Close()

    if err = client.Auth(auth); err != nil {
        return fmt.Errorf("SMTP auth failed: %w", err)
    }

    if err = client.Mail(a.fromAddress); err != nil {
        return fmt.Errorf("SMTP MAIL command failed: %w", err)
    }

    for _, recipient := range recipients {
        if err = client.Rcpt(recipient); err != nil {
            return fmt.Errorf("SMTP RCPT command failed: %w", err)
        }
    }

    writer, err := client.Data()
    if err != nil {
        return fmt.Errorf("SMTP DATA command failed: %w", err)
    }

    _, err = writer.Write(message)
    if err != nil {
        return fmt.Errorf("writing message failed: %w", err)
    }

    err = writer.Close()
    if err != nil {
        return fmt.Errorf("closing writer failed: %w", err)
    }

    return client.Quit()
}

func (a *EmailNotifyAction) buildEmailBody(target remediation.Target, priority, errorMessage string) string {
    return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; }
        .alert { padding: 20px; border-radius: 5px; margin: 20px 0; }
        .critical { background-color: #fee2e2; border: 1px solid #ef4444; }
        .high { background-color: #fff7ed; border: 1px solid #f97316; }
        .medium { background-color: #fef9c3; border: 1px solid #eab308; }
        .low { background-color: #dbeafe; border: 1px solid #3b82f6; }
        .details { background-color: #f3f4f6; padding: 15px; border-radius: 5px; }
    </style>
</head>
<body>
    <h2>Kube-Sentinel Alert</h2>
    <div class="alert %s">
        <strong>Priority:</strong> %s
    </div>
    <div class="details">
        <p><strong>Namespace:</strong> %s</p>
        <p><strong>Pod:</strong> %s</p>
        <p><strong>Container:</strong> %s</p>
        <p><strong>Error:</strong> %s</p>
    </div>
    <p>This alert was generated by Kube-Sentinel.</p>
</body>
</html>`,
        a.priorityClass(priority),
        priority,
        target.Namespace,
        target.Pod,
        target.Container,
        errorMessage,
    )
}

func (a *EmailNotifyAction) priorityClass(priority string) string {
    switch priority {
    case "P1":
        return "critical"
    case "P2":
        return "high"
    case "P3":
        return "medium"
    default:
        return "low"
    }
}

func (a *EmailNotifyAction) Validate(params map[string]string) error {
    return nil
}
```

---

### Generic Webhook Pattern

For maximum flexibility, implement a generic webhook action that supports any HTTP endpoint.

#### Implementation Pattern

```go
package notifications

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "text/template"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/remediation"
)

// GenericWebhookAction sends HTTP requests to arbitrary endpoints
type GenericWebhookAction struct {
    client *http.Client
}

func NewGenericWebhookAction() *GenericWebhookAction {
    return &GenericWebhookAction{
        client: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
}

func (a *GenericWebhookAction) Name() string {
    return "webhook"
}

func (a *GenericWebhookAction) Execute(ctx context.Context, target remediation.Target, params map[string]string) error {
    url := params["url"]
    if url == "" {
        return fmt.Errorf("url parameter is required")
    }

    method := params["method"]
    if method == "" {
        method = "POST"
    }

    // Build payload from template or use default
    payload, err := a.buildPayload(target, params)
    if err != nil {
        return fmt.Errorf("building payload: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }

    // Set headers
    contentType := params["content_type"]
    if contentType == "" {
        contentType = "application/json"
    }
    req.Header.Set("Content-Type", contentType)

    // Add authorization header if provided
    if auth := params["authorization"]; auth != "" {
        req.Header.Set("Authorization", auth)
    }

    // Add custom headers (format: "Header1:Value1,Header2:Value2")
    if headers := params["headers"]; headers != "" {
        for _, h := range splitHeaders(headers) {
            req.Header.Set(h.Key, h.Value)
        }
    }

    resp, err := a.client.Do(req)
    if err != nil {
        return fmt.Errorf("sending webhook: %w", err)
    }
    defer resp.Body.Close()

    // Check response
    if resp.StatusCode >= 400 {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
    }

    return nil
}

func (a *GenericWebhookAction) buildPayload(target remediation.Target, params map[string]string) ([]byte, error) {
    // Check for custom payload template
    if tmpl := params["payload_template"]; tmpl != "" {
        t, err := template.New("payload").Parse(tmpl)
        if err != nil {
            return nil, fmt.Errorf("parsing template: %w", err)
        }

        data := map[string]interface{}{
            "Target":    target,
            "Namespace": target.Namespace,
            "Pod":       target.Pod,
            "Container": target.Container,
            "Params":    params,
        }

        var buf bytes.Buffer
        if err := t.Execute(&buf, data); err != nil {
            return nil, fmt.Errorf("executing template: %w", err)
        }
        return buf.Bytes(), nil
    }

    // Default JSON payload
    payload := map[string]interface{}{
        "source":    "kube-sentinel",
        "timestamp": time.Now().UTC().Format(time.RFC3339),
        "target": map[string]string{
            "namespace": target.Namespace,
            "pod":       target.Pod,
            "container": target.Container,
        },
        "event": map[string]string{
            "type":     params["event_type"],
            "priority": params["priority"],
            "message":  params["message"],
            "rule":     params["rule_name"],
        },
    }

    return json.Marshal(payload)
}

func (a *GenericWebhookAction) Validate(params map[string]string) error {
    if params["url"] == "" {
        return fmt.Errorf("url parameter is required")
    }
    return nil
}

type header struct {
    Key   string
    Value string
}

func splitHeaders(s string) []header {
    var headers []header
    // Implementation: parse "Key1:Value1,Key2:Value2" format
    // ...
    return headers
}
```

#### Configuration

```yaml
rules:
  - name: external-webhook-notification
    match:
      pattern: "(?i)critical.*error"
    priority: P1
    remediation:
      action: webhook
      params:
        url: "https://api.example.com/alerts"
        method: "POST"
        authorization: "Bearer ${WEBHOOK_TOKEN}"
        headers: "X-Source:kube-sentinel,X-Environment:production"
        event_type: "kubernetes_error"
      cooldown: 2m
    enabled: true
```

---

## Custom Remediation Actions

### The Action Interface

Kube-Sentinel's remediation system is built around the `Action` interface, which provides a clean contract for implementing custom remediation behaviors.

#### Interface Definition

From `/Users/tim/kube-sentinel/internal/remediation/actions.go`:

```go
// Action represents a remediation action that can be executed
type Action interface {
    Name() string
    Execute(ctx context.Context, target Target, params map[string]string) error
    Validate(params map[string]string) error
}

// Target identifies the Kubernetes resource to act on
type Target struct {
    Namespace  string
    Pod        string
    Deployment string
    Container  string
}
```

#### Interface Methods

| Method | Purpose |
|--------|---------|
| `Name()` | Returns the unique identifier for the action, used in rule configurations |
| `Execute()` | Performs the remediation action; receives context, target, and parameters |
| `Validate()` | Validates parameters before execution; called during rule loading |

#### Built-in Actions

Kube-Sentinel ships with these built-in actions:

| Action | Description |
|--------|-------------|
| `restart-pod` | Deletes the pod to trigger a restart via the controller |
| `scale-up` | Increases deployment replica count |
| `scale-down` | Decreases deployment replica count |
| `rollback` | Rolls back deployment to previous revision |
| `delete-stuck-pods` | Force-deletes pods stuck in Terminating state |
| `none` | No-op action for alert-only rules |

---

### Registering Custom Actions

Custom actions are registered with the remediation engine using the `RegisterAction` method.

#### Registration Flow

```go
// From /Users/tim/kube-sentinel/internal/remediation/engine.go

// RegisterAction registers a remediation action
func (e *Engine) RegisterAction(action Action) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.actions[action.Name()] = action
}
```

#### Integration Example

In your application startup code:

```go
package main

import (
    "log/slog"
    "os"

    "github.com/kube-sentinel/kube-sentinel/internal/remediation"
    "github.com/kube-sentinel/kube-sentinel/internal/store"
    "k8s.io/client-go/kubernetes"
)

func main() {
    // Initialize Kubernetes client
    client, err := kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatal(err)
    }

    // Initialize store
    memStore := store.NewMemoryStore()

    // Create remediation engine
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    engine := remediation.NewEngine(client, memStore, remediation.EngineConfig{
        Enabled:           true,
        DryRun:            false,
        MaxActionsPerHour: 50,
        ExcludedNamespaces: []string{"kube-system"},
    }, logger)

    // Register custom actions
    engine.RegisterAction(NewSlackNotifyAction(os.Getenv("SLACK_WEBHOOK_URL")))
    engine.RegisterAction(NewPagerDutyAction(os.Getenv("PAGERDUTY_ROUTING_KEY")))
    engine.RegisterAction(NewGenericWebhookAction())
    engine.RegisterAction(NewExecScriptAction("/scripts"))

    // ... continue with application startup
}
```

---

### exec-script Action

The `exec-script` action enables running shell commands as remediation actions, providing maximum flexibility for custom remediation logic.

#### Implementation

```go
package remediation

import (
    "bytes"
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

// ExecScriptAction executes shell scripts for remediation
type ExecScriptAction struct {
    scriptsDir string
    timeout    time.Duration
    allowedEnv []string
}

type ExecScriptConfig struct {
    ScriptsDir string
    Timeout    time.Duration
    AllowedEnv []string
}

func NewExecScriptAction(cfg ExecScriptConfig) *ExecScriptAction {
    timeout := cfg.Timeout
    if timeout == 0 {
        timeout = 30 * time.Second
    }

    return &ExecScriptAction{
        scriptsDir: cfg.ScriptsDir,
        timeout:    timeout,
        allowedEnv: cfg.AllowedEnv,
    }
}

func (a *ExecScriptAction) Name() string {
    return "exec-script"
}

func (a *ExecScriptAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    scriptName := params["script"]
    if scriptName == "" {
        return fmt.Errorf("script parameter is required")
    }

    // Sanitize script name to prevent path traversal
    scriptName = filepath.Base(scriptName)
    scriptPath := filepath.Join(a.scriptsDir, scriptName)

    // Verify script exists and is executable
    info, err := os.Stat(scriptPath)
    if err != nil {
        return fmt.Errorf("script not found: %s", scriptName)
    }
    if info.IsDir() {
        return fmt.Errorf("script path is a directory: %s", scriptName)
    }

    // Create context with timeout
    ctx, cancel := context.WithTimeout(ctx, a.timeout)
    defer cancel()

    // Build command
    cmd := exec.CommandContext(ctx, scriptPath)

    // Set environment variables
    env := os.Environ()
    env = append(env,
        fmt.Sprintf("KUBE_SENTINEL_NAMESPACE=%s", target.Namespace),
        fmt.Sprintf("KUBE_SENTINEL_POD=%s", target.Pod),
        fmt.Sprintf("KUBE_SENTINEL_CONTAINER=%s", target.Container),
        fmt.Sprintf("KUBE_SENTINEL_DEPLOYMENT=%s", target.Deployment),
    )

    // Add custom parameters as environment variables
    for key, value := range params {
        if key != "script" {
            envKey := fmt.Sprintf("KUBE_SENTINEL_PARAM_%s", strings.ToUpper(key))
            env = append(env, fmt.Sprintf("%s=%s", envKey, value))
        }
    }
    cmd.Env = env

    // Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // Execute
    if err := cmd.Run(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("script execution timed out after %v", a.timeout)
        }
        return fmt.Errorf("script execution failed: %w\nstderr: %s", err, stderr.String())
    }

    return nil
}

func (a *ExecScriptAction) Validate(params map[string]string) error {
    if params["script"] == "" {
        return fmt.Errorf("script parameter is required")
    }

    // Validate script exists during rule loading
    scriptName := filepath.Base(params["script"])
    scriptPath := filepath.Join(a.scriptsDir, scriptName)

    if _, err := os.Stat(scriptPath); err != nil {
        return fmt.Errorf("script not found: %s", scriptName)
    }

    return nil
}
```

#### Example Script

Create a remediation script at `/scripts/custom-restart.sh`:

```bash
#!/bin/bash
set -euo pipefail

echo "Executing custom restart for ${KUBE_SENTINEL_NAMESPACE}/${KUBE_SENTINEL_POD}"

# Custom pre-restart logic
kubectl annotate pod "${KUBE_SENTINEL_POD}" \
    -n "${KUBE_SENTINEL_NAMESPACE}" \
    kube-sentinel.io/last-remediation="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --overwrite

# Optionally notify external systems
if [ -n "${KUBE_SENTINEL_PARAM_NOTIFY_URL:-}" ]; then
    curl -X POST "${KUBE_SENTINEL_PARAM_NOTIFY_URL}" \
        -H "Content-Type: application/json" \
        -d "{\"pod\": \"${KUBE_SENTINEL_POD}\", \"action\": \"restart\"}"
fi

# Perform the restart
kubectl delete pod "${KUBE_SENTINEL_POD}" -n "${KUBE_SENTINEL_NAMESPACE}" --grace-period=0

echo "Restart initiated successfully"
```

#### Configuration

```yaml
rules:
  - name: custom-script-remediation
    match:
      pattern: "custom-app.*error.*retry-limit"
    priority: P2
    remediation:
      action: exec-script
      params:
        script: "custom-restart.sh"
        notify_url: "https://api.example.com/events"
      cooldown: 5m
    enabled: true
```

---

### Custom Scaling Logic Example

Implement intelligent scaling based on custom metrics or business logic.

#### Implementation

```go
package remediation

import (
    "context"
    "fmt"
    "strconv"

    appsv1 "k8s.io/api/apps/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/kubernetes"
)

// AdaptiveScaleAction implements intelligent scaling based on error patterns
type AdaptiveScaleAction struct {
    client       kubernetes.Interface
    metricsClient MetricsClient  // Custom metrics interface
}

type MetricsClient interface {
    GetPodCPUUsage(namespace, pod string) (float64, error)
    GetPodMemoryUsage(namespace, pod string) (float64, error)
    GetQueueDepth(namespace, deployment string) (int, error)
}

func NewAdaptiveScaleAction(client kubernetes.Interface, metrics MetricsClient) *AdaptiveScaleAction {
    return &AdaptiveScaleAction{
        client:       client,
        metricsClient: metrics,
    }
}

func (a *AdaptiveScaleAction) Name() string {
    return "adaptive-scale"
}

func (a *AdaptiveScaleAction) Execute(ctx context.Context, target Target, params map[string]string) error {
    deployment, err := a.getDeployment(ctx, target)
    if err != nil {
        return err
    }

    currentReplicas := int32(1)
    if deployment.Spec.Replicas != nil {
        currentReplicas = *deployment.Spec.Replicas
    }

    // Calculate desired replicas based on strategy
    strategy := params["strategy"]
    var newReplicas int32

    switch strategy {
    case "queue-based":
        newReplicas, err = a.calculateQueueBasedScale(ctx, target, currentReplicas, params)
    case "resource-based":
        newReplicas, err = a.calculateResourceBasedScale(ctx, target, currentReplicas, params)
    case "error-rate":
        newReplicas, err = a.calculateErrorRateScale(currentReplicas, params)
    default:
        // Default: simple increment
        newReplicas = currentReplicas + 1
    }

    if err != nil {
        return fmt.Errorf("calculating scale: %w", err)
    }

    // Apply constraints
    newReplicas = a.applyConstraints(newReplicas, params)

    // Skip if no change needed
    if newReplicas == currentReplicas {
        return nil
    }

    // Apply the scale change
    patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, newReplicas)
    _, err = a.client.AppsV1().Deployments(target.Namespace).Patch(
        ctx,
        deployment.Name,
        types.MergePatchType,
        []byte(patch),
        metav1.PatchOptions{},
    )
    if err != nil {
        return fmt.Errorf("scaling deployment: %w", err)
    }

    return nil
}

func (a *AdaptiveScaleAction) calculateQueueBasedScale(
    ctx context.Context,
    target Target,
    current int32,
    params map[string]string,
) (int32, error) {
    queueDepth, err := a.metricsClient.GetQueueDepth(target.Namespace, target.Deployment)
    if err != nil {
        return current, err
    }

    // Target: 100 messages per replica
    targetPerReplica := 100
    if val, ok := params["target_per_replica"]; ok {
        if parsed, err := strconv.Atoi(val); err == nil {
            targetPerReplica = parsed
        }
    }

    desired := int32(queueDepth / targetPerReplica)
    if desired < 1 {
        desired = 1
    }

    return desired, nil
}

func (a *AdaptiveScaleAction) calculateResourceBasedScale(
    ctx context.Context,
    target Target,
    current int32,
    params map[string]string,
) (int32, error) {
    cpuUsage, _ := a.metricsClient.GetPodCPUUsage(target.Namespace, target.Pod)
    memUsage, _ := a.metricsClient.GetPodMemoryUsage(target.Namespace, target.Pod)

    // Scale up if either CPU or memory exceeds 80%
    threshold := 0.8
    if val, ok := params["threshold"]; ok {
        if parsed, err := strconv.ParseFloat(val, 64); err == nil {
            threshold = parsed
        }
    }

    if cpuUsage > threshold || memUsage > threshold {
        // Scale up by 50%
        return int32(float64(current) * 1.5), nil
    }

    return current, nil
}

func (a *AdaptiveScaleAction) calculateErrorRateScale(current int32, params map[string]string) (int32, error) {
    // Simple error-triggered scale: increase by configured increment
    increment := int32(1)
    if val, ok := params["increment"]; ok {
        if parsed, err := strconv.ParseInt(val, 10, 32); err == nil {
            increment = int32(parsed)
        }
    }

    return current + increment, nil
}

func (a *AdaptiveScaleAction) applyConstraints(replicas int32, params map[string]string) int32 {
    // Apply minimum
    minReplicas := int32(1)
    if val, ok := params["min_replicas"]; ok {
        if parsed, err := strconv.ParseInt(val, 10, 32); err == nil {
            minReplicas = int32(parsed)
        }
    }
    if replicas < minReplicas {
        replicas = minReplicas
    }

    // Apply maximum
    if val, ok := params["max_replicas"]; ok {
        if parsed, err := strconv.ParseInt(val, 10, 32); err == nil {
            maxReplicas := int32(parsed)
            if replicas > maxReplicas {
                replicas = maxReplicas
            }
        }
    }

    return replicas
}

func (a *AdaptiveScaleAction) getDeployment(ctx context.Context, target Target) (*appsv1.Deployment, error) {
    if target.Deployment != "" {
        return a.client.AppsV1().Deployments(target.Namespace).Get(
            ctx, target.Deployment, metav1.GetOptions{},
        )
    }
    return nil, fmt.Errorf("deployment name required for scaling")
}

func (a *AdaptiveScaleAction) Validate(params map[string]string) error {
    if strategy := params["strategy"]; strategy != "" {
        validStrategies := map[string]bool{
            "queue-based":    true,
            "resource-based": true,
            "error-rate":     true,
        }
        if !validStrategies[strategy] {
            return fmt.Errorf("invalid strategy: %s", strategy)
        }
    }
    return nil
}
```

#### Configuration

```yaml
rules:
  - name: queue-overflow-scale
    match:
      pattern: "(?i)queue.*overflow|message.*backlog"
      labels:
        app.kubernetes.io/component: "worker"
    priority: P2
    remediation:
      action: adaptive-scale
      params:
        strategy: "queue-based"
        target_per_replica: "100"
        min_replicas: "2"
        max_replicas: "20"
      cooldown: 5m
    enabled: true
```

---

## Webhook Extensions

### Outbound Webhooks on Error Detection

Configure Kube-Sentinel to send webhook notifications when errors are detected.

#### Event Emission Pattern

Extend the error processing pipeline to emit webhook events:

```go
package webhook

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/rules"
    "github.com/kube-sentinel/kube-sentinel/internal/store"
)

// ErrorWebhookEmitter sends webhooks when errors are detected
type ErrorWebhookEmitter struct {
    mu          sync.RWMutex
    endpoints   []WebhookEndpoint
    client      *http.Client
    retryPolicy RetryPolicy
}

type WebhookEndpoint struct {
    URL           string
    Headers       map[string]string
    EventTypes    []string  // "error.detected", "error.resolved", etc.
    Namespaces    []string  // Filter by namespace (empty = all)
    MinPriority   rules.Priority
}

type RetryPolicy struct {
    MaxRetries     int
    InitialBackoff time.Duration
    MaxBackoff     time.Duration
}

func NewErrorWebhookEmitter(endpoints []WebhookEndpoint) *ErrorWebhookEmitter {
    return &ErrorWebhookEmitter{
        endpoints: endpoints,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
        retryPolicy: RetryPolicy{
            MaxRetries:     3,
            InitialBackoff: 1 * time.Second,
            MaxBackoff:     30 * time.Second,
        },
    }
}

// OnErrorDetected is called when a new error is detected
func (e *ErrorWebhookEmitter) OnErrorDetected(ctx context.Context, err *store.Error, rule *rules.Rule) {
    event := ErrorEvent{
        Type:      "error.detected",
        Timestamp: time.Now().UTC(),
        Error:     err,
        Rule: &RuleInfo{
            Name:     rule.Name,
            Priority: rule.Priority,
        },
    }

    e.emit(ctx, event)
}

// OnErrorResolved is called when an error is marked as resolved
func (e *ErrorWebhookEmitter) OnErrorResolved(ctx context.Context, err *store.Error) {
    event := ErrorEvent{
        Type:      "error.resolved",
        Timestamp: time.Now().UTC(),
        Error:     err,
    }

    e.emit(ctx, event)
}

func (e *ErrorWebhookEmitter) emit(ctx context.Context, event ErrorEvent) {
    e.mu.RLock()
    endpoints := e.endpoints
    e.mu.RUnlock()

    for _, endpoint := range endpoints {
        if e.shouldSend(endpoint, event) {
            go e.sendWithRetry(ctx, endpoint, event)
        }
    }
}

func (e *ErrorWebhookEmitter) shouldSend(endpoint WebhookEndpoint, event ErrorEvent) bool {
    // Check event type filter
    if len(endpoint.EventTypes) > 0 {
        found := false
        for _, t := range endpoint.EventTypes {
            if t == event.Type {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }

    // Check namespace filter
    if len(endpoint.Namespaces) > 0 && event.Error != nil {
        found := false
        for _, ns := range endpoint.Namespaces {
            if ns == event.Error.Namespace {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }

    // Check priority filter
    if endpoint.MinPriority != "" && event.Rule != nil {
        if event.Rule.Priority.Weight() > endpoint.MinPriority.Weight() {
            return false
        }
    }

    return true
}

func (e *ErrorWebhookEmitter) sendWithRetry(ctx context.Context, endpoint WebhookEndpoint, event ErrorEvent) {
    backoff := e.retryPolicy.InitialBackoff

    for attempt := 0; attempt <= e.retryPolicy.MaxRetries; attempt++ {
        if attempt > 0 {
            select {
            case <-ctx.Done():
                return
            case <-time.After(backoff):
                backoff *= 2
                if backoff > e.retryPolicy.MaxBackoff {
                    backoff = e.retryPolicy.MaxBackoff
                }
            }
        }

        if err := e.send(ctx, endpoint, event); err == nil {
            return
        }
    }
}

func (e *ErrorWebhookEmitter) send(ctx context.Context, endpoint WebhookEndpoint, event ErrorEvent) error {
    payload, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("marshaling event: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewReader(payload))
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("User-Agent", "Kube-Sentinel/1.0")

    for key, value := range endpoint.Headers {
        req.Header.Set(key, value)
    }

    resp, err := e.client.Do(req)
    if err != nil {
        return fmt.Errorf("sending request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return fmt.Errorf("webhook returned status %d", resp.StatusCode)
    }

    return nil
}

// Event types
type ErrorEvent struct {
    Type      string      `json:"type"`
    Timestamp time.Time   `json:"timestamp"`
    Error     *store.Error `json:"error,omitempty"`
    Rule      *RuleInfo   `json:"rule,omitempty"`
}

type RuleInfo struct {
    Name     string         `json:"name"`
    Priority rules.Priority `json:"priority"`
}
```

---

### Outbound Webhooks on Remediation

Send webhooks when remediation actions are executed.

#### Implementation

```go
package webhook

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/store"
)

// RemediationWebhookEmitter sends webhooks for remediation events
type RemediationWebhookEmitter struct {
    endpoints []WebhookEndpoint
    client    *http.Client
}

func NewRemediationWebhookEmitter(endpoints []WebhookEndpoint) *RemediationWebhookEmitter {
    return &RemediationWebhookEmitter{
        endpoints: endpoints,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}

// OnRemediationStarted is called before a remediation action begins
func (e *RemediationWebhookEmitter) OnRemediationStarted(ctx context.Context, log *store.RemediationLog) {
    event := RemediationEvent{
        Type:      "remediation.started",
        Timestamp: time.Now().UTC(),
        Log:       log,
    }
    e.emit(ctx, event)
}

// OnRemediationCompleted is called after a remediation action finishes
func (e *RemediationWebhookEmitter) OnRemediationCompleted(ctx context.Context, log *store.RemediationLog) {
    eventType := "remediation.success"
    if log.Status == "failed" {
        eventType = "remediation.failed"
    } else if log.Status == "skipped" {
        eventType = "remediation.skipped"
    }

    event := RemediationEvent{
        Type:      eventType,
        Timestamp: time.Now().UTC(),
        Log:       log,
    }
    e.emit(ctx, event)
}

func (e *RemediationWebhookEmitter) emit(ctx context.Context, event RemediationEvent) {
    for _, endpoint := range e.endpoints {
        go e.send(ctx, endpoint, event)
    }
}

func (e *RemediationWebhookEmitter) send(ctx context.Context, endpoint WebhookEndpoint, event RemediationEvent) error {
    payload, err := json.Marshal(event)
    if err != nil {
        return err
    }

    req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewReader(payload))
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", "application/json")
    for key, value := range endpoint.Headers {
        req.Header.Set(key, value)
    }

    resp, err := e.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return nil
}

type RemediationEvent struct {
    Type      string                 `json:"type"`
    Timestamp time.Time              `json:"timestamp"`
    Log       *store.RemediationLog  `json:"remediation"`
}
```

---

### Payload Format Examples

#### Error Detection Webhook Payload

```json
{
  "type": "error.detected",
  "timestamp": "2026-01-19T14:30:00Z",
  "error": {
    "id": "abc123def456",
    "fingerprint": "hash-of-error-signature",
    "timestamp": "2026-01-19T14:29:55Z",
    "namespace": "production",
    "pod": "api-server-7b9c4d6f8-xk2m3",
    "container": "api",
    "message": "panic: runtime error: index out of range",
    "labels": {
      "app": "api-server",
      "environment": "production",
      "team": "backend"
    },
    "priority": "P1",
    "rule_name": "panic",
    "count": 1,
    "first_seen": "2026-01-19T14:29:55Z",
    "last_seen": "2026-01-19T14:29:55Z"
  },
  "rule": {
    "name": "panic",
    "priority": "P1"
  }
}
```

#### Remediation Webhook Payload

```json
{
  "type": "remediation.success",
  "timestamp": "2026-01-19T14:30:05Z",
  "remediation": {
    "id": "rem-789xyz",
    "error_id": "abc123def456",
    "timestamp": "2026-01-19T14:30:02Z",
    "action": "restart-pod",
    "target": "production/api-server-7b9c4d6f8-xk2m3",
    "status": "success",
    "message": "action executed successfully",
    "dry_run": false
  }
}
```

#### Webhook Configuration

```yaml
# webhook-config.yaml
webhooks:
  error_detection:
    - url: "https://hooks.slack.com/services/xxx/yyy/zzz"
      headers:
        Content-Type: "application/json"
      event_types:
        - "error.detected"
      min_priority: "P2"

    - url: "https://api.pagerduty.com/v2/enqueue"
      headers:
        Authorization: "Token token=your-api-key"
        Content-Type: "application/json"
      event_types:
        - "error.detected"
      namespaces:
        - "production"
      min_priority: "P1"

  remediation:
    - url: "https://api.example.com/audit/remediation"
      headers:
        Authorization: "Bearer ${AUDIT_API_TOKEN}"
      event_types:
        - "remediation.started"
        - "remediation.success"
        - "remediation.failed"
```

---

## API Integration

### REST API Endpoint Reference

Kube-Sentinel exposes a RESTful API for programmatic access to errors, rules, and remediation history.

#### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/errors` | List errors with pagination and filtering |
| `GET` | `/api/errors/{id}` | Get error details and remediation history |
| `GET` | `/api/rules` | List all configured rules |
| `POST` | `/api/rules/test` | Test a pattern against sample text |
| `GET` | `/api/remediations` | List remediation logs |
| `GET` | `/api/stats` | Get aggregated statistics |
| `GET` | `/api/settings` | Get current settings |
| `POST` | `/api/settings` | Update settings (enable/disable, dry-run) |
| `GET` | `/health` | Health check endpoint |
| `GET` | `/ready` | Readiness probe endpoint |
| `GET` | `/ws` | WebSocket for real-time updates |

#### Query Parameters

**GET /api/errors**

| Parameter | Type | Description |
|-----------|------|-------------|
| `page` | int | Page number (default: 1) |
| `pageSize` | int | Items per page (default: 20, max: 100) |
| `namespace` | string | Filter by namespace |
| `pod` | string | Filter by pod name |
| `search` | string | Search in error message |
| `priority` | string | Filter by priority (P1, P2, P3, P4) |

**GET /api/remediations**

| Parameter | Type | Description |
|-----------|------|-------------|
| `page` | int | Page number (default: 1) |
| `pageSize` | int | Items per page (default: 50, max: 100) |

---

### Authentication Patterns

#### API Key Authentication

Implement API key authentication for secure access:

```go
package web

import (
    "crypto/subtle"
    "net/http"
    "strings"
)

// APIKeyMiddleware validates API keys
func APIKeyMiddleware(validKeys []string) func(http.Handler) http.Handler {
    keySet := make(map[string]bool)
    for _, key := range validKeys {
        keySet[key] = true
    }

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip authentication for health checks
            if r.URL.Path == "/health" || r.URL.Path == "/ready" {
                next.ServeHTTP(w, r)
                return
            }

            // Check Authorization header
            auth := r.Header.Get("Authorization")
            if auth == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }

            // Support "Bearer <token>" format
            parts := strings.SplitN(auth, " ", 2)
            if len(parts) != 2 || parts[0] != "Bearer" {
                http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
                return
            }

            token := parts[1]
            if !keySet[token] {
                http.Error(w, "Invalid API key", http.StatusUnauthorized)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

#### JWT Authentication

For more sophisticated authentication:

```go
package web

import (
    "context"
    "fmt"
    "net/http"
    "strings"

    "github.com/golang-jwt/jwt/v5"
)

type JWTConfig struct {
    Secret     []byte
    Issuer     string
    Audience   string
}

func JWTMiddleware(cfg JWTConfig) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip authentication for health checks
            if r.URL.Path == "/health" || r.URL.Path == "/ready" {
                next.ServeHTTP(w, r)
                return
            }

            auth := r.Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }

            tokenString := strings.TrimPrefix(auth, "Bearer ")

            token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
                if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, fmt.Errorf("unexpected signing method")
                }
                return cfg.Secret, nil
            })

            if err != nil || !token.Valid {
                http.Error(w, "Invalid token", http.StatusUnauthorized)
                return
            }

            claims, ok := token.Claims.(jwt.MapClaims)
            if !ok {
                http.Error(w, "Invalid claims", http.StatusUnauthorized)
                return
            }

            // Add claims to context
            ctx := context.WithValue(r.Context(), "claims", claims)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

---

### Example curl Commands

#### List Recent Errors

```bash
# Get the first page of errors
curl -X GET "http://localhost:8080/api/errors" \
  -H "Authorization: Bearer your-api-key"

# Filter by namespace and priority
curl -X GET "http://localhost:8080/api/errors?namespace=production&priority=P1&pageSize=50" \
  -H "Authorization: Bearer your-api-key"

# Search for specific error messages
curl -X GET "http://localhost:8080/api/errors?search=OOMKilled" \
  -H "Authorization: Bearer your-api-key"
```

#### Get Error Details

```bash
curl -X GET "http://localhost:8080/api/errors/abc123def456" \
  -H "Authorization: Bearer your-api-key"
```

#### List Rules

```bash
curl -X GET "http://localhost:8080/api/rules" \
  -H "Authorization: Bearer your-api-key"
```

#### Test Rule Pattern

```bash
curl -X POST "http://localhost:8080/api/rules/test" \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "pattern": "(?i)OOMKilled|Out of memory",
    "sample": "Container was OOMKilled due to memory limit exceeded"
  }'
```

#### Get Statistics

```bash
curl -X GET "http://localhost:8080/api/stats" \
  -H "Authorization: Bearer your-api-key"
```

#### Update Settings

```bash
# Enable remediation
curl -X POST "http://localhost:8080/api/settings" \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "dry_run": false
  }'

# Enable dry-run mode
curl -X POST "http://localhost:8080/api/settings" \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "dry_run": true
  }'
```

#### Get Remediation History

```bash
curl -X GET "http://localhost:8080/api/remediations?page=1&pageSize=100" \
  -H "Authorization: Bearer your-api-key"
```

---

### Building Dashboards with the API

#### Grafana Data Source

Configure Grafana to query Kube-Sentinel's API using the JSON data source plugin.

```yaml
# grafana-datasource.yaml
apiVersion: 1
datasources:
  - name: KubeSentinel
    type: marcusolsson-json-datasource
    url: http://kube-sentinel.monitoring:8080
    access: proxy
    jsonData:
      httpHeaderName1: "Authorization"
    secureJsonData:
      httpHeaderValue1: "Bearer ${KUBE_SENTINEL_API_KEY}"
```

#### React Dashboard Component

```typescript
// ErrorDashboard.tsx
import React, { useState, useEffect } from 'react';

interface Error {
  id: string;
  timestamp: string;
  namespace: string;
  pod: string;
  message: string;
  priority: string;
  count: number;
}

interface Stats {
  total_errors: number;
  errors_by_priority: Record<string, number>;
  errors_by_namespace: Record<string, number>;
  remediations_today: number;
}

const API_BASE = 'http://localhost:8080';
const API_KEY = process.env.REACT_APP_KUBE_SENTINEL_API_KEY;

const headers = {
  'Authorization': `Bearer ${API_KEY}`,
  'Content-Type': 'application/json',
};

export const ErrorDashboard: React.FC = () => {
  const [errors, setErrors] = useState<Error[]>([]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [errorsRes, statsRes] = await Promise.all([
          fetch(`${API_BASE}/api/errors?pageSize=20`, { headers }),
          fetch(`${API_BASE}/api/stats`, { headers }),
        ]);

        const errorsData = await errorsRes.json();
        const statsData = await statsRes.json();

        setErrors(errorsData.errors || []);
        setStats(statsData);
      } catch (error) {
        console.error('Failed to fetch data:', error);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 30000); // Refresh every 30s
    return () => clearInterval(interval);
  }, []);

  // WebSocket for real-time updates
  useEffect(() => {
    const ws = new WebSocket(`ws://localhost:8080/ws`);

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      if (data.type === 'error') {
        setErrors(prev => [data.error, ...prev.slice(0, 19)]);
      }
    };

    return () => ws.close();
  }, []);

  if (loading) return <div>Loading...</div>;

  return (
    <div className="dashboard">
      <div className="stats-grid">
        <StatCard title="Total Errors" value={stats?.total_errors || 0} />
        <StatCard title="Critical (P1)" value={stats?.errors_by_priority['P1'] || 0} color="red" />
        <StatCard title="High (P2)" value={stats?.errors_by_priority['P2'] || 0} color="orange" />
        <StatCard title="Remediations Today" value={stats?.remediations_today || 0} color="green" />
      </div>

      <div className="error-list">
        <h2>Recent Errors</h2>
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Priority</th>
              <th>Namespace</th>
              <th>Pod</th>
              <th>Message</th>
              <th>Count</th>
            </tr>
          </thead>
          <tbody>
            {errors.map(error => (
              <tr key={error.id} className={`priority-${error.priority.toLowerCase()}`}>
                <td>{new Date(error.timestamp).toLocaleString()}</td>
                <td><PriorityBadge priority={error.priority} /></td>
                <td>{error.namespace}</td>
                <td>{error.pod}</td>
                <td className="message">{error.message}</td>
                <td>{error.count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

const StatCard: React.FC<{ title: string; value: number; color?: string }> = ({ title, value, color }) => (
  <div className={`stat-card ${color || ''}`}>
    <div className="stat-value">{value}</div>
    <div className="stat-title">{title}</div>
  </div>
);

const PriorityBadge: React.FC<{ priority: string }> = ({ priority }) => (
  <span className={`badge priority-${priority.toLowerCase()}`}>{priority}</span>
);
```

---

## Multi-tenant Configurations

### Namespace-based Isolation

Configure Kube-Sentinel to operate in multi-tenant environments with namespace isolation.

#### Architecture

```
+------------------+
|  Kube-Sentinel   |
|  (Central)       |
+--------+---------+
         |
    +----+----+----+----+
    |         |         |
+---v---+ +---v---+ +---v---+
|Tenant A| |Tenant B| |Tenant C|
|ns: team-a| |ns: team-b| |ns: team-c|
+---------+ +---------+ +---------+
```

#### Namespace-scoped Rules

```yaml
# rules-tenant-a.yaml
rules:
  - name: team-a-oom-alert
    match:
      pattern: "OOMKilled"
      namespaces:
        - team-a-prod
        - team-a-staging
    priority: P1
    remediation:
      action: slack-notify
      params:
        channel: "#team-a-alerts"
      cooldown: 5m
    enabled: true

  - name: team-a-restart-crashloop
    match:
      pattern: "CrashLoopBackOff"
      namespaces:
        - team-a-prod
    priority: P1
    remediation:
      action: restart-pod
      cooldown: 10m
    enabled: true
```

#### Configuration Structure

```yaml
# multi-tenant-config.yaml
tenants:
  team-a:
    namespaces:
      - team-a-prod
      - team-a-staging
      - team-a-dev
    rules_file: /etc/kube-sentinel/rules/team-a.yaml
    notification_channel: "#team-a-alerts"

  team-b:
    namespaces:
      - team-b-prod
      - team-b-staging
    rules_file: /etc/kube-sentinel/rules/team-b.yaml
    notification_channel: "#team-b-alerts"

  platform:
    namespaces:
      - kube-system
      - monitoring
      - logging
    rules_file: /etc/kube-sentinel/rules/platform.yaml
    notification_channel: "#platform-alerts"
```

---

### Rule Sets per Tenant

Implement tenant-specific rule loading.

```go
package rules

import (
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "gopkg.in/yaml.v3"
)

// TenantRuleManager manages rules for multiple tenants
type TenantRuleManager struct {
    mu       sync.RWMutex
    tenants  map[string]*TenantConfig
    rulesets map[string][]Rule
}

type TenantConfig struct {
    Name               string   `yaml:"name"`
    Namespaces         []string `yaml:"namespaces"`
    RulesFile          string   `yaml:"rules_file"`
    NotificationChannel string  `yaml:"notification_channel"`
}

func NewTenantRuleManager() *TenantRuleManager {
    return &TenantRuleManager{
        tenants:  make(map[string]*TenantConfig),
        rulesets: make(map[string][]Rule),
    }
}

// LoadTenant loads rules for a specific tenant
func (m *TenantRuleManager) LoadTenant(cfg *TenantConfig) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    rules, err := loadRulesFromFile(cfg.RulesFile)
    if err != nil {
        return fmt.Errorf("loading rules for tenant %s: %w", cfg.Name, err)
    }

    // Validate and enhance rules with tenant namespaces
    for i := range rules {
        if len(rules[i].Match.Namespaces) == 0 {
            rules[i].Match.Namespaces = cfg.Namespaces
        }
    }

    m.tenants[cfg.Name] = cfg
    m.rulesets[cfg.Name] = rules
    return nil
}

// GetRulesForNamespace returns rules applicable to a namespace
func (m *TenantRuleManager) GetRulesForNamespace(namespace string) []Rule {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var applicable []Rule
    for tenantName, cfg := range m.tenants {
        if containsNamespace(cfg.Namespaces, namespace) {
            applicable = append(applicable, m.rulesets[tenantName]...)
        }
    }
    return applicable
}

// GetTenantForNamespace returns the tenant owning a namespace
func (m *TenantRuleManager) GetTenantForNamespace(namespace string) *TenantConfig {
    m.mu.RLock()
    defer m.mu.RUnlock()

    for _, cfg := range m.tenants {
        if containsNamespace(cfg.Namespaces, namespace) {
            return cfg
        }
    }
    return nil
}

func loadRulesFromFile(path string) ([]Rule, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    var config RulesConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    return config.Rules, nil
}

func containsNamespace(namespaces []string, ns string) bool {
    for _, n := range namespaces {
        if n == ns {
            return true
        }
    }
    return false
}
```

---

### RBAC Considerations

Configure Kubernetes RBAC for multi-tenant Kube-Sentinel deployments.

#### ClusterRole for Kube-Sentinel

```yaml
# rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-sentinel
rules:
  # Read access to pods for monitoring
  - apiGroups: [""]
    resources: ["pods", "pods/log"]
    verbs: ["get", "list", "watch"]

  # Write access to pods for remediation (delete for restart)
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["delete"]

  # Read and update deployments for scaling/rollback
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch", "patch", "update"]

  # Read events for additional context
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
```

#### Namespace-scoped Roles for Tenant Isolation

```yaml
# tenant-role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kube-sentinel-tenant
  namespace: team-a-prod
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-sentinel-tenant
  namespace: team-a-prod
subjects:
  - kind: ServiceAccount
    name: kube-sentinel
    namespace: monitoring
roleRef:
  kind: Role
  name: kube-sentinel-tenant
  apiGroup: rbac.authorization.k8s.io
```

#### Multi-tenant ServiceAccount Pattern

```yaml
# For complete isolation, use per-tenant ServiceAccounts
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-sentinel-team-a
  namespace: team-a-prod
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-sentinel-team-a
  namespace: team-a-prod
subjects:
  - kind: ServiceAccount
    name: kube-sentinel-team-a
    namespace: team-a-prod
roleRef:
  kind: Role
  name: kube-sentinel-tenant
  apiGroup: rbac.authorization.k8s.io
```

---

## CI/CD Integration

### Deploying Rule Changes via GitOps

Manage Kube-Sentinel rules using GitOps workflows.

#### Repository Structure

```
kube-sentinel-config/
├── base/
│   ├── kustomization.yaml
│   ├── deployment.yaml
│   ├── service.yaml
│   └── configmap-rules.yaml
├── overlays/
│   ├── development/
│   │   ├── kustomization.yaml
│   │   └── rules-patch.yaml
│   ├── staging/
│   │   ├── kustomization.yaml
│   │   └── rules-patch.yaml
│   └── production/
│       ├── kustomization.yaml
│       └── rules-patch.yaml
└── rules/
    ├── common.yaml
    ├── development.yaml
    ├── staging.yaml
    └── production.yaml
```

#### Kustomization Configuration

```yaml
# base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
  - service.yaml
  - configmap-rules.yaml

configMapGenerator:
  - name: kube-sentinel-rules
    files:
      - rules.yaml=../rules/common.yaml
```

```yaml
# overlays/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../base

patchesStrategicMerge:
  - rules-patch.yaml

configMapGenerator:
  - name: kube-sentinel-rules
    behavior: merge
    files:
      - rules.yaml=../../rules/production.yaml
```

#### ArgoCD Application

```yaml
# argocd-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kube-sentinel
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/org/kube-sentinel-config
    targetRevision: main
    path: overlays/production
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

#### Flux Kustomization

```yaml
# flux-kustomization.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: kube-sentinel
  namespace: flux-system
spec:
  interval: 5m
  path: ./overlays/production
  prune: true
  sourceRef:
    kind: GitRepository
    name: kube-sentinel-config
  healthChecks:
    - apiVersion: apps/v1
      kind: Deployment
      name: kube-sentinel
      namespace: monitoring
```

---

### Testing Rules in CI

Implement automated testing for rule configurations.

#### Rule Validator Script

```go
// cmd/rule-validator/main.go
package main

import (
    "fmt"
    "os"
    "regexp"

    "github.com/kube-sentinel/kube-sentinel/internal/rules"
    "gopkg.in/yaml.v3"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Println("Usage: rule-validator <rules-file>")
        os.Exit(1)
    }

    rulesFile := os.Args[1]

    data, err := os.ReadFile(rulesFile)
    if err != nil {
        fmt.Printf("Error reading file: %v\n", err)
        os.Exit(1)
    }

    var config rules.RulesConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        fmt.Printf("Error parsing YAML: %v\n", err)
        os.Exit(1)
    }

    hasErrors := false
    for i, rule := range config.Rules {
        fmt.Printf("Validating rule %d: %s\n", i+1, rule.Name)

        if err := rule.Validate(); err != nil {
            fmt.Printf("  ERROR: %v\n", err)
            hasErrors = true
            continue
        }

        // Validate regex patterns compile
        if rule.Match.Pattern != "" {
            if _, err := regexp.Compile(rule.Match.Pattern); err != nil {
                fmt.Printf("  ERROR: Invalid regex pattern: %v\n", err)
                hasErrors = true
                continue
            }
        }

        fmt.Printf("  OK\n")
    }

    if hasErrors {
        os.Exit(1)
    }

    fmt.Printf("\nAll %d rules validated successfully\n", len(config.Rules))
}
```

#### GitHub Actions Workflow

```yaml
# .github/workflows/validate-rules.yaml
name: Validate Kube-Sentinel Rules

on:
  pull_request:
    paths:
      - 'rules/**'
      - 'overlays/**/rules*.yaml'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Build validator
        run: go build -o rule-validator ./cmd/rule-validator

      - name: Validate common rules
        run: ./rule-validator rules/common.yaml

      - name: Validate production rules
        run: ./rule-validator rules/production.yaml

      - name: Validate staging rules
        run: ./rule-validator rules/staging.yaml

      - name: Validate development rules
        run: ./rule-validator rules/development.yaml

  test-patterns:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run pattern tests
        run: |
          # Test that patterns match expected samples
          ./scripts/test-patterns.sh rules/production.yaml test/samples/
```

#### Pattern Test Script

```bash
#!/bin/bash
# scripts/test-patterns.sh

RULES_FILE=$1
SAMPLES_DIR=$2

echo "Testing patterns from ${RULES_FILE} against samples in ${SAMPLES_DIR}"

# Extract patterns and test against sample files
yq -r '.rules[] | select(.match.pattern != null) | "\(.name)|\(.match.pattern)"' "${RULES_FILE}" | while IFS='|' read -r name pattern; do
    echo "Testing rule: ${name}"

    # Look for corresponding sample file
    SAMPLE_FILE="${SAMPLES_DIR}/${name}.txt"
    if [ -f "${SAMPLE_FILE}" ]; then
        if grep -qE "${pattern}" "${SAMPLE_FILE}"; then
            echo "  PASS: Pattern matches sample"
        else
            echo "  FAIL: Pattern does not match sample"
            exit 1
        fi
    else
        echo "  SKIP: No sample file found"
    fi
done
```

---

### Canary Rule Deployments

Implement gradual rollout of new rules to minimize risk.

#### Canary Rule Configuration

```yaml
# rules/canary.yaml
rules:
  # Canary rule - enabled only for specific namespace prefix
  - name: new-oom-handler-canary
    match:
      pattern: "OOMKilled.*memory limit"
      namespaces:
        - canary-*
    priority: P1
    remediation:
      action: adaptive-scale
      params:
        strategy: "resource-based"
        max_replicas: "10"
      cooldown: 5m
    enabled: true
    metadata:
      canary: true
      canary_percentage: 10
      promotion_criteria:
        min_samples: 100
        max_error_rate: 0.01
```

#### Canary Deployment Controller

```go
package canary

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/kube-sentinel/kube-sentinel/internal/rules"
)

// CanaryController manages gradual rule rollouts
type CanaryController struct {
    mu              sync.RWMutex
    canaryRules     map[string]*CanaryRule
    promotedRules   map[string]bool
    metricsCollector MetricsCollector
}

type CanaryRule struct {
    Rule                rules.Rule
    CanaryPercentage    int
    MinSamples          int
    MaxErrorRate        float64
    StartTime           time.Time
    SamplesProcessed    int
    ErrorsEncountered   int
}

type MetricsCollector interface {
    RecordSample(ruleName string, success bool)
    GetMetrics(ruleName string) (samples int, errors int)
}

func NewCanaryController(metrics MetricsCollector) *CanaryController {
    return &CanaryController{
        canaryRules:      make(map[string]*CanaryRule),
        promotedRules:    make(map[string]bool),
        metricsCollector: metrics,
    }
}

// RegisterCanaryRule adds a rule for canary evaluation
func (c *CanaryController) RegisterCanaryRule(rule rules.Rule, percentage int, minSamples int, maxErrorRate float64) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.canaryRules[rule.Name] = &CanaryRule{
        Rule:             rule,
        CanaryPercentage: percentage,
        MinSamples:       minSamples,
        MaxErrorRate:     maxErrorRate,
        StartTime:        time.Now(),
    }
}

// ShouldApplyRule determines if a rule should be applied based on canary status
func (c *CanaryController) ShouldApplyRule(ruleName string) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()

    // Check if rule is promoted
    if c.promotedRules[ruleName] {
        return true
    }

    // Check if rule is in canary
    canary, ok := c.canaryRules[ruleName]
    if !ok {
        return true // Not a canary rule, apply normally
    }

    // Apply based on percentage (simple hash-based selection)
    return shouldIncludeInCanary(canary.CanaryPercentage)
}

// RecordResult records the outcome of a rule application
func (c *CanaryController) RecordResult(ruleName string, success bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    canary, ok := c.canaryRules[ruleName]
    if !ok {
        return
    }

    canary.SamplesProcessed++
    if !success {
        canary.ErrorsEncountered++
    }

    c.metricsCollector.RecordSample(ruleName, success)
}

// EvaluatePromotion checks if canary rules are ready for promotion
func (c *CanaryController) EvaluatePromotion(ctx context.Context) []string {
    c.mu.Lock()
    defer c.mu.Unlock()

    var promoted []string

    for name, canary := range c.canaryRules {
        if canary.SamplesProcessed < canary.MinSamples {
            continue
        }

        errorRate := float64(canary.ErrorsEncountered) / float64(canary.SamplesProcessed)
        if errorRate <= canary.MaxErrorRate {
            // Promote the rule
            c.promotedRules[name] = true
            delete(c.canaryRules, name)
            promoted = append(promoted, name)
        }
    }

    return promoted
}

// GetCanaryStatus returns the current status of all canary rules
func (c *CanaryController) GetCanaryStatus() map[string]CanaryStatus {
    c.mu.RLock()
    defer c.mu.RUnlock()

    status := make(map[string]CanaryStatus)
    for name, canary := range c.canaryRules {
        errorRate := float64(0)
        if canary.SamplesProcessed > 0 {
            errorRate = float64(canary.ErrorsEncountered) / float64(canary.SamplesProcessed)
        }

        status[name] = CanaryStatus{
            RuleName:        name,
            Percentage:      canary.CanaryPercentage,
            SamplesProcessed: canary.SamplesProcessed,
            ErrorRate:       errorRate,
            StartTime:       canary.StartTime,
            ReadyForPromotion: canary.SamplesProcessed >= canary.MinSamples && errorRate <= canary.MaxErrorRate,
        }
    }

    return status
}

type CanaryStatus struct {
    RuleName          string
    Percentage        int
    SamplesProcessed  int
    ErrorRate         float64
    StartTime         time.Time
    ReadyForPromotion bool
}

func shouldIncludeInCanary(percentage int) bool {
    // Simple random selection based on percentage
    // In production, use consistent hashing for deterministic selection
    return randomInt(100) < percentage
}

func randomInt(max int) int {
    // Implementation
    return 0
}
```

#### Canary Promotion Workflow

```yaml
# .github/workflows/canary-promotion.yaml
name: Canary Rule Promotion

on:
  schedule:
    - cron: '0 */4 * * *'  # Every 4 hours
  workflow_dispatch:

jobs:
  evaluate-canaries:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Get canary status
        id: canary-status
        run: |
          STATUS=$(curl -s -H "Authorization: Bearer ${{ secrets.KUBE_SENTINEL_API_KEY }}" \
            https://kube-sentinel.example.com/api/canary/status)
          echo "status=${STATUS}" >> $GITHUB_OUTPUT

      - name: Check promotion readiness
        run: |
          echo "${{ steps.canary-status.outputs.status }}" | jq '.[] | select(.ready_for_promotion == true)'

      - name: Create promotion PR
        if: contains(steps.canary-status.outputs.status, 'ready_for_promotion')
        run: |
          # Script to create PR promoting canary rules to production
          ./scripts/create-promotion-pr.sh
```

---

## Appendix

### A. Complete Example: Full Integration Setup

```yaml
# complete-config.yaml
loki:
  url: http://loki.monitoring:3100
  query: '{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception)"'
  poll_interval: 30s
  lookback: 5m

kubernetes:
  in_cluster: true

web:
  listen: ":8080"

remediation:
  enabled: true
  dry_run: false
  max_actions_per_hour: 100
  excluded_namespaces:
    - kube-system
    - kube-public

rules_file: /etc/kube-sentinel/rules.yaml

store:
  type: sqlite
  path: /data/sentinel.db

# Custom extensions
extensions:
  notifications:
    slack:
      webhook_url: "${SLACK_WEBHOOK_URL}"
      default_channel: "#alerts"
    pagerduty:
      routing_key: "${PAGERDUTY_ROUTING_KEY}"
    email:
      smtp_host: smtp.example.com
      smtp_port: 587
      username: "${SMTP_USERNAME}"
      password: "${SMTP_PASSWORD}"
      from_address: "kube-sentinel@example.com"

  webhooks:
    endpoints:
      - url: "https://api.example.com/events"
        headers:
          Authorization: "Bearer ${WEBHOOK_TOKEN}"
        event_types:
          - "error.detected"
          - "remediation.completed"

  scripts:
    directory: /scripts
    timeout: 30s
```

### B. Troubleshooting Guide

| Issue | Possible Cause | Solution |
|-------|----------------|----------|
| Actions not executing | Remediation disabled | Check `enabled: true` in config |
| Cooldown blocking actions | Recent action on same target | Wait for cooldown or clear with API |
| Webhook failures | Network/auth issues | Check endpoint URL and credentials |
| Rules not matching | Pattern syntax error | Test pattern with `/api/rules/test` |
| Permission denied | Missing RBAC permissions | Verify ClusterRole/RoleBindings |

### C. Performance Tuning

| Setting | Default | Recommended (High Volume) |
|---------|---------|---------------------------|
| `poll_interval` | 30s | 10s |
| `max_actions_per_hour` | 50 | 200 |
| `store.type` | memory | sqlite |
| `web.timeout` | 15s | 30s |

### D. Security Best Practices

1. **API Authentication**: Always enable API key or JWT authentication in production
2. **RBAC**: Use namespace-scoped roles for tenant isolation
3. **Secrets**: Store credentials in Kubernetes Secrets or external vault
4. **Network Policies**: Restrict Kube-Sentinel network access
5. **Audit Logging**: Enable remediation logging for compliance

---

## References

- Kube-Sentinel GitHub Repository
- Kubernetes RBAC Documentation
- Loki Query Language (LogQL) Reference
- ArgoCD GitOps Guide
- Flux CD Documentation

---

*This whitepaper is maintained by the Kube-Sentinel team. For questions or contributions, please open an issue on the GitHub repository.*
