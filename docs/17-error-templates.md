# Error Templates

This document provides comprehensive documentation for the error display templates in kube-sentinel. The error templates power the web interface for viewing, filtering, and managing detected errors across your Kubernetes cluster.

## Table of Contents

- [Overview](#overview)
- [Template Architecture](#template-architecture)
- [Error List Page](#error-list-page)
  - [Page Structure](#page-structure)
  - [Filtering Capabilities](#filtering-capabilities)
  - [Error Table Display](#error-table-display)
  - [Pagination](#pagination)
- [Error Detail Page](#error-detail-page)
  - [Error Header](#error-header)
  - [Error Message Display](#error-message-display)
  - [Remediation History](#remediation-history)
  - [Metadata Sidebar](#metadata-sidebar)
  - [Labels Display](#labels-display)
- [Template Functions](#template-functions)
- [Data Structures](#data-structures)
- [Styling and Visual Design](#styling-and-visual-design)
- [Future Improvements](#future-improvements)

---

## Overview

The error templates provide a user-friendly interface for operators to monitor and investigate errors detected by kube-sentinel. The template system consists of two primary views:

1. **Error List Page (`errors.html`)**: Displays a filterable, paginated list of all detected errors with summary information
2. **Error Detail Page (`error_detail.html`)**: Shows comprehensive information about a specific error, including its full message, metadata, and remediation history

Both templates extend a base template and use Go's `html/template` package for rendering, ensuring proper HTML escaping and security.

---

## Template Architecture

The error templates follow the standard kube-sentinel template structure:

```
templates/
├── base.html           # Base layout with navigation and common elements
├── errors.html         # Error list page template
└── error_detail.html   # Individual error detail template
```

### Template Inheritance

Both error templates extend the base template using Go's template composition:

```go
{{template "base" .}}

{{define "title"}}Page Title - Kube Sentinel{{end}}

{{define "content"}}
    <!-- Page-specific content -->
{{end}}
```

This ensures consistent styling, navigation, and layout across all pages in the web interface.

---

## Error List Page

The error list page (`errors.html`) provides a comprehensive view of all detected errors with powerful filtering and navigation capabilities.

### Page Structure

The page is organized into four main sections:

| Section | Description |
|---------|-------------|
| Header | Page title with total error count |
| Filters | Form controls for narrowing down displayed errors |
| Error Table | Tabular display of errors with key information |
| Pagination | Navigation controls for large result sets |

### Filtering Capabilities

The error list provides three filtering mechanisms that can be combined:

#### Namespace Filter

```html
<select name="namespace">
    <option value="">All namespaces</option>
    {{range .Namespaces}}
    <option value="{{.}}" {{if eq . $.Filter.Namespace}}selected{{end}}>{{.}}</option>
    {{end}}
</select>
```

- **Type**: Dropdown select
- **Options**: Dynamically populated from available namespaces
- **Default**: All namespaces
- **Behavior**: Filters errors to show only those from the selected namespace

#### Priority Filter

```html
<select name="priority">
    <option value="">All priorities</option>
    <option value="P1">P1 - Critical</option>
    <option value="P2">P2 - High</option>
    <option value="P3">P3 - Medium</option>
    <option value="P4">P4 - Low</option>
</select>
```

- **Type**: Dropdown select
- **Options**: Fixed priority levels (P1-P4)
- **Default**: All priorities
- **Behavior**: Filters errors by severity level

| Priority | Label | Typical Use Case |
|----------|-------|------------------|
| P1 | Critical | Production-impacting issues requiring immediate attention |
| P2 | High | Significant issues that need prompt resolution |
| P3 | Medium | Issues that should be addressed in normal workflow |
| P4 | Low | Minor issues or informational alerts |

#### Text Search

```html
<input type="text" name="search" value="{{.Filter.Search}}" placeholder="Search errors...">
```

- **Type**: Text input
- **Behavior**: Free-text search across error content
- **Use Cases**: Finding specific error messages, pod names, or patterns

#### Filter Actions

- **Filter Button**: Submits the filter form to update results
- **Clear Link**: Resets all filters by navigating to `/errors` without query parameters

### Error Table Display

The error table presents key information in a scannable format:

| Column | Description | Data Source |
|--------|-------------|-------------|
| Priority | Color-coded severity badge | `.Priority` |
| Namespace/Pod | Two-line display of location | `.Namespace`, `.Pod` |
| Message | Truncated error message with rule name | `.Message`, `.RuleMatched` |
| Count | Occurrence frequency | `.Count` |
| Last Seen | Human-readable timestamp | `.LastSeen` |
| Status | Remediated or Active indicator | `.Remediated` |

#### Row Interaction

Each row is clickable and navigates to the error detail page:

```html
<tr class="hover:bg-gray-50 cursor-pointer" onclick="window.location='/errors/{{.ID}}'">
```

#### Message Truncation

Long error messages are truncated to maintain table readability:

```html
<div class="text-sm text-gray-900 max-w-md truncate">{{truncate .Message 80}}</div>
```

#### Status Indicators

Errors display one of two status badges:

- **Remediated** (green): Error has been successfully addressed by automated remediation
- **Active** (yellow): Error is still present and has not been remediated

#### Empty State

When no errors match the current filters:

```html
<tr>
    <td colspan="6" class="px-6 py-4 text-center text-gray-500">No errors found</td>
</tr>
```

### Pagination

The error list supports pagination for large datasets:

#### Visibility Condition

Pagination controls only appear when total results exceed the page size:

```html
{{if gt .Total .PageSize}}
    <!-- Pagination controls -->
{{end}}
```

#### Information Display

Shows the current range and total count:

```
Showing 1 to 20 of 150
```

#### Navigation Controls

- **Previous**: Enabled when not on first page (`{{if gt .Page 1}}`)
- **Next**: Enabled when more results exist (`{{if lt (mul .Page .PageSize) .Total}}`)

#### Filter Preservation

Pagination links preserve current filter selections:

```html
<a href="?page={{add .Page 1}}&namespace={{.Filter.Namespace}}&priority={{.Filter.Priority}}&search={{.Filter.Search}}">
```

---

## Error Detail Page

The error detail page (`error_detail.html`) provides comprehensive information about a single error, including its full context, metadata, and remediation history.

### Error Header

The header section displays the error's identity and status:

```html
<div class="bg-white rounded-lg shadow overflow-hidden priority-{{priorityColor .Error.Priority}}">
```

#### Components

| Element | Description |
|---------|-------------|
| Priority Badge | Color-coded badge with priority level and label (e.g., "P1 - Critical") |
| Status Badge | Remediated (green) or Active (yellow) indicator |
| Error ID | Unique identifier for reference and debugging |
| Resource Location | Namespace/Pod combination as the main title |
| Container Name | Displayed when the error is container-specific |

#### Navigation

A back link allows easy return to the error list:

```html
<a href="/errors" class="text-gray-500 hover:text-gray-700">&larr; Back to errors</a>
```

### Error Message Display

The full error message is displayed in a code-formatted block:

```html
<div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-lg font-medium text-gray-900 mb-4">Error Message</h2>
    <pre class="bg-gray-900 text-gray-100 p-4 rounded-lg overflow-x-auto text-sm">{{.Error.Message}}</pre>
</div>
```

#### Design Considerations

- **Dark Background**: High contrast for readability of technical content
- **Monospace Font**: Preserves formatting of stack traces and structured output
- **Horizontal Scroll**: Handles long lines without breaking layout
- **Full Content**: Unlike the list view, the complete message is shown

### Remediation History

The remediation history section documents all automated and manual remediation attempts:

```html
<div class="bg-white rounded-lg shadow">
    <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-lg font-medium text-gray-900">Remediation History</h2>
    </div>
    <div class="divide-y divide-gray-200">
        {{range .Remediations}}
        <!-- Remediation entry -->
        {{else}}
        <div class="p-4 text-center text-gray-500">No remediation attempts</div>
        {{end}}
    </div>
</div>
```

#### Remediation Entry Fields

Each remediation entry displays:

| Field | Description | Visual Treatment |
|-------|-------------|------------------|
| Status | Success, Failed, or Skipped | Color-coded badge |
| Action | The remediation action taken | Bold text |
| Dry Run | Indicates if action was simulated | Yellow label |
| Timestamp | When the remediation occurred | Gray text, right-aligned |
| Message | Additional context or error details | Displayed below when present |

#### Status Badges

| Status | Badge Color | Meaning |
|--------|-------------|---------|
| Success | Green | Remediation completed successfully |
| Failed | Red | Remediation attempted but failed |
| Skipped | Gray | Remediation was not executed |

#### Dry Run Indicator

When a remediation was executed in dry-run mode (simulation without actual changes):

```html
{{if .DryRun}}
<span class="text-xs text-yellow-600">(dry run)</span>
{{end}}
```

### Metadata Sidebar

The right sidebar displays detailed error metadata:

```html
<div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-lg font-medium text-gray-900 mb-4">Details</h2>
    <dl class="space-y-3">
        <!-- Definition list items -->
    </dl>
</div>
```

#### Metadata Fields

| Field | Description |
|-------|-------------|
| Rule Matched | The detection rule that identified this error |
| Occurrence Count | Total number of times this error has occurred |
| First Seen | Timestamp of the initial occurrence |
| Last Seen | Timestamp of the most recent occurrence |
| Fingerprint | Unique identifier for error deduplication |

#### Fingerprint Display

The fingerprint is displayed in monospace font for technical accuracy:

```html
<dd class="text-sm text-gray-900 font-mono">{{.Error.Fingerprint}}</dd>
```

### Labels Display

When an error has associated labels, they are displayed in a separate card:

```html
{{if .Error.Labels}}
<div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-lg font-medium text-gray-900 mb-4">Labels</h2>
    <div class="space-y-2">
        {{range $k, $v := .Error.Labels}}
        <div class="flex items-center text-sm">
            <span class="font-medium text-gray-500 mr-2">{{$k}}:</span>
            <span class="text-gray-900">{{$v}}</span>
        </div>
        {{end}}
    </div>
</div>
{{end}}
```

Labels are key-value pairs that provide additional context, such as:

- Kubernetes labels from the affected resource
- Custom annotations added by detection rules
- Environment or deployment metadata

---

## Template Functions

The error templates utilize several custom template functions:

| Function | Usage | Description |
|----------|-------|-------------|
| `priorityColor` | `{{priorityColor .Priority}}` | Returns a color name (red, orange, yellow, gray) based on priority level |
| `priorityLabel` | `{{priorityLabel .Priority}}` | Returns the human-readable label for a priority (Critical, High, etc.) |
| `truncate` | `{{truncate .Message 80}}` | Truncates a string to the specified length with ellipsis |
| `formatTime` | `{{formatTime .LastSeen}}` | Formats a timestamp in a human-readable format |
| `add` | `{{add .Page 1}}` | Arithmetic addition for pagination |
| `sub` | `{{sub .Page 1}}` | Arithmetic subtraction for pagination |
| `mul` | `{{mul .Page .PageSize}}` | Arithmetic multiplication for pagination |
| `eq` | `{{eq .Status "success"}}` | Equality comparison for conditionals |
| `gt` | `{{gt .Total .PageSize}}` | Greater-than comparison |
| `lt` | `{{lt (mul .Page .PageSize) .Total}}` | Less-than comparison |

---

## Data Structures

### Error List Page Data

```go
type ErrorListData struct {
    Errors     []ErrorSummary  // List of errors to display
    Total      int             // Total number of errors matching filters
    Page       int             // Current page number (1-indexed)
    PageSize   int             // Number of errors per page
    Namespaces []string        // Available namespaces for filtering
    Filter     ErrorFilter     // Current filter state
}

type ErrorFilter struct {
    Namespace string    // Selected namespace filter
    Priority  Priority  // Selected priority filter
    Search    string    // Search query text
}

type ErrorSummary struct {
    ID          string    // Unique error identifier
    Namespace   string    // Kubernetes namespace
    Pod         string    // Pod name
    Message     string    // Error message (may be truncated)
    Priority    Priority  // Error priority level
    RuleMatched string    // Name of the matched detection rule
    Count       int       // Number of occurrences
    LastSeen    time.Time // Most recent occurrence
    Remediated  bool      // Whether error has been remediated
}
```

### Error Detail Page Data

```go
type ErrorDetailData struct {
    Error        ErrorDetail    // Full error information
    Remediations []Remediation  // History of remediation attempts
}

type ErrorDetail struct {
    ID          string            // Unique error identifier
    Namespace   string            // Kubernetes namespace
    Pod         string            // Pod name
    Container   string            // Container name (optional)
    Message     string            // Full error message
    Priority    Priority          // Error priority level
    RuleMatched string            // Name of the matched detection rule
    Count       int               // Number of occurrences
    FirstSeen   time.Time         // Initial occurrence
    LastSeen    time.Time         // Most recent occurrence
    Fingerprint string            // Deduplication fingerprint
    Labels      map[string]string // Associated labels
    Remediated  bool              // Whether error has been remediated
}

type Remediation struct {
    Action    string    // Name of the remediation action
    Status    string    // success, failed, or skipped
    DryRun    bool      // Whether this was a simulation
    Timestamp time.Time // When the remediation was attempted
    Message   string    // Additional context or error message
}
```

---

## Styling and Visual Design

The error templates use Tailwind CSS for styling, following these design principles:

### Color Coding

Priority levels use consistent color coding throughout the interface:

| Priority | Color | CSS Classes |
|----------|-------|-------------|
| P1 (Critical) | Red | `badge-red`, `priority-red` |
| P2 (High) | Orange | `badge-orange`, `priority-orange` |
| P3 (Medium) | Yellow | `badge-yellow`, `priority-yellow` |
| P4 (Low) | Gray | `badge-gray`, `priority-gray` |

### Status Colors

| Status | Background | Text |
|--------|------------|------|
| Remediated | `bg-green-100` | `text-green-800` |
| Active | `bg-yellow-100` | `text-yellow-800` |
| Success | `badge-green` | - |
| Failed | `badge-red` | - |
| Skipped | `badge-gray` | - |

### Responsive Layout

The error detail page uses a responsive grid layout:

```html
<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
    <div class="lg:col-span-2">
        <!-- Main content (message, remediation history) -->
    </div>
    <div>
        <!-- Sidebar (metadata, labels) -->
    </div>
</div>
```

- **Mobile**: Single column layout with stacked sections
- **Desktop**: Three-column grid with main content spanning two columns

### Interactive Elements

- Clickable table rows with hover highlight (`hover:bg-gray-50`)
- Visual cursor change for interactive elements (`cursor-pointer`)
- Focused form controls with ring and border color changes

---

## Future Improvements

The following enhancements are planned or under consideration for the error templates:

### User Experience Improvements

| Enhancement | Description | Priority |
|-------------|-------------|----------|
| Real-time Updates | WebSocket-based live updates for new errors | High |
| Bulk Actions | Select multiple errors for batch remediation or acknowledgment | High |
| Error Grouping | Collapse similar errors into groups with expand/collapse | Medium |
| Keyboard Navigation | Navigate and act on errors using keyboard shortcuts | Medium |
| Dark Mode | System-preference-aware dark theme | Low |
| Export Functionality | Export error data to CSV, JSON, or PDF | Low |

### Error Categorization

| Feature | Description | Status |
|---------|-------------|--------|
| Category Tags | Automatic categorization by error type (OOM, Crash, Network, etc.) | Planned |
| Custom Labels | User-defined labels for organizing errors | Planned |
| Error Patterns | Recognize and group recurring error patterns | Under consideration |
| Severity Auto-Adjustment | Dynamic priority based on impact and frequency | Under consideration |

### Advanced Filtering

| Filter | Description | Status |
|--------|-------------|--------|
| Date Range | Filter errors by time window | Planned |
| Multiple Namespaces | Select multiple namespaces simultaneously | Planned |
| Rule Filter | Filter by specific detection rules | Planned |
| Status Filter | Filter by remediation status | Planned |
| Label Filters | Filter by associated labels | Under consideration |
| Saved Filters | Save and recall filter combinations | Under consideration |

### Visualization and Analytics

| Feature | Description | Status |
|---------|-------------|--------|
| Error Timeline | Visualize error frequency over time | Planned |
| Error Heatmap | Show error distribution across namespaces | Under consideration |
| Trend Analysis | Identify increasing/decreasing error patterns | Under consideration |
| Impact Dashboard | Correlate errors with service health metrics | Under consideration |

### Integration Enhancements

| Integration | Description | Status |
|-------------|-------------|--------|
| Log Deep Links | Link to full logs in external log aggregation systems | Planned |
| Trace Correlation | Connect errors to distributed traces | Planned |
| Runbook Links | Associate errors with documentation and runbooks | Planned |
| Notification Preferences | Configure per-error notification channels | Under consideration |

### Accessibility Improvements

| Enhancement | Description | Status |
|-------------|-------------|--------|
| ARIA Labels | Comprehensive screen reader support | Planned |
| Focus Management | Improved keyboard focus indicators and flow | Planned |
| Color Contrast | Ensure WCAG AA compliance for all color combinations | Planned |
| Reduced Motion | Respect prefers-reduced-motion for animations | Under consideration |

---

## See Also

- [Web Interface Overview](./14-web-interface.md) - General web interface documentation
- [Remediation Actions](./10-remediation-actions.md) - Available remediation actions
- [Rule Configuration](./06-rule-loader.md) - Configuring detection rules
- [Store Interface](./08-store-interface.md) - Error storage and retrieval
