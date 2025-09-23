package plugin

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEventSource is a mock implementation of EventSource for testing
type MockEventSource struct {
	mock.Mock
	name         string
	version      string
	eventTypes   []string
	eventChannel chan Event
}

func NewMockEventSource(name, version string, eventTypes []string) *MockEventSource {
	return &MockEventSource{
		name:         name,
		version:      version,
		eventTypes:   eventTypes,
		eventChannel: make(chan Event, 10),
	}
}

func (m *MockEventSource) Name() string {
	return m.name
}

func (m *MockEventSource) Version() string {
	return m.version
}

func (m *MockEventSource) Initialize(ctx context.Context, config map[string]interface{}) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockEventSource) WatchEvents(ctx context.Context) (<-chan Event, error) {
	args := m.Called(ctx)
	return m.eventChannel, args.Error(0)
}

func (m *MockEventSource) SupportedEventTypes() []string {
	return m.eventTypes
}

func (m *MockEventSource) Stop() error {
	args := m.Called()
	close(m.eventChannel)
	return args.Error(0)
}

func TestNewManager(t *testing.T) {
	logger := logr.Discard()
	pluginPaths := []string{"/path/to/plugin1.so", "/path/to/plugin2.so"}

	manager := NewManager(logger, pluginPaths)

	assert.NotNil(t, manager)
	assert.Equal(t, pluginPaths, manager.pluginPaths)
	assert.NotNil(t, manager.registry)
	assert.NotNil(t, manager.channelManager)
	assert.NotNil(t, manager.ctx)
}

func TestManagerValidatePlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	tests := []struct {
		name        string
		metadata    *PluginMetadata
		eventSource EventSource
		shouldErr   bool
	}{
		{
			name: "valid plugin",
			metadata: &PluginMetadata{
				Name:       "test-plugin",
				Version:    "1.0.0",
				EventTypes: []string{"TestEvent"},
			},
			eventSource: NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"}),
			shouldErr:   false,
		},
		{
			name: "empty name",
			metadata: &PluginMetadata{
				Name:       "",
				Version:    "1.0.0",
				EventTypes: []string{"TestEvent"},
			},
			eventSource: NewMockEventSource("", "1.0.0", []string{"TestEvent"}),
			shouldErr:   true,
		},
		{
			name: "empty version",
			metadata: &PluginMetadata{
				Name:       "test-plugin",
				Version:    "",
				EventTypes: []string{"TestEvent"},
			},
			eventSource: NewMockEventSource("test-plugin", "", []string{"TestEvent"}),
			shouldErr:   true,
		},
		{
			name: "no event types",
			metadata: &PluginMetadata{
				Name:       "test-plugin",
				Version:    "1.0.0",
				EventTypes: []string{},
			},
			eventSource: NewMockEventSource("test-plugin", "1.0.0", []string{}),
			shouldErr:   true,
		},
		{
			name: "nil event source",
			metadata: &PluginMetadata{
				Name:       "test-plugin",
				Version:    "1.0.0",
				EventTypes: []string{"TestEvent"},
			},
			eventSource: nil,
			shouldErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.validatePlugin(tt.metadata, tt.eventSource)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManagerInitializePlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})
	mockEventSource.On("Initialize", mock.Anything, mock.Anything).Return(nil)

	// Manually add a plugin to the manager
	manager.registry.RegisterPlugin("test-plugin", &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      false,
	})

	// Test initialization
	config := map[string]interface{}{"key": "value"}
	err := manager.InitializePlugin("test-plugin", config)

	assert.NoError(t, err)
	plugin, exists := manager.registry.GetPlugin("test-plugin")
	assert.True(t, exists)
	assert.True(t, plugin.Active)
	mockEventSource.AssertExpectations(t)
}

func TestManagerInitializePluginNotFound(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	err := manager.InitializePlugin("nonexistent-plugin", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin nonexistent-plugin not found")
}

func TestManagerStartPlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})
	mockEventSource.On("WatchEvents", mock.Anything).Return(nil)

	// Manually add an active plugin to the manager
	manager.registry.RegisterPlugin("test-plugin", &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      true,
	}

	// Test starting the plugin
	err := manager.StartPlugin("test-plugin")

	assert.NoError(t, err)
	assert.Contains(t, manager.eventChannels, "test-plugin")
	mockEventSource.AssertExpectations(t)
}

func TestManagerStartPluginNotActive(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})

	// Manually add an inactive plugin to the manager
	manager.registry.RegisterPlugin("test-plugin", &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      false,
	}

	// Test starting the plugin
	err := manager.StartPlugin("test-plugin")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin test-plugin is not initialized")
}

func TestManagerStopPlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})
	mockEventSource.On("Stop").Return(nil)

	// Manually add an active plugin to the manager
	manager.registry.RegisterPlugin("test-plugin", &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      true,
	}
	manager.eventChannels["test-plugin"] = make(chan Event)

	// Test stopping the plugin
	err := manager.StopPlugin("test-plugin")

	assert.NoError(t, err)
	assert.False(t, manager.plugins["test-plugin"].Active)
	assert.NotContains(t, manager.eventChannels, "test-plugin")
	mockEventSource.AssertExpectations(t)
}

func TestManagerGetPlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})

	// Manually add a plugin to the manager
	expectedPlugin := &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      false,
	}
	manager.plugins["test-plugin"] = expectedPlugin

	// Test getting the plugin
	plugin, exists := manager.GetPlugin("test-plugin")
	assert.True(t, exists)
	assert.Equal(t, expectedPlugin, plugin)

	// Test getting non-existent plugin
	plugin, exists = manager.GetPlugin("nonexistent")
	assert.False(t, exists)
	assert.Nil(t, plugin)
}

func TestManagerGetActivePlugins(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Add active and inactive plugins
	activePlugin := &LoadedPlugin{
		Metadata: &PluginMetadata{Name: "active-plugin"},
		Active:   true,
	}
	inactivePlugin := &LoadedPlugin{
		Metadata: &PluginMetadata{Name: "inactive-plugin"},
		Active:   false,
	}

	manager.plugins["active-plugin"] = activePlugin
	manager.plugins["inactive-plugin"] = inactivePlugin

	// Test getting active plugins
	activePlugins := manager.GetActivePlugins()
	assert.Len(t, activePlugins, 1)
	assert.Contains(t, activePlugins, "active-plugin")
	assert.NotContains(t, activePlugins, "inactive-plugin")
}

func TestManagerGetPluginsByEventType(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Add plugins with different event types
	plugin1 := &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "plugin1",
			EventTypes: []string{"EventA", "EventB"},
		},
	}
	plugin2 := &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "plugin2",
			EventTypes: []string{"EventB", "EventC"},
		},
	}
	plugin3 := &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "plugin3",
			EventTypes: []string{"EventD"},
		},
	}

	manager.plugins["plugin1"] = plugin1
	manager.plugins["plugin2"] = plugin2
	manager.plugins["plugin3"] = plugin3

	// Test getting plugins by event type
	pluginsForEventB := manager.GetPluginsByEventType("EventB")
	assert.Len(t, pluginsForEventB, 2)

	pluginsForEventD := manager.GetPluginsByEventType("EventD")
	assert.Len(t, pluginsForEventD, 1)
	assert.Equal(t, "plugin3", pluginsForEventD[0].Metadata.Name)

	pluginsForEventX := manager.GetPluginsByEventType("EventX")
	assert.Len(t, pluginsForEventX, 0)
}

func TestManagerUnloadPlugin(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create a mock event source
	mockEventSource := NewMockEventSource("test-plugin", "1.0.0", []string{"TestEvent"})
	mockEventSource.On("Stop").Return(nil)

	// Manually add an active plugin to the manager
	manager.registry.RegisterPlugin("test-plugin", &LoadedPlugin{
		Metadata: &PluginMetadata{
			Name:       "test-plugin",
			Version:    "1.0.0",
			EventTypes: []string{"TestEvent"},
		},
		EventSource: mockEventSource,
		Active:      true,
	}
	manager.eventChannels["test-plugin"] = make(chan Event)

	// Test unloading the plugin
	err := manager.UnloadPlugin("test-plugin")

	assert.NoError(t, err)
	assert.NotContains(t, manager.plugins, "test-plugin")
	assert.NotContains(t, manager.eventChannels, "test-plugin")
	mockEventSource.AssertExpectations(t)
}

func TestManagerShutdown(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	// Create mock event sources
	mockEventSource1 := NewMockEventSource("plugin1", "1.0.0", []string{"TestEvent"})
	mockEventSource1.On("Stop").Return(nil)

	mockEventSource2 := NewMockEventSource("plugin2", "1.0.0", []string{"TestEvent"})
	mockEventSource2.On("Stop").Return(nil)

	// Add active plugins
	manager.plugins["plugin1"] = &LoadedPlugin{
		EventSource: mockEventSource1,
		Active:      true,
	}
	manager.plugins["plugin2"] = &LoadedPlugin{
		EventSource: mockEventSource2,
		Active:      true,
	}

	// Test shutdown
	err := manager.Shutdown()

	assert.NoError(t, err)
	assert.Len(t, manager.plugins, 0)
	assert.Len(t, manager.eventChannels, 0)
	mockEventSource1.AssertExpectations(t)
	mockEventSource2.AssertExpectations(t)
}

func TestManagerValidatePluginPath(t *testing.T) {
	logger := logr.Discard()
	manager := NewManager(logger, []string{})

	tests := []struct {
		name      string
		path      string
		shouldErr bool
	}{
		{
			name:      "invalid extension",
			path:      "/path/to/plugin.txt",
			shouldErr: true,
		},
		{
			name:      "valid extension but invalid file",
			path:      "/nonexistent/plugin.so",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.ValidatePluginPath(tt.path)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
