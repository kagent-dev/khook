package deduplication

import (
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestNewManager(t *testing.T) {
	manager := NewManager()
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.hookEvents)
	assert.Equal(t, 0, len(manager.hookEvents))
}

func TestEventKey(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	key := manager.eventKey(event)
	expected := "pod-restart:default:test-pod"
	assert.Equal(t, expected, key)
}

func TestShouldProcessEvent_NewEvent(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// New event should be processed
	shouldProcess := manager.ShouldProcessEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	assert.True(t, shouldProcess)
}

func TestShouldProcessEvent_DuplicateWithinTimeout(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record the event first
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	// Same event within timeout should not be processed
	shouldProcess := manager.ShouldProcessEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	assert.False(t, shouldProcess)
}

func TestShouldProcessEvent_ExpiredEvent(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record the event
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	// Manually set the event to be older than timeout
	hookEventMap, exists := manager.hookEvents[types.NamespacedName{Name: "test-hook", Namespace: "default"}.String()]
	require.True(t, exists)
	key := manager.eventKey(event)
	hookEventMap[key].FirstSeen = time.Now().Add(-EventTimeoutDuration - time.Minute)

	// Expired event should be processed again
	shouldProcess := manager.ShouldProcessEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	assert.True(t, shouldProcess)
}

func TestRecordEvent_NewEvent(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	// Verify event was recorded
	activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	assert.Equal(t, 1, len(activeEvents))
	assert.Equal(t, "pod-restart", activeEvents[0].EventType)
	assert.Equal(t, "test-pod", activeEvents[0].ResourceName)
	assert.Equal(t, StatusFiring, activeEvents[0].Status)
}

func TestRecordEvent_UpdateExistingEvent(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record event first time
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	firstSeen := activeEvents[0].FirstSeen

	// Wait a bit and record same event again
	time.Sleep(10 * time.Millisecond)
	err = manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	// Verify event was updated, not duplicated
	activeEvents = manager.GetActiveEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	assert.Equal(t, 1, len(activeEvents))
	assert.Equal(t, firstSeen, activeEvents[0].FirstSeen)     // FirstSeen should not change
	assert.True(t, activeEvents[0].LastSeen.After(firstSeen)) // LastSeen should be updated
}

func TestRecordEvent_MultipleHooks(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record same event for different hooks
	err := manager.RecordEvent(types.NamespacedName{Name: "hook1", Namespace: "default"}, event)
	require.NoError(t, err)

	err = manager.RecordEvent(types.NamespacedName{Name: "hook2", Namespace: "default"}, event)
	require.NoError(t, err)

	// Verify both hooks have the event
	activeEvents1 := manager.GetActiveEvents(types.NamespacedName{Name: "hook1", Namespace: "default"})
	activeEvents2 := manager.GetActiveEvents(types.NamespacedName{Name: "hook2", Namespace: "default"})

	assert.Equal(t, 1, len(activeEvents1))
	assert.Equal(t, 1, len(activeEvents2))
	assert.Equal(t, 2, manager.GetEventCount())
}

func TestCleanupExpiredEvents(t *testing.T) {
	manager := NewManager()

	// Create events with different ages
	recentEvent := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "recent-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	oldEvent := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "old-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record both events
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, recentEvent)
	require.NoError(t, err)

	err = manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, oldEvent)
	require.NoError(t, err)

	// Manually age the old event
	hookEventMap, exists := manager.hookEvents[types.NamespacedName{Name: "test-hook", Namespace: "default"}.String()]
	require.True(t, exists)
	oldKey := manager.eventKey(oldEvent)
	hookEventMap[oldKey].FirstSeen = time.Now().Add(-EventTimeoutDuration - time.Minute)

	// Cleanup expired events
	err = manager.CleanupExpiredEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	require.NoError(t, err)

	// Verify only recent event remains
	activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	assert.Equal(t, 1, len(activeEvents))
	assert.Equal(t, "recent-pod", activeEvents[0].ResourceName)
}

func TestCleanupExpiredEvents_EmptyHook(t *testing.T) {
	manager := NewManager()

	// Cleanup non-existent hook should not error
	err := manager.CleanupExpiredEvents(types.NamespacedName{Name: "non-existent-hook", Namespace: "default"})
	assert.NoError(t, err)
}

func TestCleanupExpiredEvents_AllEventsExpired(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record event
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	require.NoError(t, err)

	// Age the event
	hookEventMap, exists := manager.hookEvents[types.NamespacedName{Name: "test-hook", Namespace: "default"}.String()]
	require.True(t, exists)
	key := manager.eventKey(event)
	hookEventMap[key].FirstSeen = time.Now().Add(-EventTimeoutDuration - time.Minute)

	// Cleanup expired events
	err = manager.CleanupExpiredEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	require.NoError(t, err)

	// Verify hook map is cleaned up
	_, exists = manager.hookEvents[types.NamespacedName{Name: "test-hook", Namespace: "default"}.String()]
	assert.False(t, exists)

	activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	assert.Equal(t, 0, len(activeEvents))
}

func TestGetActiveEvents_EmptyHook(t *testing.T) {
	manager := NewManager()

	activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: "non-existent-hook", Namespace: "default"})
	assert.Equal(t, 0, len(activeEvents))
	assert.NotNil(t, activeEvents) // Should return empty slice, not nil
}

func TestGetActiveEvents_WithExpiredEvents(t *testing.T) {
	manager := NewManager()

	// Create recent and old events
	recentEvent := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "recent-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	oldEvent := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "old-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record both events
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, recentEvent)
	require.NoError(t, err)

	err = manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, oldEvent)
	require.NoError(t, err)

	// Age the old event
	hookEventMap, exists := manager.hookEvents[types.NamespacedName{Name: "test-hook", Namespace: "default"}.String()]
	require.True(t, exists)
	oldKey := manager.eventKey(oldEvent)
	hookEventMap[oldKey].FirstSeen = time.Now().Add(-EventTimeoutDuration - time.Minute)

	// Get active events with status (should mark old event as resolved)
	activeEvents := manager.GetActiveEventsWithStatus(types.NamespacedName{Name: "test-hook", Namespace: "default"})
	assert.Equal(t, 2, len(activeEvents))

	// Find the events and check their status
	var recentEventStatus, oldEventStatus string
	for _, event := range activeEvents {
		switch event.ResourceName {
		case "recent-pod":
			recentEventStatus = event.Status
		case "old-pod":
			oldEventStatus = event.Status
		}
	}

	assert.Equal(t, StatusFiring, recentEventStatus)
	assert.Equal(t, StatusResolved, oldEventStatus)
}

func TestGetAllHookNames(t *testing.T) {
	manager := NewManager()

	event1 := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "pod1",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	event2 := interfaces.Event{
		Type:         "pod-pending",
		ResourceName: "pod2",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record events for different hooks
	err := manager.RecordEvent(types.NamespacedName{Name: "hook1", Namespace: "default"}, event1)
	require.NoError(t, err)

	err = manager.RecordEvent(types.NamespacedName{Name: "hook2", Namespace: "default"}, event2)
	require.NoError(t, err)

	hookNames := manager.GetAllHookNames()
	assert.Equal(t, 2, len(hookNames))
	assert.Contains(t, hookNames, "default/hook1")
	assert.Contains(t, hookNames, "default/hook2")
}

func TestGetEventCount(t *testing.T) {
	manager := NewManager()

	assert.Equal(t, 0, manager.GetEventCount())

	event1 := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "pod1",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	event2 := interfaces.Event{
		Type:         "pod-pending",
		ResourceName: "pod2",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record events
	err := manager.RecordEvent(types.NamespacedName{Name: "hook1", Namespace: "default"}, event1)
	require.NoError(t, err)
	assert.Equal(t, 1, manager.GetEventCount())

	err = manager.RecordEvent(types.NamespacedName{Name: "hook1", Namespace: "default"}, event2)
	require.NoError(t, err)
	assert.Equal(t, 2, manager.GetEventCount())

	err = manager.RecordEvent(types.NamespacedName{Name: "hook2", Namespace: "default"}, event1)
	require.NoError(t, err)
	assert.Equal(t, 3, manager.GetEventCount())
}

func TestConcurrentAccess(t *testing.T) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Test concurrent access
	done := make(chan bool, 10)

	// Start multiple goroutines
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			hookName := fmt.Sprintf("hook-%d", id)

			// Record event
			err := manager.RecordEvent(types.NamespacedName{Name: hookName, Namespace: "default"}, event)
			assert.NoError(t, err)

			// Check if should process
			shouldProcess := manager.ShouldProcessEvent(types.NamespacedName{Name: hookName, Namespace: "default"}, event)
			assert.False(t, shouldProcess) // Should be false since we just recorded it

			// Get active events
			activeEvents := manager.GetActiveEvents(types.NamespacedName{Name: hookName, Namespace: "default"})
			assert.Equal(t, 1, len(activeEvents))

			// Cleanup
			err = manager.CleanupExpiredEvents(types.NamespacedName{Name: hookName, Namespace: "default"})
			assert.NoError(t, err)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state
	assert.Equal(t, 10, manager.GetEventCount())
}

// Benchmark tests
func BenchmarkRecordEvent(b *testing.B) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShouldProcessEvent(b *testing.B) {
	manager := NewManager()

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	// Record event first
	err := manager.RecordEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.ShouldProcessEvent(types.NamespacedName{Name: "test-hook", Namespace: "default"}, event)
	}
}
