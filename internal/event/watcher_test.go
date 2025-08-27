package event

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kagent/hook-controller/internal/interfaces"
)

func TestMapEventType(t *testing.T) {
	watcher := &Watcher{}

	tests := []struct {
		name     string
		event    *eventsv1.Event
		expected string
	}{
		{
			name: "pod restart - backoff reason",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "BackOff",
				Note:      "Back-off restarting failed container test in pod test_default",
				Type:      "Warning",
			},
			expected: "pod-restart",
		},
		{
			name: "oom kill",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "OOMKilling",
				Note:      "Memory cgroup out of memory: Killed process",
				Type:      "Warning",
			},
			expected: "oom-kill",
		},
		{
			name: "pod pending",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "FailedScheduling",
				Note:      "0/1 nodes are available: 1 Insufficient memory",
				Type:      "Warning",
			},
			expected: "pod-pending",
		},
		{
			name: "probe failed",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "Unhealthy",
				Note:      "Liveness probe failed: HTTP probe failed",
				Type:      "Warning",
			},
			expected: "probe-failed",
		},
		{
			name: "unrelated event",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Service"},
				Reason:    "Created",
				Note:      "Service created",
				Type:      "Normal",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := watcher.mapEventType(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapKubernetesEvent(t *testing.T) {
	watcher := &Watcher{}

	eventTime := metav1.NewMicroTime(time.Now())
	k8sEvent := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "test-uid",
			Namespace: "test-namespace",
		},
		Regarding: corev1.ObjectReference{
			Kind:       "Pod",
			Name:       "test-pod",
			APIVersion: "v1",
		},
		Reason:              "BackOff",
		Note:                "Back-off restarting failed container",
		Type:                "Warning",
		EventTime:           eventTime,
		DeprecatedCount:     3,
		ReportingController: "kubelet",
		ReportingInstance:   "node1",
	}

	result := watcher.mapKubernetesEvent(k8sEvent)
	require.NotNil(t, result)

	assert.Equal(t, "pod-restart", result.Type)
	assert.Equal(t, "test-pod", result.ResourceName)
	assert.Equal(t, "test-namespace", result.Namespace)
	assert.Equal(t, "BackOff", result.Reason)
	assert.Equal(t, "Back-off restarting failed container", result.Message)
	assert.Equal(t, "test-uid", result.UID)
	assert.Equal(t, "Pod", result.Metadata["kind"])
	assert.Equal(t, "v1", result.Metadata["apiVersion"])
	assert.Equal(t, "3", result.Metadata["count"])
	assert.Equal(t, "Warning", result.Metadata["type"])
	assert.Equal(t, "kubelet", result.Metadata["reportingController"])
	assert.Equal(t, "node1", result.Metadata["reportingInstance"])
}

func TestFilterEvent(t *testing.T) {
	watcher := &Watcher{}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "test-namespace",
		Timestamp:    time.Now(),
	}

	// For now, just test that FilterEvent returns empty matches
	// This will be expanded when we implement the actual filtering logic
	hooks := []interface{}{}
	matches := watcher.FilterEvent(event, hooks)

	// Should return empty matches for now
	assert.Len(t, matches, 0)
}

func TestNewWatcher(t *testing.T) {
	client := fake.NewSimpleClientset()
	namespace := "test-namespace"

	watcher := NewWatcher(client, namespace)
	require.NotNil(t, watcher)

	// Type assertion to access internal fields
	w, ok := watcher.(*Watcher)
	require.True(t, ok)
	assert.Equal(t, client, w.client)
	assert.Equal(t, namespace, w.namespace)
}

func TestWatcherStartStop(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := NewWatcher(client, "test-namespace")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Start the watcher
	err := watcher.Start(ctx)
	assert.NoError(t, err)

	// Stop the watcher
	err = watcher.Stop()
	assert.NoError(t, err)
}
