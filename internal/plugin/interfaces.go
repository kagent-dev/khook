package plugin

import (
	"context"
	"fmt"
	"time"
)

// EventSource defines the interface that all event source plugins must implement
type EventSource interface {
	// Name returns the unique name of the event source
	Name() string

	// Version returns the version of the event source implementation
	Version() string

	// Initialize sets up the event source with configuration
	Initialize(ctx context.Context, config map[string]interface{}) error

	// WatchEvents returns a channel of events from this source
	WatchEvents(ctx context.Context) (<-chan Event, error)

	// SupportedEventTypes returns the list of event types this source can provide
	SupportedEventTypes() []string

	// Stop gracefully shuts down the event source
	Stop() error
}

// Event represents a unified event format for all event sources
type Event struct {
	// Type is the event type (e.g., "PodRestart", "KafkaMessage", "WebhookEvent")
	Type string `json:"type"`

	// ResourceName is the name of the resource that triggered the event
	ResourceName string `json:"resourceName"`

	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Namespace where the event occurred (empty for non-Kubernetes sources)
	Namespace string `json:"namespace,omitempty"`

	// Reason provides additional context about the event
	Reason string `json:"reason,omitempty"`

	// Message contains the detailed event message
	Message string `json:"message"`

	// Source identifies the event source (e.g., "kubernetes", "kafka", "webhook")
	Source string `json:"source"`

	// Metadata contains source-specific additional information
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Tags are key-value pairs for event categorization and filtering
	Tags map[string]string `json:"tags,omitempty"`
}

// NewEvent creates a new event with the given parameters
func NewEvent(eventType, resourceName, namespace, reason, message, source string) *Event {
	return &Event{
		Type:         eventType,
		ResourceName: resourceName,
		Timestamp:    time.Now(),
		Namespace:    namespace,
		Reason:       reason,
		Message:      message,
		Source:       source,
		Metadata:     make(map[string]interface{}),
		Tags:         make(map[string]string),
	}
}

// WithMetadata adds metadata to an event
func (e *Event) WithMetadata(key string, value interface{}) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithTag adds a tag to an event
func (e *Event) WithTag(key, value string) *Event {
	if e.Tags == nil {
		e.Tags = make(map[string]string)
	}
	e.Tags[key] = value
	return e
}

// IsValid checks if the event has all required fields
func (e *Event) IsValid() bool {
	return e.Type != "" && e.ResourceName != "" && e.Source != "" && e.Message != ""
}

// String returns a string representation of the event
func (e *Event) String() string {
	return fmt.Sprintf("[%s] %s: %s (%s)", e.Timestamp.Format(time.RFC3339), e.Type, e.Message, e.Source)
}

// PluginMetadata contains metadata about a loaded plugin
type PluginMetadata struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Path         string   `json:"path"`
	EventTypes   []string `json:"eventTypes"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// PluginLoader handles loading and validation of event source plugins
type PluginLoader interface {
	LoadPlugin(path string) (*PluginMetadata, EventSource, error)
	ValidatePlugin(metadata *PluginMetadata) error
	UnloadPlugin(name string) error
}

// EventMappingLoader handles loading event type mappings from configuration files
type EventMappingLoader interface {
	LoadMappings(filePath string) error
	GetMapping(eventSource, eventType string) (*EventMapping, bool)
	GetAllMappings() map[string]*EventMapping
}

// EventMapping defines how external events map to internal event types
type EventMapping struct {
	EventSource  string            `yaml:"eventSource" json:"eventSource"`
	EventType    string            `yaml:"eventType" json:"eventType"`
	InternalType string            `yaml:"internalType" json:"internalType"`
	Description  string            `yaml:"description" json:"description"`
	Severity     string            `yaml:"severity" json:"severity"`
	Tags         map[string]string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Enabled      bool              `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}
