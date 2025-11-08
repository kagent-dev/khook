package apiserver

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
)

// HookHandlers handles hook-related API endpoints
type HookHandlers struct {
	server *Server
}

// NewHookHandlers creates a new hook handlers instance
func NewHookHandlers(server *Server) *HookHandlers {
	return &HookHandlers{server: server}
}

// HookResponse represents a hook in the API response
type HookResponse struct {
	Name                string                              `json:"name"`
	Namespace           string                              `json:"namespace"`
	EventConfigurations []kagentv1alpha2.EventConfiguration `json:"eventConfigurations"`
	Status              kagentv1alpha2.HookStatus           `json:"status"`
}

// HookListResponse represents a list of hooks
type HookListResponse struct {
	Items []HookResponse `json:"items"`
	Total int            `json:"total"`
}

// ListHooks handles GET /api/v1/hooks
// @Summary List hooks
// @Description Returns a list of all Hook configurations
// @Tags hooks
// @Accept json
// @Produce json
// @Success 200 {object} HookListResponse
// @Router /hooks [get]
func (h *HookHandlers) ListHooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.server.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := r.Context()

	// List all hooks
	var hookList kagentv1alpha2.HookList
	if err := h.server.k8sClient.List(ctx, &hookList, &client.ListOptions{}); err != nil {
		h.server.logger.Error(err, "Failed to list hooks")
		h.server.writeError(w, http.StatusInternalServerError, "Failed to list hooks")
		return
	}

	// Convert to API response format
	items := make([]HookResponse, 0, len(hookList.Items))
	for _, hook := range hookList.Items {
		items = append(items, HookResponse{
			Name:                hook.Name,
			Namespace:           hook.Namespace,
			EventConfigurations: hook.Spec.EventConfigurations,
			Status:              hook.Status,
		})
	}

	response := HookListResponse{
		Items: items,
		Total: len(items),
	}

	h.server.writeJSON(w, http.StatusOK, response)
}
