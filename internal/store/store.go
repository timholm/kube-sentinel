package store

import (
	"time"

	"github.com/kube-sentinel/kube-sentinel/internal/rules"
)

// Error represents a stored error
type Error struct {
	ID           string
	Fingerprint  string
	Timestamp    time.Time
	Namespace    string
	Pod          string
	Container    string
	Message      string
	Priority     rules.Priority
	Count        int
	FirstSeen    time.Time
	LastSeen     time.Time
	RuleMatched  string
	Remediated   bool
	RemediatedAt *time.Time
	Labels       map[string]string
}

// RemediationLog represents a remediation action log entry
type RemediationLog struct {
	ID        string
	ErrorID   string
	Action    string
	Target    string // namespace/pod or namespace/deployment
	Status    string // success, failed, skipped
	Message   string
	Timestamp time.Time
	DryRun    bool
}

// ErrorFilter defines filtering options for error queries
type ErrorFilter struct {
	Namespace  string
	Pod        string
	Priority   rules.Priority
	Remediated *bool
	Since      time.Time
	Search     string
}

// PaginationOptions defines pagination for queries
type PaginationOptions struct {
	Offset int
	Limit  int
}

// Store defines the interface for error and remediation storage
type Store interface {
	// Error operations
	SaveError(err *Error) error
	GetError(id string) (*Error, error)
	GetErrorByFingerprint(fingerprint string) (*Error, error)
	ListErrors(filter ErrorFilter, opts PaginationOptions) ([]*Error, int, error)
	UpdateError(err *Error) error
	DeleteError(id string) error
	DeleteOldErrors(before time.Time) (int, error)

	// Remediation log operations
	SaveRemediationLog(log *RemediationLog) error
	GetRemediationLog(id string) (*RemediationLog, error)
	ListRemediationLogs(opts PaginationOptions) ([]*RemediationLog, int, error)
	ListRemediationLogsForError(errorID string) ([]*RemediationLog, error)
	DeleteOldRemediationLogs(before time.Time) (int, error)

	// Statistics
	GetStats() (*Stats, error)

	// Lifecycle
	Close() error
}

// Stats contains aggregate statistics
type Stats struct {
	TotalErrors       int
	ErrorsByPriority  map[rules.Priority]int
	ErrorsByNamespace map[string]int
	RemediationCount  int
	SuccessfulActions int
	FailedActions     int
	LastError         *time.Time
	LastRemediation   *time.Time
}
