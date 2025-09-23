package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewKubernetesEventSource(t *testing.T) {
	source := NewKubernetesEventSource()
	assert.NotNil(t, source)

	k8sSource, ok := source.(*KubernetesEventSource)
	assert.True(t, ok)
	assert.Equal(t, "kubernetes", k8sSource.Name())
	assert.Equal(t, "1.0.0", k8sSource.Version())
}

func TestKubernetesEventSourceInitialize(t *testing.T) {
	source := NewKubernetesEventSource()
	k8sSource := source.(*KubernetesEventSource)

	// Create a fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	tests := []struct {
		name      string
		config    map[string]interface{}
		shouldErr bool
	}{
		{
			name: "valid config with client and namespace",
			config: map[string]interface{}{
				"client":    fakeClient,
				"namespace": "test-namespace",
			},
			shouldErr: false,
		},
		{
			name: "valid config with client, default namespace",
			config: map[string]interface{}{
				"client": fakeClient,
			},
			shouldErr: false,
		},
		{
			name: "invalid namespace",
			config: map[string]interface{}{
				"client":    fakeClient,
				"namespace": "invalid-namespace-with-very-long-name-that-exceeds-kubernetes-limits-and-should-fail",
			},
			shouldErr: true,
		},
		{
			name: "invalid client type",
			config: map[string]interface{}{
				"client": "not-a-client",
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := k8sSource.Initialize(ctx, tt.config)

			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, k8sSource.client)
				if ns, ok := tt.config["namespace"].(string); ok {
					assert.Equal(t, ns, k8sSource.namespace)
				} else {
					assert.Equal(t, "default", k8sSource.namespace)
				}
			}
		})
	}
}

func TestKubernetesEventSourceSupportedEventTypes(t *testing.T) {
	source := NewKubernetesEventSource()
	eventTypes := source.SupportedEventTypes()

	expectedTypes := []string{
		"pod-restart",
		"oom-kill",
		"pod-pending",
		"probe-failed",
	}

	assert.ElementsMatch(t, expectedTypes, eventTypes)
}

func TestValidateNamespace(t *testing.T) {
	source := &KubernetesEventSource{
		logger: logr.Discard(),
	}

	tests := []struct {
		name      string
		namespace string
		shouldErr bool
	}{
		{
			name:      "valid namespace",
			namespace: "test-namespace",
			shouldErr: false,
		},
		{
			name:      "valid single character",
			namespace: "a",
			shouldErr: false,
		},
		{
			name:      "valid with numbers",
			namespace: "test123",
			shouldErr: false,
		},
		{
			name:      "empty namespace",
			namespace: "",
			shouldErr: true,
		},
		{
			name:      "too long namespace",
			namespace: "this-is-a-very-long-namespace-name-that-exceeds-the-kubernetes-limit-of-sixty-three-characters",
			shouldErr: true,
		},
		{
			name:      "starts with hyphen",
			namespace: "-invalid",
			shouldErr: true,
		},
		{
			name:      "ends with hyphen",
			namespace: "invalid-",
			shouldErr: true,
		},
		{
			name:      "contains invalid character",
			namespace: "test_namespace",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := source.validateNamespace(tt.namespace)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMapEventType(t *testing.T) {
	source := &KubernetesEventSource{
		logger: logr.Discard(),
	}

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
				Type:      "Warning",
				Note:      "Back-off restarting failed container",
			},
			expected: "pod-restart",
		},
		{
			name: "oom kill",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "OOMKilling",
				Type:      "Warning",
				Note:      "Memory cgroup out of memory",
			},
			expected: "oom-kill",
		},
		{
			name: "pod pending",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "FailedScheduling",
				Type:      "Warning",
				Note:      "0/1 nodes are available",
			},
			expected: "pod-pending",
		},
		{
			name: "probe failed",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "Unhealthy",
				Type:      "Warning",
				Note:      "Liveness probe failed",
			},
			expected: "probe-failed",
		},
		{
			name: "unrelated event",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Service"},
				Reason:    "SomeReason",
				Type:      "Normal",
				Note:      "Some normal event",
			},
			expected: "",
		},
		{
			name: "normal pod event",
			event: &eventsv1.Event{
				Regarding: corev1.ObjectReference{Kind: "Pod"},
				Reason:    "Started",
				Type:      "Normal",
				Note:      "Container started",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := source.mapEventType(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapKubernetesEvent(t *testing.T) {
	source := &KubernetesEventSource{
		logger: logr.Discard(),
	}

	now := time.Now()
	k8sEvent := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			UID:               "test-uid",
			CreationTimestamp: metav1.NewTime(now),
			Namespace:         "test-namespace",
		},
		Regarding: corev1.ObjectReference{
			Kind:       "Pod",
			Name:       "test-pod",
			APIVersion: "v1",
		},
		Reason:              "BackOff",
		Type:                "Warning",
		Note:                "Back-off restarting failed container",
		ReportingController: "kubelet",
		ReportingInstance:   "node1",
		EventTime:           metav1.NewMicroTime(now.Add(time.Minute)),
		DeprecatedCount:     3,
	}

	event := source.mapKubernetesEvent(k8sEvent)
	require.NotNil(t, event)

	assert.Equal(t, "pod-restart", event.Type)
	assert.Equal(t, "test-pod", event.ResourceName)
	assert.Equal(t, "test-namespace", event.Namespace)
	assert.Equal(t, "BackOff", event.Reason)
	assert.Equal(t, "Back-off restarting failed container", event.Message)
	assert.Equal(t, "kubernetes", event.Source)
	assert.Equal(t, now.Add(time.Minute).Truncate(time.Microsecond), event.Timestamp.Truncate(time.Microsecond))

	// Check metadata
	assert.Equal(t, "Pod", event.Metadata["kind"])
	assert.Equal(t, "v1", event.Metadata["apiVersion"])
	assert.Equal(t, "3", event.Metadata["count"])
	assert.Equal(t, "Warning", event.Metadata["type"])
	assert.Equal(t, "kubelet", event.Metadata["reportingController"])
	assert.Equal(t, "node1", event.Metadata["reportingInstance"])
	assert.Equal(t, "test-uid", event.Metadata["uid"])
}

func TestMapKubernetesEventIgnored(t *testing.T) {
	source := &KubernetesEventSource{
		logger: logr.Discard(),
	}

	// Test event that should be ignored (normal type)
	k8sEvent := &eventsv1.Event{
		Regarding: corev1.ObjectReference{Kind: "Pod"},
		Reason:    "Started",
		Type:      "Normal",
		Note:      "Container started",
	}

	event := source.mapKubernetesEvent(k8sEvent)
	assert.Nil(t, event)
}

func TestKubernetesEventSourceStop(t *testing.T) {
	source := NewKubernetesEventSource()
	k8sSource := source.(*KubernetesEventSource)

	// Initialize first
	fakeClient := fake.NewSimpleClientset()
	config := map[string]interface{}{
		"client":    fakeClient,
		"namespace": "test",
	}
	ctx := context.Background()
	err := k8sSource.Initialize(ctx, config)
	require.NoError(t, err)

	// Test stop
	err = k8sSource.Stop()
	assert.NoError(t, err)
}
