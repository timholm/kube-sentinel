package remediation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kube-sentinel/kube-sentinel/internal/rules"
	"github.com/kube-sentinel/kube-sentinel/internal/store"
	"k8s.io/client-go/kubernetes"
)

// Engine handles remediation actions with safety controls
type Engine struct {
	mu sync.RWMutex

	enabled            bool
	dryRun             bool
	maxActionsPerHour  int
	excludedNamespaces map[string]bool

	actions   map[string]Action
	cooldowns map[string]time.Time // key: rule+target, value: cooldown expires at
	hourlyLog []time.Time          // timestamps of actions in the last hour

	store  store.Store
	logger *slog.Logger
}

// EngineConfig configures the remediation engine
type EngineConfig struct {
	Enabled            bool
	DryRun             bool
	MaxActionsPerHour  int
	ExcludedNamespaces []string
}

// NewEngine creates a new remediation engine
func NewEngine(client kubernetes.Interface, store store.Store, cfg EngineConfig, logger *slog.Logger) *Engine {
	excluded := make(map[string]bool)
	for _, ns := range cfg.ExcludedNamespaces {
		excluded[ns] = true
	}

	e := &Engine{
		enabled:            cfg.Enabled,
		dryRun:             cfg.DryRun,
		maxActionsPerHour:  cfg.MaxActionsPerHour,
		excludedNamespaces: excluded,
		actions:            make(map[string]Action),
		cooldowns:          make(map[string]time.Time),
		hourlyLog:          []time.Time{},
		store:              store,
		logger:             logger,
	}

	// Register built-in actions
	if client != nil {
		e.RegisterAction(NewRestartPodAction(client))
		e.RegisterAction(NewScaleUpAction(client))
		e.RegisterAction(NewScaleDownAction(client))
		e.RegisterAction(NewRollbackAction(client))
		e.RegisterAction(NewDeleteStuckPodsAction(client))
	}
	e.RegisterAction(NewNoneAction())

	return e
}

// RegisterAction registers a remediation action
func (e *Engine) RegisterAction(action Action) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actions[action.Name()] = action
}

// GetAction returns an action by name
func (e *Engine) GetAction(name string) (Action, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	action, ok := e.actions[name]
	return action, ok
}

// Execute runs a remediation action for a matched error
func (e *Engine) Execute(ctx context.Context, err *rules.MatchedError, rule *rules.Rule) (*store.RemediationLog, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	logEntry := &store.RemediationLog{
		ID:        generateLogID(),
		ErrorID:   err.ID,
		Timestamp: time.Now(),
		DryRun:    e.dryRun,
	}

	target := Target{
		Namespace: err.Namespace,
		Pod:       err.Pod,
		Container: err.Container,
	}
	logEntry.Target = target.String()

	// Check if remediation is enabled
	if !e.enabled {
		logEntry.Action = string(rule.Remediation.Action)
		logEntry.Status = "skipped"
		logEntry.Message = "remediation disabled"
		e.saveLog(logEntry)
		return logEntry, nil
	}

	// Check if action is "none"
	if rule.Remediation.Action == rules.ActionNone {
		logEntry.Action = "none"
		logEntry.Status = "skipped"
		logEntry.Message = "no remediation action configured"
		e.saveLog(logEntry)
		return logEntry, nil
	}

	logEntry.Action = string(rule.Remediation.Action)

	// Check excluded namespaces
	if e.excludedNamespaces[err.Namespace] {
		logEntry.Status = "skipped"
		logEntry.Message = fmt.Sprintf("namespace %s is excluded", err.Namespace)
		e.saveLog(logEntry)
		return logEntry, nil
	}

	// Check cooldown
	cooldownKey := fmt.Sprintf("%s:%s", rule.Name, target.String())
	if expiresAt, ok := e.cooldowns[cooldownKey]; ok && time.Now().Before(expiresAt) {
		logEntry.Status = "skipped"
		logEntry.Message = fmt.Sprintf("cooldown active until %s", expiresAt.Format(time.RFC3339))
		e.saveLog(logEntry)
		return logEntry, nil
	}

	// Check hourly rate limit
	e.cleanupHourlyLog()
	if len(e.hourlyLog) >= e.maxActionsPerHour {
		logEntry.Status = "skipped"
		logEntry.Message = fmt.Sprintf("hourly limit reached (%d actions)", e.maxActionsPerHour)
		e.saveLog(logEntry)
		return logEntry, nil
	}

	// Get the action
	action, ok := e.actions[string(rule.Remediation.Action)]
	if !ok {
		logEntry.Status = "failed"
		logEntry.Message = fmt.Sprintf("unknown action: %s", rule.Remediation.Action)
		e.saveLog(logEntry)
		return logEntry, fmt.Errorf("unknown action: %s", rule.Remediation.Action)
	}

	// Validate params
	if err := action.Validate(rule.Remediation.Params); err != nil {
		logEntry.Status = "failed"
		logEntry.Message = fmt.Sprintf("invalid params: %v", err)
		e.saveLog(logEntry)
		return logEntry, err
	}

	// Execute (or dry run)
	if e.dryRun {
		logEntry.Status = "success"
		logEntry.Message = "dry run - would execute action"
		e.logger.Info("dry run remediation",
			"action", rule.Remediation.Action,
			"target", target.String(),
			"rule", rule.Name,
		)
	} else {
		e.logger.Info("executing remediation",
			"action", rule.Remediation.Action,
			"target", target.String(),
			"rule", rule.Name,
		)

		if execErr := action.Execute(ctx, target, rule.Remediation.Params); execErr != nil {
			logEntry.Status = "failed"
			logEntry.Message = execErr.Error()
			e.saveLog(logEntry)
			return logEntry, execErr
		}

		logEntry.Status = "success"
		logEntry.Message = "action executed successfully"
	}

	// Set cooldown
	e.cooldowns[cooldownKey] = time.Now().Add(rule.Remediation.Cooldown)

	// Record in hourly log
	e.hourlyLog = append(e.hourlyLog, time.Now())

	e.saveLog(logEntry)
	return logEntry, nil
}

// SetEnabled enables or disables remediation
func (e *Engine) SetEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = enabled
}

// SetDryRun enables or disables dry run mode
func (e *Engine) SetDryRun(dryRun bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dryRun = dryRun
}

// IsEnabled returns whether remediation is enabled
func (e *Engine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// IsDryRun returns whether dry run mode is enabled
func (e *Engine) IsDryRun() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dryRun
}

// GetActionsThisHour returns the number of actions taken in the last hour
func (e *Engine) GetActionsThisHour() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cleanupHourlyLog()
	return len(e.hourlyLog)
}

// ClearCooldown removes the cooldown for a specific rule and target
func (e *Engine) ClearCooldown(ruleName, target string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cooldowns, fmt.Sprintf("%s:%s", ruleName, target))
}

// ClearAllCooldowns removes all cooldowns
func (e *Engine) ClearAllCooldowns() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cooldowns = make(map[string]time.Time)
}

func (e *Engine) cleanupHourlyLog() {
	cutoff := time.Now().Add(-time.Hour)
	var kept []time.Time
	for _, t := range e.hourlyLog {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	e.hourlyLog = kept
}

func (e *Engine) saveLog(log *store.RemediationLog) {
	if e.store != nil {
		if err := e.store.SaveRemediationLog(log); err != nil {
			e.logger.Error("failed to save remediation log", "error", err)
		}
	}
}

func generateLogID() string {
	data := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}

// ProcessError handles an error by matching rules and executing remediation
func (e *Engine) ProcessError(ctx context.Context, err *rules.MatchedError, ruleEngine *rules.Engine) (*store.RemediationLog, error) {
	rule := ruleEngine.GetRuleByName(err.RuleName)
	if rule == nil {
		return nil, fmt.Errorf("rule not found: %s", err.RuleName)
	}

	if rule.Remediation == nil {
		return nil, nil
	}

	return e.Execute(ctx, err, rule)
}
