package event

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
)

// MappingLoader handles loading event type mappings from configuration files
type MappingLoader struct {
	logger   logr.Logger
	mappings map[string]*EventMapping
}

// NewMappingLoader creates a new event mapping loader
func NewMappingLoader(logger logr.Logger) *MappingLoader {
	return &MappingLoader{
		logger:   logger,
		mappings: make(map[string]*EventMapping),
	}
}

// LoadMappings loads event mappings from a YAML configuration file
func (ml *MappingLoader) LoadMappings(filePath string) error {
	ml.logger.Info("Loading event mappings", "file", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read mapping file %s: %w", filePath, err)
	}

	var config EventMappingConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse mapping file %s: %w", filePath, err)
	}

	// Clear existing mappings
	ml.mappings = make(map[string]*EventMapping)

	// Load new mappings
	for i, mapping := range config.Mappings {
		// Validate mapping
		if err := ml.validateMapping(&config.Mappings[i]); err != nil {
			ml.logger.Error(err, "Invalid event mapping", "source", mapping.EventSource, "type", mapping.EventType)
			continue
		}

		// Set default enabled to true if not specified
		if config.Mappings[i].Enabled == false && !strings.Contains(string(data), "enabled: false") {
			config.Mappings[i].Enabled = true
		}

		key := ml.makeKey(mapping.EventSource, mapping.EventType)
		ml.mappings[key] = &config.Mappings[i]

		ml.logger.Info("Loaded event mapping",
			"source", mapping.EventSource,
			"eventType", mapping.EventType,
			"internalType", mapping.InternalType,
			"severity", mapping.Severity,
			"enabled", config.Mappings[i].Enabled)
	}

	ml.logger.Info("Successfully loaded event mappings",
		"count", len(ml.mappings),
		"file", filePath)

	return nil
}

// GetMapping retrieves an event mapping by source and event type
func (ml *MappingLoader) GetMapping(eventSource, eventType string) (*EventMapping, bool) {
	key := ml.makeKey(eventSource, eventType)
	mapping, exists := ml.mappings[key]
	return mapping, exists
}

// GetAllMappings returns all loaded event mappings
func (ml *MappingLoader) GetAllMappings() map[string]*EventMapping {
	return ml.mappings
}

// GetMappingsBySource returns all mappings for a specific event source
func (ml *MappingLoader) GetMappingsBySource(eventSource string) []*EventMapping {
	var mappings []*EventMapping
	for _, mapping := range ml.mappings {
		if mapping.EventSource == eventSource {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

// GetEnabledMappings returns only enabled event mappings
func (ml *MappingLoader) GetEnabledMappings() []*EventMapping {
	var mappings []*EventMapping
	for _, mapping := range ml.mappings {
		if mapping.Enabled {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

// ReloadMappings reloads the event mappings from the file
func (ml *MappingLoader) ReloadMappings(filePath string) error {
	ml.logger.Info("Reloading event mappings", "file", filePath)
	return ml.LoadMappings(filePath)
}

// ValidateMapping validates an event mapping
func (ml *MappingLoader) validateMapping(mapping *EventMapping) error {
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
		case SeverityInfo, SeverityWarning, SeverityError, SeverityCritical:
			// Valid severity
		default:
			return fmt.Errorf("invalid severity '%s', must be one of: %s, %s, %s, %s",
				mapping.Severity, SeverityInfo, SeverityWarning, SeverityError, SeverityCritical)
		}
	}
	return nil
}

// makeKey creates a unique key for event mapping lookup
func (ml *MappingLoader) makeKey(eventSource, eventType string) string {
	return fmt.Sprintf("%s:%s", eventSource, eventType)
}

// AddMapping manually adds a mapping to the loader (useful for testing or default mappings)
func (ml *MappingLoader) AddMapping(key string, mapping *EventMapping) {
	ml.mappings[key] = mapping
	ml.logger.V(1).Info("Added mapping",
		"key", key,
		"source", mapping.EventSource,
		"eventType", mapping.EventType,
		"internalType", mapping.InternalType)
}

// ValidateAllMappings validates all loaded mappings
func (ml *MappingLoader) ValidateAllMappings() []error {
	var errors []error
	for key, mapping := range ml.mappings {
		if err := ml.validateMapping(mapping); err != nil {
			errors = append(errors, fmt.Errorf("mapping %s: %w", key, err))
		}
	}
	return errors
}
