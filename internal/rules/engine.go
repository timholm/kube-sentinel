package rules

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/kube-sentinel/kube-sentinel/internal/loki"
)

// Engine handles rule matching and prioritization
type Engine struct {
	mu     sync.RWMutex
	rules  []Rule
	logger *slog.Logger

	// Compiled regex patterns
	patterns map[string]*regexp.Regexp
}

// NewEngine creates a new rule engine
func NewEngine(rules []Rule, logger *slog.Logger) (*Engine, error) {
	e := &Engine{
		rules:    rules,
		logger:   logger,
		patterns: make(map[string]*regexp.Regexp),
	}

	// Pre-compile regex patterns
	for _, rule := range rules {
		if rule.Match.Pattern != "" {
			re, err := regexp.Compile(rule.Match.Pattern)
			if err != nil {
				return nil, err
			}
			e.patterns[rule.Name] = re
		}
	}

	return e, nil
}

// UpdateRules replaces the current rules with new ones
func (e *Engine) UpdateRules(rules []Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	patterns := make(map[string]*regexp.Regexp)
	for _, rule := range rules {
		if rule.Match.Pattern != "" {
			re, err := regexp.Compile(rule.Match.Pattern)
			if err != nil {
				return err
			}
			patterns[rule.Name] = re
		}
	}

	e.rules = rules
	e.patterns = patterns
	return nil
}

// GetRules returns a copy of the current rules
func (e *Engine) GetRules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Rule, len(e.rules))
	copy(result, e.rules)
	return result
}

// Match attempts to match a parsed error against all rules
// Returns the matched error with priority, or nil if no rules matched
func (e *Engine) Match(err loki.ParsedError) *MatchedError {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Try rules in order (first match wins)
	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}

		if e.matchRule(rule, err) {
			return &MatchedError{
				ID:          err.ID,
				Fingerprint: err.Fingerprint,
				Timestamp:   err.Timestamp,
				Namespace:   err.Namespace,
				Pod:         err.Pod,
				Container:   err.Container,
				Message:     err.Message,
				Labels:      err.Labels,
				Raw:         err.Raw,
				Priority:    rule.Priority,
				RuleName:    rule.Name,
				Count:       1,
				FirstSeen:   err.Timestamp,
				LastSeen:    err.Timestamp,
			}
		}
	}

	// No rule matched - assign default low priority
	return &MatchedError{
		ID:          err.ID,
		Fingerprint: err.Fingerprint,
		Timestamp:   err.Timestamp,
		Namespace:   err.Namespace,
		Pod:         err.Pod,
		Container:   err.Container,
		Message:     err.Message,
		Labels:      err.Labels,
		Raw:         err.Raw,
		Priority:    PriorityLow,
		RuleName:    "default",
		Count:       1,
		FirstSeen:   err.Timestamp,
		LastSeen:    err.Timestamp,
	}
}

// MatchBatch matches multiple errors and returns all matched errors
func (e *Engine) MatchBatch(errors []loki.ParsedError) []*MatchedError {
	result := make([]*MatchedError, 0, len(errors))
	for _, err := range errors {
		if matched := e.Match(err); matched != nil {
			result = append(result, matched)
		}
	}
	return result
}

// GetRuleByName returns a rule by its name
func (e *Engine) GetRuleByName(name string) *Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if rule.Name == name {
			return &rule
		}
	}
	return nil
}

func (e *Engine) matchRule(rule Rule, err loki.ParsedError) bool {
	// Check namespace filter
	if len(rule.Match.Namespaces) > 0 {
		if !e.matchNamespace(rule.Match.Namespaces, err.Namespace) {
			return false
		}
	}

	// Check label matchers
	if len(rule.Match.Labels) > 0 {
		if !e.matchLabels(rule.Match.Labels, err.Labels) {
			return false
		}
	}

	// Check pattern
	if rule.Match.Pattern != "" {
		re := e.patterns[rule.Name]
		if re != nil {
			// Match against both message and raw log line
			if !re.MatchString(err.Message) && !re.MatchString(err.Raw) {
				return false
			}
		}
	}

	// Check keywords
	if len(rule.Match.Keywords) > 0 {
		if !e.matchKeywords(rule.Match.Keywords, err.Message, err.Raw) {
			return false
		}
	}

	return true
}

func (e *Engine) matchNamespace(allowed []string, namespace string) bool {
	for _, ns := range allowed {
		// Support negation with !
		if strings.HasPrefix(ns, "!") {
			if namespace == ns[1:] {
				return false
			}
		} else if namespace == ns {
			return true
		}
	}

	// If all rules are negations, and none matched, allow
	allNegations := true
	for _, ns := range allowed {
		if !strings.HasPrefix(ns, "!") {
			allNegations = false
			break
		}
	}
	return allNegations
}

func (e *Engine) matchLabels(matchers map[string]string, labels map[string]string) bool {
	for key, expected := range matchers {
		actual, exists := labels[key]

		// Support negation with !
		if strings.HasPrefix(expected, "!") {
			if actual == expected[1:] {
				return false
			}
			continue
		}

		if !exists {
			return false
		}

		// Support regex matching with ~
		if strings.HasPrefix(expected, "~") {
			re, err := regexp.Compile(expected[1:])
			if err != nil {
				return false
			}
			if !re.MatchString(actual) {
				return false
			}
			continue
		}

		if actual != expected {
			return false
		}
	}
	return true
}

func (e *Engine) matchKeywords(keywords []string, message, raw string) bool {
	combined := strings.ToLower(message + " " + raw)
	for _, kw := range keywords {
		if strings.Contains(combined, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// TestPattern tests if a pattern matches sample text
func (e *Engine) TestPattern(pattern, sample string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(sample), nil
}
