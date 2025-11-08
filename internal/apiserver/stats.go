package apiserver

import (
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// StatsHandlers handles statistics-related API endpoints
type StatsHandlers struct {
	server *Server
}

// NewStatsHandlers creates a new stats handlers instance
func NewStatsHandlers(server *Server) *StatsHandlers {
	return &StatsHandlers{server: server}
}

// EventSummaryResponse represents event summary statistics
type EventSummaryResponse struct {
	Total       int            `json:"total"`
	Firing      int            `json:"firing"`
	Resolved    int            `json:"resolved"`
	BySeverity  map[string]int `json:"bySeverity"`
	ByEventType map[string]int `json:"byEventType"`
}

// EventsByTypeResponse represents events grouped by type
type EventsByTypeResponse struct {
	ByType map[string]TypeStats `json:"byType"`
	Total  int                  `json:"total"`
}

// TypeStats represents statistics for a specific event type
type TypeStats struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// EventSummary handles GET /api/v1/stats/events/summary
// @Summary Get event summary
// @Description Returns event summary statistics including counts by severity and type
// @Tags statistics
// @Accept json
// @Produce json
// @Success 200 {object} EventSummaryResponse
// @Router /stats/events/summary [get]
func (h *StatsHandlers) EventSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	hookNames := h.server.dedupManager.GetAllHookNames()

	total := 0
	firing := 0
	resolved := 0
	byEventType := make(map[string]int)
	bySeverity := make(map[string]int)

	// Collect statistics from all hooks
	for _, hookNameStr := range hookNames {
		parts := strings.Split(hookNameStr, "/")
		if len(parts) != 2 {
			continue
		}

		hookRef := types.NamespacedName{
			Namespace: parts[0],
			Name:      parts[1],
		}

		activeEvents := h.server.dedupManager.GetActiveEventsWithStatus(hookRef)
		for _, event := range activeEvents {
			total++
			byEventType[event.EventType]++

			if event.Status == "firing" {
				firing++
			} else if event.Status == "resolved" {
				resolved++
			}

			// Determine severity based on event type (default mapping)
			severity := determineSeverity(event.EventType)
			bySeverity[severity]++
		}
	}

	response := EventSummaryResponse{
		Total:       total,
		Firing:      firing,
		Resolved:    resolved,
		BySeverity:  bySeverity,
		ByEventType: byEventType,
	}

	h.server.writeJSON(w, http.StatusOK, response)
}

// EventsByType handles GET /api/v1/stats/events/by-type
// @Summary Get events by type
// @Description Returns events grouped by type with counts and percentages
// @Tags statistics
// @Accept json
// @Produce json
// @Success 200 {object} EventsByTypeResponse
// @Router /stats/events/by-type [get]
func (h *StatsHandlers) EventsByType(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	hookNames := h.server.dedupManager.GetAllHookNames()

	byType := make(map[string]int)
	total := 0

	// Count events by type
	for _, hookNameStr := range hookNames {
		parts := strings.Split(hookNameStr, "/")
		if len(parts) != 2 {
			continue
		}

		hookRef := types.NamespacedName{
			Namespace: parts[0],
			Name:      parts[1],
		}

		activeEvents := h.server.dedupManager.GetActiveEventsWithStatus(hookRef)
		for _, event := range activeEvents {
			byType[event.EventType]++
			total++
		}
	}

	// Calculate percentages
	byTypeStats := make(map[string]TypeStats)
	for eventType, count := range byType {
		percentage := 0.0
		if total > 0 {
			percentage = float64(count) / float64(total) * 100.0
		}
		byTypeStats[eventType] = TypeStats{
			Count:      count,
			Percentage: percentage,
		}
	}

	response := EventsByTypeResponse{
		ByType: byTypeStats,
		Total:  total,
	}

	h.server.writeJSON(w, http.StatusOK, response)
}

// determineSeverity determines severity based on event type using a default mapping
func determineSeverity(eventType string) string {
	switch eventType {
	case "oom-kill":
		return "critical"
	case "probe-failed":
		return "warning"
	case "pod-restart":
		return "warning"
	case "pod-pending":
		return "info"
	case "node-not-ready":
		return "critical"
	default:
		return "info"
	}
}
