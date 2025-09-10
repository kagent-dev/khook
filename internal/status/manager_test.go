package status

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

func TestNewManager(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)

	manager := NewManager(fakeClient, fakeRecorder)

	assert.NotNil(t, manager)
	assert.Equal(t, fakeClient, manager.client)
	assert.Equal(t, fakeRecorder, manager.recorder)
}

func TestUpdateHookStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	tests := []struct {
		name         string
		hook         *v1alpha2.Hook
		activeEvents []interfaces.ActiveEvent
		expectError  bool
	}{
		{
			name: "successful status update with active events",
			hook: &v1alpha2.Hook{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hook",
					Namespace: "default",
				},
				Spec: v1alpha2.HookSpec{
					EventConfigurations: []v1alpha2.EventConfiguration{
						{
							EventType: "pod-restart",
							AgentRef:  v1alpha2.ObjectReference{Name: "test-agent"},
							Prompt:    "test prompt",
						},
					},
				},
			},
			activeEvents: []interfaces.ActiveEvent{
				{
					EventType:    "pod-restart",
					ResourceName: "test-pod",
					FirstSeen:    time.Now().Add(-5 * time.Minute),
					LastSeen:     time.Now(),
					Status:       "firing",
				},
			},
			expectError: false,
		},
		{
			name: "successful status update with no active events",
			hook: &v1alpha2.Hook{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hook-empty",
					Namespace: "default",
				},
				Spec: v1alpha2.HookSpec{
					EventConfigurations: []v1alpha2.EventConfiguration{
						{
							EventType: "pod-pending",
							AgentRef:  v1alpha2.ObjectReference{Name: "test-agent"},
							Prompt:    "test prompt",
						},
					},
				},
			},
			activeEvents: []interfaces.ActiveEvent{},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.hook).WithStatusSubresource(&v1alpha2.Hook{}).Build()
			fakeRecorder := record.NewFakeRecorder(100)
			manager := NewManager(fakeClient, fakeRecorder)

			ctx := context.Background()
			err := manager.UpdateHookStatus(ctx, tt.hook, tt.activeEvents)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the status was updated
				updatedHook := &v1alpha2.Hook{}
				key := client.ObjectKey{Name: tt.hook.Name, Namespace: tt.hook.Namespace}
				require.NoError(t, fakeClient.Get(ctx, key, updatedHook))

				assert.Len(t, updatedHook.Status.ActiveEvents, len(tt.activeEvents))
				assert.False(t, updatedHook.Status.LastUpdated.IsZero())

				// Verify active events match
				if len(tt.activeEvents) > 0 {
					for i, expectedEvent := range tt.activeEvents {
						actualEvent := updatedHook.Status.ActiveEvents[i]
						assert.Equal(t, expectedEvent.EventType, actualEvent.EventType)
						assert.Equal(t, expectedEvent.ResourceName, actualEvent.ResourceName)
						assert.Equal(t, expectedEvent.Status, actualEvent.Status)
					}
				}
			}
		})
	}
}

func TestRecordEventFiring(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Timestamp:    time.Now(),
		Namespace:    "default",
		Reason:       "Unhealthy",
		Message:      "Pod restarted due to health check failure",
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordEventFiring(ctx, hook, event, types.NamespacedName{Name: "test-agent", Namespace: "default"})

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "EventFiring")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "test-agent")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestRecordEventResolved(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordEventResolved(ctx, hook, "pod-restart", "test-pod")

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "EventResolved")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "timeout")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestRecordError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Timestamp:    time.Now(),
		Namespace:    "default",
	}

	testError := errors.New("test processing error")

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordError(ctx, hook, event, testError, types.NamespacedName{Name: "test-agent", Namespace: "default"})

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "EventProcessingError")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "test-agent")
		assert.Contains(t, recordedEvent, "test processing error")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestRecordAgentCallSuccess(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Timestamp:    time.Now(),
		Namespace:    "default",
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordAgentCallSuccess(ctx, hook, event, types.NamespacedName{Name: "test-agent", Namespace: "default"}, "req-123")

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "AgentCallSuccess")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "test-agent")
		assert.Contains(t, recordedEvent, "req-123")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestRecordAgentCallFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Timestamp:    time.Now(),
		Namespace:    "default",
	}

	testError := errors.New("agent call failed")

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordAgentCallFailure(ctx, hook, event, types.NamespacedName{Name: "test-agent", Namespace: "default"}, testError)

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "AgentCallFailure")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "test-agent")
		assert.Contains(t, recordedEvent, "agent call failed")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestRecordDuplicateEvent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
	}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Timestamp:    time.Now(),
		Namespace:    "default",
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	err := manager.RecordDuplicateEvent(ctx, hook, event)

	assert.NoError(t, err)

	// Verify event was recorded
	select {
	case recordedEvent := <-fakeRecorder.Events:
		assert.Contains(t, recordedEvent, "DuplicateEventIgnored")
		assert.Contains(t, recordedEvent, "pod-restart")
		assert.Contains(t, recordedEvent, "test-pod")
		assert.Contains(t, recordedEvent, "deduplication window")
	case <-time.After(time.Second):
		t.Fatal("Expected event was not recorded")
	}
}

func TestGetHookStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	hook := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
		Status: v1alpha2.HookStatus{
			ActiveEvents: []v1alpha2.ActiveEventStatus{
				{
					EventType:    "pod-restart",
					ResourceName: "test-pod",
					FirstSeen:    metav1.NewTime(time.Now().Add(-5 * time.Minute)),
					LastSeen:     metav1.NewTime(time.Now()),
					Status:       "firing",
				},
			},
			LastUpdated: metav1.NewTime(time.Now()),
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(hook).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	status, err := manager.GetHookStatus(ctx, "test-hook", "default")

	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.Len(t, status.ActiveEvents, 1)
	assert.Equal(t, "pod-restart", status.ActiveEvents[0].EventType)
	assert.Equal(t, "test-pod", status.ActiveEvents[0].ResourceName)
	assert.Equal(t, "firing", status.ActiveEvents[0].Status)
}

func TestGetHookStatusNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	status, err := manager.GetHookStatus(ctx, "nonexistent-hook", "default")

	assert.Error(t, err)
	assert.Nil(t, status)
	assert.Contains(t, err.Error(), "failed to get hook")
}

func TestLogControllerStartup(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()
	config := map[string]interface{}{
		"logLevel": "info",
		"port":     8080,
	}

	// This should not panic or error
	manager.LogControllerStartup(ctx, "v1.0.0", config)
}

func TestLogControllerShutdown(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	manager := NewManager(fakeClient, fakeRecorder)

	ctx := context.Background()

	// This should not panic or error
	manager.LogControllerShutdown(ctx, "graceful shutdown")
}
