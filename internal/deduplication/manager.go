package deduplication

import (
	"fmt"
	"sync"
	"time"

	"github.com/kagent/hook-controller/internal/interfaces"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// EventTimeoutDuration is the duration after which events are considered resolved
	EventTimeoutDuration = 10 * time.Minute
	// NotificationSuppressionDuration is the window to suppress re-sending after success
	NotificationSuppressionDuration = 10 * time.Minute

	// StatusFiring indicates an event is currently active
	StatusFiring = "firing"

	// StatusResolved indicates an event has been resolved (timed out)
	StatusResolved = "resolved"
)

// Manager implements the DeduplicationManager interface with in-memory storage
type Manager struct {
	// hookEvents maps hook names to their active events
	// hookName -> eventKey -> ActiveEvent
	hookEvents map[string]map[string]*interfaces.ActiveEvent
	mutex      sync.RWMutex
}

// NewManager creates a new DeduplicationManager instance
func NewManager() *Manager {
	return &Manager{
		hookEvents: make(map[string]map[string]*interfaces.ActiveEvent),
	}
}

// eventKey generates a unique key for an event based on type and resource
func (m *Manager) eventKey(event interfaces.Event) string {
	return fmt.Sprintf("%s:%s:%s", event.Type, event.Namespace, event.ResourceName)
}

// ShouldProcessEvent determines if an event should be processed based on deduplication logic
func (m *Manager) ShouldProcessEvent(hookName string, event interfaces.Event) bool {
	logger := log.Log.WithName("dedup").WithValues("hook", hookName, "eventType", event.Type, "resource", event.ResourceName)
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hookEventMap, exists := m.hookEvents[hookName]
	if !exists {
		// No events for this hook, should process
		logger.V(1).Info("No existing events for hook; will process")
		return true
	}

	key := m.eventKey(event)
	activeEvent, exists := hookEventMap[key]
	if !exists {
		// Event doesn't exist, should process
		logger.V(1).Info("First occurrence of event; will process")
		return true
	}

	// Suppress if we recently notified and within suppression window
	if activeEvent.LastNotifiedAt != nil && time.Since(*activeEvent.LastNotifiedAt) < NotificationSuppressionDuration {
		logger.V(1).Info("Within notification suppression window; will ignore",
			"lastNotifiedAt", *activeEvent.LastNotifiedAt)
		return false
	}

	// Check if event has expired (more than 10 minutes old)
	if time.Since(activeEvent.FirstSeen) > EventTimeoutDuration {
		// Event has expired, should process as new event
		logger.V(1).Info("Event expired; will process as new", "firstSeen", activeEvent.FirstSeen)
		return true
	}

	// Event is still active within timeout window, should not process
	logger.V(2).Info("Duplicate within timeout; will ignore", "firstSeen", activeEvent.FirstSeen)
	return false
}

// RecordEvent records an event in the deduplication storage
func (m *Manager) RecordEvent(hookName string, event interfaces.Event) error {
	logger := log.Log.WithName("dedup").WithValues("hook", hookName, "eventType", event.Type, "resource", event.ResourceName)
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Initialize hook event map if it doesn't exist
	if m.hookEvents[hookName] == nil {
		m.hookEvents[hookName] = make(map[string]*interfaces.ActiveEvent)
	}

	key := m.eventKey(event)
	now := time.Now()

	// Check if event already exists
	if existingEvent, exists := m.hookEvents[hookName][key]; exists {
		// Update existing event
		existingEvent.LastSeen = now
		existingEvent.Status = StatusFiring
		logger.V(1).Info("Updated existing active event", "lastSeen", existingEvent.LastSeen)
	} else {
		// Create new event record
		m.hookEvents[hookName][key] = &interfaces.ActiveEvent{
			EventType:    event.Type,
			ResourceName: event.ResourceName,
			FirstSeen:    now,
			LastSeen:     now,
			Status:       StatusFiring,
		}
		logger.Info("Recorded new active event", "firstSeen", now)
	}

	return nil
}

// MarkNotified marks that we successfully notified the agent for this event now
func (m *Manager) MarkNotified(hookName string, event interfaces.Event) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.hookEvents[hookName] == nil {
		m.hookEvents[hookName] = make(map[string]*interfaces.ActiveEvent)
	}
	key := m.eventKey(event)
	now := time.Now()
	if ae, ok := m.hookEvents[hookName][key]; ok {
		ae.LastNotifiedAt = &now
		if ae.NotifiedAt == nil {
			ae.NotifiedAt = &now
		}
	} else {
		m.hookEvents[hookName][key] = &interfaces.ActiveEvent{
			EventType:      event.Type,
			ResourceName:   event.ResourceName,
			FirstSeen:      now,
			LastSeen:       now,
			Status:         StatusFiring,
			NotifiedAt:     &now,
			LastNotifiedAt: &now,
		}
	}
}

// CleanupExpiredEvents removes events that have exceeded the timeout duration
func (m *Manager) CleanupExpiredEvents(hookName string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	hookEventMap, exists := m.hookEvents[hookName]
	if !exists {
		// No events for this hook
		return nil
	}

	now := time.Now()
	expiredKeys := make([]string, 0)

	// Find expired events
	for key, activeEvent := range hookEventMap {
		if now.Sub(activeEvent.FirstSeen) > EventTimeoutDuration {
			// Mark as resolved before removal
			activeEvent.Status = StatusResolved
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Remove expired events
	for _, key := range expiredKeys {
		delete(hookEventMap, key)
	}

	// Clean up empty hook map
	if len(hookEventMap) == 0 {
		delete(m.hookEvents, hookName)
	}

	return nil
}

// GetActiveEvents returns all active events for a specific hook
func (m *Manager) GetActiveEvents(hookName string) []interfaces.ActiveEvent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hookEventMap, exists := m.hookEvents[hookName]
	if !exists {
		return []interfaces.ActiveEvent{}
	}

	// Clean up expired events first (mark as resolved)
	now := time.Now()
	activeEvents := make([]interfaces.ActiveEvent, 0, len(hookEventMap))

	for _, activeEvent := range hookEventMap {
		// Create a copy to avoid returning pointers to internal data
		eventCopy := *activeEvent

		// Check if event should be marked as resolved
		if now.Sub(activeEvent.FirstSeen) > EventTimeoutDuration {
			eventCopy.Status = StatusResolved
		}

		activeEvents = append(activeEvents, eventCopy)
	}

	return activeEvents
}

// GetAllHookNames returns all hook names that have active events
func (m *Manager) GetAllHookNames() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hookNames := make([]string, 0, len(m.hookEvents))
	for hookName := range m.hookEvents {
		hookNames = append(hookNames, hookName)
	}

	return hookNames
}

// GetEventCount returns the total number of active events across all hooks
func (m *Manager) GetEventCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	count := 0
	for _, hookEventMap := range m.hookEvents {
		count += len(hookEventMap)
	}

	return count
}
