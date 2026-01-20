# Base Template Documentation

This document provides a comprehensive overview of the base HTML template used in the kube-sentinel web dashboard, covering the layout strategy, navigation structure, real-time WebSocket connectivity, template inheritance patterns, and external dependencies.

## Table of Contents

1. [Overview](#overview)
2. [Base HTML Template and Layout Strategy](#base-html-template-and-layout-strategy)
3. [Navigation Structure](#navigation-structure)
4. [WebSocket Connection Management](#websocket-connection-management)
5. [Template Inheritance with Go Template Blocks](#template-inheritance-with-go-template-blocks)
6. [External Dependencies](#external-dependencies)
7. [Custom CSS Classes](#custom-css-classes)
8. [Future Improvements](#future-improvements)

---

## Overview

The base template (`base.html`) serves as the foundational layout for all pages in the kube-sentinel web dashboard. It establishes the HTML document structure, common navigation, styling framework, and real-time communication infrastructure that child templates inherit and extend.

**File Location**: `/internal/web/templates/base.html`

---

## Base HTML Template and Layout Strategy

### Document Structure

The base template follows a standard HTML5 document structure with semantic elements:

```html
{{define "base"}}
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{block "title" .}}Kube Sentinel{{end}}</title>
    <!-- External scripts and styles -->
</head>
<body class="bg-gray-100 min-h-screen">
    <nav><!-- Navigation --></nav>
    <main>{{block "content" .}}{{end}}</main>
    <script><!-- WebSocket logic --></script>
</body>
</html>
{{end}}
```

### Layout Strategy

The layout follows a **fixed-header with scrollable content** pattern:

| Component | Purpose | Tailwind Classes |
|-----------|---------|------------------|
| `<body>` | Page container with minimum full viewport height | `bg-gray-100 min-h-screen` |
| `<nav>` | Fixed navigation bar | `bg-gray-800 text-white shadow-lg` |
| `<main>` | Content area with responsive width constraints | `max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8` |

### Responsive Breakpoints

The template uses Tailwind CSS responsive prefixes:

- **Default**: Mobile-first base styles
- **`sm:`**: Small devices (640px and up)
- **`md:`**: Medium devices (768px and up)
- **`lg:`**: Large devices (1024px and up)

The main content area adjusts padding at different breakpoints (`px-4 sm:px-6 lg:px-8`) for optimal readability.

---

## Navigation Structure

### Navigation Layout

The navigation bar is structured as a dark-themed header with two primary sections:

```html
<nav class="bg-gray-800 text-white shadow-lg">
    <div class="max-w-7xl mx-auto px-4">
        <div class="flex items-center justify-between h-16">
            <!-- Left: Brand and links -->
            <!-- Right: Connection status -->
        </div>
    </div>
</nav>
```

### Navigation Links

The navigation provides access to five main sections of the dashboard:

| Route | Label | Description |
|-------|-------|-------------|
| `/` | Dashboard | Main overview with statistics and recent activity |
| `/errors` | Errors | List of detected errors with filtering capabilities |
| `/rules` | Rules | Rule management and configuration |
| `/history` | History | Historical remediation actions and outcomes |
| `/settings` | Settings | Application configuration options |

### Link Styling

Each navigation link uses consistent styling:

```html
<a href="/" class="px-3 py-2 rounded-md text-sm font-medium hover:bg-gray-700">
    Dashboard
</a>
```

- **Padding**: `px-3 py-2` provides comfortable click targets
- **Typography**: `text-sm font-medium` ensures readability
- **Interaction**: `hover:bg-gray-700` provides visual feedback

### Connection Status Indicator

The navigation includes a real-time connection status indicator on the right side:

```html
<div id="connection-status" class="flex items-center">
    <span class="w-2 h-2 bg-gray-500 rounded-full mr-2"></span>
    <span class="text-sm text-gray-400">Connecting...</span>
</div>
```

The indicator displays three states:

| State | Dot Color | Text Color | Label |
|-------|-----------|------------|-------|
| Connecting | `bg-gray-500` | `text-gray-400` | Connecting... |
| Connected | `bg-green-500` | `text-green-400` | Connected |
| Disconnected | `bg-red-500` | `text-red-400` | Disconnected |

---

## WebSocket Connection Management

### Overview

The base template includes a comprehensive WebSocket client that establishes real-time communication with the server for live updates. This enables the dashboard to reflect changes immediately without requiring page refreshes.

### Connection Establishment

```javascript
function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${protocol}//${window.location.host}/ws`);
    // Event handlers...
}
```

Key features of the connection logic:

- **Protocol Detection**: Automatically uses `wss:` for HTTPS and `ws:` for HTTP
- **Dynamic Host**: Uses `window.location.host` to connect to the same server
- **Endpoint**: Connects to the `/ws` WebSocket endpoint

### Event Handlers

The WebSocket client handles three primary events:

#### Connection Open (`onopen`)

Updates the status indicator to show a successful connection:

```javascript
ws.onopen = function() {
    document.getElementById('connection-status').innerHTML = `
        <span class="w-2 h-2 bg-green-500 rounded-full mr-2"></span>
        <span class="text-sm text-green-400">Connected</span>
    `;
};
```

#### Connection Close (`onclose`)

Updates the status indicator and initiates automatic reconnection:

```javascript
ws.onclose = function() {
    document.getElementById('connection-status').innerHTML = `
        <span class="w-2 h-2 bg-red-500 rounded-full mr-2"></span>
        <span class="text-sm text-red-400">Disconnected</span>
    `;
    reconnectTimer = setTimeout(connectWebSocket, 3000);
};
```

- **Auto-reconnect**: Attempts reconnection after 3 seconds (3000ms)
- **Timer Reference**: Stores timer in `reconnectTimer` variable for potential cleanup

#### Message Handling (`onmessage`)

Processes incoming messages and triggers appropriate HTMX events:

```javascript
ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    if (data.type === 'error') {
        htmx.trigger(document.body, 'newError');
    } else if (data.type === 'remediation') {
        htmx.trigger(document.body, 'newRemediation');
    } else if (data.type === 'stats') {
        htmx.trigger(document.body, 'statsUpdate');
    }
};
```

### Message Types and HTMX Events

| Message Type | HTMX Event | Purpose |
|--------------|------------|---------|
| `error` | `newError` | New error detected, refresh error lists |
| `remediation` | `newRemediation` | Remediation action completed, update displays |
| `stats` | `statsUpdate` | Statistics changed, refresh dashboard metrics |

### Integration with HTMX

The WebSocket messages trigger custom HTMX events on `document.body`. Child templates can listen for these events using HTMX's `hx-trigger` attribute:

```html
<div hx-get="/api/errors/recent"
     hx-trigger="newError from:body"
     hx-swap="innerHTML">
    <!-- Content refreshed on new errors -->
</div>
```

---

## Template Inheritance with Go Template Blocks

### Block Definition Pattern

The base template uses Go's `{{define}}` and `{{block}}` directives to establish an inheritance system:

```go
{{define "base"}}
<!-- Template content with blocks -->
{{end}}
```

### Available Blocks

The base template defines two customizable blocks:

#### Title Block

```go
<title>{{block "title" .}}Kube Sentinel{{end}}</title>
```

- **Default Value**: "Kube Sentinel"
- **Purpose**: Allows child templates to set page-specific titles
- **Context**: Receives the template data context (`.`)

#### Content Block

```go
<main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    {{block "content" .}}{{end}}
</main>
```

- **Default Value**: Empty
- **Purpose**: Main content area for page-specific content
- **Context**: Receives the template data context (`.`)

### Child Template Pattern

Child templates extend the base template using this pattern:

```go
{{template "base" .}}

{{define "title"}}Page Title - Kube Sentinel{{end}}

{{define "content"}}
<div class="space-y-6">
    <!-- Page-specific content -->
</div>
{{end}}
```

### Example: Dashboard Template

The dashboard template demonstrates proper inheritance:

```go
{{template "base" .}}

{{define "title"}}Dashboard - Kube Sentinel{{end}}

{{define "content"}}
<div class="space-y-6">
    <!-- Stats Cards -->
    <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
        <!-- Dashboard content -->
    </div>
</div>
{{end}}
```

### Template Files in the Project

| Template File | Title Override | Purpose |
|---------------|----------------|---------|
| `dashboard.html` | Dashboard - Kube Sentinel | Main overview page |
| `errors.html` | Errors - Kube Sentinel | Error list with filtering |
| `error_detail.html` | Error Detail - Kube Sentinel | Individual error view |
| `rules.html` | Rules - Kube Sentinel | Rule management |
| `history.html` | History - Kube Sentinel | Remediation history |
| `settings.html` | Settings - Kube Sentinel | Configuration options |

---

## External Dependencies

### Tailwind CSS

**Source**: `https://cdn.tailwindcss.com`

```html
<script src="https://cdn.tailwindcss.com"></script>
```

Tailwind CSS is a utility-first CSS framework that provides:

- **Utility Classes**: Pre-built classes for margins, padding, colors, typography
- **Responsive Design**: Breakpoint prefixes (`sm:`, `md:`, `lg:`, `xl:`)
- **Flexbox/Grid**: Layout utilities (`flex`, `grid`, `items-center`)
- **Interactive States**: Hover, focus, and active state modifiers

**Note**: The CDN version is used for development convenience. For production deployments, consider:
- Self-hosting the Tailwind build
- Using Tailwind CLI for custom builds
- Purging unused styles for smaller bundle sizes

### HTMX

**Source**: `https://unpkg.com/htmx.org@1.9.10`

```html
<script src="https://unpkg.com/htmx.org@1.9.10"></script>
```

HTMX enables HTML-driven interactivity without writing JavaScript:

| Feature | Description |
|---------|-------------|
| `hx-get` / `hx-post` | AJAX requests from HTML attributes |
| `hx-trigger` | Custom event triggers for requests |
| `hx-swap` | Content replacement strategies |
| `hx-target` | Specify where to place response content |

**Version**: 1.9.10 (pinned for stability)

### Integration Between HTMX and WebSocket

The template creates a bridge between WebSocket messages and HTMX updates:

1. WebSocket receives real-time message from server
2. JavaScript parses message and determines type
3. `htmx.trigger()` fires custom event on `document.body`
4. HTMX components listening for that event refresh their content

---

## Custom CSS Classes

The base template defines custom CSS classes for priority-based styling:

### Priority Row Classes

Used for error list items with left border indicators:

```css
.priority-red { background-color: #fee2e2; border-left: 4px solid #ef4444; }
.priority-orange { background-color: #ffedd5; border-left: 4px solid #f97316; }
.priority-yellow { background-color: #fef3c7; border-left: 4px solid #eab308; }
.priority-blue { background-color: #dbeafe; border-left: 4px solid #3b82f6; }
.priority-gray { background-color: #f3f4f6; border-left: 4px solid #9ca3af; }
```

### Badge Classes

Used for inline priority and status indicators:

```css
.badge-red { background-color: #ef4444; color: white; }
.badge-orange { background-color: #f97316; color: white; }
.badge-yellow { background-color: #eab308; color: white; }
.badge-blue { background-color: #3b82f6; color: white; }
.badge-green { background-color: #22c55e; color: white; }
.badge-gray { background-color: #9ca3af; color: white; }
```

### Priority Color Mapping

Templates use a `priorityColor` template function to map priority levels:

| Priority | Color | Visual Meaning |
|----------|-------|----------------|
| P1 | red | Critical - Immediate attention required |
| P2 | orange | High - Address soon |
| P3 | yellow | Medium - Normal priority |
| P4 | blue | Low - When convenient |
| Unknown | gray | Unclassified |

---

## Future Improvements

### Layout Enhancements

1. **Sidebar Navigation**: For complex deployments, consider a collapsible sidebar to accommodate additional navigation items and nested routes.

2. **Breadcrumb Navigation**: Add breadcrumbs for deeper page hierarchies (e.g., Errors > Error Detail > Related Events).

3. **Dark Mode Support**: Implement a theme toggle with CSS custom properties for user preference persistence.

4. **Responsive Navigation**: Add a mobile hamburger menu for navigation on smaller screens where the horizontal links may overflow.

### Accessibility Improvements

1. **ARIA Labels**: Add `aria-label` attributes to navigation links and interactive elements:
   ```html
   <nav aria-label="Main navigation">
   ```

2. **Skip Links**: Add a "Skip to main content" link for keyboard navigation:
   ```html
   <a href="#main-content" class="sr-only focus:not-sr-only">
       Skip to main content
   </a>
   ```

3. **Focus Management**: Ensure visible focus indicators for all interactive elements.

4. **Screen Reader Announcements**: Use ARIA live regions for connection status changes:
   ```html
   <div id="connection-status" role="status" aria-live="polite">
   ```

5. **Semantic HTML**: Consider using `<header>` around the nav element for improved document structure.

6. **Keyboard Navigation**: Ensure all navigation items are accessible via keyboard Tab navigation.

### WebSocket Improvements

1. **Exponential Backoff**: Implement progressive delays for reconnection attempts:
   ```javascript
   let reconnectDelay = 1000;
   const maxDelay = 30000;

   ws.onclose = function() {
       setTimeout(connectWebSocket, reconnectDelay);
       reconnectDelay = Math.min(reconnectDelay * 2, maxDelay);
   };
   ```

2. **Connection Health Checks**: Implement ping/pong heartbeats to detect stale connections.

3. **Message Queue**: Buffer messages during disconnection for replay upon reconnection.

4. **Graceful Degradation**: Provide polling fallback when WebSocket is unavailable.

### Performance Optimizations

1. **Asset Bundling**: Bundle Tailwind and HTMX for reduced network requests.

2. **CSS Purging**: Use Tailwind CLI to remove unused styles in production builds.

3. **Preconnect Hints**: Add resource hints for CDN connections:
   ```html
   <link rel="preconnect" href="https://cdn.tailwindcss.com">
   <link rel="preconnect" href="https://unpkg.com">
   ```

4. **Script Loading**: Consider `defer` or `async` attributes for non-critical scripts.

### Template Organization

1. **Partial Templates**: Extract reusable components (cards, badges, tables) into partial templates.

2. **Layout Variants**: Create alternative base templates for different page types (full-width, centered, etc.).

3. **Error Pages**: Add dedicated error page templates (404, 500) that extend the base layout.

---

## Summary

The base template provides a solid foundation for the kube-sentinel web dashboard with:

- Clean, responsive layout using Tailwind CSS utility classes
- Intuitive navigation with real-time connection status
- WebSocket-powered live updates integrated with HTMX
- Flexible template inheritance via Go template blocks
- Consistent visual styling through custom CSS classes

Child templates can focus entirely on page-specific content while inheriting the complete infrastructure for navigation, styling, and real-time updates.
