package test

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kagent-dev/khook/internal/event"
	"github.com/kagent-dev/khook/internal/plugin"
	"github.com/kagent-dev/khook/internal/plugin/kubernetes"
)

// TestPluginSystemIntegration demonstrates the complete plugin system workflow
func TestPluginSystemIntegration(t *testing.T) {
	logger := logr.Discard()

	// 1. Create and configure the plugin manager
	pluginManager := plugin.NewManager(logger, []string{})

	// 2. Create a Kubernetes event source (simulating built-in plugin)
	k8sEventSource := kubernetes.NewKubernetesEventSource()

	// Initialize the Kubernetes plugin with a fake client
	fakeClient := fake.NewSimpleClientset()
	config := map[string]interface{}{
		"client":    fakeClient,
		"namespace": "test-namespace",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := k8sEventSource.Initialize(ctx, config)
	require.NoError(t, err)

	// 3. Test that we can get plugin information (no plugins loaded initially)
	allPlugins := pluginManager.GetAllPlugins()
	assert.Len(t, allPlugins, 0) // No plugins loaded yet

	// 4. Create event mapping loader and load mappings
	mappingLoader := event.NewMappingLoader(logger)
	err = mappingLoader.LoadMappings("../config/event-mappings.yaml")
	require.NoError(t, err)

	// 5. Verify mappings are loaded
	k8sMappings := mappingLoader.GetMappingsBySource("kubernetes")
	assert.Len(t, k8sMappings, 4) // pod-restart, oom-kill, pod-pending, probe-failed

	enabledMappings := mappingLoader.GetEnabledMappings()
	assert.Len(t, enabledMappings, 4) // All Kubernetes mappings should be enabled

	// 6. Test event mapping functionality
	podRestartMapping, exists := mappingLoader.GetMapping("kubernetes", "pod-restart")
	assert.True(t, exists)
	assert.Equal(t, "PodRestart", podRestartMapping.InternalType)
	assert.Equal(t, "warning", podRestartMapping.Severity)
	assert.True(t, podRestartMapping.Enabled)

	// 7. Test Kubernetes event source functionality
	assert.Equal(t, "kubernetes", k8sEventSource.Name())
	assert.Equal(t, "1.0.0", k8sEventSource.Version())

	supportedTypes := k8sEventSource.SupportedEventTypes()
	assert.Contains(t, supportedTypes, "pod-restart")
	assert.Contains(t, supportedTypes, "oom-kill")
	assert.Contains(t, supportedTypes, "pod-pending")
	assert.Contains(t, supportedTypes, "probe-failed")

	// 8. Test graceful shutdown
	err = pluginManager.Shutdown()
	assert.NoError(t, err)

	// 9. Test event creation and validation
	testEvent := plugin.NewEvent("pod-restart", "test-pod", "default", "BackOff", "Container restart", "kubernetes")
	assert.True(t, testEvent.IsValid())
	assert.Equal(t, "pod-restart", testEvent.Type)
	assert.Equal(t, "test-pod", testEvent.ResourceName)
	assert.Equal(t, "kubernetes", testEvent.Source)

	// Test event with metadata and tags
	testEvent.WithMetadata("count", "3").WithTag("severity", "warning")
	assert.Equal(t, "3", testEvent.Metadata["count"])
	assert.Equal(t, "warning", testEvent.Tags["severity"])
}

// TestEventMappingIntegration tests the event mapping system
func TestEventMappingIntegration(t *testing.T) {
	logger := logr.Discard()
	mappingLoader := event.NewMappingLoader(logger)

	// Load the real configuration
	err := mappingLoader.LoadMappings("../config/event-mappings.yaml")
	require.NoError(t, err)

	// Test Kubernetes mappings
	tests := []struct {
		source       string
		eventType    string
		expectedType string
		expectedSev  string
		enabled      bool
	}{
		{"kubernetes", "pod-restart", "PodRestart", "warning", true},
		{"kubernetes", "oom-kill", "OOMKill", "error", true},
		{"kubernetes", "pod-pending", "PodPending", "warning", true},
		{"kubernetes", "probe-failed", "ProbeFailed", "warning", true},
		{"kafka", "consumer-lag", "ConsumerLag", "warning", false},
		{"prometheus", "high-cpu", "HighCPU", "warning", false},
	}

	for _, tt := range tests {
		t.Run(tt.source+"_"+tt.eventType, func(t *testing.T) {
			mapping, exists := mappingLoader.GetMapping(tt.source, tt.eventType)
			assert.True(t, exists, "Mapping should exist for %s:%s", tt.source, tt.eventType)
			assert.Equal(t, tt.expectedType, mapping.InternalType)
			assert.Equal(t, tt.expectedSev, mapping.Severity)
			assert.Equal(t, tt.enabled, mapping.Enabled)
		})
	}

	// Test filtering by enabled status
	enabledMappings := mappingLoader.GetEnabledMappings()
	enabledCount := 0
	for _, mapping := range enabledMappings {
		if mapping.EventSource == "kubernetes" {
			enabledCount++
		}
	}
	assert.Equal(t, 4, enabledCount, "Should have 4 enabled Kubernetes mappings")

	// Test validation
	errors := mappingLoader.ValidateAllMappings()
	assert.Empty(t, errors, "All mappings should be valid")
}

// TestKubernetesPluginIntegration tests the Kubernetes plugin specifically
func TestKubernetesPluginIntegration(t *testing.T) {
	// Create Kubernetes event source
	k8sEventSource := kubernetes.NewKubernetesEventSource()

	// Test basic properties
	assert.Equal(t, "kubernetes", k8sEventSource.Name())
	assert.Equal(t, "1.0.0", k8sEventSource.Version())

	supportedTypes := k8sEventSource.SupportedEventTypes()
	expectedTypes := []string{"pod-restart", "oom-kill", "pod-pending", "probe-failed"}
	assert.ElementsMatch(t, expectedTypes, supportedTypes)

	// Test initialization with fake client
	fakeClient := fake.NewSimpleClientset()
	config := map[string]interface{}{
		"client":    fakeClient,
		"namespace": "test-namespace",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := k8sEventSource.Initialize(ctx, config)
	require.NoError(t, err)

	// Test stop
	err = k8sEventSource.Stop()
	assert.NoError(t, err)
}
