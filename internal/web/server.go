package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kube-sentinel/kube-sentinel/internal/remediation"
	"github.com/kube-sentinel/kube-sentinel/internal/rules"
	"github.com/kube-sentinel/kube-sentinel/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server handles the web dashboard
type Server struct {
	addr        string
	basePath    string
	store       store.Store
	ruleEngine  *rules.Engine
	remEngine   *remediation.Engine
	logger      *slog.Logger
	templates   map[string]*template.Template
	router      *mux.Router
	httpServer  *http.Server

	// WebSocket clients
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	upgrader websocket.Upgrader
}

// NewServer creates a new web server
func NewServer(addr string, basePath string, store store.Store, ruleEngine *rules.Engine, remEngine *remediation.Engine, logger *slog.Logger) (*Server, error) {
	s := &Server{
		addr:       addr,
		basePath:   basePath,
		store:      store,
		ruleEngine: ruleEngine,
		remEngine:  remEngine,
		logger:     logger,
		clients:    make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for simplicity
			},
		},
	}

	// Parse templates - each page template is parsed with base.html
	s.templates = make(map[string]*template.Template)
	pageTemplates := []string{
		"dashboard.html",
		"errors.html",
		"error_detail.html",
		"rules.html",
		"history.html",
		"settings.html",
	}
	for _, page := range pageTemplates {
		tmpl, err := template.New("").Funcs(s.templateFuncs()).ParseFS(templatesFS, "templates/base.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", page, err)
		}
		s.templates[page] = tmpl
	}

	// Setup routes
	s.router = mux.NewRouter()
	s.setupRoutes()

	return s, nil
}

func (s *Server) setupRoutes() {
	// Static files
	staticSub, _ := fs.Sub(staticFS, "static")
	s.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Pages
	s.router.HandleFunc("/", s.handleDashboard).Methods("GET")
	s.router.HandleFunc("/errors", s.handleErrors).Methods("GET")
	s.router.HandleFunc("/errors/{id}", s.handleErrorDetail).Methods("GET")
	s.router.HandleFunc("/rules", s.handleRules).Methods("GET")
	s.router.HandleFunc("/history", s.handleHistory).Methods("GET")
	s.router.HandleFunc("/settings", s.handleSettings).Methods("GET")

	// API endpoints
	s.router.HandleFunc("/api/errors", s.handleAPIErrors).Methods("GET")
	s.router.HandleFunc("/api/errors/{id}", s.handleAPIErrorDetail).Methods("GET")
	s.router.HandleFunc("/api/rules", s.handleAPIRules).Methods("GET")
	s.router.HandleFunc("/api/rules/test", s.handleAPIRulesTest).Methods("POST")
	s.router.HandleFunc("/api/remediations", s.handleAPIRemediations).Methods("GET")
	s.router.HandleFunc("/api/stats", s.handleAPIStats).Methods("GET")
	s.router.HandleFunc("/api/settings", s.handleAPISettings).Methods("GET", "POST")

	// WebSocket for real-time updates
	s.router.HandleFunc("/ws", s.handleWebSocket)

	// Health endpoints
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/ready", s.handleReady).Methods("GET")
}

// Start begins serving HTTP requests
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("starting web server", "addr", s.addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down web server")

	// Close all WebSocket connections
	s.mu.Lock()
	for client := range s.clients {
		client.Close()
	}
	s.mu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

// BroadcastError sends a new error to all connected WebSocket clients
func (s *Server) BroadcastError(err *store.Error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg := map[string]interface{}{
		"type":  "error",
		"error": err,
	}

	for client := range s.clients {
		if err := client.WriteJSON(msg); err != nil {
			s.logger.Debug("failed to send to websocket client", "error", err)
		}
	}
}

// BroadcastRemediation sends a remediation log to all connected clients
func (s *Server) BroadcastRemediation(log *store.RemediationLog) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg := map[string]interface{}{
		"type":        "remediation",
		"remediation": log,
	}

	for client := range s.clients {
		if err := client.WriteJSON(msg); err != nil {
			s.logger.Debug("failed to send to websocket client", "error", err)
		}
	}
}

// BroadcastStats sends updated stats to all connected clients
func (s *Server) BroadcastStats() {
	stats, err := s.store.GetStats()
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	msg := map[string]interface{}{
		"type":  "stats",
		"stats": stats,
	}

	for client := range s.clients {
		if err := client.WriteJSON(msg); err != nil {
			s.logger.Debug("failed to send to websocket client", "error", err)
		}
	}
}

func (s *Server) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"basePath": func() string {
			return s.basePath
		},
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"formatDuration": func(d time.Duration) string {
			if d < time.Minute {
				return d.Round(time.Second).String()
			}
			if d < time.Hour {
				return d.Round(time.Minute).String()
			}
			return d.Round(time.Hour).String()
		},
		"timeAgo": func(t time.Time) string {
			d := time.Since(t)
			if d < time.Minute {
				return "just now"
			}
			if d < time.Hour {
				mins := int(d.Minutes())
				if mins == 1 {
					return "1 minute ago"
				}
				return fmt.Sprintf("%d minutes ago", mins)
			}
			if d < 24*time.Hour {
				hours := int(d.Hours())
				if hours == 1 {
					return "1 hour ago"
				}
				return fmt.Sprintf("%d hours ago", hours)
			}
			days := int(d.Hours() / 24)
			if days == 1 {
				return "1 day ago"
			}
			return fmt.Sprintf("%d days ago", days)
		},
		"priorityColor": func(p rules.Priority) string {
			return p.Color()
		},
		"priorityLabel": func(p rules.Priority) string {
			return p.Label()
		},
		"priorityCount": func(m map[rules.Priority]int, key string) int {
			return m[rules.Priority(key)]
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"mul": func(a, b int) int {
			return a * b
		},
	}
}
