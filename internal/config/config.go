package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main application configuration
type Config struct {
	Loki        LokiConfig        `yaml:"loki"`
	Kubernetes  KubernetesConfig  `yaml:"kubernetes"`
	Web         WebConfig         `yaml:"web"`
	Remediation RemediationConfig `yaml:"remediation"`
	RulesFile   string            `yaml:"rules_file"`
	Store       StoreConfig       `yaml:"store"`
}

// LokiConfig holds Loki connection settings
type LokiConfig struct {
	URL          string        `yaml:"url"`
	Query        string        `yaml:"query"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Lookback     time.Duration `yaml:"lookback"`
	TenantID     string        `yaml:"tenant_id,omitempty"`
	Username     string        `yaml:"username,omitempty"`
	Password     string        `yaml:"password,omitempty"`
}

// KubernetesConfig holds Kubernetes connection settings
type KubernetesConfig struct {
	InCluster  bool   `yaml:"in_cluster"`
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
}

// WebConfig holds web server settings
type WebConfig struct {
	Listen   string `yaml:"listen"`
	BasePath string `yaml:"base_path"`
}

// RemediationConfig holds remediation engine settings
type RemediationConfig struct {
	Enabled           bool     `yaml:"enabled"`
	DryRun            bool     `yaml:"dry_run"`
	MaxActionsPerHour int      `yaml:"max_actions_per_hour"`
	ExcludedNamespaces []string `yaml:"excluded_namespaces"`
}

// StoreConfig holds data store settings
type StoreConfig struct {
	Type string `yaml:"type"` // memory or sqlite
	Path string `yaml:"path,omitempty"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Loki: LokiConfig{
			URL:          "http://loki.monitoring:3100",
			Query:        `{namespace=~".+"} |~ "(?i)(error|fatal|panic|exception|fail)"`,
			PollInterval: 30 * time.Second,
			Lookback:     5 * time.Minute,
		},
		Kubernetes: KubernetesConfig{
			InCluster: true,
		},
		Web: WebConfig{
			Listen: ":8080",
		},
		Remediation: RemediationConfig{
			Enabled:           true,
			DryRun:            false,
			MaxActionsPerHour: 50,
			ExcludedNamespaces: []string{
				"kube-system",
				"monitoring",
			},
		},
		RulesFile: "/etc/kube-sentinel/rules.yaml",
		Store: StoreConfig{
			Type: "memory",
		},
	}
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// LoadOrDefault tries to load config from path, returns defaults if file doesn't exist
func LoadOrDefault(path string) (*Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	return Load(path)
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Loki.URL == "" {
		return fmt.Errorf("loki.url is required")
	}

	if c.Loki.Query == "" {
		return fmt.Errorf("loki.query is required")
	}

	if c.Loki.PollInterval < time.Second {
		return fmt.Errorf("loki.poll_interval must be at least 1s")
	}

	if c.Loki.Lookback < c.Loki.PollInterval {
		return fmt.Errorf("loki.lookback must be >= poll_interval")
	}

	if c.Web.Listen == "" {
		return fmt.Errorf("web.listen is required")
	}

	if c.Remediation.MaxActionsPerHour < 0 {
		return fmt.Errorf("remediation.max_actions_per_hour must be >= 0")
	}

	if c.Store.Type != "memory" && c.Store.Type != "sqlite" {
		return fmt.Errorf("store.type must be 'memory' or 'sqlite'")
	}

	return nil
}
