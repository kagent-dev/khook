package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kagent-dev/khook/internal/plugin"
	k8splugin "github.com/kagent-dev/khook/internal/plugin/kubernetes"
)

// TestPluginFrameworkWithKubernetesPlugin demonstrates how to use the plugin test framework
func TestPluginFrameworkWithKubernetesPlugin(t *testing.T) {
	framework := NewPluginTestFramework(t)
	source := k8splugin.NewKubernetesEventSource()

	t.Run("Interface", func(t *testing.T) {
		framework.TestPluginInterface(source)
	})

	t.Run("Configuration", func(t *testing.T) {
		testCases := []ConfigTestCase{
			{
				Name:          "missing client",
				Config:        map[string]interface{}{},
				ExpectError:   true,
				ErrorContains: "kubernetesClient",
			},
			{
				Name: "invalid client type",
				Config: map[string]interface{}{
					"kubernetesClient": "not-a-client",
				},
				ExpectError:   true,
				ErrorContains: "kubernetesClient",
			},
			{
				Name: "invalid namespace",
				Config: map[string]interface{}{
					"kubernetesClient": nil, // This will still fail, but for testing
					"namespace":        "invalid/namespace",
				},
				ExpectError: true,
			},
		}

		framework.TestPluginConfiguration(source, testCases)
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		errorScenarios := []ErrorScenario{
			{
				Name:        "initialize without client",
				Phase:       "initialize",
				Config:      map[string]interface{}{},
				ExpectError: true,
			},
			{
				Name:  "start without initialize",
				Phase: "start",
				Setup: func(source plugin.EventSource) {
					// Don't initialize
				},
				ExpectError: false, // Start should handle this gracefully
			},
		}

		framework.TestPluginErrorHandling(source, errorScenarios)
	})
}

// TestPluginFrameworkWithMockPlugin demonstrates testing with a mock plugin
func TestPluginFrameworkWithMockPlugin(t *testing.T) {
	framework := NewPluginTestFramework(t)
	source := NewMockEventSource("test-plugin", "1.0.0", []string{"test-event"})

	t.Run("Interface", func(t *testing.T) {
		framework.TestPluginInterface(source)
	})

	t.Run("Lifecycle", func(t *testing.T) {
		config := map[string]interface{}{
			"test": "config",
		}
		framework.TestPluginLifecycle(source, config)

		// Verify mock state
		assert.True(t, source.IsInitialized())
		assert.True(t, source.IsStarted())
		assert.True(t, source.IsStopped())
	})

	t.Run("EventGeneration", func(t *testing.T) {
		config := map[string]interface{}{}
		events := framework.TestEventGeneration(source, config, 1*time.Second)

		// Should have received at least one event from mock
		assert.NotEmpty(t, events, "Mock should generate events")

		if len(events) > 0 {
			event := events[0]
			assert.Equal(t, "test-event", event.Type)
			assert.Equal(t, "test-resource", event.ResourceName)
			assert.Equal(t, "test-plugin", event.Source)
		}
	})

	t.Run("Performance", func(t *testing.T) {
		config := map[string]interface{}{}
		metrics := framework.TestPluginPerformance(source, config, 2*time.Second)

		// Mock should generate at least one event
		assert.Greater(t, metrics.EventCount, int64(0))
		assert.Greater(t, metrics.EventsPerSecond, 0.0)

		t.Logf("Performance: %d events in %v (%.2f events/sec)",
			metrics.EventCount, metrics.Duration, metrics.EventsPerSecond)
	})

	t.Run("Concurrency", func(t *testing.T) {
		config := map[string]interface{}{}
		// Test with 10 concurrent goroutines
		framework.TestPluginConcurrency(source, config, 10)
	})

	t.Run("ResourceCleanup", func(t *testing.T) {
		config := map[string]interface{}{}
		framework.TestPluginResourceCleanup(source, config)
	})
}

// TestPluginFrameworkConfiguration demonstrates configuration testing patterns
func TestPluginFrameworkConfiguration(t *testing.T) {
	framework := NewPluginTestFramework(t)
	source := NewMockEventSource("config-test", "1.0.0", []string{"config-event"})

	configTests := []ConfigTestCase{
		{
			Name:        "empty config",
			Config:      map[string]interface{}{},
			ExpectError: false,
		},
		{
			Name: "valid config",
			Config: map[string]interface{}{
				"endpoint": "http://example.com",
				"timeout":  30,
				"enabled":  true,
			},
			ExpectError: false,
		},
		{
			Name: "invalid type",
			Config: map[string]interface{}{
				"timeout": "invalid",
			},
			ExpectError: false, // Mock doesn't validate
		},
	}

	framework.TestPluginConfiguration(source, configTests)
}

// TestPluginFrameworkErrorScenarios demonstrates error scenario testing
func TestPluginFrameworkErrorScenarios(t *testing.T) {
	framework := NewPluginTestFramework(t)
	source := NewMockEventSource("error-test", "1.0.0", []string{"error-event"})

	errorScenarios := []ErrorScenario{
		{
			Name:        "normal initialize",
			Phase:       "initialize",
			Config:      map[string]interface{}{},
			ExpectError: false,
		},
		{
			Name:        "normal start",
			Phase:       "start",
			Config:      map[string]interface{}{},
			ExpectError: false,
		},
		{
			Name:        "normal stop",
			Phase:       "stop",
			Config:      map[string]interface{}{},
			ExpectError: false,
		},
		{
			Name:  "start without initialize",
			Phase: "start",
			Setup: func(source plugin.EventSource) {
				// Reset mock state if needed
				if mock, ok := source.(*MockEventSource); ok {
					mock.initialized = false
				}
			},
			ExpectError: true, // Mock requires initialization
		},
	}

	framework.TestPluginErrorHandling(source, errorScenarios)
}

// BenchmarkPluginFramework benchmarks the plugin framework itself
func BenchmarkPluginFramework(b *testing.B) {
	_ = NewMockEventSource("benchmark", "1.0.0", []string{"bench-event"})
	config := map[string]interface{}{}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Create fresh source for each iteration
		testSource := NewMockEventSource("benchmark", "1.0.0", []string{"bench-event"})

		// Test lifecycle
		err := testSource.Initialize(nil, config)
		if err != nil {
			b.Fatal(err)
		}

		_, err = testSource.WatchEvents(context.Background())
		if err != nil {
			b.Fatal(err)
		}

		err = testSource.Stop()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ExamplePluginTestFramework shows how to use the plugin test framework
func ExamplePluginTestFramework() {
	// This would be in a real test function
	// t := &testing.T{} // In real usage, this comes from the test function

	// Create framework
	// framework := NewPluginTestFramework(t)

	// Create your plugin
	// source := YourPlugin.NewEventSource()

	// Test the interface
	// framework.TestPluginInterface(source)

	// Test configuration
	// configTests := []ConfigTestCase{
	//     {
	//         Name:        "valid config",
	//         Config:      map[string]interface{}{"key": "value"},
	//         ExpectError: false,
	//     },
	// }
	// framework.TestPluginConfiguration(source, configTests)

	// Test lifecycle
	// config := map[string]interface{}{"key": "value"}
	// framework.TestPluginLifecycle(source, config)
}
