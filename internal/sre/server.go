package sre

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
	"k8s.io/apimachinery/pkg/types"
)

// Alert represents an alert for SRE-IDE
type Alert struct {
	ID                string    `json:"id"`
	HookName          string    `json:"hookName"`
	Namespace         string    `json:"namespace"`
	EventType         string    `json:"eventType"`
	ResourceName      string    `json:"resourceName"`
	Severity          string    `json:"severity"`
	Status            string    `json:"status"`
	Timestamp         time.Time `json:"timestamp"`
	FirstSeen         string    `json:"firstSeen"`
	LastSeen          string    `json:"lastSeen"`
	Message           string    `json:"message"`
	AgentID           string    `json:"agentId"`
	SessionID         *string   `json:"sessionId,omitempty"`
	TaskID            *string   `json:"taskId,omitempty"`
	RemediationStatus *string   `json:"remediationStatus,omitempty"`
}

// AlertSummary represents alert statistics
type AlertSummary struct {
	Total        int            `json:"total"`
	Firing       int            `json:"firing"`
	Resolved     int            `json:"resolved"`
	Acknowledged int            `json:"acknowledged"`
	BySeverity   map[string]int `json:"bySeverity"`
	ByEventType  map[string]int `json:"byEventType"`
}

// Server provides HTTP API for SRE-IDE integration
type Server struct {
	port      int
	logger    logr.Logger
	alerts    map[string]*Alert
	mu        sync.RWMutex
	clients   map[chan Alert]bool
	clientsMu sync.RWMutex
	wsClients map[*websocket.Conn]bool
	wsMu      sync.RWMutex
	client    client.Client
	startTime time.Time
	upgrader  websocket.Upgrader
}

// NewServer creates a new SRE-IDE server
func NewServer(port int, client client.Client) *Server {
	return &Server{
		port:      port,
		logger:    log.Log.WithName("sre-server"),
		alerts:    make(map[string]*Alert),
		clients:   make(map[chan Alert]bool),
		wsClients: make(map[*websocket.Conn]bool),
		client:    client,
		startTime: time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API v1 endpoints
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/events/types/", s.handleEventTypes)
	mux.HandleFunc("/api/v1/events/", s.handleEventsByNamespace)
	mux.HandleFunc("/api/v1/stats/events/summary", s.handleEventSummary)
	mux.HandleFunc("/api/v1/stats/events/by-type", s.handleEventStatsByType)
	mux.HandleFunc("/api/v1/stats/hooks/", s.handleHookStats)
	mux.HandleFunc("/api/v1/stats/trends", s.handleEventTrends)
	mux.HandleFunc("/api/v1/events/stream", s.handleEventStream)
	mux.HandleFunc("/api/v1/events/ws", s.handleWebSocket)

	// Hooks endpoints
	mux.HandleFunc("/api/v1/hooks", s.handleHooks)
	mux.HandleFunc("/api/v1/hooks/validate", s.handleHookValidation)
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
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/health", s.handleHealth)

	// Add CORS middleware
	handler := s.corsMiddleware(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: handler,
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

// corsMiddleware adds CORS headers to all responses
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Add CORS headers to all responses
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		next.ServeHTTP(w, r)
	})
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
	// Broadcast to SSE clients
	s.clientsMu.RLock()
	for client := range s.clients {
		select {
		case client <- alert:
		default:
			// Client channel is full, skip
		}
	}
	s.clientsMu.RUnlock()

	// Broadcast to WebSocket clients
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	alertJSON, err := json.Marshal(alert)
	if err != nil {
		s.logger.Error(err, "Failed to marshal alert for WebSocket broadcast")
		return
	}

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, alertJSON); err != nil {
			s.logger.Error(err, "Failed to send alert to WebSocket client")
			delete(s.wsClients, conn)
			conn.Close()
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

	// Use agent name directly for SRE-IDE compatibility
	agentID := agentRef.Name

	alert := &Alert{
		ID:                alertID,
		HookName:          hook.Name,
		Namespace:         hook.Namespace,
		EventType:         event.Type,
		ResourceName:      event.ResourceName,
		Severity:          severity,
		Status:            "firing",
		Timestamp:         time.Now(),
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

		// Return the full Kubernetes Hook objects as expected by SRE-IDE
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": hookList.Items,
		})
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

// handleHookActions handles /api/v1/hooks/{namespace}/{name} and /api/v1/hooks/validate
func (s *Server) handleHookActions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	// Handle validation endpoint
	if strings.HasSuffix(path, "/validate") {
		s.handleHookValidation(w, r)
		return
	}
	
	// Parse path to extract namespace and name
	if len(path) < len("/api/v1/hooks/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	
	parts := strings.Split(path[len("/api/v1/hooks/"):], "/")
	if len(parts) < 2 {
		http.Error(w, "Missing namespace or name", http.StatusBadRequest)
		return
	}
	
	namespace := parts[0]
	hookName := parts[1]
	
	switch r.Method {
	case http.MethodGet:
		s.handleGetHook(w, r, namespace, hookName)
	case http.MethodPut:
		s.handleUpdateHook(w, r, namespace, hookName)
	case http.MethodDelete:
		s.handleDeleteHook(w, r, namespace, hookName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetHook handles GET /api/v1/hooks/{namespace}/{name}
func (s *Server) handleGetHook(w http.ResponseWriter, r *http.Request, namespace, name string) {
	var hook v1alpha2.Hook
	key := types.NamespacedName{Namespace: namespace, Name: name}
	
	if err := s.client.Get(context.Background(), key, &hook); err != nil {
		s.logger.Error(err, "Failed to get hook", "namespace", namespace, "name", name)
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hook)
}

// handleUpdateHook handles PUT /api/v1/hooks/{namespace}/{name}
func (s *Server) handleUpdateHook(w http.ResponseWriter, r *http.Request, namespace, name string) {
	var hook v1alpha2.Hook
	key := types.NamespacedName{Namespace: namespace, Name: name}
	
	// Get existing hook
	if err := s.client.Get(context.Background(), key, &hook); err != nil {
		s.logger.Error(err, "Failed to get hook for update", "namespace", namespace, "name", name)
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}
	
	// Parse request body
	var updateHook v1alpha2.Hook
	if err := json.NewDecoder(r.Body).Decode(&updateHook); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Update the spec
	hook.Spec = updateHook.Spec
	
	// Update the hook
	if err := s.client.Update(context.Background(), &hook); err != nil {
		s.logger.Error(err, "Failed to update hook", "namespace", namespace, "name", name)
		http.Error(w, "Failed to update hook", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hook)
}

// handleDeleteHook handles DELETE /api/v1/hooks/{namespace}/{name}
func (s *Server) handleDeleteHook(w http.ResponseWriter, r *http.Request, namespace, name string) {
	var hook v1alpha2.Hook
	key := types.NamespacedName{Namespace: namespace, Name: name}
	
	// Get existing hook
	if err := s.client.Get(context.Background(), key, &hook); err != nil {
		s.logger.Error(err, "Failed to get hook for deletion", "namespace", namespace, "name", name)
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}
	
	// Delete the hook
	if err := s.client.Delete(context.Background(), &hook); err != nil {
		s.logger.Error(err, "Failed to delete hook", "namespace", namespace, "name", name)
		http.Error(w, "Failed to delete hook", http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}

// handleHookValidation handles POST /api/v1/hooks/validate
func (s *Server) handleHookValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var hook v1alpha2.Hook
	if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Basic validation
	validationResult := map[string]interface{}{
		"valid": true,
		"errors": []string{},
	}
	
	// Validate event configurations
	if len(hook.Spec.EventConfigurations) == 0 {
		validationResult["valid"] = false
		validationResult["errors"] = append(validationResult["errors"].([]string), "At least one event configuration is required")
	}
	
	for i, config := range hook.Spec.EventConfigurations {
		if config.EventType == "" {
			validationResult["valid"] = false
			validationResult["errors"] = append(validationResult["errors"].([]string), 
				fmt.Sprintf("Event configuration %d: eventType is required", i))
		}
		if config.AgentRef.Name == "" {
			validationResult["valid"] = false
			validationResult["errors"] = append(validationResult["errors"].([]string), 
				fmt.Sprintf("Event configuration %d: agentRef.name is required", i))
		}
		if config.Prompt == "" {
			validationResult["valid"] = false
			validationResult["errors"] = append(validationResult["errors"].([]string), 
				fmt.Sprintf("Event configuration %d: prompt is required", i))
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validationResult)
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
	
	// Time-based filtering
	var startTime, endTime *time.Time
	if startTimeStr := query.Get("startTime"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			startTime = &t
		}
	}
	if endTimeStr := query.Get("endTime"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			endTime = &t
		}
	}
	
	// Sorting
	sortBy := query.Get("sort") // timestamp, eventType, resourceName
	sortOrder := query.Get("order") // asc, desc (default: desc)
	if sortOrder == "" {
		sortOrder = "desc"
	}
	
	// Pagination parameters
	limit := 100 // default limit
	offset := 0  // default offset
	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	s.mu.RLock()
	allAlerts := make([]Alert, 0, len(s.alerts))
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
		
		// Time-based filtering
		if startTime != nil && alert.Timestamp.Before(*startTime) {
			continue
		}
		if endTime != nil && alert.Timestamp.After(*endTime) {
			continue
		}
		
		allAlerts = append(allAlerts, *alert)
	}
	s.mu.RUnlock()

	// Apply sorting
	if sortBy != "" {
		s.sortAlerts(allAlerts, sortBy, sortOrder)
	}

	// Apply pagination
	total := len(allAlerts)
	start := offset
	end := offset + limit

	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	var alerts []Alert
	if start < end {
		alerts = allAlerts[start:end]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":     alerts,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": end < total,
	})
}

// handleEventsByNamespace handles /api/v1/events/{namespace}
func (s *Server) handleEventsByNamespace(w http.ResponseWriter, r *http.Request) {
	// Parse path to extract namespace
	path := r.URL.Path
	if len(path) < len("/api/v1/events/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	namespace := path[len("/api/v1/events/"):]
	if namespace == "" {
		http.Error(w, "Missing namespace", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	eventType := query.Get("eventType")
	resourceName := query.Get("resourceName")
	status := query.Get("status")
	limitStr := query.Get("limit")
	offsetStr := query.Get("offset")

	// Parse pagination
	limit := 100 // Default limit
	offset := 0  // Default offset
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Filter alerts by namespace and other parameters
	s.mu.RLock()
	allAlerts := make([]Alert, 0)
	for _, alert := range s.alerts {
		if alert.Namespace == namespace {
			// Apply additional filters
			if eventType != "" && alert.EventType != eventType {
				continue
			}
			if resourceName != "" && alert.ResourceName != resourceName {
				continue
			}
			if status != "" && alert.Status != status {
				continue
			}
			allAlerts = append(allAlerts, *alert)
		}
	}
	s.mu.RUnlock()

	// Apply pagination
	total := len(allAlerts)
	start := offset
	end := offset + limit

	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	var alerts []Alert
	if start < end {
		alerts = allAlerts[start:end]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":     alerts,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": end < total,
		"namespace": namespace,
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
		"data":  alerts,
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
		"api_server_status":                "running",
		"uptime":                           time.Since(s.startTime).String(),
		"event_processing_pipeline_health": "healthy",
		"kagent_api_connectivity":          s.checkKagentConnectivity(),
		"plugin_status": map[string]string{
			"kubernetes_events": "active",
		},
		"memory_usage": map[string]interface{}{
			"alerts_count":       len(s.alerts),
			"active_connections": len(s.clients),
		},
		"server_info": map[string]interface{}{
			"port":      s.port,
			"start_time": s.startTime.Format(time.RFC3339),
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
		"khook_events_total":        summary.Total,
		"khook_active_events":       summary.Firing,
		"khook_resolved_events":     summary.Resolved,
		"khook_acknowledged_events": summary.Acknowledged,
		"khook_events_by_type":      summary.ByEventType,
		"khook_events_by_severity":  summary.BySeverity,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// checkKagentConnectivity checks if the Kagent API is reachable
func (s *Server) checkKagentConnectivity() string {
	// This is a simplified connectivity check
	// In a real implementation, you might want to make an actual HTTP request
	// to the Kagent API endpoint to verify connectivity
	
	// For now, we'll return "unknown" since we don't have direct access
	// to the Kagent client configuration in this context
	// A more sophisticated implementation would:
	// 1. Get the Kagent API URL from environment variables or config
	// 2. Make a health check request to the API
	// 3. Return "connected", "disconnected", or "unknown" based on the response
	
	return "unknown"
}

// handleEventTypes handles GET /api/v1/events/types/{eventType}
func (s *Server) handleEventTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path to extract eventType
	path := r.URL.Path
	if len(path) < len("/api/v1/events/types/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	eventType := path[len("/api/v1/events/types/"):]
	if eventType == "" {
		http.Error(w, "Missing event type", http.StatusBadRequest)
		return
	}

	// Parse query parameters for filtering
	query := r.URL.Query()
	namespace := query.Get("namespace")
	resourceName := query.Get("resourceName")
	status := query.Get("status")

	// Time-based filtering
	var startTime, endTime *time.Time
	if startTimeStr := query.Get("startTime"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			startTime = &t
		}
	}
	if endTimeStr := query.Get("endTime"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			endTime = &t
		}
	}

	s.mu.RLock()
	alerts := make([]Alert, 0)
	for _, alert := range s.alerts {
		// Filter by event type
		if alert.EventType != eventType {
			continue
		}
		// Apply other filters
		if namespace != "" && alert.Namespace != namespace {
			continue
		}
		if resourceName != "" && alert.ResourceName != resourceName {
			continue
		}
		if status != "" && alert.Status != status {
			continue
		}
		// Time-based filtering
		if startTime != nil && alert.Timestamp.Before(*startTime) {
			continue
		}
		if endTime != nil && alert.Timestamp.After(*endTime) {
			continue
		}
		alerts = append(alerts, *alert)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":      alerts,
		"total":     len(alerts),
		"eventType": eventType,
	})
}

// handleHookStats handles GET /api/v1/stats/hooks/{namespace}/{name}/metrics
func (s *Server) handleHookStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path to extract namespace and name
	path := r.URL.Path
	if len(path) < len("/api/v1/stats/hooks/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	parts := strings.Split(path[len("/api/v1/stats/hooks/"):], "/")
	if len(parts) < 2 {
		http.Error(w, "Missing namespace or name", http.StatusBadRequest)
		return
	}

	namespace := parts[0]
	hookName := parts[1]

	// Get hook from Kubernetes
	var hook v1alpha2.Hook
	key := types.NamespacedName{Name: hookName, Namespace: namespace}
	if err := s.client.Get(context.Background(), key, &hook); err != nil {
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}

	// Count events for this hook
	s.mu.RLock()
	totalEvents := 0
	eventsByType := make(map[string]int)
	eventsByStatus := make(map[string]int)
	eventsBySeverity := make(map[string]int)

	for _, alert := range s.alerts {
		if alert.Namespace == namespace && alert.HookName == hookName {
			totalEvents++
			eventsByType[alert.EventType]++
			eventsByStatus[alert.Status]++
			eventsBySeverity[alert.Severity]++
		}
	}
	s.mu.RUnlock()

	metrics := map[string]interface{}{
		"hook": map[string]interface{}{
			"name":      hookName,
			"namespace": namespace,
			"status":    "active", // Hook is active if it exists
		},
		"events": map[string]interface{}{
			"total":           totalEvents,
			"by_type":         eventsByType,
			"by_status":       eventsByStatus,
			"by_severity":     eventsBySeverity,
		},
		"configuration": map[string]interface{}{
			"event_configurations": len(hook.Spec.EventConfigurations),
			"created_at":           hook.CreationTimestamp.Time.Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// handleEventTrends handles GET /api/v1/stats/trends
func (s *Server) handleEventTrends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	timeRange := query.Get("timeRange") // 1h, 24h, 7d, 30d
	if timeRange == "" {
		timeRange = "24h"
	}

	// Calculate time window
	var window time.Duration
	switch timeRange {
	case "1h":
		window = time.Hour
	case "24h":
		window = 24 * time.Hour
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	default:
		window = 24 * time.Hour
	}

	now := time.Now()
	startTime := now.Add(-window)

	s.mu.RLock()
	trends := make(map[string]interface{})
	hourlyCounts := make(map[string]int)
	dailyCounts := make(map[string]int)
	
	// Group events by hour and day
	for _, alert := range s.alerts {
		if alert.Timestamp.Before(startTime) {
			continue
		}
		
		hourKey := alert.Timestamp.Format("2006-01-02 15:00")
		dayKey := alert.Timestamp.Format("2006-01-02")
		
		hourlyCounts[hourKey]++
		dailyCounts[dayKey]++
	}
	s.mu.RUnlock()

	// Calculate trends
	trends["time_range"] = timeRange
	trends["hourly_counts"] = hourlyCounts
	trends["daily_counts"] = dailyCounts
	trends["total_events"] = len(hourlyCounts)
	trends["window_start"] = startTime.Format(time.RFC3339)
	trends["window_end"] = now.Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trends)
}

// sortAlerts sorts alerts by the specified field and order
func (s *Server) sortAlerts(alerts []Alert, sortBy, order string) {
	switch sortBy {
	case "timestamp":
		if order == "asc" {
			// Sort by timestamp ascending
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].Timestamp.After(alerts[j].Timestamp) {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		} else {
			// Sort by timestamp descending (default)
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].Timestamp.Before(alerts[j].Timestamp) {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		}
	case "eventType":
		if order == "asc" {
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].EventType > alerts[j].EventType {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		} else {
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].EventType < alerts[j].EventType {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		}
	case "resourceName":
		if order == "asc" {
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].ResourceName > alerts[j].ResourceName {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		} else {
			for i := 0; i < len(alerts)-1; i++ {
				for j := i + 1; j < len(alerts); j++ {
					if alerts[i].ResourceName < alerts[j].ResourceName {
						alerts[i], alerts[j] = alerts[j], alerts[i]
					}
				}
			}
		}
	}
}

// handleWebSocket handles WebSocket connections for real-time event streaming
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error(err, "Failed to upgrade connection to WebSocket")
		return
	}
	defer conn.Close()

	// Add client to WebSocket clients
	s.wsMu.Lock()
	s.wsClients[conn] = true
	s.wsMu.Unlock()

	// Remove client when connection closes
	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, conn)
		s.wsMu.Unlock()
	}()

	s.logger.Info("WebSocket client connected")

	// Send initial data
	initialData := map[string]interface{}{
		"type":    "connected",
		"message": "WebSocket connection established",
		"time":    time.Now().Format(time.RFC3339),
	}
	if err := conn.WriteJSON(initialData); err != nil {
		s.logger.Error(err, "Failed to send initial data to WebSocket client")
		return
	}

	// Keep connection alive and handle incoming messages
	for {
		// Read message from client (for ping/pong or commands)
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Error(err, "WebSocket error")
			}
			break
		}

		// Handle different message types
		switch messageType {
		case websocket.TextMessage:
			// Handle text messages (could be commands like "ping", "subscribe", etc.)
			command := string(message)
			switch command {
			case "ping":
				// Respond with pong
				if err := conn.WriteMessage(websocket.TextMessage, []byte("pong")); err != nil {
					s.logger.Error(err, "Failed to send pong to WebSocket client")
					return
				}
			case "subscribe":
				// Client wants to subscribe to all events
				response := map[string]interface{}{
					"type":    "subscribed",
					"message": "Subscribed to all events",
					"time":    time.Now().Format(time.RFC3339),
				}
				if err := conn.WriteJSON(response); err != nil {
					s.logger.Error(err, "Failed to send subscription confirmation")
					return
				}
			}
		case websocket.PingMessage:
			// Respond to ping with pong
			if err := conn.WriteMessage(websocket.PongMessage, nil); err != nil {
				s.logger.Error(err, "Failed to send pong to WebSocket client")
				return
			}
		}
	}

	s.logger.Info("WebSocket client disconnected")
}

// LoadExistingEvents loads existing active events from all hooks
func (s *Server) LoadExistingEvents(ctx context.Context) {
	s.loadExistingEvents(ctx)
}

// loadExistingEvents loads existing active events from all hooks on startup
func (s *Server) loadExistingEvents(ctx context.Context) {
	s.logger.Info("Loading existing events from hooks...")

	// Get all hooks from all namespaces
	var hookList v1alpha2.HookList
	if err := s.client.List(ctx, &hookList); err != nil {
		s.logger.Error(err, "Failed to list hooks")
		return
	}

	loadedCount := 0
	for _, hook := range hookList.Items {
		// Load active events from hook status
		for _, activeEvent := range hook.Status.ActiveEvents {
			// Find the agent ID for this specific event type
			agentID := "k8s-agent" // Default fallback
			for _, eventConfig := range hook.Spec.EventConfigurations {
				if eventConfig.EventType == activeEvent.EventType {
					if eventConfig.AgentRef.Name != "" {
						agentID = eventConfig.AgentRef.Name
					} else if eventConfig.AgentId != "" {
						// Parse legacy agentId format
						parts := strings.Split(eventConfig.AgentId, "/")
						if len(parts) == 2 {
							agentID = parts[1] // Use the name part
						} else {
							agentID = parts[0] // Use the whole string if no slash
						}
					}
					break // Use the first matching configuration
				}
			}
			// Convert active event to alert
			alert := &Alert{
				ID:           fmt.Sprintf("%s-%s-%s-%s", hook.Namespace, hook.Name, activeEvent.EventType, activeEvent.ResourceName),
				HookName:     hook.Name,
				Namespace:    hook.Namespace,
				EventType:    activeEvent.EventType,
				ResourceName: activeEvent.ResourceName,
				Severity:     s.determineSeverity(activeEvent.EventType),
				Status:       activeEvent.Status,
				Timestamp:    activeEvent.FirstSeen.Time,
				FirstSeen:    activeEvent.FirstSeen.Format(time.RFC3339),
				LastSeen:     activeEvent.LastSeen.Format(time.RFC3339),
				Message:      fmt.Sprintf("Event %s for resource %s", activeEvent.EventType, activeEvent.ResourceName),
				AgentID:      agentID, // Use the determined agent ID
			}

			// Add to alerts map
			s.mu.Lock()
			s.alerts[alert.ID] = alert
			s.mu.Unlock()

			loadedCount++
			s.logger.Info("Loaded existing event", 
				"alertId", alert.ID, 
				"eventType", alert.EventType, 
				"status", alert.Status,
				"hook", hook.Name)
		}
	}

	s.logger.Info("Finished loading existing events", "count", loadedCount)
}

// determineSeverity determines alert severity based on event type
func (s *Server) determineSeverity(eventType string) string {
	switch eventType {
	case "pod-restart":
		return "high"
	case "oom-kill":
		return "critical"
	case "probe-failed":
		return "high"
	case "pod-pending":
		return "medium"
	default:
		return "medium"
	}
}
