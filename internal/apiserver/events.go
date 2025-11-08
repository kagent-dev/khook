package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// EventHandlers handles event-related API endpoints
type EventHandlers struct {
	server *Server
}

// NewEventHandlers creates a new event handlers instance
func NewEventHandlers(server *Server) *EventHandlers {
	return &EventHandlers{server: server}
}

// EventResponse represents an event in the API response
type EventResponse struct {
	EventType    string            `json:"eventType"`
	ResourceName string            `json:"resourceName"`
	Namespace    string            `json:"namespace"`
	FirstSeen    time.Time         `json:"firstSeen"`
	LastSeen     time.Time         `json:"lastSeen"`
	Status       string            `json:"status"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// EventListResponse represents a paginated list of events
type EventListResponse struct {
	Data  []EventResponse `json:"data"`
	Total int             `json:"total"`
}

// ListEvents handles GET /api/v1/events
// @Summary List events
// @Description Returns a list of all active events with optional filtering
// @Tags events
// @Accept json
// @Produce json
// @Param namespace query string false "Filter by namespace"
// @Param eventType query string false "Filter by event type (pod-restart, oom-kill, pod-pending, probe-failed, node-not-ready)"
// @Param resourceName query string false "Filter by resource name"
// @Param status query string false "Filter by status (firing, resolved)"
// @Success 200 {object} EventListResponse
// @Router /events [get]
func (h *EventHandlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query parameters
	namespace := r.URL.Query().Get("namespace")
	eventType := r.URL.Query().Get("eventType")
	resourceName := r.URL.Query().Get("resourceName")
	status := r.URL.Query().Get("status")

	// Get all hooks that have active events
	hookNames := h.server.dedupManager.GetAllHookNames()

	var allEvents []EventResponse

	// Collect events from all hooks
	for _, hookNameStr := range hookNames {
		// Parse hook reference (format: "namespace/name")
		parts := strings.Split(hookNameStr, "/")
		if len(parts) != 2 {
			h.server.logger.V(1).Info("Invalid hook name format", "hookName", hookNameStr)
			continue
		}

		hookRef := types.NamespacedName{
			Namespace: parts[0],
			Name:      parts[1],
		}

		// Get active events for this hook
		activeEvents := h.server.dedupManager.GetActiveEventsWithStatus(hookRef)

		// Convert to API response format
		for _, event := range activeEvents {
			// Apply filters
			if namespace != "" && parts[0] != namespace {
				continue
			}
			if eventType != "" && event.EventType != eventType {
				continue
			}
			if resourceName != "" && event.ResourceName != resourceName {
				continue
			}
			if status != "" && event.Status != status {
				continue
			}

			allEvents = append(allEvents, EventResponse{
				EventType:    event.EventType,
				ResourceName: event.ResourceName,
				Namespace:    parts[0],
				FirstSeen:    event.FirstSeen,
				LastSeen:     event.LastSeen,
				Status:       event.Status,
			})
		}
	}

	response := EventListResponse{
		Data:  allEvents,
		Total: len(allEvents),
	}

	h.server.writeJSON(w, http.StatusOK, response)
}

// StreamEvents handles GET /api/v1/events/stream (Server-Sent Events)
// @Summary Stream events
// @Description Returns a Server-Sent Events stream for real-time event updates
// @Tags events
// @Accept json
// @Produce text/event-stream
// @Success 200 {string} string "Event stream"
// @Router /events/stream [get]
func (h *EventHandlers) StreamEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for events
	eventCh := make(chan EventResponse, 10)

	// Start a goroutine to periodically send events
	go h.streamEventLoop(r.Context(), eventCh)

	// Send initial connection message
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected"}`)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Send heartbeat every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			h.server.logger.Info("Client disconnected from event stream")
			return

		case event := <-eventCh:
			eventJSON, err := json.Marshal(event)
			if err != nil {
				h.server.logger.Error(err, "Failed to marshal event")
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", eventJSON)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case <-ticker.C:
			// Send heartbeat
			fmt.Fprintf(w, ": heartbeat\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// streamEventLoop periodically collects and sends events
func (h *EventHandlers) streamEventLoop(ctx context.Context, eventCh chan<- EventResponse) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			// Get all active events
			hookNames := h.server.dedupManager.GetAllHookNames()
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
					select {
					case eventCh <- EventResponse{
						EventType:    event.EventType,
						ResourceName: event.ResourceName,
						Namespace:    parts[0],
						FirstSeen:    event.FirstSeen,
						LastSeen:     event.LastSeen,
						Status:       event.Status,
					}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}
}
