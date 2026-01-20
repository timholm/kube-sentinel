package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kube-sentinel/kube-sentinel/internal/rules"
	"github.com/kube-sentinel/kube-sentinel/internal/store"
)

// Page data structures
type dashboardData struct {
	Stats            *store.Stats
	RecentErrors     []*store.Error
	RecentRemediations []*store.RemediationLog
	RemEnabled       bool
	DryRun           bool
	ActionsThisHour  int
}

type errorsData struct {
	Errors     []*store.Error
	Total      int
	Page       int
	PageSize   int
	Filter     store.ErrorFilter
	Namespaces []string
}

type errorDetailData struct {
	Error        *store.Error
	Remediations []*store.RemediationLog
}

type rulesData struct {
	Rules []rules.Rule
}

type historyData struct {
	Logs     []*store.RemediationLog
	Total    int
	Page     int
	PageSize int
}

type settingsData struct {
	RemEnabled      bool
	DryRun          bool
	MaxActionsPerHour int
	ActionsThisHour int
}

// Page handlers

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.store.GetStats()
	errors, _, _ := s.store.ListErrors(store.ErrorFilter{}, store.PaginationOptions{Limit: 10})
	logs, _, _ := s.store.ListRemediationLogs(store.PaginationOptions{Limit: 5})

	data := dashboardData{
		Stats:             stats,
		RecentErrors:      errors,
		RecentRemediations: logs,
		RemEnabled:        s.remEngine.IsEnabled(),
		DryRun:            s.remEngine.IsDryRun(),
		ActionsThisHour:   s.remEngine.GetActionsThisHour(),
	}

	s.renderTemplate(w, "dashboard.html", data)
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 20

	filter := store.ErrorFilter{
		Namespace: r.URL.Query().Get("namespace"),
		Pod:       r.URL.Query().Get("pod"),
		Search:    r.URL.Query().Get("search"),
	}

	if p := r.URL.Query().Get("priority"); p != "" {
		if priority, err := rules.ParsePriority(p); err == nil {
			filter.Priority = priority
		}
	}

	errors, total, _ := s.store.ListErrors(filter, store.PaginationOptions{
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})

	// Get unique namespaces for filter dropdown
	allErrors, _, _ := s.store.ListErrors(store.ErrorFilter{}, store.PaginationOptions{Limit: 10000})
	nsMap := make(map[string]bool)
	for _, e := range allErrors {
		nsMap[e.Namespace] = true
	}
	var namespaces []string
	for ns := range nsMap {
		namespaces = append(namespaces, ns)
	}

	data := errorsData{
		Errors:     errors,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		Filter:     filter,
		Namespaces: namespaces,
	}

	s.renderTemplate(w, "errors.html", data)
}

func (s *Server) handleErrorDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	errObj, err := s.store.GetError(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	logs, _ := s.store.ListRemediationLogsForError(id)

	data := errorDetailData{
		Error:        errObj,
		Remediations: logs,
	}

	s.renderTemplate(w, "error_detail.html", data)
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	data := rulesData{
		Rules: s.ruleEngine.GetRules(),
	}

	s.renderTemplate(w, "rules.html", data)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 50

	logs, total, _ := s.store.ListRemediationLogs(store.PaginationOptions{
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})

	data := historyData{
		Logs:     logs,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}

	s.renderTemplate(w, "history.html", data)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	data := settingsData{
		RemEnabled:      s.remEngine.IsEnabled(),
		DryRun:          s.remEngine.IsDryRun(),
		ActionsThisHour: s.remEngine.GetActionsThisHour(),
	}

	s.renderTemplate(w, "settings.html", data)
}

// API handlers

func (s *Server) handleAPIErrors(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	filter := store.ErrorFilter{
		Namespace: r.URL.Query().Get("namespace"),
		Pod:       r.URL.Query().Get("pod"),
		Search:    r.URL.Query().Get("search"),
	}

	if p := r.URL.Query().Get("priority"); p != "" {
		if priority, err := rules.ParsePriority(p); err == nil {
			filter.Priority = priority
		}
	}

	errors, total, err := s.store.ListErrors(filter, store.PaginationOptions{
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"errors": errors,
		"total":  total,
		"page":   page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleAPIErrorDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	errObj, err := s.store.GetError(id)
	if err != nil {
		s.jsonError(w, "error not found", http.StatusNotFound)
		return
	}

	logs, _ := s.store.ListRemediationLogsForError(id)

	s.jsonResponse(w, map[string]interface{}{
		"error":        errObj,
		"remediations": logs,
	})
}

func (s *Server) handleAPIRules(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, map[string]interface{}{
		"rules": s.ruleEngine.GetRules(),
	})
}

func (s *Server) handleAPIRulesTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pattern string `json:"pattern"`
		Sample  string `json:"sample"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	matches, err := s.ruleEngine.TestPattern(req.Pattern, req.Sample)
	if err != nil {
		s.jsonResponse(w, map[string]interface{}{
			"matches": false,
			"error":   err.Error(),
		})
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"matches": matches,
	})
}

func (s *Server) handleAPIRemediations(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	logs, total, err := s.store.ListRemediationLogs(store.PaginationOptions{
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"remediations": logs,
		"total":        total,
		"page":         page,
		"pageSize":     pageSize,
	})
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetStats()
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, stats)
}

func (s *Server) handleAPISettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Enabled bool `json:"enabled"`
			DryRun  bool `json:"dry_run"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		s.remEngine.SetEnabled(req.Enabled)
		s.remEngine.SetDryRun(req.DryRun)
	}

	s.jsonResponse(w, map[string]interface{}{
		"enabled":           s.remEngine.IsEnabled(),
		"dry_run":           s.remEngine.IsDryRun(),
		"actions_this_hour": s.remEngine.GetActionsThisHour(),
	})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	// Keep connection alive and handle incoming messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Debug("websocket closed", "error", err)
			}
			break
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

// Helper functions

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("template render failed", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
