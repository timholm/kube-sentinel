package rules

import (
	"fmt"
	"time"
)

// Priority represents the severity level of an error
type Priority string

const (
	PriorityCritical Priority = "P1"
	PriorityHigh     Priority = "P2"
	PriorityMedium   Priority = "P3"
	PriorityLow      Priority = "P4"
)

// ParsePriority parses a string into a Priority
func ParsePriority(s string) (Priority, error) {
	switch s {
	case "P1", "p1", "critical", "CRITICAL":
		return PriorityCritical, nil
	case "P2", "p2", "high", "HIGH":
		return PriorityHigh, nil
	case "P3", "p3", "medium", "MEDIUM":
		return PriorityMedium, nil
	case "P4", "p4", "low", "LOW":
		return PriorityLow, nil
	default:
		return "", fmt.Errorf("unknown priority: %s", s)
	}
}

// Weight returns the numeric weight of a priority (lower = more important)
func (p Priority) Weight() int {
	switch p {
	case PriorityCritical:
		return 1
	case PriorityHigh:
		return 2
	case PriorityMedium:
		return 3
	case PriorityLow:
		return 4
	default:
		return 5
	}
}

// String returns the string representation of a priority
func (p Priority) String() string {
	return string(p)
}

// Label returns a human-readable label for the priority
func (p Priority) Label() string {
	switch p {
	case PriorityCritical:
		return "Critical"
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "Unknown"
	}
}

// Color returns a CSS color class for the priority
func (p Priority) Color() string {
	switch p {
	case PriorityCritical:
		return "red"
	case PriorityHigh:
		return "orange"
	case PriorityMedium:
		return "yellow"
	case PriorityLow:
		return "blue"
	default:
		return "gray"
	}
}

// ActionType represents the type of remediation action
type ActionType string

const (
	ActionNone             ActionType = "none"
	ActionRestartPod       ActionType = "restart-pod"
	ActionScaleUp          ActionType = "scale-up"
	ActionScaleDown        ActionType = "scale-down"
	ActionRollback         ActionType = "rollback"
	ActionDeleteStuckPods  ActionType = "delete-stuck-pods"
	ActionExecScript       ActionType = "exec-script"
)

// Rule defines a matching rule for errors
type Rule struct {
	Name        string       `yaml:"name"`
	Match       Match        `yaml:"match"`
	Priority    Priority     `yaml:"priority"`
	Remediation *Remediation `yaml:"remediation,omitempty"`
	Enabled     bool         `yaml:"enabled"`
}

// Match defines the conditions for matching an error
type Match struct {
	Pattern    string            `yaml:"pattern"`              // Regex pattern
	Keywords   []string          `yaml:"keywords,omitempty"`   // Simple keyword match
	Labels     map[string]string `yaml:"labels,omitempty"`     // Label matchers
	Namespaces []string          `yaml:"namespaces,omitempty"` // Namespace whitelist
}

// Remediation defines the action to take when a rule matches
type Remediation struct {
	Action   ActionType        `yaml:"action"`
	Params   map[string]string `yaml:"params,omitempty"`
	Cooldown time.Duration     `yaml:"cooldown"`
}

// RulesConfig represents the top-level rules configuration file
type RulesConfig struct {
	Rules []Rule `yaml:"rules"`
}

// Validate checks if a rule is valid
func (r *Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if r.Match.Pattern == "" && len(r.Match.Keywords) == 0 {
		return fmt.Errorf("rule %s: either pattern or keywords is required", r.Name)
	}

	if _, err := ParsePriority(string(r.Priority)); err != nil {
		return fmt.Errorf("rule %s: %w", r.Name, err)
	}

	return nil
}

// MatchedError represents an error that matched a rule
type MatchedError struct {
	ID          string
	Fingerprint string
	Timestamp   time.Time
	Namespace   string
	Pod         string
	Container   string
	Message     string
	Labels      map[string]string
	Raw         string
	Priority    Priority
	RuleName    string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Remediated  bool
}
