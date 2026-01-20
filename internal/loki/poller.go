package loki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ParsedError represents a parsed and enriched error from logs
type ParsedError struct {
	ID          string
	Fingerprint string
	Timestamp   time.Time
	Namespace   string
	Pod         string
	Container   string
	Message     string
	Labels      map[string]string
	Raw         string
}

// ErrorHandler is called when new errors are found
type ErrorHandler func([]ParsedError)

// Poller continuously polls Loki for errors
type Poller struct {
	client       *Client
	query        string
	pollInterval time.Duration
	lookback     time.Duration
	handler      ErrorHandler
	logger       *slog.Logger

	// Deduplication
	mu            sync.RWMutex
	seenErrors    map[string]time.Time
	windowSize    time.Duration
	lastPollEnd   time.Time
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithLogger sets the logger for the poller
func WithLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithWindowSize sets the deduplication window size
func WithWindowSize(d time.Duration) PollerOption {
	return func(p *Poller) {
		p.windowSize = d
	}
}

// NewPoller creates a new Loki poller
func NewPoller(client *Client, query string, pollInterval, lookback time.Duration, handler ErrorHandler, opts ...PollerOption) *Poller {
	p := &Poller{
		client:       client,
		query:        query,
		pollInterval: pollInterval,
		lookback:     lookback,
		handler:      handler,
		logger:       slog.Default(),
		seenErrors:   make(map[string]time.Time),
		windowSize:   30 * time.Minute,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Start begins the polling loop
func (p *Poller) Start(ctx context.Context) error {
	p.logger.Info("starting loki poller",
		"query", p.query,
		"poll_interval", p.pollInterval,
		"lookback", p.lookback,
	)

	// Do an initial poll immediately
	if err := p.poll(ctx); err != nil {
		p.logger.Error("initial poll failed", "error", err)
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping loki poller")
			return ctx.Err()

		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.logger.Error("poll failed", "error", err)
			}

		case <-cleanupTicker.C:
			p.cleanupSeenErrors()
		}
	}
}

func (p *Poller) poll(ctx context.Context) error {
	end := time.Now()
	start := end.Add(-p.lookback)

	// If we have a last poll time, use that to avoid re-fetching
	if !p.lastPollEnd.IsZero() && p.lastPollEnd.After(start) {
		start = p.lastPollEnd
	}

	p.logger.Debug("polling loki", "start", start, "end", end)

	entries, err := p.client.QueryRange(ctx, p.query, start, end, 1000)
	if err != nil {
		return fmt.Errorf("querying loki: %w", err)
	}

	p.lastPollEnd = end

	if len(entries) == 0 {
		return nil
	}

	p.logger.Debug("received log entries", "count", len(entries))

	// Parse and deduplicate
	var newErrors []ParsedError
	for _, entry := range entries {
		parsed := p.parseEntry(entry)
		if parsed == nil {
			continue
		}

		if p.isNew(parsed.Fingerprint) {
			newErrors = append(newErrors, *parsed)
			p.markSeen(parsed.Fingerprint)
		}
	}

	if len(newErrors) > 0 {
		p.logger.Info("found new errors", "count", len(newErrors))
		p.handler(newErrors)
	}

	return nil
}

func (p *Poller) parseEntry(entry LogEntry) *ParsedError {
	namespace := entry.Labels["namespace"]
	pod := entry.Labels["pod"]
	container := entry.Labels["container"]

	// Try to extract structured info from the log line
	message := extractMessage(entry.Line)

	// Generate fingerprint for deduplication
	fingerprint := generateFingerprint(namespace, pod, container, message)

	return &ParsedError{
		ID:          generateID(),
		Fingerprint: fingerprint,
		Timestamp:   entry.Timestamp,
		Namespace:   namespace,
		Pod:         pod,
		Container:   container,
		Message:     message,
		Labels:      entry.Labels,
		Raw:         entry.Line,
	}
}

func (p *Poller) isNew(fingerprint string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, seen := p.seenErrors[fingerprint]
	return !seen
}

func (p *Poller) markSeen(fingerprint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seenErrors[fingerprint] = time.Now()
}

func (p *Poller) cleanupSeenErrors() {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoff := time.Now().Add(-p.windowSize)
	for fp, seenAt := range p.seenErrors {
		if seenAt.Before(cutoff) {
			delete(p.seenErrors, fp)
		}
	}

	p.logger.Debug("cleaned up seen errors", "remaining", len(p.seenErrors))
}

// extractMessage attempts to extract a clean error message from a log line
func extractMessage(line string) string {
	// Try to extract JSON message field
	if msg := extractJSONField(line, "message", "msg", "error", "err"); msg != "" {
		return msg
	}

	// Try to extract common log format message
	// e.g., "2024-01-01 10:00:00 ERROR some error message"
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:error|fatal|panic|exception|fail(?:ed|ure)?)\b[:\s]+(.+)`),
		regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[^\s]*\s+\w+\s+(.+)`),
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(line); len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}

	// Return the whole line, truncated
	if len(line) > 500 {
		return line[:500] + "..."
	}
	return line
}

// extractJSONField tries to extract a field from a JSON log line
func extractJSONField(line string, fields ...string) string {
	for _, field := range fields {
		// Simple regex-based extraction (faster than full JSON parsing)
		pattern := regexp.MustCompile(fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, field))
		if matches := pattern.FindStringSubmatch(line); len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// generateFingerprint creates a fingerprint for deduplication
// Uses namespace, pod base name, container, and normalized message
func generateFingerprint(namespace, pod, container, message string) string {
	// Normalize pod name by removing random suffix
	// e.g., "my-app-7d4f8b9c5d-abc12" -> "my-app"
	podBase := normalizePodName(pod)

	// Normalize message by removing variable parts
	normalizedMsg := normalizeMessage(message)

	data := fmt.Sprintf("%s|%s|%s|%s", namespace, podBase, container, normalizedMsg)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}

// normalizePodName removes the random suffix from pod names
func normalizePodName(pod string) string {
	// Match deployment pods: name-<replicaset-hash>-<pod-hash>
	deployPattern := regexp.MustCompile(`^(.+)-[a-z0-9]{8,10}-[a-z0-9]{5}$`)
	if matches := deployPattern.FindStringSubmatch(pod); len(matches) > 1 {
		return matches[1]
	}

	// Match statefulset pods: name-<ordinal>
	stsPattern := regexp.MustCompile(`^(.+)-\d+$`)
	if matches := stsPattern.FindStringSubmatch(pod); len(matches) > 1 {
		return matches[1]
	}

	// Match job pods: name-<random>
	jobPattern := regexp.MustCompile(`^(.+)-[a-z0-9]{5}$`)
	if matches := jobPattern.FindStringSubmatch(pod); len(matches) > 1 {
		return matches[1]
	}

	return pod
}

// normalizeMessage removes variable parts from error messages
func normalizeMessage(message string) string {
	// Remove timestamps
	msg := regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}[.\d]*[Z]?`).ReplaceAllString(message, "")

	// Remove UUIDs
	msg = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).ReplaceAllString(msg, "<UUID>")

	// Remove hex IDs
	msg = regexp.MustCompile(`\b[a-f0-9]{24,}\b`).ReplaceAllString(msg, "<ID>")

	// Remove IP addresses
	msg = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?`).ReplaceAllString(msg, "<IP>")

	// Remove numbers (but keep error codes like 404, 500)
	msg = regexp.MustCompile(`\b\d{6,}\b`).ReplaceAllString(msg, "<NUM>")

	return strings.TrimSpace(msg)
}

// generateID creates a unique ID for an error
func generateID() string {
	data := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}
