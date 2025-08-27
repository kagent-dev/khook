package status

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent/hook-controller/api/v1alpha2"
	"github.com/kagent/hook-controller/internal/interfaces"
	"github.com/kagent/hook-controller/internal/logging"
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
		logger:   logging.NewLogger("status-manager"),
	}
}

// UpdateHookStatus updates the status of a Hook resource with active events
func (m *Manager) UpdateHookStatus(ctx context.Context, hookInterface interface{}, activeEvents []interfaces.ActiveEvent) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
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

	// Update the hook status
	hook.Status.ActiveEvents = statusEvents
	hook.Status.LastUpdated = metav1.NewTime(time.Now())

	if err := m.client.Status().Update(ctx, hook); err != nil {
		m.logger.Error(err, "Failed to update hook status",
			"hook", hook.Name,
			"namespace", hook.Namespace)
		return fmt.Errorf("failed to update hook status: %w", err)
	}

	m.logger.Info("Successfully updated hook status",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"lastUpdated", hook.Status.LastUpdated.Time)

	return nil
}

// RecordEventFiring records that an event has started firing
func (m *Manager) RecordEventFiring(ctx context.Context, hookInterface interface{}, event interfaces.Event, agentId string) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
	m.logger.Info("Recording event firing",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentId", agentId)

	// Emit Kubernetes event for audit trail
	m.recorder.Event(hook, corev1.EventTypeNormal, "EventFiring",
		fmt.Sprintf("Event %s fired for resource %s, calling agent %s",
			event.Type, event.ResourceName, agentId))

	return nil
}

// RecordEventResolved records that an event has been resolved
func (m *Manager) RecordEventResolved(ctx context.Context, hookInterface interface{}, eventType, resourceName string) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
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
func (m *Manager) RecordError(ctx context.Context, hookInterface interface{}, event interfaces.Event, err error, agentId string) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
	m.logger.Error(err, "Recording event processing error",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentId", agentId)

	// Emit Kubernetes event for error tracking
	m.recorder.Event(hook, corev1.EventTypeWarning, "EventProcessingError",
		fmt.Sprintf("Failed to process event %s for resource %s with agent %s: %v",
			event.Type, event.ResourceName, agentId, err))

	return nil
}

// RecordAgentCallSuccess records a successful agent call
func (m *Manager) RecordAgentCallSuccess(ctx context.Context, hookInterface interface{}, event interfaces.Event, agentId, requestId string) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
	m.logger.Info("Recording successful agent call",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentId", agentId,
		"requestId", requestId)

	// Emit Kubernetes event for successful processing
	m.recorder.Event(hook, corev1.EventTypeNormal, "AgentCallSuccess",
		fmt.Sprintf("Successfully called agent %s for event %s on resource %s (request: %s)",
			agentId, event.Type, event.ResourceName, requestId))

	return nil
}

// RecordAgentCallFailure records a failed agent call
func (m *Manager) RecordAgentCallFailure(ctx context.Context, hookInterface interface{}, event interfaces.Event, agentId string, err error) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
	m.logger.Error(err, "Recording failed agent call",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentId", agentId)

	// Emit Kubernetes event for failed processing
	m.recorder.Event(hook, corev1.EventTypeWarning, "AgentCallFailure",
		fmt.Sprintf("Failed to call agent %s for event %s on resource %s: %v",
			agentId, event.Type, event.ResourceName, err))

	return nil
}

// RecordDuplicateEvent records that a duplicate event was ignored
func (m *Manager) RecordDuplicateEvent(ctx context.Context, hookInterface interface{}, event interfaces.Event) error {
	hook, ok := hookInterface.(*v1alpha2.Hook)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.Hook, got %T", hookInterface)
	}
	m.logger.Info("Recording duplicate event ignored",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"eventTimestamp", event.Timestamp)

	// Emit Kubernetes event for duplicate tracking (using Normal type to avoid noise)
	m.recorder.Event(hook, corev1.EventTypeNormal, "DuplicateEventIgnored",
		fmt.Sprintf("Duplicate event %s ignored for resource %s (within deduplication window)",
			event.Type, event.ResourceName))

	return nil
}

// GetHookStatus retrieves the current status of a Hook resource
func (m *Manager) GetHookStatus(ctx context.Context, hookName, namespace string) (interface{}, error) {
	hook := &v1alpha2.Hook{}
	key := client.ObjectKey{Name: hookName, Namespace: namespace}

	if err := m.client.Get(ctx, key, hook); err != nil {
		m.logger.Error(err, "Failed to get hook for status retrieval",
			"hook", hookName,
			"namespace", namespace)
		return nil, fmt.Errorf("failed to get hook %s/%s: %w", namespace, hookName, err)
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
