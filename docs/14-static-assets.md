# Static Assets and Styling

This document provides comprehensive documentation for the static assets and styling system used in the kube-sentinel web dashboard. It covers CSS organization, custom animations, UI component patterns, and considerations for future enhancements.

## Table of Contents

- [Overview](#overview)
- [Role of Static Assets](#role-of-static-assets)
- [CSS Architecture](#css-architecture)
  - [Tailwind CSS Integration](#tailwind-css-integration)
  - [Custom Stylesheet Organization](#custom-stylesheet-organization)
  - [Priority Indicator Styles](#priority-indicator-styles)
  - [Status Badge System](#status-badge-system)
- [Custom Animations](#custom-animations)
  - [Pulse Animation](#pulse-animation)
  - [Slide-In Animation](#slide-in-animation)
  - [Spin Animation](#spin-animation)
- [UI Components](#ui-components)
  - [Custom Scrollbars](#custom-scrollbars)
  - [Code Blocks](#code-blocks)
  - [Loading Spinner](#loading-spinner)
  - [Toast Notifications](#toast-notifications)
  - [Toggle Switches](#toggle-switches)
- [Template Structure](#template-structure)
- [Future Enhancements](#future-enhancements)
  - [Dark Mode Support](#dark-mode-support)
  - [Theming System](#theming-system)
  - [Asset Bundling](#asset-bundling)
  - [Performance Optimizations](#performance-optimizations)

---

## Overview

The kube-sentinel web dashboard employs a hybrid styling approach that combines the utility-first Tailwind CSS framework with custom CSS for specialized components and animations. This architecture provides rapid development capabilities while maintaining consistent visual design across all dashboard pages.

The static assets are organized to support:

- **Real-time monitoring interfaces** with live connection status indicators
- **Priority-based visual hierarchy** for error classification
- **Responsive layouts** that adapt to various screen sizes
- **Accessible UI patterns** with proper focus states and contrast

---

## Role of Static Assets

Static assets in kube-sentinel serve several critical functions within the web dashboard:

### Visual Communication

Static assets enable clear visual communication of system state through:

| Asset Type | Purpose |
|------------|---------|
| Color-coded priorities | Immediate recognition of error severity (P1-P5) |
| Status badges | Clear indication of remediation states |
| Animations | Feedback for user actions and system events |
| Icons and indicators | Connection status and loading states |

### User Experience

The styling system enhances user experience by providing:

- Consistent visual language across all dashboard views
- Smooth transitions and animations for state changes
- Clear interactive affordances for buttons and controls
- Readable typography optimized for monitoring dashboards

### Performance Considerations

The current asset architecture prioritizes:

- **CDN delivery** for Tailwind CSS and HTMX libraries
- **Minimal custom CSS** to reduce payload size
- **CSS-only animations** to avoid JavaScript overhead
- **No external font dependencies** for faster initial load

---

## CSS Architecture

### Tailwind CSS Integration

Kube-sentinel uses Tailwind CSS via CDN for the majority of its styling needs. This approach provides several benefits:

```html
<script src="https://cdn.tailwindcss.com"></script>
```

**Benefits of CDN Integration:**

- Zero build step required for development
- Automatic access to the complete Tailwind utility class library
- JIT (Just-In-Time) compilation for only used styles
- Easy updates to new Tailwind versions

**Tailwind Classes in Use:**

| Category | Example Classes | Purpose |
|----------|-----------------|---------|
| Layout | `max-w-7xl`, `mx-auto`, `px-4`, `grid`, `flex` | Page structure and responsive grids |
| Spacing | `space-y-6`, `gap-4`, `p-6`, `mt-2` | Consistent spacing between elements |
| Typography | `text-2xl`, `font-bold`, `text-gray-900` | Text hierarchy and styling |
| Colors | `bg-white`, `text-red-600`, `border-gray-200` | Color palette application |
| Interactive | `hover:bg-gray-700`, `focus:ring-4` | User interaction feedback |
| Responsive | `md:grid-cols-4`, `lg:grid-cols-2` | Breakpoint-specific layouts |

### Custom Stylesheet Organization

The custom stylesheet (`app.css`) supplements Tailwind with specialized styles that cannot be easily achieved with utility classes:

```
internal/web/static/
└── app.css          # Custom styles for specialized components
```

**Custom CSS Categories:**

1. **Browser-specific customizations** - Custom scrollbar styling
2. **Typography enhancements** - Monospace font stacks for code
3. **Animation definitions** - Keyframe animations for UI feedback
4. **Component-specific styles** - Toast notifications, spinners

### Priority Indicator Styles

Error priority is communicated through a color-coded system defined in the base template:

```css
/* Priority row backgrounds with left border accent */
.priority-red { background-color: #fee2e2; border-left: 4px solid #ef4444; }
.priority-orange { background-color: #ffedd5; border-left: 4px solid #f97316; }
.priority-yellow { background-color: #fef3c7; border-left: 4px solid #eab308; }
.priority-blue { background-color: #dbeafe; border-left: 4px solid #3b82f6; }
.priority-gray { background-color: #f3f4f6; border-left: 4px solid #9ca3af; }
```

**Priority Color Mapping:**

| Priority | Color | Background | Use Case |
|----------|-------|------------|----------|
| P1 (Critical) | Red | `#fee2e2` | System outages, data loss risks |
| P2 (High) | Orange | `#ffedd5` | Service degradation, urgent issues |
| P3 (Medium) | Yellow | `#fef3c7` | Important but non-urgent issues |
| P4 (Low) | Blue | `#dbeafe` | Minor issues, informational |
| P5 (Info) | Gray | `#f3f4f6` | Notices, low-impact events |

The left border accent provides a strong visual indicator while the subtle background maintains readability.

### Status Badge System

Status badges provide compact, high-visibility indicators for various states:

```css
/* Colored badges for status indicators */
.badge-red { background-color: #ef4444; color: white; }
.badge-orange { background-color: #f97316; color: white; }
.badge-yellow { background-color: #eab308; color: white; }
.badge-blue { background-color: #3b82f6; color: white; }
.badge-green { background-color: #22c55e; color: white; }
.badge-gray { background-color: #9ca3af; color: white; }
```

**Badge Usage Context:**

| Badge | Typical Use |
|-------|-------------|
| `badge-red` | Failed actions, critical errors |
| `badge-orange` | High priority items |
| `badge-yellow` | Dry run mode, warnings |
| `badge-green` | Success states, active status |
| `badge-blue` | Informational badges, P4 priority |
| `badge-gray` | Disabled states, skipped items |

---

## Custom Animations

### Pulse Animation

The pulse animation provides a subtle breathing effect for elements that need attention:

```css
@keyframes pulse {
    0%, 100% {
        opacity: 1;
    }
    50% {
        opacity: 0.5;
    }
}

.animate-pulse {
    animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}
```

**Characteristics:**

- **Duration:** 2 seconds per cycle
- **Easing:** Cubic bezier for smooth, natural motion
- **Behavior:** Continuous loop, fades to 50% opacity at midpoint

**Use Cases:**

- Loading indicators
- Items awaiting action
- Connection status during reconnection

### Slide-In Animation

Toast notifications use a slide-in animation for non-intrusive appearance:

```css
@keyframes slideIn {
    from {
        transform: translateX(100%);
        opacity: 0;
    }
    to {
        transform: translateX(0);
        opacity: 1;
    }
}
```

**Characteristics:**

- **Duration:** 0.3 seconds
- **Easing:** `ease-out` for deceleration into final position
- **Direction:** Enters from the right side of the viewport

### Spin Animation

The spin animation provides continuous rotation for loading indicators:

```css
@keyframes spin {
    0% { transform: rotate(0deg); }
    100% { transform: rotate(360deg); }
}
```

**Characteristics:**

- **Duration:** 1 second per rotation
- **Easing:** Linear for constant speed
- **Behavior:** Continuous 360-degree rotation

---

## UI Components

### Custom Scrollbars

Custom scrollbar styling improves visual consistency in WebKit-based browsers:

```css
::-webkit-scrollbar {
    width: 8px;
    height: 8px;
}

::-webkit-scrollbar-track {
    background: #f1f1f1;
}

::-webkit-scrollbar-thumb {
    background: #888;
    border-radius: 4px;
}

::-webkit-scrollbar-thumb:hover {
    background: #555;
}
```

**Design Choices:**

| Property | Value | Rationale |
|----------|-------|-----------|
| Width/Height | 8px | Compact but usable size |
| Track color | `#f1f1f1` | Subtle, matches page background |
| Thumb color | `#888` | Visible but not distracting |
| Border radius | 4px | Rounded ends for modern appearance |
| Hover state | `#555` | Darker on interaction for feedback |

**Browser Support:**

- Chrome, Safari, Edge (Chromium): Full support
- Firefox: Falls back to system scrollbars
- Internet Explorer: Falls back to system scrollbars

### Code Blocks

Monospace typography is configured for displaying log messages, error details, and configuration snippets:

```css
pre {
    font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
    white-space: pre-wrap;
    word-wrap: break-word;
}

code {
    font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
}
```

**Font Stack Priority:**

1. **Menlo** - macOS default monospace
2. **Monaco** - Legacy macOS monospace
3. **Courier New** - Cross-platform fallback
4. **monospace** - System default

**Text Wrapping:**

- `pre-wrap` preserves whitespace while allowing wrapping
- `break-word` prevents horizontal overflow on long lines

### Loading Spinner

A CSS-only spinner provides loading feedback without JavaScript:

```css
.spinner {
    border: 2px solid #f3f4f6;
    border-top: 2px solid #3b82f6;
    border-radius: 50%;
    width: 20px;
    height: 20px;
    animation: spin 1s linear infinite;
}
```

**Design Specifications:**

| Property | Value | Purpose |
|----------|-------|---------|
| Size | 20x20px | Compact, suitable for inline use |
| Border width | 2px | Visible but not heavy |
| Track color | `#f3f4f6` | Light gray background |
| Indicator color | `#3b82f6` | Blue accent (Tailwind blue-500) |
| Speed | 1 second | Fast enough to indicate activity |

### Toast Notifications

Toast notifications provide non-blocking feedback for user actions:

```css
.toast {
    position: fixed;
    bottom: 20px;
    right: 20px;
    padding: 12px 24px;
    border-radius: 8px;
    box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
    z-index: 1000;
    animation: slideIn 0.3s ease-out;
}
```

**Positioning Strategy:**

- Fixed positioning ensures visibility regardless of scroll position
- Bottom-right placement follows common UI conventions
- High z-index (1000) ensures toasts appear above other content

**Visual Design:**

- Generous padding (12px vertical, 24px horizontal) for readability
- Rounded corners (8px) for modern appearance
- Subtle shadow for depth and separation from content

**Implementation Pattern:**

```javascript
// Creating a toast (conceptual)
function showToast(message, type) {
    const toast = document.createElement('div');
    toast.className = `toast bg-${type}-100 text-${type}-800`;
    toast.textContent = message;
    document.body.appendChild(toast);

    setTimeout(() => toast.remove(), 3000);
}
```

### Toggle Switches

Settings pages use custom toggle switches built with Tailwind's peer modifier system:

```html
<label class="relative inline-flex items-center cursor-pointer">
    <input type="checkbox" class="sr-only peer" checked>
    <div class="w-11 h-6 bg-gray-200 peer-focus:ring-4
                peer-focus:ring-blue-300 rounded-full peer
                peer-checked:after:translate-x-full
                peer-checked:after:border-white
                after:content-[''] after:absolute
                after:top-[2px] after:left-[2px]
                after:bg-white after:border-gray-300
                after:border after:rounded-full
                after:h-5 after:w-5 after:transition-all
                peer-checked:bg-blue-600"></div>
</label>
```

**Component Features:**

- **Accessibility:** Hidden checkbox with `sr-only` class maintains form semantics
- **Visual feedback:** Color change on toggle, focus ring for keyboard navigation
- **Smooth transition:** CSS transitions for the sliding knob
- **Custom states:** Yellow background for dry-run mode, blue for active

---

## Template Structure

The styling system is organized across the template hierarchy:

```
internal/web/templates/
├── base.html          # Layout, navigation, inline critical CSS
├── dashboard.html     # Dashboard-specific layouts
├── errors.html        # Error list styling
├── error_detail.html  # Error detail view
├── rules.html         # Rules management interface
├── history.html       # Remediation history
└── settings.html      # Settings page with toggle components
```

**Base Template Responsibilities:**

1. Loading external dependencies (Tailwind, HTMX)
2. Defining inline critical CSS (priority colors, badges)
3. Establishing page structure (navigation, main content area)
4. Initializing WebSocket connection and status indicator

**Page Template Responsibilities:**

1. Extending base template with specific content
2. Using Tailwind utilities for layout
3. Applying component classes for specialized styling
4. Including page-specific JavaScript when needed

---

## Future Enhancements

### Dark Mode Support

Adding dark mode would significantly improve the dashboard experience for users working in low-light environments or preferring darker interfaces.

**Implementation Strategy:**

```css
/* CSS custom properties approach */
:root {
    --bg-primary: #ffffff;
    --bg-secondary: #f3f4f6;
    --text-primary: #111827;
    --text-secondary: #6b7280;
    --border-color: #e5e7eb;
}

@media (prefers-color-scheme: dark) {
    :root {
        --bg-primary: #1f2937;
        --bg-secondary: #111827;
        --text-primary: #f9fafb;
        --text-secondary: #9ca3af;
        --border-color: #374151;
    }
}
```

**Tailwind Dark Mode Configuration:**

```javascript
// tailwind.config.js
module.exports = {
    darkMode: 'class', // or 'media' for system preference
    // ...
}
```

**Key Considerations:**

- Priority colors must maintain sufficient contrast in dark mode
- Status badges may need adjusted colors for visibility
- Code blocks benefit from dark mode with syntax highlighting themes
- User preference toggle with localStorage persistence

### Theming System

A flexible theming system would allow customization for different organizational branding:

**CSS Custom Properties Theme:**

```css
:root {
    /* Primary brand colors */
    --brand-primary: #3b82f6;
    --brand-secondary: #1d4ed8;

    /* Status colors */
    --status-success: #22c55e;
    --status-warning: #eab308;
    --status-error: #ef4444;

    /* Surface colors */
    --surface-primary: #ffffff;
    --surface-elevated: #f9fafb;
}

/* Theme override example */
[data-theme="corporate"] {
    --brand-primary: #0066cc;
    --brand-secondary: #004499;
}
```

**Benefits:**

- Consistent brand integration across deployments
- Easy A/B testing of visual designs
- Accessibility improvements through high-contrast themes

### Asset Bundling

For production deployments, asset bundling would improve performance:

**Recommended Tools:**

| Tool | Purpose | Benefit |
|------|---------|---------|
| esbuild | JavaScript bundling | Fast builds, tree shaking |
| Tailwind CLI | CSS optimization | Purged, minified output |
| Asset hashing | Cache busting | Long-term caching |

**Build Pipeline Example:**

```bash
# Generate optimized Tailwind CSS
npx tailwindcss -i ./static/input.css -o ./static/app.min.css --minify

# Bundle JavaScript
npx esbuild ./static/app.js --bundle --minify --outfile=./static/app.min.js
```

**Go Embed Integration:**

```go
//go:embed static/*
var staticFiles embed.FS
```

### Performance Optimizations

**Critical CSS Inlining:**

Move essential styles inline to eliminate render-blocking requests:

```html
<style>
    /* Inline critical CSS for above-the-fold content */
    .nav { /* navigation styles */ }
    .card { /* card component styles */ }
</style>
<link rel="preload" href="/static/app.css" as="style" onload="this.rel='stylesheet'">
```

**Font Optimization:**

```html
<!-- Preconnect to font origins -->
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>

<!-- Use font-display: swap for system font fallback -->
<style>
    @font-face {
        font-family: 'Inter';
        font-display: swap;
        src: url('...') format('woff2');
    }
</style>
```

**Lazy Loading:**

```html
<!-- Defer non-critical JavaScript -->
<script src="https://cdn.tailwindcss.com" defer></script>
<script src="https://unpkg.com/htmx.org@1.9.10" defer></script>
```

**Service Worker Caching:**

For offline-capable dashboards in air-gapped environments:

```javascript
// service-worker.js
const CACHE_NAME = 'kube-sentinel-v1';
const urlsToCache = [
    '/static/app.css',
    '/static/app.js'
];

self.addEventListener('install', event => {
    event.waitUntil(
        caches.open(CACHE_NAME)
            .then(cache => cache.addAll(urlsToCache))
    );
});
```

---

## See Also

- [Web Server and Dashboard](./11-web-server.md) - Web server implementation details
- [Configuration Guide](./02-configuration.md) - Application configuration options
- [Project Initialization](./01-project-initialization.md) - Setting up the development environment
