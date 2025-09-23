package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"plugin"

	"github.com/go-logr/logr"
)

// Manager handles loading and managing event source plugins
type Manager struct {
	logger         logr.Logger
	registry       PluginRegistry
	channelManager EventChannelManager
	pluginPaths    []string
	ctx            context.Context
	cancel         context.CancelFunc
}

// LoadedPlugin represents a loaded event source plugin
type LoadedPlugin struct {
	Metadata    *PluginMetadata
	EventSource EventSource
	Plugin      *plugin.Plugin
	Active      bool
}

// NewManager creates a new plugin manager
func NewManager(logger logr.Logger, pluginPaths []string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		logger:         logger,
		registry:       NewPluginRegistry(),
		channelManager: NewEventChannelManager(),
		pluginPaths:    pluginPaths,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// LoadPlugins loads all plugins from the configured paths
func (m *Manager) LoadPlugins() error {
	m.logger.Info("Loading plugins", "paths", m.pluginPaths)

	for _, pluginPath := range m.pluginPaths {
		if err := m.loadPluginFromPath(pluginPath); err != nil {
			m.logger.Error(err, "Failed to load plugin", "path", pluginPath)
			continue
		}
	}

	allPlugins := m.registry.GetAllPlugins()
	m.logger.Info("Successfully loaded plugins", "count", len(allPlugins))
	return nil
}

// loadPluginFromPath loads a single plugin from the given path
func (m *Manager) loadPluginFromPath(pluginPath string) error {
	m.logger.Info("Loading plugin", "path", pluginPath)

	// Load the plugin
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to open plugin %s: %w", pluginPath, err)
	}

	// Look for the NewEventSource symbol
	newEventSourceSym, err := p.Lookup("NewEventSource")
	if err != nil {
		return fmt.Errorf("plugin %s does not export NewEventSource function: %w", pluginPath, err)
	}

	// Cast to the expected function type
	newEventSource, ok := newEventSourceSym.(func() EventSource)
	if !ok {
		return fmt.Errorf("plugin %s NewEventSource has incorrect signature", pluginPath)
	}

	// Create the event source instance
	eventSource := newEventSource()

	// Create metadata
	metadata := &PluginMetadata{
		Name:        eventSource.Name(),
		Version:     eventSource.Version(),
		Path:        pluginPath,
		EventTypes:  eventSource.SupportedEventTypes(),
		Description: fmt.Sprintf("Event source plugin: %s", eventSource.Name()),
	}

	// Validate the plugin
	if err := m.validatePlugin(metadata, eventSource); err != nil {
		return fmt.Errorf("plugin validation failed for %s: %w", pluginPath, err)
	}

	// Store the loaded plugin
	loadedPlugin := &LoadedPlugin{
		Metadata:    metadata,
		EventSource: eventSource,
		Plugin:      p,
		Active:      false,
	}

	if err := m.registry.RegisterPlugin(metadata.Name, loadedPlugin); err != nil {
		return fmt.Errorf("failed to register plugin %s: %w", metadata.Name, err)
	}

	m.logger.Info("Successfully loaded plugin",
		"name", metadata.Name,
		"version", metadata.Version,
		"eventTypes", metadata.EventTypes)

	return nil
}

// validatePlugin validates a loaded plugin
func (m *Manager) validatePlugin(metadata *PluginMetadata, eventSource EventSource) error {
	if metadata.Name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if metadata.Version == "" {
		return fmt.Errorf("plugin version cannot be empty")
	}
	if len(metadata.EventTypes) == 0 {
		return fmt.Errorf("plugin must support at least one event type")
	}
	if eventSource == nil {
		return fmt.Errorf("event source cannot be nil")
	}
	return nil
}

// InitializePlugin initializes a specific plugin with configuration
func (m *Manager) InitializePlugin(pluginName string, config map[string]interface{}) error {
	loadedPlugin, exists := m.registry.GetPlugin(pluginName)
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	m.logger.Info("Initializing plugin", "name", pluginName, "config", config)

	if err := loadedPlugin.EventSource.Initialize(m.ctx, config); err != nil {
		return fmt.Errorf("failed to initialize plugin %s: %w", pluginName, err)
	}

	loadedPlugin.Active = true
	m.logger.Info("Successfully initialized plugin", "name", pluginName)
	return nil
}

// StartPlugin starts watching events from a specific plugin
func (m *Manager) StartPlugin(pluginName string) error {
	loadedPlugin, exists := m.registry.GetPlugin(pluginName)
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	if !loadedPlugin.Active {
		return fmt.Errorf("plugin %s is not initialized", pluginName)
	}

	m.logger.Info("Starting plugin event watching", "name", pluginName)

	eventChan, err := loadedPlugin.EventSource.WatchEvents(m.ctx)
	if err != nil {
		return fmt.Errorf("failed to start watching events for plugin %s: %w", pluginName, err)
	}

	m.channelManager.RegisterChannel(pluginName, eventChan)
	m.logger.Info("Successfully started plugin event watching", "name", pluginName)
	return nil
}

// StopPlugin stops a specific plugin
func (m *Manager) StopPlugin(pluginName string) error {
	loadedPlugin, exists := m.registry.GetPlugin(pluginName)
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	m.logger.Info("Stopping plugin", "name", pluginName)

	if err := loadedPlugin.EventSource.Stop(); err != nil {
		m.logger.Error(err, "Error stopping plugin", "name", pluginName)
	}

	m.channelManager.UnregisterChannel(pluginName)
	loadedPlugin.Active = false

	m.logger.Info("Successfully stopped plugin", "name", pluginName)
	return nil
}

// GetPlugin returns information about a loaded plugin
func (m *Manager) GetPlugin(pluginName string) (*LoadedPlugin, bool) {
	return m.registry.GetPlugin(pluginName)
}

// GetAllPlugins returns information about all loaded plugins
func (m *Manager) GetAllPlugins() map[string]*LoadedPlugin {
	return m.registry.GetAllPlugins()
}

// GetActivePlugins returns only the active plugins
func (m *Manager) GetActivePlugins() map[string]*LoadedPlugin {
	return m.registry.GetActivePlugins()
}

// GetEventChannels returns all active event channels
func (m *Manager) GetEventChannels() map[string]<-chan Event {
	return m.channelManager.GetAllChannels()
}

// UnloadPlugin unloads a specific plugin
func (m *Manager) UnloadPlugin(pluginName string) error {
	loadedPlugin, exists := m.registry.GetPlugin(pluginName)
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	m.logger.Info("Unloading plugin", "name", pluginName)

	// Stop the plugin if it's active
	if loadedPlugin.Active {
		if err := loadedPlugin.EventSource.Stop(); err != nil {
			m.logger.Error(err, "Error stopping plugin during unload", "name", pluginName)
		}
	}

	// Remove from registry and channel manager
	m.registry.UnregisterPlugin(pluginName)
	m.channelManager.UnregisterChannel(pluginName)

	m.logger.Info("Successfully unloaded plugin", "name", pluginName)
	return nil
}

// Shutdown gracefully shuts down all plugins
func (m *Manager) Shutdown() error {
	m.logger.Info("Shutting down plugin manager")

	// Cancel context to signal all plugins to stop
	m.cancel()

	// Stop all active plugins
	activePlugins := m.registry.GetActivePlugins()
	for name, loadedPlugin := range activePlugins {
		m.logger.Info("Stopping plugin during shutdown", "name", name)
		if err := loadedPlugin.EventSource.Stop(); err != nil {
			m.logger.Error(err, "Error stopping plugin during shutdown", "name", name)
		}
	}

	// Clear registry and channels - we'll recreate new instances
	m.registry = NewPluginRegistry()
	m.channelManager = NewEventChannelManager()

	m.logger.Info("Plugin manager shutdown complete")
	return nil
}

// ReloadPlugin reloads a specific plugin
func (m *Manager) ReloadPlugin(pluginName string) error {
	loadedPlugin, exists := m.registry.GetPlugin(pluginName)
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	pluginPath := loadedPlugin.Metadata.Path
	m.logger.Info("Reloading plugin", "name", pluginName, "path", pluginPath)

	// Stop and unload the current plugin
	if loadedPlugin.Active {
		if err := loadedPlugin.EventSource.Stop(); err != nil {
			m.logger.Error(err, "Error stopping plugin during reload", "name", pluginName)
		}
	}

	m.registry.UnregisterPlugin(pluginName)
	m.channelManager.UnregisterChannel(pluginName)

	// Reload the plugin
	if err := m.loadPluginFromPath(pluginPath); err != nil {
		return fmt.Errorf("failed to reload plugin %s: %w", pluginName, err)
	}

	m.logger.Info("Successfully reloaded plugin", "name", pluginName)
	return nil
}

// GetPluginByEventType returns plugins that support a specific event type
func (m *Manager) GetPluginsByEventType(eventType string) []*LoadedPlugin {
	var result []*LoadedPlugin
	allPlugins := m.registry.GetAllPlugins()
	for _, plugin := range allPlugins {
		for _, supportedType := range plugin.Metadata.EventTypes {
			if supportedType == eventType {
				result = append(result, plugin)
				break
			}
		}
	}
	return result
}

// RegisterBuiltinPlugin registers a built-in plugin (not loaded from .so file)
func (m *Manager) RegisterBuiltinPlugin(name string, loadedPlugin *LoadedPlugin) error {
	m.logger.Info("Registering built-in plugin", "name", name)

	// Validate the plugin
	if err := m.validatePlugin(loadedPlugin.Metadata, loadedPlugin.EventSource); err != nil {
		return fmt.Errorf("built-in plugin validation failed for %s: %w", name, err)
	}

	// Store the loaded plugin
	if err := m.registry.RegisterPlugin(name, loadedPlugin); err != nil {
		return fmt.Errorf("failed to register built-in plugin %s: %w", name, err)
	}

	m.logger.Info("Successfully registered built-in plugin",
		"name", loadedPlugin.Metadata.Name,
		"version", loadedPlugin.Metadata.Version,
		"eventTypes", loadedPlugin.Metadata.EventTypes)

	return nil
}

// ValidatePluginPath validates that a plugin path is valid
func (m *Manager) ValidatePluginPath(pluginPath string) error {
	// Check file extension
	ext := filepath.Ext(pluginPath)
	if ext != ".so" {
		return fmt.Errorf("plugin file must have .so extension, got %s", ext)
	}

	// Try to open the plugin (but don't load it)
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("invalid plugin file %s: %w", pluginPath, err)
	}

	// Check for required symbol
	if _, err := p.Lookup("NewEventSource"); err != nil {
		return fmt.Errorf("plugin %s does not export required NewEventSource function: %w", pluginPath, err)
	}

	return nil
}
