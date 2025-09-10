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
func (m *Manager) RecordEventFiring(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName) error {
	m.logger.Info("Recording event firing",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentRef", agentRef)

	// Emit Kubernetes event for audit trail
	m.recorder.Event(hook, corev1.EventTypeNormal, "EventFiring",
		fmt.Sprintf("Event %s fired for resource %s, calling agent %s",
			event.Type, event.ResourceName, agentRef.Name))

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
	m.logger.Error(err, "Recording event processing error",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentRef", agentRef)

	// Emit Kubernetes event for error tracking
	m.recorder.Event(hook, corev1.EventTypeWarning, "EventProcessingError",
		fmt.Sprintf("Failed to process event %s for resource %s with agent %s: %v",
			event.Type, event.ResourceName, agentRef.Name, err))

	return nil
}

// RecordAgentCallSuccess records a successful agent call
func (m *Manager) RecordAgentCallSuccess(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName, requestId string) error {
	m.logger.Info("Recording successful agent call",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentRef", agentRef,
		"requestId", requestId)

	// Emit Kubernetes event for successful processing
	m.recorder.Event(hook, corev1.EventTypeNormal, "AgentCallSuccess",
		fmt.Sprintf("Successfully called agent %s for event %s on resource %s (request: %s)",
			agentRef.Name, event.Type, event.ResourceName, requestId))

	return nil
}

// RecordAgentCallFailure records a failed agent call
func (m *Manager) RecordAgentCallFailure(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event, agentRef types.NamespacedName, err error) error {
	m.logger.Error(err, "Recording failed agent call",
		"hook", hook.Name,
		"namespace", hook.Namespace,
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"agentRef", agentRef)

	// Emit Kubernetes event for failed processing
	m.recorder.Event(hook, corev1.EventTypeWarning, "AgentCallFailure",
		fmt.Sprintf("Failed to call agent %s for event %s on resource %s: %v",
			agentRef.Name, event.Type, event.ResourceName, err))

	return nil
}

// RecordDuplicateEvent records that a duplicate event was ignored
func (m *Manager) RecordDuplicateEvent(ctx context.Context, hook *v1alpha2.Hook, event interfaces.Event) error {
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
