package event

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappingLoaderLoadMappings(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	mappingFile := filepath.Join(tmpDir, "mappings.yaml")

	mappingContent := `
mappings:
  - eventSource: "kubernetes"
    eventType: "pod-restart"
    internalType: "PodRestart"
    description: "Pod container restart detected"
    severity: "warning"
    enabled: true

  - eventSource: "kubernetes"
    eventType: "pod-pending"
    internalType: "PodPending"
    description: "Pod stuck in pending state"
    severity: "error"
    enabled: false

  - eventSource: "kafka"
    eventType: "message"
    internalType: "KafkaMessage"
    description: "Kafka message received"
    severity: "info"
`

	err := os.WriteFile(mappingFile, []byte(mappingContent), 0644)
	require.NoError(t, err)

	// Create loader and load mappings
	logger := logr.Discard()
	loader := NewMappingLoader(logger)
	err = loader.LoadMappings(mappingFile)

	assert.NoError(t, err)
	assert.Len(t, loader.mappings, 3)

	// Test getting mappings
	mapping1, exists := loader.GetMapping("kubernetes", "pod-restart")
	assert.True(t, exists)
	assert.Equal(t, "PodRestart", mapping1.InternalType)
	assert.Equal(t, "warning", mapping1.Severity)
	assert.True(t, mapping1.Enabled)

	mapping2, exists := loader.GetMapping("kubernetes", "pod-pending")
	assert.True(t, exists)
	assert.Equal(t, "PodPending", mapping2.InternalType)
	assert.Equal(t, "error", mapping2.Severity)
	assert.False(t, mapping2.Enabled)

	mapping3, exists := loader.GetMapping("kafka", "message")
	assert.True(t, exists)
	assert.Equal(t, "KafkaMessage", mapping3.InternalType)
	assert.Equal(t, "info", mapping3.Severity)
}

func TestMappingLoaderGetMappingsBySource(t *testing.T) {
	tmpDir := t.TempDir()
	mappingFile := filepath.Join(tmpDir, "mappings.yaml")

	mappingContent := `
mappings:
  - eventSource: "kubernetes"
    eventType: "pod-restart"
    internalType: "PodRestart"
    severity: "warning"

  - eventSource: "kubernetes"
    eventType: "pod-pending"
    internalType: "PodPending"
    severity: "error"

  - eventSource: "kafka"
    eventType: "message"
    internalType: "KafkaMessage"
    severity: "info"
`

	err := os.WriteFile(mappingFile, []byte(mappingContent), 0644)
	require.NoError(t, err)

	logger := logr.Discard()
	loader := NewMappingLoader(logger)
	err = loader.LoadMappings(mappingFile)
	require.NoError(t, err)

	// Test getting mappings by source
	k8sMappings := loader.GetMappingsBySource("kubernetes")
	assert.Len(t, k8sMappings, 2)

	kafkaMappings := loader.GetMappingsBySource("kafka")
	assert.Len(t, kafkaMappings, 1)

	nonExistentMappings := loader.GetMappingsBySource("nonexistent")
	assert.Len(t, nonExistentMappings, 0)
}

func TestMappingLoaderGetEnabledMappings(t *testing.T) {
	tmpDir := t.TempDir()
	mappingFile := filepath.Join(tmpDir, "mappings.yaml")

	mappingContent := `
mappings:
  - eventSource: "kubernetes"
    eventType: "pod-restart"
    internalType: "PodRestart"
    severity: "warning"
    enabled: true

  - eventSource: "kubernetes"
    eventType: "pod-pending"
    internalType: "PodPending"
    severity: "error"
    enabled: false

  - eventSource: "kafka"
    eventType: "message"
    internalType: "KafkaMessage"
    severity: "info"
    enabled: true
`

	err := os.WriteFile(mappingFile, []byte(mappingContent), 0644)
	require.NoError(t, err)

	logger := logr.Discard()
	loader := NewMappingLoader(logger)
	err = loader.LoadMappings(mappingFile)
	require.NoError(t, err)

	// Test getting enabled mappings
	enabledMappings := loader.GetEnabledMappings()
	assert.Len(t, enabledMappings, 2)

	// Verify enabled mappings
	eventTypes := make([]string, len(enabledMappings))
	for i, mapping := range enabledMappings {
		eventTypes[i] = mapping.EventType
	}
	assert.Contains(t, eventTypes, "pod-restart")
	assert.Contains(t, eventTypes, "message")
	assert.NotContains(t, eventTypes, "pod-pending")
}

func TestMappingLoaderValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	mappingFile := filepath.Join(tmpDir, "invalid-mappings.yaml")

	// Create invalid mapping file
	mappingContent := `
mappings:
  - eventType: "pod-restart"
    internalType: "PodRestart"
    severity: "warning"

  - eventSource: "kubernetes"
    internalType: "PodPending"
    severity: "error"

  - eventSource: "kubernetes"
    eventType: "pod-pending"
    severity: "invalid"

  - eventSource: "kafka"
    eventType: "message"
    internalType: "KafkaMessage"
    severity: "info"
`

	err := os.WriteFile(mappingFile, []byte(mappingContent), 0644)
	require.NoError(t, err)

	logger := logr.Discard()
	loader := NewMappingLoader(logger)
	err = loader.LoadMappings(mappingFile)

	// Should not error, but should only load valid mappings
	assert.NoError(t, err)

	// Should only have the valid mapping
	assert.Len(t, loader.mappings, 1)

	mapping, exists := loader.GetMapping("kafka", "message")
	assert.True(t, exists)
	assert.Equal(t, "KafkaMessage", mapping.InternalType)
}

func TestMappingLoaderNonExistentFile(t *testing.T) {
	logger := logr.Discard()
	loader := NewMappingLoader(logger)

	err := loader.LoadMappings("/non/existent/file.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read mapping file")
}

func TestMappingLoaderReloadMappings(t *testing.T) {
	tmpDir := t.TempDir()
	mappingFile := filepath.Join(tmpDir, "mappings.yaml")

	// Initial content
	initialContent := `
mappings:
  - eventSource: "kubernetes"
    eventType: "pod-restart"
    internalType: "PodRestart"
    severity: "warning"
`

	err := os.WriteFile(mappingFile, []byte(initialContent), 0644)
	require.NoError(t, err)

	logger := logr.Discard()
	loader := NewMappingLoader(logger)
	err = loader.LoadMappings(mappingFile)
	require.NoError(t, err)
	assert.Len(t, loader.mappings, 1)

	// Updated content
	updatedContent := `
mappings:
  - eventSource: "kubernetes"
    eventType: "pod-restart"
    internalType: "PodRestart"
    severity: "warning"

  - eventSource: "kubernetes"
    eventType: "pod-pending"
    internalType: "PodPending"
    severity: "error"

  - eventSource: "kafka"
    eventType: "message"
    internalType: "KafkaMessage"
    severity: "info"
`

	err = os.WriteFile(mappingFile, []byte(updatedContent), 0644)
	require.NoError(t, err)

	// Reload mappings
	err = loader.ReloadMappings(mappingFile)
	assert.NoError(t, err)
	assert.Len(t, loader.mappings, 3)
}

func TestMappingLoaderValidateAllMappings(t *testing.T) {
	logger := logr.Discard()
	loader := NewMappingLoader(logger)

	// Manually add an invalid mapping to test validation
	loader.mappings["invalid:mapping"] = &EventMapping{
		EventSource:  "", // Invalid - empty source
		EventType:    "invalid-mapping",
		InternalType: "InvalidMapping",
		Severity:     "info",
	}

	loader.mappings["valid:mapping"] = &EventMapping{
		EventSource:  "kubernetes",
		EventType:    "pod-restart",
		InternalType: "PodRestart",
		Severity:     "warning",
	}

	// Validate all mappings
	errors := loader.ValidateAllMappings()
	assert.Len(t, errors, 1) // Should have one validation error for the invalid mapping
	assert.Contains(t, errors[0].Error(), "eventSource cannot be empty")
}
