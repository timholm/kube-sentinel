# Remaining Web UI Templates

This document provides comprehensive documentation for the remaining web UI templates in kube-sentinel: the Rules page, Remediation History page, and Settings page. These templates work together to provide operators with visibility into the system's configuration, behavior, and operational history.

## Table of Contents

- [Overview](#overview)
- [Rules Template](#rules-template)
  - [Page Structure](#page-structure)
  - [Pattern Tester](#pattern-tester)
  - [Rules Table](#rules-table)
  - [User Interactions](#user-interactions)
- [History Template](#history-template)
  - [Remediation Audit Log](#remediation-audit-log)
  - [Pagination System](#pagination-system)
  - [Status Indicators](#status-indicators)
- [Settings Template](#settings-template)
  - [Runtime Configuration](#runtime-configuration)
  - [Current Status Display](#current-status-display)
  - [Configuration Notes](#configuration-notes)
- [API Integration](#api-integration)
- [Template Data Contracts](#template-data-contracts)
- [Future Improvements](#future-improvements)

---

## Overview

The kube-sentinel web UI consists of several pages that provide operators with comprehensive visibility and control over the system. The three templates documented here serve distinct purposes:

| Template | Purpose | Primary Use Case |
|----------|---------|------------------|
| `rules.html` | Display configured detection rules and test patterns | Understanding what kube-sentinel monitors and how it matches issues |
| `history.html` | Show remediation action history with audit trail | Reviewing past actions, debugging, and compliance auditing |
| `settings.html` | Configure runtime behavior and view system status | Adjusting operational parameters without restarts |

All templates extend the base layout template and use Tailwind CSS for styling, providing a consistent user experience across the application.

---

## Rules Template

The Rules template (`rules.html`) provides operators with visibility into the configured detection rules and offers an interactive pattern testing tool for validating regex patterns before adding them to configuration.

### Page Structure

The Rules page is organized into three main sections:

1. **Header**: Displays the page title and a count of configured rules
2. **Pattern Tester**: Interactive tool for testing regex patterns against sample text
3. **Rules Table**: Comprehensive list of all configured rules with their properties

### Pattern Tester

The Pattern Tester is an interactive component that allows operators to validate regular expression patterns before adding them to the rules configuration. This helps prevent configuration errors and reduces the feedback loop when developing new detection rules.

#### Interface Elements

| Element | Description |
|---------|-------------|
| Pattern Input | Text field for entering a regex pattern (e.g., `CrashLoopBackOff\|OOMKilled`) |
| Sample Text Input | Text field for entering sample log messages to test against |
| Test Button | Triggers the pattern matching test via API call |
| Result Display | Shows match status or error messages |

#### How It Works

1. The operator enters a regex pattern in the Pattern field
2. Sample log text is entered in the Sample Text field
3. Clicking "Test Pattern" sends a POST request to `/api/rules/test`
4. The API compiles the regex and tests it against the sample text
5. Results are displayed inline:
   - **Green "Pattern matches!"**: The regex successfully matched the sample text
   - **Yellow "No match"**: The regex is valid but did not match the sample
   - **Red "Invalid regex"**: The regex pattern has syntax errors

#### Example Patterns

| Pattern | Matches |
|---------|---------|
| `CrashLoopBackOff` | Simple string match for crash loop detection |
| `CrashLoopBackOff\|OOMKilled` | Alternation matching either condition |
| `Error:.*timeout` | Error messages containing "timeout" |
| `(?i)connection refused` | Case-insensitive connection error detection |

### Rules Table

The Rules Table displays all configured detection rules in a structured format. Each row represents a single rule with its configuration details.

#### Table Columns

| Column | Description |
|--------|-------------|
| **Name** | The rule's unique identifier |
| **Pattern** | The regex pattern used for matching (truncated to 50 characters for display) |
| **Priority** | Rule priority level with color-coded badges |
| **Action** | The remediation action to execute when triggered, or "None (alert only)" |
| **Cooldown** | Minimum time between consecutive executions of this rule |
| **Status** | Enabled or Disabled indicator |

#### Pattern Display

- Patterns are displayed in a monospace font within a code block
- Long patterns are truncated to 50 characters for readability
- If keywords are configured in addition to the pattern, they are displayed below the pattern

#### Priority Badges

Priority levels are displayed with color-coded badges to provide visual differentiation:

- **Critical**: High-visibility styling for urgent rules
- **Warning**: Medium priority rules
- **Info**: Lower priority informational rules

#### Action Types

The Action column displays one of the following:

- **None (alert only)**: Rule triggers alerts but takes no automated action
- **restart-pod**: Restarts the affected pod
- **scale-up**: Increases deployment replica count
- **scale-down**: Decreases deployment replica count
- **rollback**: Reverts deployment to previous revision
- **delete-stuck-pods**: Force-deletes pods stuck in Terminating state

### User Interactions

#### Viewing Rule Details

Users can scan the rules table to understand:
- What patterns kube-sentinel is monitoring
- What actions will be taken when patterns match
- Which rules are currently active

#### Testing New Patterns

Before modifying `rules.yaml`, operators should:

1. Use the Pattern Tester to validate regex syntax
2. Test against representative sample log messages
3. Verify both positive matches and negative non-matches
4. Document the pattern's intended behavior

#### Configuration Modification

The Rules page displays a prominent info box explaining that rules are loaded from the configuration file. To modify rules:

1. Edit the `rules.yaml` configuration file
2. Validate syntax and patterns
3. Restart the kube-sentinel application
4. Verify the new rules appear in the UI

---

## History Template

The History template (`history.html`) provides a comprehensive audit log of all remediation actions taken by kube-sentinel. This is essential for operational visibility, debugging, and compliance requirements.

### Remediation Audit Log

The audit log displays a chronological list of all remediation actions, including both successful executions and failures.

#### Table Columns

| Column | Description |
|--------|-------------|
| **Timestamp** | When the action was executed, formatted for readability |
| **Action** | The type of remediation action performed |
| **Target** | The Kubernetes resource that was the target of the action |
| **Status** | Outcome indicator: Success, Failed, or Skipped |
| **Message** | Additional context or error details (truncated for long messages) |

#### Dry Run Indication

When remediation is running in dry-run mode, actions are logged but not actually executed. These entries are clearly marked with a "dry run" label in yellow text beneath the action name.

### Pagination System

The History page implements server-side pagination to handle large audit logs efficiently.

#### Pagination Features

- **Item Count Display**: Shows "Showing X to Y of Z" to indicate current position
- **Previous Button**: Navigate to earlier entries (hidden on first page)
- **Next Button**: Navigate to more recent entries (hidden on last page)
- **Query Parameter**: Page number is passed via `?page=N` URL parameter

#### Pagination Calculation

The template uses several helper functions to calculate pagination boundaries:

```
Start: ((Page - 1) * PageSize) + 1
End: min(Page * PageSize, Total)
```

### Status Indicators

Remediation action outcomes are displayed with color-coded status badges:

| Status | Badge Color | Meaning |
|--------|-------------|---------|
| **Success** | Green | Action executed successfully |
| **Failed** | Red | Action encountered an error during execution |
| **Skipped** | Gray | Action was not executed (e.g., cooldown period, dry-run mode) |

### User Interactions

#### Reviewing Recent Actions

Operators can quickly scan the history page to:
- Verify remediation actions are being triggered appropriately
- Identify patterns in failures or successes
- Confirm dry-run mode is working as expected

#### Debugging Issues

When investigating problems:
1. Filter by timestamp to find relevant time periods
2. Look for Failed status entries with error messages
3. Correlate actions with cluster events or incidents
4. Identify if cooldown periods are preventing necessary actions

#### Compliance and Auditing

For compliance purposes, the history log provides:
- Immutable record of all automated actions
- Timestamps for forensic analysis
- Target identification for impact assessment
- Status tracking for success/failure rates

---

## Settings Template

The Settings template (`settings.html`) provides runtime configuration capabilities for kube-sentinel's remediation behavior without requiring application restarts.

### Runtime Configuration

The Settings page offers two primary toggle controls that can be modified at runtime:

#### Enable Remediation Toggle

| Setting | Description |
|---------|-------------|
| **Label** | Enable Remediation |
| **Help Text** | Allow automatic remediation actions |
| **Effect** | When disabled, no remediation actions will be executed regardless of other settings |
| **Styling** | Toggle turns blue when enabled |

#### Dry Run Mode Toggle

| Setting | Description |
|---------|-------------|
| **Label** | Dry Run Mode |
| **Help Text** | Log actions without executing them |
| **Effect** | When enabled, actions are logged but not actually performed |
| **Styling** | Toggle turns yellow when enabled |

### Saving Settings

Settings changes are persisted via the API:

1. User modifies toggle states
2. Clicking "Save Settings" sends a POST to `/api/settings`
3. The request body contains: `{ "enabled": boolean, "dry_run": boolean }`
4. Success or failure feedback is displayed inline
5. Success messages auto-dismiss after 3 seconds

### Current Status Display

The status section provides real-time visibility into the current operational state:

#### Remediation Status

Displays the combined effect of the Enable and Dry Run settings:

| State | Badge | Meaning |
|-------|-------|---------|
| **Active** | Green | Remediation enabled and executing real actions |
| **Dry Run** | Yellow | Remediation enabled but only logging actions |
| **Disabled** | Gray | Remediation completely disabled |

#### Actions This Hour

Displays a count of remediation actions executed in the current hour, useful for:
- Monitoring action frequency
- Detecting potential action storms
- Capacity planning and rate limiting decisions

### Configuration Notes

The Settings page includes an informational section explaining which settings are managed through configuration files versus the UI:

**UI-Configurable Settings:**
- Remediation enable/disable
- Dry run mode toggle

**File-Configurable Settings (require restart):**
- Loki URL and connection parameters
- Poll interval for log queries
- Excluded namespaces list
- Detection rules and patterns

---

## API Integration

The templates interact with the kube-sentinel backend through REST API endpoints:

### Rules API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/rules/test` | POST | Test a regex pattern against sample text |

**Request Body:**
```json
{
  "pattern": "CrashLoopBackOff|OOMKilled",
  "sample": "Error: CrashLoopBackOff for container app"
}
```

**Response:**
```json
{
  "matches": true,
  "error": null
}
```

### Settings API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/settings` | POST | Update runtime settings |

**Request Body:**
```json
{
  "enabled": true,
  "dry_run": false
}
```

### History Data

History data is passed to the template during server-side rendering rather than fetched via API. The page parameter is included in the URL query string for pagination.

---

## Template Data Contracts

Each template expects specific data structures to be passed during rendering:

### Rules Template Data

```go
type RulesPageData struct {
    Rules []Rule  // List of all configured rules
}

type Rule struct {
    Name        string
    Match       MatchConfig
    Priority    string
    Remediation *RemediationConfig
    Enabled     bool
}
```

### History Template Data

```go
type HistoryPageData struct {
    Logs     []RemediationLog  // Current page of logs
    Total    int               // Total number of log entries
    Page     int               // Current page number (1-indexed)
    PageSize int               // Number of entries per page
}

type RemediationLog struct {
    Timestamp time.Time
    Action    string
    Target    string
    Status    string  // "success", "failed", "skipped"
    Message   string
    DryRun    bool
}
```

### Settings Template Data

```go
type SettingsPageData struct {
    RemEnabled     bool  // Remediation enabled state
    DryRun         bool  // Dry run mode state
    ActionsThisHour int  // Count of actions in current hour
}
```

---

## Future Improvements

The following enhancements are planned or under consideration for these templates:

### Search and Filtering

| Feature | Template | Description |
|---------|----------|-------------|
| Rule search | Rules | Text search across rule names, patterns, and keywords |
| Status filter | Rules | Filter by enabled/disabled state |
| Action filter | History | Filter by action type (restart, scale, rollback) |
| Status filter | History | Filter by success/failed/skipped |
| Date range | History | Filter history by time period |
| Target search | History | Search by namespace or resource name |

### Enhanced Pagination

| Feature | Template | Description |
|---------|----------|-------------|
| Configurable page size | History | Allow users to select 25, 50, or 100 items per page |
| Jump to page | History | Direct navigation to specific page numbers |
| Infinite scroll | History | Load more entries automatically when scrolling |

### Rule Management

| Feature | Priority | Description |
|---------|----------|-------------|
| Rule editing | High | Edit rule configurations directly in the UI |
| Rule creation | High | Add new rules through a form interface |
| Rule enable/disable toggle | High | Toggle individual rules without editing files |
| Rule duplication | Medium | Clone existing rules as starting points |
| Rule import/export | Medium | Download/upload rules as YAML |
| Rule validation | Medium | Real-time validation with helpful error messages |
| Rule versioning | Low | Track changes to rules over time |

### History Enhancements

| Feature | Priority | Description |
|---------|----------|-------------|
| Export to CSV | Medium | Download history for external analysis |
| Action replay | Low | Re-execute a previous action manually |
| Linked events | Low | Link to related Kubernetes events or alerts |
| Aggregated view | Low | Group actions by target or action type |

### Settings Improvements

| Feature | Priority | Description |
|---------|----------|-------------|
| Rate limiting UI | High | Configure action rate limits through the UI |
| Namespace exclusions | Medium | Manage excluded namespaces without file edits |
| Connection testing | Medium | Test Loki connectivity from the UI |
| Configuration preview | Low | Show full merged configuration |
| Settings history | Low | Track settings changes over time |

### General UI Improvements

| Feature | Description |
|---------|-------------|
| Dark mode | Support for dark theme matching system preferences |
| Mobile responsiveness | Improved layouts for tablet and mobile devices |
| Keyboard shortcuts | Quick navigation and actions via keyboard |
| Notifications | Toast notifications for async operations |
| Real-time updates | WebSocket-based live updates for history and status |

---

## See Also

- [Web Server Implementation](./15-web-server.md) - Backend implementation details
- [Base Template Documentation](./16-base-template.md) - Shared layout and navigation
- [Dashboard Template](./17-dashboard-template.md) - Main dashboard page
- [Remediation Actions](./10-remediation-actions.md) - Available remediation action types
- [Rule Configuration](./06-rule-loader.md) - How to configure detection rules
