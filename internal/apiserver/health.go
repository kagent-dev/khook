package apiserver

import (
	"fmt"
	"net/http"
	"runtime"
)

// HealthHandlers handles health and diagnostics endpoints
type HealthHandlers struct {
	server *Server
}

// NewHealthHandlers creates a new health handlers instance
func NewHealthHandlers(server *Server) *HealthHandlers {
	return &HealthHandlers{server: server}
}

// HealthResponse represents the health status
type HealthResponse struct {
	Status string `json:"status"`
}

// DiagnosticsResponse represents diagnostic information
type DiagnosticsResponse struct {
	APIStatus      string         `json:"apiStatus"`
	MemoryUsage    MemoryStats    `json:"memoryUsage"`
	ConnectionInfo ConnectionInfo `json:"connectionInfo"`
	EventStats     EventStats     `json:"eventStats"`
}

// MemoryStats represents memory usage statistics
type MemoryStats struct {
	Alloc      uint64 `json:"alloc"`
	TotalAlloc uint64 `json:"totalAlloc"`
	Sys        uint64 `json:"sys"`
	NumGC      uint32 `json:"numGC"`
}

// ConnectionInfo represents connection information
type ConnectionInfo struct {
	ActiveEventStreams int `json:"activeEventStreams"`
}

// EventStats represents event statistics
type EventStats struct {
	TotalEvents int `json:"totalEvents"`
	TotalHooks  int `json:"totalHooks"`
}

// Health handles GET /api/v1/health
// @Summary Health check
// @Description Returns the health status of the API server
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse "Healthy status"
// @Router /health [get]
func (h *HealthHandlers) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	response := HealthResponse{
		Status: "healthy",
	}

	h.server.writeJSON(w, http.StatusOK, response)
}

// Diagnostics handles GET /api/v1/diagnostics
// @Summary Get diagnostics
// @Description Returns detailed diagnostic information including memory usage, connection info, and event statistics
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} DiagnosticsResponse
// @Router /diagnostics [get]
func (h *HealthHandlers) Diagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Get event statistics
	hookNames := h.server.dedupManager.GetAllHookNames()
	totalEvents := h.server.dedupManager.GetEventCount()

	response := DiagnosticsResponse{
		APIStatus: "running",
		MemoryUsage: MemoryStats{
			Alloc:      m.Alloc,
			TotalAlloc: m.TotalAlloc,
			Sys:        m.Sys,
			NumGC:      m.NumGC,
		},
		ConnectionInfo: ConnectionInfo{
			ActiveEventStreams: 0,
		},
		EventStats: EventStats{
			TotalEvents: totalEvents,
			TotalHooks:  len(hookNames),
		},
	}

	h.server.writeJSON(w, http.StatusOK, response)
}

// Metrics handles GET /api/v1/metrics (Prometheus-style metrics)
// @Summary Get Prometheus metrics
// @Description Returns Prometheus-style metrics in text format
// @Tags health
// @Accept json
// @Produce text/plain
// @Success 200 {string} string "Prometheus metrics"
// @Router /metrics [get]
func (h *HealthHandlers) Metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	hookNames := h.server.dedupManager.GetAllHookNames()
	totalEvents := h.server.dedupManager.GetEventCount()

	// Generate Prometheus-style metrics
	metrics := []string{
		"# HELP khook_events_total Total number of active events",
		"# TYPE khook_events_total gauge",
		fmt.Sprintf("khook_events_total %d", totalEvents),
		"",
		"# HELP khook_hooks_total Total number of hooks with active events",
		"# TYPE khook_hooks_total gauge",
		fmt.Sprintf("khook_hooks_total %d", len(hookNames)),
		"",
		"# HELP khook_memory_alloc_bytes Memory allocated in bytes",
		"# TYPE khook_memory_alloc_bytes gauge",
		fmt.Sprintf("khook_memory_alloc_bytes %d", m.Alloc),
		"",
		"# HELP khook_memory_sys_bytes System memory in bytes",
		"# TYPE khook_memory_sys_bytes gauge",
		fmt.Sprintf("khook_memory_sys_bytes %d", m.Sys),
		"",
		"# HELP khook_gc_runs_total Total number of GC runs",
		"# TYPE khook_gc_runs_total counter",
		fmt.Sprintf("khook_gc_runs_total %d", m.NumGC),
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	for _, metric := range metrics {
		w.Write([]byte(metric + "\n"))
	}
}
