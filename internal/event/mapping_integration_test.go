package event

import (
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappingLoaderWithRealConfig(t *testing.T) {
	// Use the actual config file
	configPath := filepath.Join("..", "..", "config", "event-mappings.yaml")

	logger := logr.Discard()
	loader := NewMappingLoader(logger)

	// Load the real configuration
	err := loader.LoadMappings(configPath)
	require.NoError(t, err)

	// Test that we loaded the expected mappings
	allMappings := loader.GetAllMappings()
	assert.True(t, len(allMappings) > 0, "Should have loaded mappings from config file")

	// Test specific Kubernetes mappings
	k8sMappings := loader.GetMappingsBySource("kubernetes")
	assert.Len(t, k8sMappings, 4, "Should have 4 Kubernetes mappings")

	// Test enabled mappings (only Kubernetes should be enabled)
	enabledMappings := loader.GetEnabledMappings()
	assert.Len(t, enabledMappings, 4, "Should have 4 enabled mappings (all Kubernetes)")

	// Test specific mapping retrieval
	podRestartMapping, exists := loader.GetMapping("kubernetes", "pod-restart")
	assert.True(t, exists)
	assert.Equal(t, "PodRestart", podRestartMapping.InternalType)
	assert.Equal(t, "warning", podRestartMapping.Severity)
	assert.True(t, podRestartMapping.Enabled)

	oomKillMapping, exists := loader.GetMapping("kubernetes", "oom-kill")
	assert.True(t, exists)
	assert.Equal(t, "OOMKill", oomKillMapping.InternalType)
	assert.Equal(t, "error", oomKillMapping.Severity)
	assert.True(t, oomKillMapping.Enabled)

	// Test disabled mapping
	kafkaMapping, exists := loader.GetMapping("kafka", "consumer-lag")
	assert.True(t, exists)
	assert.Equal(t, "ConsumerLag", kafkaMapping.InternalType)
	assert.Equal(t, "warning", kafkaMapping.Severity)
	assert.False(t, kafkaMapping.Enabled)

	// Test validation of all loaded mappings
	errors := loader.ValidateAllMappings()
	assert.Empty(t, errors, "All mappings should be valid")
}
