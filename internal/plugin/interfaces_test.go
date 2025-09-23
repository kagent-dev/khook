package plugin

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEventIsValid(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected bool
	}{
		{
			name: "valid event",
			event: Event{
				Type:         "PodRestart",
				ResourceName: "test-pod",
				Timestamp:    time.Now(),
				Message:      "Pod restarted",
				Source:       "kubernetes",
			},
			expected: true,
		},
		{
			name: "missing type",
			event: Event{
				ResourceName: "test-pod",
				Message:      "Pod restarted",
				Source:       "kubernetes",
			},
			expected: false,
		},
		{
			name: "missing resource name",
			event: Event{
				Type:    "PodRestart",
				Message: "Pod restarted",
				Source:  "kubernetes",
			},
			expected: false,
		},
		{
			name: "missing message",
			event: Event{
				Type:         "PodRestart",
				ResourceName: "test-pod",
				Source:       "kubernetes",
			},
			expected: false,
		},
		{
			name: "missing source",
			event: Event{
				Type:         "PodRestart",
				ResourceName: "test-pod",
				Message:      "Pod restarted",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.IsValid())
		})
	}
}

func TestEventWithMetadata(t *testing.T) {
	event := Event{
		Type:         "PodRestart",
		ResourceName: "test-pod",
		Message:      "Pod restarted",
		Source:       "kubernetes",
	}

	// Test adding metadata
	result := event.WithMetadata("podPhase", "Running")
	assert.NotNil(t, result.Metadata)
	assert.Equal(t, "Running", result.Metadata["podPhase"])

	// Test adding multiple metadata
	result = result.WithMetadata("restartCount", 3)
	assert.Equal(t, 3, result.Metadata["restartCount"])
	assert.Equal(t, "Running", result.Metadata["podPhase"])
}

func TestEventWithTag(t *testing.T) {
	event := Event{
		Type:         "PodRestart",
		ResourceName: "test-pod",
		Message:      "Pod restarted",
		Source:       "kubernetes",
	}

	// Test adding tags
	result := event.WithTag("environment", "production")
	assert.NotNil(t, result.Tags)
	assert.Equal(t, "production", result.Tags["environment"])

	// Test adding multiple tags
	result = result.WithTag("team", "platform")
	assert.Equal(t, "platform", result.Tags["team"])
	assert.Equal(t, "production", result.Tags["environment"])
}

func TestEventString(t *testing.T) {
	timestamp := time.Date(2023, 12, 25, 10, 30, 0, 0, time.UTC)
	event := Event{
		Type:         "PodRestart",
		ResourceName: "test-pod",
		Timestamp:    timestamp,
		Message:      "Pod restarted",
		Source:       "kubernetes",
	}

	expected := "[2023-12-25T10:30:00Z] PodRestart: Pod restarted (kubernetes)"
	assert.Equal(t, expected, event.String())
}

func TestNewEvent(t *testing.T) {
	event := NewEvent("PodRestart", "test-pod", "default", "ContainerCannotRun", "Pod restarted", "kubernetes")

	assert.Equal(t, "PodRestart", event.Type)
	assert.Equal(t, "test-pod", event.ResourceName)
	assert.Equal(t, "default", event.Namespace)
	assert.Equal(t, "ContainerCannotRun", event.Reason)
	assert.Equal(t, "Pod restarted", event.Message)
	assert.Equal(t, "kubernetes", event.Source)
	assert.NotNil(t, event.Metadata)
	assert.NotNil(t, event.Tags)

	// Timestamp should be recent
	assert.True(t, time.Since(event.Timestamp) < time.Minute)
}

func TestEventMappingValidation(t *testing.T) {
	tests := []struct {
		name      string
		mapping   EventMapping
		shouldErr bool
	}{
		{
			name: "valid mapping",
			mapping: EventMapping{
				EventSource:  "kubernetes",
				EventType:    "pod-restart",
				InternalType: "PodRestart",
				Description:  "Pod container restart detected",
				Severity:     "warning",
			},
			shouldErr: false,
		},
		{
			name: "missing event source",
			mapping: EventMapping{
				EventType:    "pod-restart",
				InternalType: "PodRestart",
			},
			shouldErr: true,
		},
		{
			name: "missing event type",
			mapping: EventMapping{
				EventSource:  "kubernetes",
				InternalType: "PodRestart",
			},
			shouldErr: true,
		},
		{
			name: "missing internal type",
			mapping: EventMapping{
				EventSource: "kubernetes",
				EventType:   "pod-restart",
			},
			shouldErr: true,
		},
		{
			name: "invalid severity",
			mapping: EventMapping{
				EventSource:  "kubernetes",
				EventType:    "pod-restart",
				InternalType: "PodRestart",
				Severity:     "invalid",
			},
			shouldErr: true,
		},
		{
			name: "valid severity levels",
			mapping: EventMapping{
				EventSource:  "kubernetes",
				EventType:    "pod-restart",
				InternalType: "PodRestart",
				Severity:     "info",
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEventMapping(&tt.mapping)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Helper function for testing - this would normally be a method on MappingLoader
func validateEventMapping(mapping *EventMapping) error {
	if mapping.EventSource == "" {
		return fmt.Errorf("eventSource cannot be empty")
	}
	if mapping.EventType == "" {
		return fmt.Errorf("eventType cannot be empty")
	}
	if mapping.InternalType == "" {
		return fmt.Errorf("internalType cannot be empty")
	}
	if mapping.Severity != "" {
		switch mapping.Severity {
		case "info", "warning", "error", "critical":
			// Valid severity
		default:
			return fmt.Errorf("invalid severity '%s'", mapping.Severity)
		}
	}
	return nil
}
