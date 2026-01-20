package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kube-sentinel/kube-sentinel/internal/config"
	"github.com/kube-sentinel/kube-sentinel/internal/loki"
	"github.com/kube-sentinel/kube-sentinel/internal/remediation"
	"github.com/kube-sentinel/kube-sentinel/internal/rules"
	"github.com/kube-sentinel/kube-sentinel/internal/store"
	"github.com/kube-sentinel/kube-sentinel/internal/web"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "", "Path to config file")
	rulesPath := flag.String("rules", "", "Path to rules file (overrides config)")
	showVersion := flag.Bool("version", false, "Show version information")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("kube-sentinel %s (commit: %s, built: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	// Setup logger
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	logger.Info("starting kube-sentinel",
		"version", version,
		"commit", commit,
		"build_date", buildDate,
	)

	// Load configuration
	cfg, err := config.LoadOrDefault(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override rules path if specified
	if *rulesPath != "" {
		cfg.RulesFile = *rulesPath
	}

	// Load rules
	var rulesList []rules.Rule
	if cfg.RulesFile != "" {
		loader := rules.NewLoader(cfg.RulesFile)
		rulesList, err = loader.Load()
		if err != nil {
			logger.Warn("failed to load rules file, using defaults", "error", err, "path", cfg.RulesFile)
			rulesList = rules.DefaultRules()
		}
	} else {
		rulesList = rules.DefaultRules()
	}

	logger.Info("loaded rules", "count", len(rulesList))

	// Initialize rule engine
	ruleEngine, err := rules.NewEngine(rulesList, logger)
	if err != nil {
		logger.Error("failed to create rule engine", "error", err)
		os.Exit(1)
	}

	// Initialize store
	dataStore := store.NewMemoryStore()

	// Initialize Kubernetes client (optional)
	var k8sClient kubernetes.Interface
	if cfg.Remediation.Enabled {
		k8sClient, err = createK8sClient(cfg.Kubernetes)
		if err != nil {
			logger.Warn("failed to create kubernetes client, remediation will be disabled", "error", err)
		}
	}

	// Initialize remediation engine
	remEngine := remediation.NewEngine(k8sClient, dataStore, remediation.EngineConfig{
		Enabled:            cfg.Remediation.Enabled && k8sClient != nil,
		DryRun:             cfg.Remediation.DryRun,
		MaxActionsPerHour:  cfg.Remediation.MaxActionsPerHour,
		ExcludedNamespaces: cfg.Remediation.ExcludedNamespaces,
	}, logger)

	// Initialize web server
	webServer, err := web.NewServer(cfg.Web.Listen, dataStore, ruleEngine, remEngine, logger)
	if err != nil {
		logger.Error("failed to create web server", "error", err)
		os.Exit(1)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Initialize Loki client
	lokiOpts := []loki.ClientOption{}
	if cfg.Loki.TenantID != "" {
		lokiOpts = append(lokiOpts, loki.WithTenantID(cfg.Loki.TenantID))
	}
	if cfg.Loki.Username != "" && cfg.Loki.Password != "" {
		lokiOpts = append(lokiOpts, loki.WithBasicAuth(cfg.Loki.Username, cfg.Loki.Password))
	}

	lokiClient := loki.NewClient(cfg.Loki.URL, lokiOpts...)

	// Error handler - processes errors from Loki
	errorHandler := func(errors []loki.ParsedError) {
		for _, e := range errors {
			// Match against rules
			matched := ruleEngine.Match(e)
			if matched == nil {
				continue
			}

			// Store the error
			storeErr := &store.Error{
				ID:          matched.ID,
				Fingerprint: matched.Fingerprint,
				Timestamp:   matched.Timestamp,
				Namespace:   matched.Namespace,
				Pod:         matched.Pod,
				Container:   matched.Container,
				Message:     matched.Message,
				Priority:    matched.Priority,
				Count:       matched.Count,
				FirstSeen:   matched.FirstSeen,
				LastSeen:    matched.LastSeen,
				RuleMatched: matched.RuleName,
				Labels:      matched.Labels,
			}

			if err := dataStore.SaveError(storeErr); err != nil {
				logger.Error("failed to save error", "error", err)
				continue
			}

			// Broadcast to WebSocket clients
			webServer.BroadcastError(storeErr)

			// Execute remediation
			if remEngine.IsEnabled() {
				log, err := remEngine.ProcessError(ctx, matched, ruleEngine)
				if err != nil {
					logger.Error("remediation failed", "error", err)
				}
				if log != nil {
					webServer.BroadcastRemediation(log)

					// Mark error as remediated if action succeeded
					if log.Status == "success" {
						storeErr.Remediated = true
						now := time.Now()
						storeErr.RemediatedAt = &now
						dataStore.UpdateError(storeErr)
					}
				}
			}
		}

		// Broadcast updated stats
		webServer.BroadcastStats()
	}

	// Create poller
	poller := loki.NewPoller(
		lokiClient,
		cfg.Loki.Query,
		cfg.Loki.PollInterval,
		cfg.Loki.Lookback,
		errorHandler,
		loki.WithLogger(logger),
	)

	// Start components
	errCh := make(chan error, 2)

	// Start poller
	go func() {
		logger.Info("starting loki poller")
		if err := poller.Start(ctx); err != nil && err != context.Canceled {
			errCh <- fmt.Errorf("poller error: %w", err)
		}
	}()

	// Start web server
	go func() {
		logger.Info("starting web server", "addr", cfg.Web.Listen)
		if err := webServer.Start(); err != nil && err.Error() != "http: Server closed" {
			errCh <- fmt.Errorf("web server error: %w", err)
		}
	}()

	// Start periodic cleanup
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Clean up old errors (older than 7 days)
				cutoff := time.Now().Add(-7 * 24 * time.Hour)
				deleted, _ := dataStore.DeleteOldErrors(cutoff)
				if deleted > 0 {
					logger.Info("cleaned up old errors", "count", deleted)
				}

				// Clean up old remediation logs (older than 30 days)
				logCutoff := time.Now().Add(-30 * 24 * time.Hour)
				logDeleted, _ := dataStore.DeleteOldRemediationLogs(logCutoff)
				if logDeleted > 0 {
					logger.Info("cleaned up old remediation logs", "count", logDeleted)
				}
			}
		}
	}()

	// Wait for shutdown or error
	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		logger.Error("component error", "error", err)
		cancel()
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := webServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("web server shutdown error", "error", err)
	}

	if err := dataStore.Close(); err != nil {
		logger.Error("store close error", "error", err)
	}

	logger.Info("shutdown complete")
}

func createK8sClient(cfg config.KubernetesConfig) (kubernetes.Interface, error) {
	var restConfig *rest.Config
	var err error

	if cfg.InCluster {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
		}
	} else {
		kubeconfig := cfg.Kubeconfig
		if kubeconfig == "" {
			kubeconfig = clientcmd.RecommendedHomeFile
		}
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create config from kubeconfig: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return client, nil
}
