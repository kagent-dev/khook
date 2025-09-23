package event

import (
	"github.com/kagent-dev/khook/internal/plugin"
)

// Event is an alias to the plugin.Event type for backward compatibility
type Event = plugin.Event

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

// EventMappingConfig contains the configuration for event mappings
type EventMappingConfig struct {
	Mappings []EventMapping `yaml:"mappings" json:"mappings"`
}

// Severity levels for events
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

// NewEvent creates a new event with the given parameters
var NewEvent = plugin.NewEvent
