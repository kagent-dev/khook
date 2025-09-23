package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/khook/internal/plugin"
)

// PluginTestFramework provides utilities for testing plugins
type PluginTestFramework struct {
	t      *testing.T
	logger logr.Logger
}

// NewPluginTestFramework creates a new plugin test framework
func NewPluginTestFramework(t *testing.T) *PluginTestFramework {
	return &PluginTestFramework{
		t:      t,
		logger: logr.Discard(), // Use discard logger for tests
	}
}

// TestPluginInterface verifies that a plugin correctly implements the EventSource interface
func (ptf *PluginTestFramework) TestPluginInterface(source plugin.EventSource) {
	ptf.t.Helper()

	// Test Name method
	name := source.Name()
	assert.NotEmpty(ptf.t, name, "Plugin name should not be empty")
	assert.Regexp(ptf.t, `^[a-z][a-z0-9-]*$`, name, "Plugin name should be lowercase with hyphens")

	// Test Version method
	version := source.Version()
	assert.NotEmpty(ptf.t, version, "Plugin version should not be empty")
	assert.Regexp(ptf.t, `^\d+\.\d+\.\d+`, version, "Plugin version should follow semantic versioning")

	// Test SupportedEventTypes method
	eventTypes := source.SupportedEventTypes()
	assert.NotEmpty(ptf.t, eventTypes, "Plugin should support at least one event type")
	for _, eventType := range eventTypes {
		assert.NotEmpty(ptf.t, eventType, "Event type should not be empty")
	}
}

// TestPluginLifecycle tests the complete plugin lifecycle
func (ptf *PluginTestFramework) TestPluginLifecycle(source plugin.EventSource, config map[string]interface{}) {
	ptf.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test Initialize
	err := source.Initialize(ctx, config)
	require.NoError(ptf.t, err, "Plugin initialization should succeed")

	// Test WatchEvents (equivalent to Start)
	eventChan, err := source.WatchEvents(ctx)
	require.NoError(ptf.t, err, "Plugin WatchEvents should succeed")
	require.NotNil(ptf.t, eventChan, "Event channel should not be nil")

	// Let plugin run briefly
	time.Sleep(100 * time.Millisecond)

	// Test Stop
	err = source.Stop()
	assert.NoError(ptf.t, err, "Plugin stop should succeed")
}

// TestPluginConfiguration tests plugin configuration handling
func (ptf *PluginTestFramework) TestPluginConfiguration(source plugin.EventSource, testCases []ConfigTestCase) {
	ptf.t.Helper()

	for _, tc := range testCases {
		ptf.t.Run(tc.Name, func(t *testing.T) {
			ctx := context.Background()
			err := source.Initialize(ctx, tc.Config)

			if tc.ExpectError {
				assert.Error(t, err, "Expected configuration error for: %s", tc.Name)
				if tc.ErrorContains != "" {
					assert.Contains(t, err.Error(), tc.ErrorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "Configuration should be valid for: %s", tc.Name)
			}
		})
	}
}

// ConfigTestCase represents a configuration test case
type ConfigTestCase struct {
	Name          string
	Config        map[string]interface{}
	ExpectError   bool
	ErrorContains string
}

// TestEventGeneration tests that a plugin generates valid events
func (ptf *PluginTestFramework) TestEventGeneration(source plugin.EventSource, config map[string]interface{}, timeout time.Duration) []plugin.Event {
	ptf.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	eventCh := make(chan plugin.Event, 100)
	var events []plugin.Event
	var wg sync.WaitGroup

	// Collect events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case event := <-eventCh:
				events = append(events, event)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Initialize and start plugin
	err := source.Initialize(ctx, config)
	require.NoError(ptf.t, err)

	_, err = source.WatchEvents(ctx)
	require.NoError(ptf.t, err)

	// Wait for timeout or context cancellation
	<-ctx.Done()

	// Stop plugin
	err = source.Stop()
	assert.NoError(ptf.t, err)

	// Wait for event collection to finish
	close(eventCh)
	wg.Wait()

	// Validate collected events
	for i, event := range events {
		ptf.ValidateEvent(event, fmt.Sprintf("event[%d]", i))
	}

	return events
}

// ValidateEvent validates that an event is properly formed
func (ptf *PluginTestFramework) ValidateEvent(event plugin.Event, context string) {
	ptf.t.Helper()

	assert.True(ptf.t, event.IsValid(), "%s should be valid", context)
	assert.NotEmpty(ptf.t, event.Type, "%s type should not be empty", context)
	assert.NotEmpty(ptf.t, event.ResourceName, "%s resource name should not be empty", context)
	assert.NotEmpty(ptf.t, event.Message, "%s message should not be empty", context)
	assert.NotEmpty(ptf.t, event.Source, "%s source should not be empty", context)
	assert.False(ptf.t, event.Timestamp.IsZero(), "%s timestamp should be set", context)
}

// TestPluginErrorHandling tests plugin error scenarios
func (ptf *PluginTestFramework) TestPluginErrorHandling(source plugin.EventSource, errorScenarios []ErrorScenario) {
	ptf.t.Helper()

	for _, scenario := range errorScenarios {
		ptf.t.Run(scenario.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Setup error scenario
			if scenario.Setup != nil {
				scenario.Setup(source)
			}

			// Test the scenario
			var err error
			switch scenario.Phase {
			case "initialize":
				err = source.Initialize(ctx, scenario.Config)
			case "start":
				source.Initialize(ctx, map[string]interface{}{})
				_, err = source.WatchEvents(ctx)
			case "stop":
				source.Initialize(ctx, map[string]interface{}{})
				_, _ = source.WatchEvents(ctx)
				err = source.Stop()
			}

			if scenario.ExpectError {
				assert.Error(t, err, "Expected error in phase: %s", scenario.Phase)
			} else {
				assert.NoError(t, err, "Should not error in phase: %s", scenario.Phase)
			}

			// Cleanup
			if scenario.Cleanup != nil {
				scenario.Cleanup(source)
			}
		})
	}
}

// ErrorScenario represents an error testing scenario
type ErrorScenario struct {
	Name        string
	Phase       string // "initialize", "start", "stop"
	Config      map[string]interface{}
	ExpectError bool
	Setup       func(plugin.EventSource)
	Cleanup     func(plugin.EventSource)
}

// TestPluginPerformance tests plugin performance characteristics
func (ptf *PluginTestFramework) TestPluginPerformance(source plugin.EventSource, config map[string]interface{}, duration time.Duration) PerformanceMetrics {
	ptf.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	eventCh := make(chan plugin.Event, 1000)
	metrics := PerformanceMetrics{
		StartTime: time.Now(),
	}

	// Count events
	var eventCount int64
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-eventCh:
				eventCount++
			case <-ctx.Done():
				return
			}
		}
	}()

	// Initialize and start plugin
	err := source.Initialize(ctx, config)
	require.NoError(ptf.t, err)

	startTime := time.Now()
	_, err = source.WatchEvents(ctx)
	require.NoError(ptf.t, err)

	// Wait for test duration
	<-ctx.Done()

	// Stop plugin
	stopTime := time.Now()
	err = source.Stop()
	assert.NoError(ptf.t, err)

	// Wait for event counting to finish
	close(eventCh)
	wg.Wait()

	metrics.EndTime = time.Now()
	metrics.Duration = stopTime.Sub(startTime)
	metrics.EventCount = eventCount
	metrics.EventsPerSecond = float64(eventCount) / metrics.Duration.Seconds()

	ptf.t.Logf("Performance metrics: %+v", metrics)

	return metrics
}

// PerformanceMetrics contains performance test results
type PerformanceMetrics struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	EventCount      int64
	EventsPerSecond float64
}

// TestPluginConcurrency tests plugin behavior under concurrent access
func (ptf *PluginTestFramework) TestPluginConcurrency(source plugin.EventSource, config map[string]interface{}, goroutines int) {
	ptf.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initialize plugin
	err := source.Initialize(ctx, config)
	require.NoError(ptf.t, err)

	_, err = source.WatchEvents(ctx)
	require.NoError(ptf.t, err)

	// Start multiple goroutines that interact with the plugin
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Simulate concurrent operations
			for j := 0; j < 10; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					// Perform operations that might cause race conditions
					_ = source.Name()
					_ = source.Version()
					_ = source.SupportedEventTypes()
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Stop plugin
	err = source.Stop()
	assert.NoError(ptf.t, err)
}

// TestPluginResourceCleanup tests that plugins properly clean up resources
func (ptf *PluginTestFramework) TestPluginResourceCleanup(source plugin.EventSource, config map[string]interface{}) {
	ptf.t.Helper()

	ctx := context.Background()

	// Initialize and start plugin
	err := source.Initialize(ctx, config)
	require.NoError(ptf.t, err)

	_, err = source.WatchEvents(ctx)
	require.NoError(ptf.t, err)

	// Stop plugin
	err = source.Stop()
	require.NoError(ptf.t, err)

	// Try to stop again - should not panic or error
	err = source.Stop()
	assert.NoError(ptf.t, err, "Multiple stops should be safe")

	// TODO: Add specific resource leak detection
	// This could include checking for:
	// - Open file descriptors
	// - Active goroutines
	// - Network connections
	// - Memory usage
}

// MockEventSource is a mock implementation for testing
type MockEventSource struct {
	name        string
	version     string
	eventTypes  []string
	initialized bool
	started     bool
	stopped     bool
	eventCh     chan plugin.Event
	stopCh      chan struct{}
}

// NewMockEventSource creates a new mock event source
func NewMockEventSource(name, version string, eventTypes []string) *MockEventSource {
	return &MockEventSource{
		name:       name,
		version:    version,
		eventTypes: eventTypes,
		stopCh:     make(chan struct{}),
	}
}

func (m *MockEventSource) Name() string                  { return m.name }
func (m *MockEventSource) Version() string               { return m.version }
func (m *MockEventSource) SupportedEventTypes() []string { return m.eventTypes }

func (m *MockEventSource) Initialize(ctx context.Context, config map[string]interface{}) error {
	m.initialized = true
	return nil
}

func (m *MockEventSource) WatchEvents(ctx context.Context) (<-chan plugin.Event, error) {
	if !m.initialized {
		return nil, fmt.Errorf("plugin not initialized")
	}
	m.started = true

	eventCh := make(chan plugin.Event, 10)
	m.eventCh = eventCh

	// Send a test event
	go func() {
		defer close(eventCh)
		select {
		case eventCh <- *plugin.NewEvent("test-event", "test-resource", "", "Test", "Test event", m.name):
		case <-m.stopCh:
		case <-ctx.Done():
		}
	}()

	return eventCh, nil
}

func (m *MockEventSource) Stop() error {
	m.stopped = true
	close(m.stopCh)
	return nil
}

// IsInitialized returns whether the mock was initialized
func (m *MockEventSource) IsInitialized() bool { return m.initialized }

// IsStarted returns whether the mock was started
func (m *MockEventSource) IsStarted() bool { return m.started }

// IsStopped returns whether the mock was stopped
func (m *MockEventSource) IsStopped() bool { return m.stopped }
