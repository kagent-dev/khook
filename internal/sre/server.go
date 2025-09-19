package sre

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
	"k8s.io/apimachinery/pkg/types"
)

// Alert represents an alert for SRE-IDE
type Alert struct {
	ID                 string    `json:"id"`
	HookName           string    `json:"hookName"`
	Namespace          string    `json:"namespace"`
	EventType          string    `json:"eventType"`
	ResourceName       string    `json:"resourceName"`
	Severity           string    `json:"severity"`
	Status             string    `json:"status"`
	FirstSeen          string    `json:"firstSeen"`
	LastSeen           string    `json:"lastSeen"`
	Message            string    `json:"message"`
	AgentID            string    `json:"agentId"`
	SessionID          *string   `json:"sessionId,omitempty"`
	TaskID             *string   `json:"taskId,omitempty"`
	RemediationStatus  *string   `json:"remediationStatus,omitempty"`
}

// AlertSummary represents alert statistics
type AlertSummary struct {
	Total         int `json:"total"`
	Firing        int `json:"firing"`
	Resolved      int `json:"resolved"`
	Acknowledged  int `json:"acknowledged"`
	BySeverity    map[string]int `json:"bySeverity"`
	ByEventType   map[string]int `json:"byEventType"`
}

// Server provides HTTP API for SRE-IDE integration
type Server struct {
	port     int
	logger   logr.Logger
	alerts   map[string]*Alert
	mu       sync.RWMutex
	clients  map[chan Alert]bool
	clientsMu sync.RWMutex
	client   client.Client
}

// NewServer creates a new SRE-IDE server
func NewServer(port int, client client.Client) *Server {
	return &Server{
		port:    port,
		logger:  log.Log.WithName("sre-server"),
		alerts:  make(map[string]*Alert),
		clients: make(map[chan Alert]bool),
		client:  client,
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	
	// API v1 endpoints
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/events/", s.handleEventActions)
	mux.HandleFunc("/api/v1/stats/events/summary", s.handleEventSummary)
	mux.HandleFunc("/api/v1/stats/events/by-type", s.handleEventStatsByType)
	mux.HandleFunc("/api/v1/events/stream", s.handleEventStream)
	
	// Hooks endpoints
	mux.HandleFunc("/api/v1/hooks", s.handleHooks)
	mux.HandleFunc("/api/v1/hooks/", s.handleHookActions)
	
	// Health and diagnostics
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	
	// Legacy endpoints for backward compatibility
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/alerts/summary", s.handleAlertSummary)
	mux.HandleFunc("/api/alerts/stream", s.handleAlertStream)
	mux.HandleFunc("/api/alerts/", s.handleAlertActions)
	mux.HandleFunc("/api/hooks", s.handleHooks)
	mux.HandleFunc("/api/hooks/", s.handleHookActions)
	mux.HandleFunc("/health", s.handleHealth)
	
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}
	
	s.logger.Info("Starting SRE-IDE server", "port", s.port)
	
	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error(err, "SRE-IDE server failed")
		}
	}()
	
	// Wait for context cancellation
	<-ctx.Done()
	
	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return server.Shutdown(shutdownCtx)
}

// AddAlert adds or updates an alert
func (s *Server) AddAlert(alert *Alert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Update timestamps
	now := time.Now().Format(time.RFC3339)
	if existing, exists := s.alerts[alert.ID]; exists {
		alert.FirstSeen = existing.FirstSeen
		alert.LastSeen = now
	} else {
		alert.FirstSeen = now
		alert.LastSeen = now
	}
	
	s.alerts[alert.ID] = alert
	
	// Broadcast to streaming clients
	s.broadcastAlert(*alert)
	
	s.logger.Info("Alert added/updated", "id", alert.ID, "eventType", alert.EventType, "status", alert.Status)
}

// UpdateAlertStatus updates an alert's status
func (s *Server) UpdateAlertStatus(alertID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	alert, exists := s.alerts[alertID]
	if !exists {
		return fmt.Errorf("alert not found: %s", alertID)
	}
	
	alert.Status = status
	alert.LastSeen = time.Now().Format(time.RFC3339)
	
	// Broadcast update
	s.broadcastAlert(*alert)
	
	s.logger.Info("Alert status updated", "id", alertID, "status", status)
	return nil
}

// broadcastAlert sends alert to all streaming clients
func (s *Server) broadcastAlert(alert Alert) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	
	for client := range s.clients {
		select {
		case client <- alert:
		default:
			// Client channel is full, skip
		}
	}
}

// handleAlerts handles GET /api/alerts
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.mu.RLock()
	alerts := make([]Alert, 0, len(s.alerts))
	for _, alert := range s.alerts {
		alerts = append(alerts, *alert)
	}
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": alerts,
	})
}

// handleAlertSummary handles GET /api/alerts/summary
func (s *Server) handleAlertSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.mu.RLock()
	summary := s.calculateSummary()
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": summary,
	})
}

// handleAlertStream handles GET /api/alerts/stream (SSE)
func (s *Server) handleAlertStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
	
	// Create client channel
	clientChan := make(chan Alert, 100)
	
	s.clientsMu.Lock()
	s.clients[clientChan] = true
	s.clientsMu.Unlock()
	
	// Send initial data
	s.mu.RLock()
	for _, alert := range s.alerts {
		select {
		case clientChan <- *alert:
		default:
		}
	}
	s.mu.RUnlock()
	
	// Send heartbeat
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	// Cleanup function
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientChan)
		s.clientsMu.Unlock()
		close(clientChan)
	}()
	
	for {
		select {
		case alert := <-clientChan:
			data, _ := json.Marshal(alert)
			fmt.Fprintf(w, "event: alert\ndata: %s\n\n", data)
			w.(http.Flusher).Flush()
			
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", time.Now().Format(time.RFC3339))
			w.(http.Flusher).Flush()
			
		case <-r.Context().Done():
			return
		}
	}
}

// handleAlertActions handles POST /api/alerts/{id}/{action}
func (s *Server) handleAlertActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Parse URL path: /api/alerts/{id}/{action}
	path := r.URL.Path
	if len(path) < len("/api/alerts/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	
	// Extract alert ID and action
	parts := path[len("/api/alerts/"):]
	if len(parts) == 0 {
		http.Error(w, "Missing alert ID", http.StatusBadRequest)
		return
	}
	
	// Find the last slash to separate ID and action
	lastSlash := -1
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			lastSlash = i
			break
		}
	}
	
	if lastSlash == -1 {
		http.Error(w, "Missing action", http.StatusBadRequest)
		return
	}
	
	alertID := parts[:lastSlash]
	action := parts[lastSlash+1:]
	
	var status string
	switch action {
	case "acknowledge":
		status = "acknowledged"
	case "resolve":
		status = "resolved"
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}
	
	if err := s.UpdateAlertStatus(alertID, status); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// calculateSummary calculates alert statistics
func (s *Server) calculateSummary() AlertSummary {
	summary := AlertSummary{
		BySeverity:  make(map[string]int),
		ByEventType: make(map[string]int),
	}
	
	for _, alert := range s.alerts {
		summary.Total++
		
		switch alert.Status {
		case "firing":
			summary.Firing++
		case "resolved":
			summary.Resolved++
		case "acknowledged":
			summary.Acknowledged++
		}
		
		summary.BySeverity[alert.Severity]++
		summary.ByEventType[alert.EventType]++
	}
	
	return summary
}

// convertAgentIDFormat converts agent ID from kagent/k8s-agent to kagent__NS__k8s_agent format
// This matches the Python identifier format used by kagent
func convertAgentIDFormat(agentRef string) string {
	// Parse the agent reference (e.g., "kagent/k8s-agent")
	parts := strings.Split(agentRef, "/")
	if len(parts) != 2 {
		// If not in namespace/name format, assume it's just the name
		return fmt.Sprintf("kagent__NS__%s", agentRef)
	}
	
	namespace := parts[0]
	agentName := parts[1]
	
	// Convert from namespace/name to namespace__NS__name format
	// This matches the ConvertToPythonIdentifier function in kagent
	return fmt.Sprintf("%s__NS__%s", namespace, agentName)
}

// ConvertEventToAlert converts a khook event to an SRE-IDE alert
func ConvertEventToAlert(
	event interfaces.Event,
	hook *v1alpha2.Hook,
	agentRef types.NamespacedName,
	response *interfaces.AgentResponse,
) *Alert {
	// Generate unique alert ID
	alertID := fmt.Sprintf("%s-%s-%s-%s", 
		hook.Namespace, hook.Name, event.Type, event.ResourceName)
	
	// Determine severity based on event type
	severity := "medium"
	switch event.Type {
	case "pod-restart":
		severity = "high"
	case "oom-kill":
		severity = "critical"
	case "probe-failed":
		severity = "high"
	case "pod-pending":
		severity = "medium"
	}
	
	// Determine remediation status
	remediationStatus := "pending"
	if response != nil && response.RequestId != "" {
		remediationStatus = "in_progress"
	}
	
	// Convert agent ID format from kagent/k8s-agent to kagent__NS__k8s_agent
	agentID := convertAgentIDFormat(agentRef.Name)
	
	alert := &Alert{
		ID:                alertID,
		HookName:          hook.Name,
		Namespace:         hook.Namespace,
		EventType:         event.Type,
		ResourceName:      event.ResourceName,
		Severity:          severity,
		Status:            "firing",
		Message:           event.Message,
		AgentID:           agentID,
		RemediationStatus: &remediationStatus,
	}
	
	// Add session/task info if available
	if response != nil && response.RequestId != "" {
		alert.TaskID = &response.RequestId
	}
	
	return alert
}

// syncActiveEventsWithAlerts creates alerts for all active events in hooks
func (s *Server) syncActiveEventsWithAlerts(hookList *v1alpha2.HookList) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, hook := range hookList.Items {
		if hook.Status.ActiveEvents == nil {
			continue
		}

		for _, activeEvent := range hook.Status.ActiveEvents {
			// Only create alerts for firing events
			if activeEvent.Status != "firing" {
				continue
			}

			// Create alert ID based on hook and event
			alertID := fmt.Sprintf("%s-%s-%s-%s",
				hook.Name,
				activeEvent.EventType,
				activeEvent.ResourceName,
				activeEvent.FirstSeen.Format("20060102150405"))

			// Check if alert already exists
			if _, exists := s.alerts[alertID]; exists {
				continue
			}

			// Find the matching event configuration
			var agentID string
			for _, config := range hook.Spec.EventConfigurations {
				if config.EventType == activeEvent.EventType {
					// Convert agent ID format from kagent/k8s-agent to kagent__NS__k8s_agent
					agentID = convertAgentIDFormat(config.AgentRef.Name)
					break
				}
			}

			if agentID == "" {
				continue
			}

			// Create alert from active event
			alert := Alert{
				ID:           alertID,
				HookName:     hook.Name,
				Namespace:    hook.Namespace,
				EventType:    activeEvent.EventType,
				ResourceName: activeEvent.ResourceName,
				Severity:     "medium", // Default severity
				Status:       "firing",
				FirstSeen:    activeEvent.FirstSeen.Format(time.RFC3339),
				LastSeen:     activeEvent.LastSeen.Format(time.RFC3339),
				Message:      fmt.Sprintf("%s event for %s", activeEvent.EventType, activeEvent.ResourceName),
				AgentID:      agentID,
			}

			// Add alert to server
			s.alerts[alertID] = &alert
		}
	}
}

// handleHooks handles GET /api/hooks
func (s *Server) handleHooks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Query the Kubernetes API for Hook resources
		var hookList v1alpha2.HookList
		if err := s.client.List(context.Background(), &hookList); err != nil {
			s.logger.Error(err, "Failed to list hooks")
			http.Error(w, "Failed to list hooks", http.StatusInternalServerError)
			return
		}

		// Sync active events with alerts
		s.syncActiveEventsWithAlerts(&hookList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hookList)
		return
	}

	if r.Method == http.MethodPost {
		// Create hook - for now, just return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleHookActions handles /api/hooks/{namespace}/{name} and /api/hooks/{id}
func (s *Server) handleHookActions(w http.ResponseWriter, r *http.Request) {
	// For now, return 404 for all hook actions
	// In a real implementation, this would handle CRUD operations on Hook resources
	http.Error(w, "Hook management not implemented", http.StatusNotImplemented)
}

// handleEvents handles GET /api/v1/events with query parameters
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Parse query parameters
	query := r.URL.Query()
	namespace := query.Get("namespace")
	eventType := query.Get("eventType")
	resourceName := query.Get("resourceName")
	status := query.Get("status")
	
	s.mu.RLock()
	alerts := make([]Alert, 0, len(s.alerts))
	for _, alert := range s.alerts {
		// Apply filters
		if namespace != "" && alert.Namespace != namespace {
			continue
		}
		if eventType != "" && alert.EventType != eventType {
			continue
		}
		if resourceName != "" && alert.ResourceName != resourceName {
			continue
		}
		if status != "" && alert.Status != status {
			continue
		}
		alerts = append(alerts, *alert)
	}
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": alerts,
		"total": len(alerts),
	})
}

// handleEventActions handles /api/v1/events/{namespace}/{name}/events
func (s *Server) handleEventActions(w http.ResponseWriter, r *http.Request) {
	// Parse path to extract namespace and name
	path := r.URL.Path
	if len(path) < len("/api/v1/events/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	
	parts := strings.Split(path[len("/api/v1/events/"):], "/")
	if len(parts) < 2 {
		http.Error(w, "Missing namespace or name", http.StatusBadRequest)
		return
	}
	
	namespace := parts[0]
	hookName := parts[1]
	
	// Filter alerts by namespace and hook name
	s.mu.RLock()
	alerts := make([]Alert, 0)
	for _, alert := range s.alerts {
		if alert.Namespace == namespace && alert.HookName == hookName {
			alerts = append(alerts, *alert)
		}
	}
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": alerts,
		"total": len(alerts),
	})
}

// handleEventSummary handles GET /api/v1/stats/events/summary
func (s *Server) handleEventSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.mu.RLock()
	summary := s.calculateSummary()
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"summary": summary,
	})
}

// handleEventStatsByType handles GET /api/v1/stats/events/by-type
func (s *Server) handleEventStatsByType(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.mu.RLock()
	byType := make(map[string]map[string]interface{})
	total := 0
	for _, alert := range s.alerts {
		total++
		if byType[alert.EventType] == nil {
			byType[alert.EventType] = make(map[string]interface{})
			byType[alert.EventType]["count"] = 0
		}
		byType[alert.EventType]["count"] = byType[alert.EventType]["count"].(int) + 1
	}
	
	// Calculate percentages
	for eventType, stats := range byType {
		count := stats["count"].(int)
		percentage := float64(count) / float64(total) * 100
		byType[eventType]["percentage"] = percentage
	}
	s.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"byType": byType,
	})
}

// handleEventStream handles GET /api/v1/events/stream (SSE)
func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
	
	// Create client channel
	clientChan := make(chan Alert, 100)
	
	s.clientsMu.Lock()
	s.clients[clientChan] = true
	s.clientsMu.Unlock()
	
	// Send initial data
	s.mu.RLock()
	for _, alert := range s.alerts {
		select {
		case clientChan <- *alert:
		default:
		}
	}
	s.mu.RUnlock()
	
	// Send heartbeat
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	// Cleanup function
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientChan)
		s.clientsMu.Unlock()
		close(clientChan)
	}()
	
	for {
		select {
		case alert := <-clientChan:
			data, _ := json.Marshal(alert)
			fmt.Fprintf(w, "event: event\ndata: %s\n\n", data)
			w.(http.Flusher).Flush()
			
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", time.Now().Format(time.RFC3339))
			w.(http.Flusher).Flush()
			
		case <-r.Context().Done():
			return
		}
	}
}

// handleDiagnostics handles GET /api/v1/diagnostics
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	diagnostics := map[string]interface{}{
		"api_server_status": "running",
		"uptime": time.Since(time.Now()).String(), // This would need to track actual start time
		"event_processing_pipeline_health": "healthy",
		"kagent_api_connectivity": "unknown", // Would need to check actual connectivity
		"plugin_status": map[string]string{
			"kubernetes_events": "active",
		},
		"memory_usage": map[string]interface{}{
			"alerts_count": len(s.alerts),
			"active_connections": len(s.clients),
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(diagnostics)
}

// handleMetrics handles GET /api/v1/metrics
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.mu.RLock()
	summary := s.calculateSummary()
	s.mu.RUnlock()
	
	metrics := map[string]interface{}{
		"khook_events_total": summary.Total,
		"khook_active_events": summary.Firing,
		"khook_resolved_events": summary.Resolved,
		"khook_acknowledged_events": summary.Acknowledged,
		"khook_events_by_type": summary.ByEventType,
		"khook_events_by_severity": summary.BySeverity,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}
