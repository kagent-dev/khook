package deduplication

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/kagent-dev/khook/internal/interfaces"
	"k8s.io/apimachinery/pkg/types"
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

// eventKey generates a unique key for an event using content-based hashing
// Priority:
// 1. If UID is present, use Type:UID (explicit unique identification)
// 2. Otherwise use Type:Hash where Hash is a SHA256 of event content
//
// The hash includes: Type, ResourceName, Namespace, Reason, Message, and Metadata
// This ensures events with the same semantic content are deduplicated,
// regardless of the event source (Kubernetes, Redis, IoT, etc.)
func (m *Manager) eventKey(event interfaces.Event) string {
	// Prefer UID if available for explicit unique identification
	if event.UID != "" {
		return fmt.Sprintf("%s:%s", event.Type, event.UID)
	}
	
	// Generate content-based hash for semantic deduplication
	hash := m.hashEventContent(event)
	return fmt.Sprintf("%s:%s", event.Type, hash)
}

// hashEventContent creates a stable hash of event content for deduplication
func (m *Manager) hashEventContent(event interfaces.Event) string {
	// Create a stable representation of the event
	// We hash: Type + ResourceName + Namespace + Reason + Message + Metadata
	h := sha256.New()
	
	// Add string fields
	h.Write([]byte(event.Type))
	h.Write([]byte("|"))
	h.Write([]byte(event.ResourceName))
	h.Write([]byte("|"))
	h.Write([]byte(event.Namespace))
	h.Write([]byte("|"))
	h.Write([]byte(event.Reason))
	h.Write([]byte("|"))
	h.Write([]byte(event.Message))
	h.Write([]byte("|"))
	
	// Add metadata in a stable way (sorted keys for deterministic hash)
	if event.Metadata != nil {
		// Sort keys for stable hash
		keys := make([]string, 0, len(event.Metadata))
		for k := range event.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		
		// Add each key-value pair to hash
		for _, k := range keys {
			h.Write([]byte(k))
			h.Write([]byte("="))
			// Convert value to JSON for stable representation
			if jsonBytes, err := json.Marshal(event.Metadata[k]); err == nil {
				h.Write(jsonBytes)
			}
			h.Write([]byte(";"))
		}
	}
	
	// Return first 16 characters of hex hash (sufficient for uniqueness)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ShouldProcessEvent determines if an event should be processed based on deduplication logic
func (m *Manager) ShouldProcessEvent(hookRef types.NamespacedName, event interfaces.Event) bool {
	logValues := []interface{}{"hook", hookRef.String(), "eventType", event.Type}
	if event.ResourceName != "" {
		logValues = append(logValues, "resource", event.ResourceName)
	}
	if event.UID != "" {
		logValues = append(logValues, "uid", event.UID)
	}
	logger := log.Log.WithName("dedup").WithValues(logValues...)
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hookEventMap, exists := m.hookEvents[hookRef.String()]
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
func (m *Manager) RecordEvent(hookRef types.NamespacedName, event interfaces.Event) error {
	logValues := []interface{}{"hook", hookRef.String(), "eventType", event.Type}
	if event.ResourceName != "" {
		logValues = append(logValues, "resource", event.ResourceName)
	}
	if event.UID != "" {
		logValues = append(logValues, "uid", event.UID)
	}
	logger := log.Log.WithName("dedup").WithValues(logValues...)
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Initialize hook event map if it doesn't exist
	if m.hookEvents[hookRef.String()] == nil {
		m.hookEvents[hookRef.String()] = make(map[string]*interfaces.ActiveEvent)
	}

	key := m.eventKey(event)
	now := time.Now()

	// Check if event already exists
	if existingEvent, exists := m.hookEvents[hookRef.String()][key]; exists {
		// Update existing event
		existingEvent.LastSeen = now
		existingEvent.Status = StatusFiring
		logger.V(1).Info("Updated existing active event", "lastSeen", existingEvent.LastSeen)
	} else {
		// Create new event record
		m.hookEvents[hookRef.String()][key] = &interfaces.ActiveEvent{
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
func (m *Manager) MarkNotified(hookRef types.NamespacedName, event interfaces.Event) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.hookEvents[hookRef.String()] == nil {
		m.hookEvents[hookRef.String()] = make(map[string]*interfaces.ActiveEvent)
	}
	key := m.eventKey(event)
	now := time.Now()
	if ae, ok := m.hookEvents[hookRef.String()][key]; ok {
		ae.LastNotifiedAt = &now
		if ae.NotifiedAt == nil {
			ae.NotifiedAt = &now
		}
	} else {
		m.hookEvents[hookRef.String()][key] = &interfaces.ActiveEvent{
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
func (m *Manager) CleanupExpiredEvents(hookRef types.NamespacedName) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	hookEventMap, exists := m.hookEvents[hookRef.String()]
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
		delete(m.hookEvents, hookRef.String())
	}

	return nil
}

// GetActiveEvents returns all active events for a specific hook
func (m *Manager) GetActiveEvents(hookRef types.NamespacedName) []interfaces.ActiveEvent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hookEventMap, exists := m.hookEvents[hookRef.String()]
	if !exists {
		return []interfaces.ActiveEvent{}
	}

	activeEvents := make([]interfaces.ActiveEvent, 0, len(hookEventMap))

	for _, activeEvent := range hookEventMap {
		// Create a copy to avoid returning pointers to internal data
		eventCopy := *activeEvent

		activeEvents = append(activeEvents, eventCopy)
	}

	return activeEvents
}

// GetActiveEventsWithStatus returns all active events with their current status
// This method handles status calculation without race conditions
func (m *Manager) GetActiveEventsWithStatus(hookRef types.NamespacedName) []interfaces.ActiveEvent {
	activeEvents := m.GetActiveEvents(hookRef)

	now := time.Now()
	for i := range activeEvents {
		// Check if event should be marked as resolved
		if now.Sub(activeEvents[i].FirstSeen) > EventTimeoutDuration {
			activeEvents[i].Status = StatusResolved
		}
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
