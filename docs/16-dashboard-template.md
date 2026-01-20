# Dashboard Template

This document provides comprehensive documentation for the Kube Sentinel dashboard template, which serves as the primary user interface for monitoring cluster health, viewing active errors, and tracking remediation activities.

## Table of Contents

- [Overview](#overview)
- [Template Architecture](#template-architecture)
- [Main UI Elements](#main-ui-elements)
  - [Statistics Cards](#statistics-cards)
  - [Priority Queue Section](#priority-queue-section)
  - [Recent Remediations Section](#recent-remediations-section)
- [Data Context and Template Variables](#data-context-and-template-variables)
- [Styling and Visual Design](#styling-and-visual-design)
- [Template Functions](#template-functions)
- [Real-Time Updates](#real-time-updates)
- [Future Enhancements](#future-enhancements)

---

## Overview

The dashboard template (`dashboard.html`) is the landing page for the Kube Sentinel web interface. It provides operators with an at-a-glance view of:

- **System health metrics**: Total error counts, priority breakdowns, and remediation statistics
- **Active issues**: A prioritized queue of current errors requiring attention
- **Remediation activity**: Recent automated actions taken by the system

The dashboard is designed to surface the most critical information immediately, enabling operators to quickly assess cluster health and identify issues that need attention.

---

## Template Architecture

The dashboard template follows Go's `html/template` package conventions and integrates with the base layout template:

```go
{{template "base" .}}

{{define "title"}}Dashboard - Kube Sentinel{{end}}

{{define "content"}}
    <!-- Dashboard content -->
{{end}}
```

### Template Inheritance

| Block | Purpose |
|-------|---------|
| `base` | Inherits the base layout including navigation, header, and footer |
| `title` | Defines the page title displayed in the browser tab |
| `content` | Contains the main dashboard content rendered within the base layout |

### Technology Stack

The dashboard leverages:

- **Tailwind CSS**: Utility-first CSS framework for responsive styling
- **HTMX**: Enables dynamic content updates without full page reloads
- **WebSocket**: Provides real-time updates for live monitoring

---

## Main UI Elements

The dashboard is organized into two primary sections:

1. **Statistics Cards Row**: Four cards displaying key metrics across the top
2. **Two-Column Grid**: Priority queue and recent remediations displayed side by side

### Statistics Cards

The statistics section displays four key metric cards in a responsive grid layout that adapts from single column on mobile to four columns on larger screens.

#### Card 1: Total Errors

| Property | Description |
|----------|-------------|
| Label | "Total Errors" |
| Value | `{{.Stats.TotalErrors}}` |
| Purpose | Displays the cumulative count of all detected errors in the system |
| Styling | Neutral gray text with large bold number |

This card provides a high-level indicator of overall system activity. A high total error count may indicate systemic issues requiring investigation.

#### Card 2: Critical Errors (P1)

| Property | Description |
|----------|-------------|
| Label | "Critical (P1)" |
| Value | `{{index .Stats.ErrorsByPriority "P1"}}` |
| Purpose | Shows the count of highest-priority errors requiring immediate attention |
| Styling | Red text (`text-red-600`) to indicate urgency |

Critical errors are displayed prominently in red to immediately draw operator attention. P1 errors typically represent service-impacting issues.

#### Card 3: Remediations

| Property | Description |
|----------|-------------|
| Label | "Remediations" |
| Primary Value | `{{.Stats.RemediationCount}}` |
| Secondary Values | `{{.Stats.SuccessfulActions}}` success / `{{.Stats.FailedActions}}` failed |
| Purpose | Tracks the total number of remediation actions and their outcomes |
| Styling | Green for successful actions, red for failed actions |

This card provides insight into the effectiveness of automated remediation:

- **Total count**: Overall number of remediation attempts
- **Success count**: Actions that completed successfully (green text)
- **Failed count**: Actions that encountered errors (red text)

#### Card 4: Remediation Status

| Property | Description |
|----------|-------------|
| Label | "Remediation Status" |
| Status Badge | Active (green), Dry Run (yellow), or Disabled (gray) |
| Secondary Info | `{{.ActionsThisHour}}` actions this hour |
| Purpose | Indicates the current operational mode of the remediation system |

The status badge indicates three possible states:

| State | Badge Color | Description |
|-------|-------------|-------------|
| Active | Green | Remediation is enabled and executing actions |
| Dry Run | Yellow | Remediation is enabled but only simulating actions |
| Disabled | Gray | Remediation system is turned off |

The "actions this hour" counter helps track remediation activity levels and can indicate if rate limiting is being approached.

---

### Priority Queue Section

The priority queue displays the most recent errors, sorted by priority and recency. This section occupies the left column in the two-column layout.

#### Section Header

```html
<h3 class="text-lg font-medium text-gray-900">Priority Queue</h3>
```

#### Queue Item Structure

Each error in the queue displays:

| Element | Template Variable | Description |
|---------|-------------------|-------------|
| Priority Badge | `{{.Priority}}` | Color-coded badge (P1-P4) indicating severity |
| Resource Path | `{{.Namespace}}/{{.Pod}}` | Identifies the affected Kubernetes resource |
| Error Message | `{{truncate .Message 100}}` | Truncated error description (max 100 characters) |
| Occurrence Count | `{{.Count}}x` | Number of times this error has occurred |
| Last Seen | `{{formatTime .LastSeen}}` | Human-readable timestamp of most recent occurrence |

#### Visual Priority Indicators

Each queue item uses color-coded styling based on priority:

| Priority | Background Color | Border Color | Badge Style |
|----------|------------------|--------------|-------------|
| P1 (Critical) | Light red | Red left border | Red badge |
| P2 (High) | Light orange | Orange left border | Orange badge |
| P3 (Medium) | Light yellow | Yellow left border | Yellow badge |
| P4 (Low) | Light blue | Blue left border | Blue badge |

#### Navigation

- **Item Click**: Each error item is a clickable link (`/errors/{{.ID}}`) that navigates to the detailed error view
- **View All**: Footer link (`/errors`) navigates to the complete error list

#### Empty State

When no errors are present, the queue displays:

```html
<div class="p-4 text-center text-gray-500">No errors found</div>
```

---

### Recent Remediations Section

The recent remediations section displays the latest automated actions taken by the system. This section occupies the right column in the two-column layout.

#### Section Header

```html
<h3 class="text-lg font-medium text-gray-900">Recent Remediations</h3>
```

#### Remediation Item Structure

Each remediation entry displays:

| Element | Template Variable | Description |
|---------|-------------------|-------------|
| Status Badge | `{{.Status}}` | Success (green), Failed (red), or Skipped (gray) |
| Action Name | `{{.Action}}` | Type of remediation action executed |
| Target | `{{.Target}}` | Kubernetes resource that was targeted |
| Message | `{{truncate .Message 80}}` | Optional result message (max 80 characters) |
| Timestamp | `{{formatTime .Timestamp}}` | When the action was executed |
| Dry Run Indicator | `{{if .DryRun}}` | Yellow text indicator for simulated actions |

#### Status Badges

| Status | Badge Color | Description |
|--------|-------------|-------------|
| Success | Green | Action completed successfully |
| Failed | Red | Action encountered an error |
| Skipped | Gray | Action was not executed (e.g., conditions not met) |

#### Dry Run Indication

When an action was executed in dry run mode, a small yellow "dry run" label appears next to the timestamp, helping operators distinguish between simulated and actual remediations.

#### Navigation

- **View History**: Footer link (`/history`) navigates to the complete remediation history

#### Empty State

When no remediations have occurred:

```html
<div class="p-4 text-center text-gray-500">No remediations yet</div>
```

---

## Data Context and Template Variables

The dashboard template receives a data context containing the following variables:

### Stats Object

| Field | Type | Description |
|-------|------|-------------|
| `TotalErrors` | int | Total count of all errors in the system |
| `ErrorsByPriority` | map[string]int | Map of priority levels to error counts (e.g., "P1": 5) |
| `RemediationCount` | int | Total number of remediation actions |
| `SuccessfulActions` | int | Count of successful remediations |
| `FailedActions` | int | Count of failed remediations |

### Configuration Flags

| Field | Type | Description |
|-------|------|-------------|
| `RemEnabled` | bool | Whether remediation is enabled |
| `DryRun` | bool | Whether dry run mode is active |
| `ActionsThisHour` | int | Number of actions executed in the current hour |

### Data Collections

| Field | Type | Description |
|-------|------|-------------|
| `RecentErrors` | []Error | Slice of recent error objects for the priority queue |
| `RecentRemediations` | []Remediation | Slice of recent remediation records |

### Error Object Fields

| Field | Type | Description |
|-------|------|-------------|
| `ID` | string | Unique identifier for the error |
| `Priority` | string | Priority level (P1, P2, P3, P4) |
| `Namespace` | string | Kubernetes namespace |
| `Pod` | string | Pod name |
| `Message` | string | Error message text |
| `Count` | int | Occurrence count |
| `LastSeen` | time.Time | Timestamp of last occurrence |

### Remediation Object Fields

| Field | Type | Description |
|-------|------|-------------|
| `Status` | string | Outcome status (success, failed, skipped) |
| `Action` | string | Action type name |
| `Target` | string | Target resource identifier |
| `Message` | string | Result message |
| `Timestamp` | time.Time | Execution timestamp |
| `DryRun` | bool | Whether this was a dry run |

---

## Styling and Visual Design

### Layout Structure

The dashboard uses a responsive grid system:

```
+--------------------------------------------------+
|  [Total Errors] [Critical P1] [Remediations] [Status]  |  <- Stats row (4 columns)
+--------------------------------------------------+
|  +-------------------+  +---------------------+  |
|  | Priority Queue    |  | Recent Remediations |  |  <- Two-column grid
|  |                   |  |                     |  |
|  | - Error 1         |  | - Remediation 1     |  |
|  | - Error 2         |  | - Remediation 2     |  |
|  | - ...             |  | - ...               |  |
|  +-------------------+  +---------------------+  |
+--------------------------------------------------+
```

### Responsive Breakpoints

| Breakpoint | Stats Grid | Content Grid |
|------------|------------|--------------|
| Mobile (default) | 1 column | 1 column (stacked) |
| Medium (md:) | 4 columns | 1 column |
| Large (lg:) | 4 columns | 2 columns (side by side) |

### Color Palette

The dashboard uses a consistent color scheme for visual communication:

| Color | Usage |
|-------|-------|
| Red | Critical errors, failed actions, P1 priority |
| Orange | High priority, P2 errors |
| Yellow | Medium priority, dry run mode, P3 errors |
| Blue | Low priority, P4 errors |
| Green | Success states, active status |
| Gray | Disabled states, neutral information |

---

## Template Functions

The dashboard template uses custom template functions registered by the web server:

### priorityColor

Converts a priority level to a color name for CSS class generation.

| Input | Output |
|-------|--------|
| P1 | red |
| P2 | orange |
| P3 | yellow |
| P4 | blue |
| (other) | gray |

### truncate

Truncates a string to a specified maximum length, adding ellipsis if truncated.

**Usage**: `{{truncate .Message 100}}`

### formatTime

Formats a timestamp into a human-readable relative time string (e.g., "5 minutes ago", "2 hours ago").

**Usage**: `{{formatTime .LastSeen}}`

---

## Real-Time Updates

The dashboard supports real-time updates through WebSocket integration provided by the base template.

### WebSocket Events

| Event Type | Trigger | Action |
|------------|---------|--------|
| `error` | New error detected | Triggers `newError` HTMX event |
| `remediation` | Remediation completed | Triggers `newRemediation` HTMX event |
| `stats` | Statistics updated | Triggers `statsUpdate` HTMX event |

### Connection Status

The base template displays connection status in the navigation bar:

| Status | Indicator | Description |
|--------|-----------|-------------|
| Connecting | Gray dot | Initial connection attempt |
| Connected | Green dot | WebSocket connection active |
| Disconnected | Red dot | Connection lost (auto-reconnects after 3 seconds) |

---

## Future Enhancements

The following enhancements are planned or under consideration for future releases of the dashboard:

### Filtering and Search

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Priority Filter | Filter errors by priority level (P1-P4) | High |
| Namespace Filter | Filter by Kubernetes namespace | High |
| Time Range Filter | Filter by time window (last hour, day, week) | Medium |
| Text Search | Search errors by message content | Medium |
| Saved Filters | Save and recall filter combinations | Low |

### Data Visualization

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Error Trend Chart | Line chart showing error rate over time | High |
| Priority Distribution | Pie or donut chart of errors by priority | Medium |
| Remediation Success Rate | Gauge or progress indicator for success percentage | Medium |
| Namespace Heatmap | Visual representation of errors by namespace | Low |
| Timeline View | Chronological view of errors and remediations | Low |

### Real-Time Updates

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Live Error Counter | Animated counter updates without page refresh | High |
| Toast Notifications | Pop-up alerts for new critical errors | High |
| Sound Alerts | Audio notifications for P1 errors | Medium |
| Desktop Notifications | Browser notifications when tab is not focused | Medium |
| Auto-Refresh Toggle | User preference for enabling/disabling auto-updates | Low |

### User Experience Improvements

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Dark Mode | Alternative dark theme for low-light environments | Medium |
| Customizable Layout | Drag-and-drop dashboard widget arrangement | Low |
| Keyboard Shortcuts | Navigate and interact using keyboard commands | Low |
| Accessibility | WCAG compliance improvements | Medium |
| Mobile Optimization | Touch-friendly interface for tablets | Low |

### Additional Widgets

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Cluster Overview | Node count, pod count, resource utilization | Medium |
| Rule Status | Active rules and their match counts | Medium |
| Alert Summary | Integration with external alerting systems | Low |
| Quick Actions | One-click access to common operations | Low |

### Integration Features

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Grafana Embed | Embedded Grafana panels for metrics | Medium |
| Log Viewer | Inline log viewing for selected errors | Medium |
| Kubernetes Events | Display related Kubernetes events | Medium |
| Export Functions | Export error data to CSV or JSON | Low |

---

## See Also

- [Base Template](./17-base-template.md) - Documentation for the base layout template
- [Web Server Configuration](./14-web-server.md) - Web server setup and configuration
- [API Reference](./15-api-reference.md) - REST API endpoints used by the dashboard
- [Remediation Actions](./10-remediation-actions.md) - Details on available remediation actions
