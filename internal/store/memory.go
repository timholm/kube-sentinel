package store

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kube-sentinel/kube-sentinel/internal/rules"
)

// MemoryStore implements Store with in-memory storage
type MemoryStore struct {
	mu sync.RWMutex

	errors           map[string]*Error            // by ID
	errorsByFP       map[string]*Error            // by fingerprint
	remediationLogs  map[string]*RemediationLog   // by ID
	remediationsByErr map[string][]*RemediationLog // by error ID

	maxErrors          int
	maxRemediationLogs int
}

// MemoryStoreOption configures a MemoryStore
type MemoryStoreOption func(*MemoryStore)

// WithMaxErrors sets the maximum number of errors to retain
func WithMaxErrors(max int) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.maxErrors = max
	}
}

// WithMaxRemediationLogs sets the maximum number of remediation logs to retain
func WithMaxRemediationLogs(max int) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.maxRemediationLogs = max
	}
}

// NewMemoryStore creates a new in-memory store
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		errors:            make(map[string]*Error),
		errorsByFP:        make(map[string]*Error),
		remediationLogs:   make(map[string]*RemediationLog),
		remediationsByErr: make(map[string][]*RemediationLog),
		maxErrors:         10000,
		maxRemediationLogs: 5000,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SaveError stores an error
func (s *MemoryStore) SaveError(err *Error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we already have this error by fingerprint
	if existing, ok := s.errorsByFP[err.Fingerprint]; ok {
		// Update existing error
		existing.Count++
		existing.LastSeen = err.Timestamp
		if err.Timestamp.Before(existing.FirstSeen) {
			existing.FirstSeen = err.Timestamp
		}
		return nil
	}

	// Store new error
	s.errors[err.ID] = err
	s.errorsByFP[err.Fingerprint] = err

	// Cleanup if over limit
	if len(s.errors) > s.maxErrors {
		s.cleanupOldErrors()
	}

	return nil
}

// GetError retrieves an error by ID
func (s *MemoryStore) GetError(id string) (*Error, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err, ok := s.errors[id]; ok {
		return err, nil
	}
	return nil, fmt.Errorf("error not found: %s", id)
}

// GetErrorByFingerprint retrieves an error by fingerprint
func (s *MemoryStore) GetErrorByFingerprint(fingerprint string) (*Error, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err, ok := s.errorsByFP[fingerprint]; ok {
		return err, nil
	}
	return nil, fmt.Errorf("error not found with fingerprint: %s", fingerprint)
}

// ListErrors returns errors matching the filter
func (s *MemoryStore) ListErrors(filter ErrorFilter, opts PaginationOptions) ([]*Error, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect and filter errors
	var filtered []*Error
	for _, err := range s.errors {
		if s.matchesFilter(err, filter) {
			filtered = append(filtered, err)
		}
	}

	// Sort by priority (weight) then by last seen (newest first)
	sort.Slice(filtered, func(i, j int) bool {
		wi := filtered[i].Priority.Weight()
		wj := filtered[j].Priority.Weight()
		if wi != wj {
			return wi < wj
		}
		return filtered[i].LastSeen.After(filtered[j].LastSeen)
	})

	total := len(filtered)

	// Apply pagination
	if opts.Offset > 0 {
		if opts.Offset >= len(filtered) {
			return []*Error{}, total, nil
		}
		filtered = filtered[opts.Offset:]
	}
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	return filtered, total, nil
}

// UpdateError updates an existing error
func (s *MemoryStore) UpdateError(err *Error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.errors[err.ID]; !ok {
		return fmt.Errorf("error not found: %s", err.ID)
	}

	s.errors[err.ID] = err
	s.errorsByFP[err.Fingerprint] = err
	return nil
}

// DeleteError removes an error by ID
func (s *MemoryStore) DeleteError(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err, ok := s.errors[id]
	if !ok {
		return fmt.Errorf("error not found: %s", id)
	}

	delete(s.errors, id)
	delete(s.errorsByFP, err.Fingerprint)
	return nil
}

// DeleteOldErrors removes errors older than the given time
func (s *MemoryStore) DeleteOldErrors(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, err := range s.errors {
		if err.LastSeen.Before(before) {
			delete(s.errors, id)
			delete(s.errorsByFP, err.Fingerprint)
			count++
		}
	}
	return count, nil
}

// SaveRemediationLog stores a remediation log entry
func (s *MemoryStore) SaveRemediationLog(log *RemediationLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.remediationLogs[log.ID] = log
	s.remediationsByErr[log.ErrorID] = append(s.remediationsByErr[log.ErrorID], log)

	// Cleanup if over limit
	if len(s.remediationLogs) > s.maxRemediationLogs {
		s.cleanupOldRemediationLogs()
	}

	return nil
}

// GetRemediationLog retrieves a remediation log by ID
func (s *MemoryStore) GetRemediationLog(id string) (*RemediationLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if log, ok := s.remediationLogs[id]; ok {
		return log, nil
	}
	return nil, fmt.Errorf("remediation log not found: %s", id)
}

// ListRemediationLogs returns all remediation logs with pagination
func (s *MemoryStore) ListRemediationLogs(opts PaginationOptions) ([]*RemediationLog, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var logs []*RemediationLog
	for _, log := range s.remediationLogs {
		logs = append(logs, log)
	}

	// Sort by timestamp (newest first)
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp.After(logs[j].Timestamp)
	})

	total := len(logs)

	// Apply pagination
	if opts.Offset > 0 {
		if opts.Offset >= len(logs) {
			return []*RemediationLog{}, total, nil
		}
		logs = logs[opts.Offset:]
	}
	if opts.Limit > 0 && len(logs) > opts.Limit {
		logs = logs[:opts.Limit]
	}

	return logs, total, nil
}

// ListRemediationLogsForError returns remediation logs for a specific error
func (s *MemoryStore) ListRemediationLogsForError(errorID string) ([]*RemediationLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs, ok := s.remediationsByErr[errorID]
	if !ok {
		return []*RemediationLog{}, nil
	}

	// Return a copy sorted by timestamp
	result := make([]*RemediationLog, len(logs))
	copy(result, logs)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	return result, nil
}

// DeleteOldRemediationLogs removes remediation logs older than the given time
func (s *MemoryStore) DeleteOldRemediationLogs(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, log := range s.remediationLogs {
		if log.Timestamp.Before(before) {
			delete(s.remediationLogs, id)
			count++
		}
	}

	// Clean up the by-error index
	for errID, logs := range s.remediationsByErr {
		var kept []*RemediationLog
		for _, log := range logs {
			if !log.Timestamp.Before(before) {
				kept = append(kept, log)
			}
		}
		if len(kept) == 0 {
			delete(s.remediationsByErr, errID)
		} else {
			s.remediationsByErr[errID] = kept
		}
	}

	return count, nil
}

// GetStats returns aggregate statistics
func (s *MemoryStore) GetStats() (*Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &Stats{
		TotalErrors:       len(s.errors),
		ErrorsByPriority:  make(map[rules.Priority]int),
		ErrorsByNamespace: make(map[string]int),
		RemediationCount:  len(s.remediationLogs),
	}

	var lastError time.Time
	for _, err := range s.errors {
		stats.ErrorsByPriority[err.Priority]++
		stats.ErrorsByNamespace[err.Namespace]++
		if err.LastSeen.After(lastError) {
			lastError = err.LastSeen
		}
	}
	if !lastError.IsZero() {
		stats.LastError = &lastError
	}

	var lastRemediation time.Time
	for _, log := range s.remediationLogs {
		if log.Status == "success" {
			stats.SuccessfulActions++
		} else if log.Status == "failed" {
			stats.FailedActions++
		}
		if log.Timestamp.After(lastRemediation) {
			lastRemediation = log.Timestamp
		}
	}
	if !lastRemediation.IsZero() {
		stats.LastRemediation = &lastRemediation
	}

	return stats, nil
}

// Close closes the store (no-op for memory store)
func (s *MemoryStore) Close() error {
	return nil
}

func (s *MemoryStore) matchesFilter(err *Error, filter ErrorFilter) bool {
	if filter.Namespace != "" && err.Namespace != filter.Namespace {
		return false
	}
	if filter.Pod != "" && !strings.Contains(err.Pod, filter.Pod) {
		return false
	}
	if filter.Priority != "" && err.Priority != filter.Priority {
		return false
	}
	if filter.Remediated != nil && err.Remediated != *filter.Remediated {
		return false
	}
	if !filter.Since.IsZero() && err.LastSeen.Before(filter.Since) {
		return false
	}
	if filter.Search != "" {
		search := strings.ToLower(filter.Search)
		if !strings.Contains(strings.ToLower(err.Message), search) &&
			!strings.Contains(strings.ToLower(err.Pod), search) &&
			!strings.Contains(strings.ToLower(err.Namespace), search) {
			return false
		}
	}
	return true
}

func (s *MemoryStore) cleanupOldErrors() {
	// Remove oldest errors to get back under limit
	var errors []*Error
	for _, err := range s.errors {
		errors = append(errors, err)
	}

	sort.Slice(errors, func(i, j int) bool {
		return errors[i].LastSeen.Before(errors[j].LastSeen)
	})

	// Remove oldest 10%
	toRemove := len(errors) / 10
	for i := 0; i < toRemove; i++ {
		delete(s.errors, errors[i].ID)
		delete(s.errorsByFP, errors[i].Fingerprint)
	}
}

func (s *MemoryStore) cleanupOldRemediationLogs() {
	var logs []*RemediationLog
	for _, log := range s.remediationLogs {
		logs = append(logs, log)
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp.Before(logs[j].Timestamp)
	})

	// Remove oldest 10%
	toRemove := len(logs) / 10
	for i := 0; i < toRemove; i++ {
		delete(s.remediationLogs, logs[i].ID)
	}
}
