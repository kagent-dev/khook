package plugin

import (
	"sync"
)

// PluginRegistry manages plugin registration and retrieval
type PluginRegistry interface {
	RegisterPlugin(name string, plugin *LoadedPlugin) error
	UnregisterPlugin(name string) error
	GetPlugin(name string) (*LoadedPlugin, bool)
	GetActivePlugins() map[string]*LoadedPlugin
	GetAllPlugins() map[string]*LoadedPlugin
	ListPluginNames() []string
}

// DefaultPluginRegistry is the default implementation of PluginRegistry
type DefaultPluginRegistry struct {
	plugins map[string]*LoadedPlugin
	mu      sync.RWMutex
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() PluginRegistry {
	return &DefaultPluginRegistry{
		plugins: make(map[string]*LoadedPlugin),
	}
}

// RegisterPlugin registers a plugin in the registry
func (r *DefaultPluginRegistry) RegisterPlugin(name string, plugin *LoadedPlugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.plugins[name] = plugin
	return nil
}

// UnregisterPlugin removes a plugin from the registry
func (r *DefaultPluginRegistry) UnregisterPlugin(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.plugins, name)
	return nil
}

// GetPlugin retrieves a plugin by name
func (r *DefaultPluginRegistry) GetPlugin(name string) (*LoadedPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, exists := r.plugins[name]
	return plugin, exists
}

// GetActivePlugins returns all active plugins
func (r *DefaultPluginRegistry) GetActivePlugins() map[string]*LoadedPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	activePlugins := make(map[string]*LoadedPlugin)
	for name, plugin := range r.plugins {
		if plugin.Active {
			activePlugins[name] = plugin
		}
	}
	return activePlugins
}

// GetAllPlugins returns all registered plugins
func (r *DefaultPluginRegistry) GetAllPlugins() map[string]*LoadedPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	allPlugins := make(map[string]*LoadedPlugin)
	for name, plugin := range r.plugins {
		allPlugins[name] = plugin
	}
	return allPlugins
}

// ListPluginNames returns the names of all registered plugins
func (r *DefaultPluginRegistry) ListPluginNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}
