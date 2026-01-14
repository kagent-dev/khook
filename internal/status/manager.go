package status

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

// Manager handles status updates for Hook resources
type Manager struct {
	client   client.Client
	recorder record.EventRecorder
	logger   logr.Logger
}

// NewManager creates a new status manager
func NewManager(client client.Client, recorder record.EventRecorder) *Manager {
	return &Manager{
		client:   client,
		recorder: recorder,
		logger:   log.Log.WithName("status-manager"),
	}
}

// UpdateHookStatus updates the status of a Hook resource with active events
func (m *Manager) UpdateHookStatus(ctx context.Context, hook *v1alpha2.Hook, activeEvents []interfaces.ActiveEvent) error {
	m.logger.Info("Updating hook status",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"activeEventsCount", len(activeEvents))

	// Convert ActiveEvent to ActiveEventStatus
	statusEvents := make([]v1alpha2.ActiveEventStatus, len(activeEvents))
	for i, event := range activeEvents {
		statusEvents[i] = v1alpha2.ActiveEventStatus{
			EventType:    event.EventType,
			ResourceName: event.ResourceName,
			FirstSeen:    metav1.NewTime(event.FirstSeen),
			LastSeen:     metav1.NewTime(event.LastSeen),
			Status:       event.Status,
		}
	}

	// Retry loop to handle resource version conflicts
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Fetch the latest version of the hook to get the current resource version
		latestHook := &v1alpha2.Hook{}
		key := client.ObjectKey{Name: hook.Name, Namespace: hook.Namespace}
		if err := m.client.Get(ctx, key, latestHook); err != nil {
			m.logger.Error(err, "Failed to get latest hook version",
				"hook", hook.Name,
				"namespace", hook.Namespace,
				"attempt", attempt+1)
			return fmt.Errorf("failed to get latest hook version: %w", err)
		}

		// Update the status on the latest version
		latestHook.Status.ActiveEvents = statusEvents
		latestHook.Status.LastUpdated = metav1.NewTime(time.Now())

		// Attempt to update the status
		if err := m.client.Status().Update(ctx, latestHook); err != nil {
			// Check if this is a conflict error
			if client.IgnoreNotFound(err) != nil && attempt < maxRetries-1 {
				m.logger.V(1).Info("Hook status update conflict, retrying",
					"hook", hook.Name,
					"namespace", hook.Namespace,
					"attempt", attempt+1,
					"error", err.Error())
				// Brief backoff before retry
				time.Sleep(time.Millisecond * 100 * time.Duration(attempt+1))
				continue
			}
			m.logger.Error(err, "Failed to update hook status",
				"hook", hook.Name,
				"namespace", hook.Namespace,
				"attempt", attempt+1)
			return fmt.Errorf("failed to update hook status after %d attempts: %w", attempt+1, err)
		}

		// Success
		m.logger.Info("Successfully updated hook status",
			"hook", hook.Name,
			"namespace", hook.Namespace,
			"lastUpdated", latestHook.Status.LastUpdated.Time,
			"attempt", attempt+1)
		return nil
	}

	return fmt.Errorf("failed to update hook status after %d attempts", maxRetries)
}

// RecordEventFiring records that an event has started firing
func (m *Manager) RecordEventFiring(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName) error {
	logValues := []interface{}{
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"agentRef", agentRef,
	}
	if event.ResourceName != "" {
		logValues = append(logValues, "resourceName", event.ResourceName)
	}
	if event.UID != "" {
		logValues = append(logValues, "uid", event.UID)
	}
	m.logger.Info("Recording event firing", logValues...)

	// Emit Kubernetes event for audit trail
	eventMsg := fmt.Sprintf("Event %s fired, calling agent %s", event.Type, agentRef.Name)
	if event.ResourceName != "" {
		eventMsg = fmt.Sprintf("Event %s fired for resource %s, calling agent %s",
			event.Type, event.ResourceName, agentRef.Name)
	}
	m.recorder.Event(hook, corev1.EventTypeNormal, "EventFiring", eventMsg)

	return nil
}

// RecordEventResolved records that an event has been resolved
func (m *Manager) RecordEventResolved(ctx context.Context, hook *v1alpha2.Hook, eventType, resourceName string) error {
	m.logger.Info("Recording event resolved",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", eventType,
		"resourceName", resourceName)

	// Emit Kubernetes event for audit trail
	m.recorder.Event(hook, corev1.EventTypeNormal, "EventResolved",
		fmt.Sprintf("Event %s resolved for resource %s after timeout",
			eventType, resourceName))

	return nil
}

// RecordError records an error that occurred during event processing
func (m *Manager) RecordError(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, err error, agentRef types.NamespacedName) error {
	logValues := []interface{}{
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"agentRef", agentRef,
	}
	if event.ResourceName != "" {
		logValues = append(logValues, "resourceName", event.ResourceName)
	}
	m.logger.Error(err, "Recording event processing error", logValues...)

	// Emit Kubernetes event for error tracking
	eventMsg := fmt.Sprintf("Failed to process event %s with agent %s: %v",
		event.Type, agentRef.Name, err)
	if event.ResourceName != "" {
		eventMsg = fmt.Sprintf("Failed to process event %s for resource %s with agent %s: %v",
			event.Type, event.ResourceName, agentRef.Name, err)
	}
	m.recorder.Event(hook, corev1.EventTypeWarning, "EventProcessingError", eventMsg)

	return nil
}

// RecordAgentCallSuccess records a successful agent call
func (m *Manager) RecordAgentCallSuccess(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName, requestId string) error {
	logValues := []interface{}{
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"agentRef", agentRef,
		"requestId", requestId,
	}
	if event.ResourceName != "" {
		logValues = append(logValues, "resourceName", event.ResourceName)
	}
	m.logger.Info("Recording successful agent call", logValues...)

	// Emit Kubernetes event for successful processing
	eventMsg := fmt.Sprintf("Successfully called agent %s for event %s (request: %s)",
		agentRef.Name, event.Type, requestId)
	if event.ResourceName != "" {
		eventMsg = fmt.Sprintf("Successfully called agent %s for event %s on resource %s (request: %s)",
			agentRef.Name, event.Type, event.ResourceName, requestId)
	}
	m.recorder.Event(hook, corev1.EventTypeNormal, "AgentCallSuccess", eventMsg)

	return nil
}

// RecordAgentCallFailure records a failed agent call
func (m *Manager) RecordAgentCallFailure(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName, err error) error {
	logValues := []interface{}{
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"agentRef", agentRef,
	}
	if event.ResourceName != "" {
		logValues = append(logValues, "resourceName", event.ResourceName)
	}
	m.logger.Error(err, "Recording failed agent call", logValues...)

	// Emit Kubernetes event for failed processing
	eventMsg := fmt.Sprintf("Failed to call agent %s for event %s: %v",
		agentRef.Name, event.Type, err)
	if event.ResourceName != "" {
		eventMsg = fmt.Sprintf("Failed to call agent %s for event %s on resource %s: %v",
			agentRef.Name, event.Type, event.ResourceName, err)
	}
	m.recorder.Event(hook, corev1.EventTypeWarning, "AgentCallFailure", eventMsg)

	return nil
}

// RecordDuplicateEvent records that a duplicate event was ignored
func (m *Manager) RecordDuplicateEvent(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event) error {
	logValues := []interface{}{
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"eventTimestamp", event.Timestamp,
	}
	if event.ResourceName != "" {
		logValues = append(logValues, "resourceName", event.ResourceName)
	}
	m.logger.Info("Recording duplicate event ignored", logValues...)

	// Emit Kubernetes event for duplicate tracking (using Normal type to avoid noise)
	eventMsg := fmt.Sprintf("Duplicate event %s ignored (within deduplication window)", event.Type)
	if event.ResourceName != "" {
		eventMsg = fmt.Sprintf("Duplicate event %s ignored for resource %s (within deduplication window)",
			event.Type, event.ResourceName)
	}
	m.recorder.Event(hook, corev1.EventTypeNormal, "DuplicateEventIgnored", eventMsg)

	return nil
}

// GetHookStatus retrieves the current status of a Hook resource
func (m *Manager) GetHookStatus(ctx context.Context, hookRef types.NamespacedName) (*v1alpha2.HookStatus, error) {
	hook := &v1alpha2.Hook{}
	key := client.ObjectKey{Name: hookRef.Name, Namespace: hookRef.Namespace}

	if err := m.client.Get(ctx, key, hook); err != nil {
		m.logger.Error(err, "Failed to get hook for status retrieval",
			"hook", hookRef)
		return nil, fmt.Errorf("failed to get hook %s: %w", hookRef, err)
	}

	return &hook.Status, nil
}

// LogControllerStartup logs controller initialization details
func (m *Manager) LogControllerStartup(ctx context.Context, version string, config map[string]interface{}) {
	m.logger.Info("KAgent Hook Controller starting up",
		"version", version,
		"config", config)
}

// LogControllerShutdown logs controller shutdown details
func (m *Manager) LogControllerShutdown(ctx context.Context, reason string) {
	m.logger.Info("KAgent Hook Controller shutting down",
		"reason", reason)
}
