package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	"github.com/kagent/hook-controller/api/v1alpha2"
	"github.com/kagent/hook-controller/internal/deduplication"
	"github.com/kagent/hook-controller/internal/event"
	"github.com/kagent/hook-controller/internal/interfaces"
	"github.com/kagent/hook-controller/internal/status"
)

// MockKagentClientForIntegration provides a simple mock for integration testing
type MockKagentClientForIntegration struct {
	responses map[string]*interfaces.AgentResponse
	calls     []interfaces.AgentRequest
}

func NewMockKagentClientForIntegration() *MockKagentClientForIntegration {
	return &MockKagentClientForIntegration{
		responses: make(map[string]*interfaces.AgentResponse),
		calls:     make([]interfaces.AgentRequest, 0),
	}
}

func (m *MockKagentClientForIntegration) CallAgent(ctx context.Context, request interfaces.AgentRequest) (*interfaces.AgentResponse, error) {
	m.calls = append(m.calls, request)

	if response, exists := m.responses[request.AgentId]; exists {
		if response == nil {
			return nil, errors.New("mock agent call failed")
		}
		return response, nil
	}

	// Default response
	return &interfaces.AgentResponse{
		Success:   true,
		Message:   "Mock response",
		RequestId: "mock-request-" + request.AgentId,
	}, nil
}

func (m *MockKagentClientForIntegration) Authenticate() error {
	return nil
}

func (m *MockKagentClientForIntegration) SetResponse(agentId string, response *interfaces.AgentResponse) {
	m.responses[agentId] = response
}

func (m *MockKagentClientForIntegration) GetCalls() []interfaces.AgentRequest {
	return m.calls
}

func (m *MockKagentClientForIntegration) ClearCalls() {
	m.calls = make([]interfaces.AgentRequest, 0)
}

// TestEventProcessingIntegration tests the complete event processing pipeline
func TestEventProcessingIntegration(t *testing.T) {
	// Create real components (except Kagent client which we mock)
	k8sClient := fake.NewSimpleClientset()
	eventRecorder := record.NewFakeRecorder(100)

	// Create real components
	eventWatcher := event.NewWatcher(k8sClient, "default")
	deduplicationManager := deduplication.NewManager()
	mockKagentClient := NewMockKagentClientForIntegration()
	statusManager := status.NewManager(nil, eventRecorder) // nil client for this test

	// Create processor
	processor := NewProcessor(eventWatcher, deduplicationManager, mockKagentClient, statusManager)

	// Create test hooks
	hook1 := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-restart-hook",
			Namespace: "default",
		},
		Spec: v1alpha2.HookSpec{
			EventConfigurations: []v1alpha2.EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "restart-agent",
					Prompt:    "Pod {{.ResourceName}} restarted in {{.Namespace}}",
				},
			},
		},
	}

	hook2 := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-event-hook",
			Namespace: "default",
		},
		Spec: v1alpha2.HookSpec{
			EventConfigurations: []v1alpha2.EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "multi-restart-agent",
					Prompt:    "Multi-hook: Pod {{.ResourceName}} restarted",
				},
				{
					EventType: "oom-kill",
					AgentId:   "oom-agent",
					Prompt:    "OOM kill detected for {{.ResourceName}}",
				},
			},
		},
	}

	hooks := []*v1alpha2.Hook{hook1, hook2}
	ctx := context.Background()

	// Test 1: Process pod-restart event
	t.Run("ProcessPodRestartEvent", func(t *testing.T) {
		mockKagentClient.ClearCalls()

		event := interfaces.Event{
			Type:         "pod-restart",
			ResourceName: "test-pod-1",
			Namespace:    "default",
			Timestamp:    time.Now(),
			Reason:       "BackOff",
			Message:      "Container failed to start",
			UID:          "test-uid-1",
			Metadata: map[string]string{
				"kind": "Pod",
			},
		}

		err := processor.ProcessEvent(ctx, event, hooks)
		require.NoError(t, err)

		// Verify agent calls
		calls := mockKagentClient.GetCalls()
		assert.Len(t, calls, 2, "Should call both agents for pod-restart event")

		// Verify first call (restart-agent)
		call1 := calls[0]
		assert.Equal(t, "restart-agent", call1.AgentId)
		assert.Equal(t, "pod-restart", call1.EventName)
		assert.Equal(t, "test-pod-1", call1.ResourceName)
		assert.Contains(t, call1.Prompt, "Pod test-pod-1 restarted in default")

		// Verify second call (multi-restart-agent)
		call2 := calls[1]
		assert.Equal(t, "multi-restart-agent", call2.AgentId)
		assert.Equal(t, "pod-restart", call2.EventName)
		assert.Equal(t, "test-pod-1", call2.ResourceName)
		assert.Contains(t, call2.Prompt, "Multi-hook: Pod test-pod-1 restarted")

		// Verify deduplication state
		activeEvents1 := deduplicationManager.GetActiveEvents("default/pod-restart-hook")
		assert.Len(t, activeEvents1, 1)
		assert.Equal(t, "pod-restart", activeEvents1[0].EventType)
		assert.Equal(t, "test-pod-1", activeEvents1[0].ResourceName)

		activeEvents2 := deduplicationManager.GetActiveEvents("default/multi-event-hook")
		assert.Len(t, activeEvents2, 1)
		assert.Equal(t, "pod-restart", activeEvents2[0].EventType)
		assert.Equal(t, "test-pod-1", activeEvents2[0].ResourceName)
	})

	// Test 2: Process duplicate event (should be ignored)
	t.Run("ProcessDuplicateEvent", func(t *testing.T) {
		mockKagentClient.ClearCalls()

		// Same event as before - should be deduplicated
		event := interfaces.Event{
			Type:         "pod-restart",
			ResourceName: "test-pod-1",
			Namespace:    "default",
			Timestamp:    time.Now(),
			Reason:       "BackOff",
			Message:      "Container failed to start again",
			UID:          "test-uid-1-duplicate",
		}

		err := processor.ProcessEvent(ctx, event, hooks)
		require.NoError(t, err)

		// Verify no agent calls were made
		calls := mockKagentClient.GetCalls()
		assert.Len(t, calls, 0, "Should not call agents for duplicate event")
	})

	// Test 3: Process OOM kill event
	t.Run("ProcessOOMKillEvent", func(t *testing.T) {
		mockKagentClient.ClearCalls()

		event := interfaces.Event{
			Type:         "oom-kill",
			ResourceName: "test-pod-2",
			Namespace:    "default",
			Timestamp:    time.Now(),
			Reason:       "OOMKilling",
			Message:      "Memory limit exceeded",
			UID:          "test-uid-2",
			Metadata: map[string]string{
				"kind": "Pod",
			},
		}

		err := processor.ProcessEvent(ctx, event, hooks)
		require.NoError(t, err)

		// Verify agent calls - only multi-event-hook should match
		calls := mockKagentClient.GetCalls()
		assert.Len(t, calls, 1, "Should call only the OOM agent")

		call := calls[0]
		assert.Equal(t, "oom-agent", call.AgentId)
		assert.Equal(t, "oom-kill", call.EventName)
		assert.Equal(t, "test-pod-2", call.ResourceName)
		assert.Contains(t, call.Prompt, "OOM kill detected for test-pod-2")
	})

	// Test 4: Process event with no matching hooks
	t.Run("ProcessUnmatchedEvent", func(t *testing.T) {
		mockKagentClient.ClearCalls()

		event := interfaces.Event{
			Type:         "probe-failed",
			ResourceName: "test-pod-3",
			Namespace:    "default",
			Timestamp:    time.Now(),
			Reason:       "Unhealthy",
			Message:      "Liveness probe failed",
			UID:          "test-uid-3",
		}

		err := processor.ProcessEvent(ctx, event, hooks)
		require.NoError(t, err)

		// Verify no agent calls were made
		calls := mockKagentClient.GetCalls()
		assert.Len(t, calls, 0, "Should not call agents for unmatched event")
	})

	// Test 5: Verify active events state
	t.Run("VerifyActiveEventsState", func(t *testing.T) {
		// Check active events for pod-restart-hook
		activeEvents1 := deduplicationManager.GetActiveEvents("default/pod-restart-hook")
		assert.Len(t, activeEvents1, 1)
		assert.Equal(t, "pod-restart", activeEvents1[0].EventType)
		assert.Equal(t, "test-pod-1", activeEvents1[0].ResourceName)
		assert.Equal(t, "firing", activeEvents1[0].Status)

		// Check active events for multi-event-hook
		activeEvents2 := deduplicationManager.GetActiveEvents("default/multi-event-hook")
		assert.Len(t, activeEvents2, 2) // pod-restart and oom-kill

		eventTypes := make(map[string]bool)
		for _, event := range activeEvents2 {
			eventTypes[event.EventType] = true
		}
		assert.True(t, eventTypes["pod-restart"])
		assert.True(t, eventTypes["oom-kill"])
	})
}

// TestEventProcessingWithErrors tests error handling in the pipeline
func TestEventProcessingWithErrors(t *testing.T) {
	// Create components
	k8sClient := fake.NewSimpleClientset()
	eventRecorder := record.NewFakeRecorder(100)

	eventWatcher := event.NewWatcher(k8sClient, "default")
	deduplicationManager := deduplication.NewManager()
	mockKagentClient := NewMockKagentClientForIntegration()
	statusManager := status.NewManager(nil, eventRecorder)

	processor := NewProcessor(eventWatcher, deduplicationManager, mockKagentClient, statusManager)

	// Create test hooks - separate hooks to avoid deduplication interference
	hook1 := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failing-hook",
			Namespace: "default",
		},
		Spec: v1alpha2.HookSpec{
			EventConfigurations: []v1alpha2.EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "failing-agent",
					Prompt:    "This will fail",
				},
			},
		},
	}

	hook2 := &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "working-hook",
			Namespace: "default",
		},
		Spec: v1alpha2.HookSpec{
			EventConfigurations: []v1alpha2.EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "working-agent",
					Prompt:    "This will work",
				},
			},
		},
	}

	hooks := []*v1alpha2.Hook{hook1, hook2}
	ctx := context.Background()

	// Set up one agent to fail and one to succeed
	mockKagentClient.SetResponse("failing-agent", nil) // This will cause an error
	mockKagentClient.SetResponse("working-agent", &interfaces.AgentResponse{
		Success:   true,
		Message:   "Success",
		RequestId: "working-request",
	})

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "error-test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
		Reason:       "BackOff",
		Message:      "Container failed",
		UID:          "error-test-uid",
	}

	// Process event - should continue processing even with one failure
	err := processor.ProcessEvent(ctx, event, hooks)

	// The processor should continue processing other configurations even if one fails
	// So we expect an error but the working agent should still be called
	assert.Error(t, err)

	calls := mockKagentClient.GetCalls()
	assert.Len(t, calls, 2, "Should attempt to call both agents")

	// Verify both agents were attempted
	agentIds := make(map[string]bool)
	for _, call := range calls {
		agentIds[call.AgentId] = true
	}
	assert.True(t, agentIds["failing-agent"])
	assert.True(t, agentIds["working-agent"])
}

// TestPromptTemplateExpansion tests the prompt template expansion functionality
func TestPromptTemplateExpansion(t *testing.T) {
	processor := &Processor{}

	testCases := []struct {
		name     string
		template string
		event    interfaces.Event
		expected string
	}{
		{
			name:     "Basic template expansion",
			template: "Event {{.EventType}} for {{.ResourceName}}",
			event: interfaces.Event{
				Type:         "pod-restart",
				ResourceName: "my-pod",
			},
			expected: "Event pod-restart for my-pod",
		},
		{
			name:     "Full template expansion",
			template: "{{.EventType}} in {{.Namespace}}: {{.ResourceName}} - {{.Reason}} ({{.Message}}) at {{.Timestamp}}",
			event: interfaces.Event{
				Type:         "oom-kill",
				ResourceName: "memory-hog",
				Namespace:    "production",
				Reason:       "OOMKilling",
				Message:      "Container exceeded memory limit",
				Timestamp:    time.Date(2023, 12, 25, 10, 30, 0, 0, time.UTC),
			},
			expected: "oom-kill in production: memory-hog - OOMKilling (Container exceeded memory limit) at 2023-12-25T10:30:00Z",
		},
		{
			name:     "Template with no placeholders",
			template: "Static message without placeholders",
			event: interfaces.Event{
				Type:         "pod-restart",
				ResourceName: "test-pod",
			},
			expected: "Static message without placeholders",
		},
		{
			name:     "Template with unknown placeholders",
			template: "Known: {{.EventType}}, Unknown: {{.UnknownField}}",
			event: interfaces.Event{
				Type: "pod-restart",
			},
			expected: "Known: pod-restart, Unknown: {{.UnknownField}}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := processor.expandPromptTemplate(tc.template, tc.event)
			assert.Equal(t, tc.expected, result)
		})
	}
}
