package interfaces

import (
	"context"
	"time"

	"github.com/kagent/hook-controller/api/v1alpha2"
)

// ControllerManager orchestrates the controller lifecycle and watches
type ControllerManager interface {
	Start(ctx context.Context) error
	Stop() error
	AddHookWatch(hook interface{}) error
	RemoveHookWatch(hookName string) error
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
	Hook          interface{} `json:"hook"`
	Configuration interface{} `json:"configuration"`
	Event         Event       `json:"event"`
}

// EventWatcher monitors Kubernetes events and filters them against hook configurations
type EventWatcher interface {
	WatchEvents(ctx context.Context, eventTypes []string) (<-chan Event, error)
	FilterEvent(event Event, hooks []interface{}) []EventMatch
	Start(ctx context.Context) error
	Stop() error
}

// AgentRequest represents a request to the Kagent API
type AgentRequest struct {
	AgentId      string                 `json:"agentId"`
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
	ShouldProcessEvent(hookName string, event Event) bool
	RecordEvent(hookName string, event Event) error
	CleanupExpiredEvents(hookName string) error
	GetActiveEvents(hookName string) []ActiveEvent
	MarkNotified(hookName string, event Event)
}

// StatusManager handles status updates and event recording for Hook resources
type StatusManager interface {
	UpdateHookStatus(ctx context.Context, hook interface{}, activeEvents []ActiveEvent) error
	RecordEventFiring(ctx context.Context, hook interface{}, event Event, agentId string) error
	RecordEventResolved(ctx context.Context, hook interface{}, eventType, resourceName string) error
	RecordError(ctx context.Context, hook interface{}, event Event, err error, agentId string) error
	RecordAgentCallSuccess(ctx context.Context, hook interface{}, event Event, agentId, requestId string) error
	RecordAgentCallFailure(ctx context.Context, hook interface{}, event Event, agentId string, err error) error
	RecordDuplicateEvent(ctx context.Context, hook interface{}, event Event) error
	GetHookStatus(ctx context.Context, hookName, namespace string) (*v1alpha2.HookStatus, error)
	LogControllerStartup(ctx context.Context, version string, config map[string]interface{})
	LogControllerShutdown(ctx context.Context, reason string)
}
