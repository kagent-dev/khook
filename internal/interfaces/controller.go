package interfaces

import (
	"context"
	"time"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// ControllerManager orchestrates the controller lifecycle and watches
type ControllerManager interface {
	Start(ctx context.Context) error
	Stop() error
	AddHookWatch(hook *v1alpha2.Hook) error
	RemoveHookWatch(hookRef types.NamespacedName) error
}

// Event represents a Kubernetes event with relevant metadata
type Event struct {
	Type         string            `json:"type"`
	ResourceName string            `json:"resourceName"`
	Timestamp    time.Time         `json:"timestamp"`
	Namespace    string            `json:"namespace"`
	Reason       string            `json:"reason"`
	Message      string            `json:"message"`
	UID          string            `json:"uid"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// EventMatch represents a matched event with its corresponding hook configuration
type EventMatch struct {
	Hook  *v1alpha2.Hook `json:"hook"`
	Event Event          `json:"event"`
}

// EventWatcher monitors Kubernetes events and filters them against hook configurations
type EventWatcher interface {
	WatchEvents(ctx context.Context) (<-chan Event, error)
	FilterEvent(event Event, hooks []*v1alpha2.Hook) []EventMatch
	Start(ctx context.Context) error
	Stop() error
}

// AgentRequest represents a request to the Kagent API
type AgentRequest struct {
	AgentRef     types.NamespacedName   `json:"agentId"`
	Prompt       string                 `json:"prompt"`
	EventName    string                 `json:"eventName"`
	EventTime    time.Time              `json:"eventTime"`
	ResourceName string                 `json:"resourceName"`
	Context      map[string]interface{} `json:"context"`
}

// AgentResponse represents a response from the Kagent API
type AgentResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	RequestId string `json:"requestId"`
}

// KagentClient handles communication with the Kagent platform
type KagentClient interface {
	CallAgent(ctx context.Context, request AgentRequest) (*AgentResponse, error)
	Authenticate() error
}

// ActiveEvent represents an event that is currently being tracked
type ActiveEvent struct {
	EventType      string     `json:"eventType"`
	ResourceName   string     `json:"resourceName"`
	FirstSeen      time.Time  `json:"firstSeen"`
	LastSeen       time.Time  `json:"lastSeen"`
	Status         string     `json:"status"`
	NotifiedAt     *time.Time `json:"notifiedAt,omitempty"`
	LastNotifiedAt *time.Time `json:"lastNotifiedAt,omitempty"`
}

// DeduplicationManager implements event deduplication logic with timeout
type DeduplicationManager interface {
	ShouldProcessEvent(hookRef types.NamespacedName, event Event) bool
	RecordEvent(hookRef types.NamespacedName, event Event) error
	CleanupExpiredEvents(hookRef types.NamespacedName) error
	GetActiveEvents(hookRef types.NamespacedName) []ActiveEvent
	GetActiveEventsWithStatus(hookRef types.NamespacedName) []ActiveEvent
	MarkNotified(hookRef types.NamespacedName, event Event)
}

// EventRecorder handles Kubernetes event recording
type EventRecorder interface {
	Event(object runtime.Object, eventtype, reason, message string)
	Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
	AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{})
}

// StatusManager handles status updates and event recording for Hook resources
type StatusManager interface {
	UpdateHookStatus(ctx context.Context, hook *v1alpha2.Hook, activeEvents []ActiveEvent) error
	RecordEventFiring(ctx context.Context, hook *v1alpha2.Hook, event Event, agentRef types.NamespacedName) error
	RecordEventResolved(ctx context.Context, hook *v1alpha2.Hook, eventType, resourceName string) error
	RecordError(ctx context.Context, hook *v1alpha2.Hook, event Event, err error, agentRef types.NamespacedName) error
	RecordAgentCallSuccess(ctx context.Context, hook *v1alpha2.Hook, event Event, agentRef types.NamespacedName, requestId string) error
	RecordAgentCallFailure(ctx context.Context, hook *v1alpha2.Hook, event Event, agentRef types.NamespacedName, err error) error
	RecordDuplicateEvent(ctx context.Context, hook *v1alpha2.Hook, event Event) error
	GetHookStatus(ctx context.Context, hookRef types.NamespacedName) (*v1alpha2.HookStatus, error)
	LogControllerStartup(ctx context.Context, version string, config map[string]interface{})
	LogControllerShutdown(ctx context.Context, reason string)
}
