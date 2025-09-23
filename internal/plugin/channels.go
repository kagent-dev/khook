package plugin

import (
	"sync"
)

// EventChannelManager manages event channels for plugins
type EventChannelManager interface {
	RegisterChannel(pluginName string, ch <-chan Event)
	UnregisterChannel(pluginName string)
	GetChannel(pluginName string) (<-chan Event, bool)
	GetAllChannels() map[string]<-chan Event
	ListChannelNames() []string
}

// DefaultEventChannelManager is the default implementation of EventChannelManager
type DefaultEventChannelManager struct {
	channels map[string]<-chan Event
	mu       sync.RWMutex
}

// NewEventChannelManager creates a new event channel manager
func NewEventChannelManager() EventChannelManager {
	return &DefaultEventChannelManager{
		channels: make(map[string]<-chan Event),
	}
}

// RegisterChannel registers an event channel for a plugin
func (m *DefaultEventChannelManager) RegisterChannel(pluginName string, ch <-chan Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.channels[pluginName] = ch
}

// UnregisterChannel removes an event channel for a plugin
func (m *DefaultEventChannelManager) UnregisterChannel(pluginName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.channels, pluginName)
}

// GetChannel retrieves an event channel by plugin name
func (m *DefaultEventChannelManager) GetChannel(pluginName string) (<-chan Event, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ch, exists := m.channels[pluginName]
	return ch, exists
}

// GetAllChannels returns all registered event channels
func (m *DefaultEventChannelManager) GetAllChannels() map[string]<-chan Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allChannels := make(map[string]<-chan Event)
	for name, ch := range m.channels {
		allChannels[name] = ch
	}
	return allChannels
}

// ListChannelNames returns the names of all registered channels
func (m *DefaultEventChannelManager) ListChannelNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}
