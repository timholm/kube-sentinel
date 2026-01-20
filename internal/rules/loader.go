package rules

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Loader handles loading and reloading rules from YAML files
type Loader struct {
	path string
}

// NewLoader creates a new rule loader
func NewLoader(path string) *Loader {
	return &Loader{path: path}
}

// Load reads rules from the configured file path
func (l *Loader) Load() ([]Rule, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("reading rules file: %w", err)
	}

	return ParseRules(data)
}

// ParseRules parses rules from YAML bytes
func ParseRules(data []byte) ([]Rule, error) {
	var config RulesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing rules YAML: %w", err)
	}

	// Set defaults and validate
	for i := range config.Rules {
		rule := &config.Rules[i]

		// Default enabled to true
		if !rule.Enabled {
			rule.Enabled = true
		}

		// Default cooldown
		if rule.Remediation != nil && rule.Remediation.Cooldown == 0 {
			rule.Remediation.Cooldown = 5 * time.Minute
		}

		// Default action to none
		if rule.Remediation == nil {
			rule.Remediation = &Remediation{
				Action:   ActionNone,
				Cooldown: 5 * time.Minute,
			}
		}

		if err := rule.Validate(); err != nil {
			return nil, err
		}
	}

	return config.Rules, nil
}

// DefaultRules returns a set of sensible default rules
func DefaultRules() []Rule {
	return []Rule{
		{
			Name: "crashloop-backoff",
			Match: Match{
				Pattern: `CrashLoopBackOff|Back-off restarting failed container`,
			},
			Priority: PriorityCritical,
			Remediation: &Remediation{
				Action:   ActionRestartPod,
				Cooldown: 5 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "oom-killed",
			Match: Match{
				Pattern: `OOMKilled|Out of memory|memory cgroup out of memory`,
			},
			Priority: PriorityCritical,
			Remediation: &Remediation{
				Action:   ActionNone, // Can't auto-fix OOM without resource changes
				Cooldown: 10 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "image-pull-error",
			Match: Match{
				Pattern: `ImagePullBackOff|ErrImagePull|failed to pull image`,
			},
			Priority: PriorityHigh,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 5 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "readiness-probe-failed",
			Match: Match{
				Pattern: `Readiness probe failed|Liveness probe failed`,
			},
			Priority: PriorityHigh,
			Remediation: &Remediation{
				Action:   ActionRestartPod,
				Cooldown: 3 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "connection-refused",
			Match: Match{
				Pattern: `connection refused|ECONNREFUSED|dial tcp.*refused`,
			},
			Priority: PriorityMedium,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 2 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "context-deadline-exceeded",
			Match: Match{
				Pattern: `context deadline exceeded|context canceled|timeout`,
			},
			Priority: PriorityMedium,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 2 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "panic",
			Match: Match{
				Pattern: `(?i)panic:|runtime error:|fatal error:`,
			},
			Priority: PriorityCritical,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 5 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "permission-denied",
			Match: Match{
				Pattern: `permission denied|access denied|forbidden|unauthorized`,
			},
			Priority: PriorityMedium,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 5 * time.Minute,
			},
			Enabled: true,
		},
		{
			Name: "generic-error",
			Match: Match{
				Pattern: `(?i)\berror\b`,
			},
			Priority: PriorityLow,
			Remediation: &Remediation{
				Action:   ActionNone,
				Cooldown: 5 * time.Minute,
			},
			Enabled: true,
		},
	}
}

// Watch starts watching the rules file for changes
// Returns a channel that emits when rules are updated
func (l *Loader) Watch() (<-chan []Rule, error) {
	ch := make(chan []Rule, 1)

	// For simplicity, poll the file every 30 seconds
	// Could use fsnotify for proper file watching
	go func() {
		var lastModTime time.Time
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			info, err := os.Stat(l.path)
			if err != nil {
				continue
			}

			if info.ModTime().After(lastModTime) {
				lastModTime = info.ModTime()
				rules, err := l.Load()
				if err != nil {
					continue
				}
				ch <- rules
			}
		}
	}()

	return ch, nil
}
