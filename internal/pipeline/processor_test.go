package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

// Mock implementations for testing
type MockEventWatcher struct {
	mock.Mock
}

func (m *MockEventWatcher) WatchEvents(ctx context.Context) (<-chan interfaces.Event, error) {
	args := m.Called(ctx)
	return args.Get(0).(<-chan interfaces.Event), args.Error(1)
}

func (m *MockEventWatcher) FilterEvent(event interfaces.Event, hooks []interface{}) []interfaces.EventMatch {
	args := m.Called(event, hooks)
	return args.Get(0).([]interfaces.EventMatch)
}

func (m *MockEventWatcher) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockEventWatcher) Stop() error {
	args := m.Called()
	return args.Error(0)
}

type MockDeduplicationManager struct {
	mock.Mock
}

func (m *MockDeduplicationManager) ShouldProcessEvent(hookName string, event interfaces.Event) bool {
	args := m.Called(hookName, event)
	return args.Bool(0)
}

func (m *MockDeduplicationManager) RecordEvent(hookName string, event interfaces.Event) error {
	args := m.Called(hookName, event)
	return args.Error(0)
}

func (m *MockDeduplicationManager) CleanupExpiredEvents(hookName string) error {
	args := m.Called(hookName)
	return args.Error(0)
}

func (m *MockDeduplicationManager) GetActiveEvents(hookName string) []interfaces.ActiveEvent {
	args := m.Called(hookName)
	return args.Get(0).([]interfaces.ActiveEvent)
}

func (m *MockDeduplicationManager) GetActiveEventsWithStatus(hookName string) []interfaces.ActiveEvent {
	args := m.Called(hookName)
	return args.Get(0).([]interfaces.ActiveEvent)
}

func (m *MockDeduplicationManager) MarkNotified(hookName string, event interfaces.Event) {
	m.Called(hookName, event)
}

type MockKagentClient struct {
	mock.Mock
}

func (m *MockKagentClient) CallAgent(ctx context.Context, request interfaces.AgentRequest) (*interfaces.AgentResponse, error) {
	args := m.Called(ctx, request)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.AgentResponse), args.Error(1)
}

func (m *MockKagentClient) Authenticate() error {
	args := m.Called()
	return args.Error(0)
}

type MockStatusManager struct {
	mock.Mock
}

func (m *MockStatusManager) UpdateHookStatus(ctx context.Context, hook interface{}, activeEvents []interfaces.ActiveEvent) error {
	args := m.Called(ctx, hook, activeEvents)
	return args.Error(0)
}

func (m *MockStatusManager) RecordEventFiring(ctx context.Context, hook interface{}, event interfaces.Event, agentRef types.NamespacedName) error {
	args := m.Called(ctx, hook, event, agentRef)
	return args.Error(0)
}

func (m *MockStatusManager) RecordEventResolved(ctx context.Context, hook interface{}, eventType, resourceName string) error {
	args := m.Called(ctx, hook, eventType, resourceName)
	return args.Error(0)
}

func (m *MockStatusManager) RecordError(ctx context.Context, hook interface{}, event interfaces.Event, err error, agentRef types.NamespacedName) error {
	args := m.Called(ctx, hook, event, err, agentRef)
	return args.Error(0)
}

func (m *MockStatusManager) RecordAgentCallSuccess(ctx context.Context, hook interface{}, event interfaces.Event, agentRef types.NamespacedName, requestId string) error {
	args := m.Called(ctx, hook, event, agentRef, requestId)
	return args.Error(0)
}

func (m *MockStatusManager) RecordAgentCallFailure(ctx context.Context, hook interface{}, event interfaces.Event, agentRef types.NamespacedName, err error) error {
	args := m.Called(ctx, hook, event, agentRef, err)
	return args.Error(0)
}

func (m *MockStatusManager) RecordDuplicateEvent(ctx context.Context, hook interface{}, event interfaces.Event) error {
	args := m.Called(ctx, hook, event)
	return args.Error(0)
}

func (m *MockStatusManager) GetHookStatus(ctx context.Context, hookName, namespace string) (*v1alpha2.HookStatus, error) {
	args := m.Called(ctx, hookName, namespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*v1alpha2.HookStatus), args.Error(1)
}

func (m *MockStatusManager) LogControllerStartup(ctx context.Context, version string, config map[string]interface{}) {
	m.Called(ctx, version, config)
}

func (m *MockStatusManager) LogControllerShutdown(ctx context.Context, reason string) {
	m.Called(ctx, reason)
}

// Test helper functions
func createTestHook(name, namespace string, eventConfigs []v1alpha2.EventConfiguration) *v1alpha2.Hook {
	return &v1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.HookSpec{
			EventConfigurations: eventConfigs,
		},
	}
}

func createTestEvent(eventType, resourceName, namespace string) interfaces.Event {
	return interfaces.Event{
		Type:         eventType,
		ResourceName: resourceName,
		Namespace:    namespace,
		Timestamp:    time.Now(),
		Reason:       "TestReason",
		Message:      "Test message",
		UID:          "test-uid",
		Metadata: map[string]string{
			"kind": "Pod",
		},
	}
}

func TestProcessor_ProcessEvent_Success(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "pod-restart",
			AgentRef: v1alpha2.ObjectReference{
				Name: "test-agent",
			},
			Prompt: "Handle pod restart for {{.ResourceName}}",
		},
	})

	event := createTestEvent("pod-restart", "test-pod", "default")
	hooks := []*v1alpha2.Hook{hook}

	ctx := context.Background()

	// Setup expectations
	mockDeduplicationManager.On("ShouldProcessEvent", "default/test-hook", event).Return(true)
	mockDeduplicationManager.On("RecordEvent", "default/test-hook", event).Return(nil)
	mockStatusManager.On("RecordEventFiring", ctx, hook, event, "test-agent").Return(nil)

	expectedResponse := &interfaces.AgentResponse{
		Success:   true,
		Message:   "Success",
		RequestId: "test-request-id",
	}
	mockKagentClient.On("CallAgent", ctx, mock.MatchedBy(func(req interfaces.AgentRequest) bool {
		return req.AgentRef.Name == "test-agent" &&
			req.EventName == "pod-restart" &&
			req.ResourceName == "test-pod"
	})).Return(expectedResponse, nil)

	mockStatusManager.On("RecordAgentCallSuccess", ctx, hook, event, "test-agent", "test-request-id").Return(nil)
	mockDeduplicationManager.On("MarkNotified", "default/test-hook", event).Return()

	// Execute
	err := processor.ProcessEvent(ctx, event, hooks)

	// Assert
	assert.NoError(t, err)
	mockDeduplicationManager.AssertExpectations(t)
	mockKagentClient.AssertExpectations(t)
	mockStatusManager.AssertExpectations(t)
}

func TestProcessor_ProcessEvent_DuplicateEvent(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "pod-restart",
			AgentRef: v1alpha2.ObjectReference{
				Name: "test-agent",
			},
			Prompt: "Handle pod restart",
		},
	})

	event := createTestEvent("pod-restart", "test-pod", "default")
	hooks := []*v1alpha2.Hook{hook}

	ctx := context.Background()

	// Setup expectations - event should be ignored due to deduplication
	mockDeduplicationManager.On("ShouldProcessEvent", "default/test-hook", event).Return(false)
	mockStatusManager.On("RecordDuplicateEvent", ctx, hook, event).Return(nil)

	// Execute
	err := processor.ProcessEvent(ctx, event, hooks)

	// Assert
	assert.NoError(t, err)
	mockDeduplicationManager.AssertExpectations(t)
	mockStatusManager.AssertExpectations(t)
	// Kagent client should not be called for duplicate events
	mockKagentClient.AssertNotCalled(t, "CallAgent")
}

func TestProcessor_ProcessEvent_AgentCallFailure(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "pod-restart",
			AgentRef: v1alpha2.ObjectReference{
				Name: "test-agent",
			},
			Prompt: "Handle pod restart",
		},
	})

	event := createTestEvent("pod-restart", "test-pod", "default")
	hooks := []*v1alpha2.Hook{hook}

	ctx := context.Background()
	agentError := errors.New("agent call failed")

	// Setup expectations
	mockDeduplicationManager.On("ShouldProcessEvent", "default/test-hook", event).Return(true)
	mockDeduplicationManager.On("RecordEvent", "default/test-hook", event).Return(nil)
	mockStatusManager.On("RecordEventFiring", ctx, hook, event, "test-agent").Return(nil)
	mockKagentClient.On("CallAgent", ctx, mock.AnythingOfType("interfaces.AgentRequest")).Return(nil, agentError)
	mockStatusManager.On("RecordAgentCallFailure", ctx, hook, event, "test-agent", agentError).Return(nil)

	// Execute
	err := processor.ProcessEvent(ctx, event, hooks)

	// Assert - should return error but continue processing
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to call agent test-agent")
	mockDeduplicationManager.AssertExpectations(t)
	mockKagentClient.AssertExpectations(t)
	mockStatusManager.AssertExpectations(t)
}

func TestProcessor_ProcessEvent_MultipleHooks(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data - two hooks that both match the same event type
	hook1 := createTestHook("hook1", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "pod-restart",
			AgentRef: v1alpha2.ObjectReference{
				Name: "agent1",
			},
			Prompt: "Agent 1 prompt",
		},
	})

	hook2 := createTestHook("hook2", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "pod-restart",
			AgentRef: v1alpha2.ObjectReference{
				Name: "agent2",
			},
			Prompt: "Agent 2 prompt",
		},
	})

	event := createTestEvent("pod-restart", "test-pod", "default")
	hooks := []*v1alpha2.Hook{hook1, hook2}

	ctx := context.Background()

	// Setup expectations for both hooks
	mockDeduplicationManager.On("ShouldProcessEvent", "default/hook1", event).Return(true)
	mockDeduplicationManager.On("RecordEvent", "default/hook1", event).Return(nil)
	mockStatusManager.On("RecordEventFiring", ctx, hook1, event, "agent1").Return(nil)

	mockDeduplicationManager.On("ShouldProcessEvent", "default/hook2", event).Return(true)
	mockDeduplicationManager.On("RecordEvent", "default/hook2", event).Return(nil)
	mockStatusManager.On("RecordEventFiring", ctx, hook2, event, "agent2").Return(nil)

	response1 := &interfaces.AgentResponse{Success: true, Message: "Success 1", RequestId: "req1"}
	response2 := &interfaces.AgentResponse{Success: true, Message: "Success 2", RequestId: "req2"}

	mockKagentClient.On("CallAgent", ctx, mock.MatchedBy(func(req interfaces.AgentRequest) bool {
		return req.AgentRef.Name == "agent1"
	})).Return(response1, nil)

	mockKagentClient.On("CallAgent", ctx, mock.MatchedBy(func(req interfaces.AgentRequest) bool {
		return req.AgentRef.Name == "agent2"
	})).Return(response2, nil)

	mockStatusManager.On("RecordAgentCallSuccess", ctx, hook1, event, "agent1", "req1").Return(nil)
	mockStatusManager.On("RecordAgentCallSuccess", ctx, hook2, event, "agent2", "req2").Return(nil)
	mockDeduplicationManager.On("MarkNotified", "default/hook1", event).Return()
	mockDeduplicationManager.On("MarkNotified", "default/hook2", event).Return()

	// Execute
	err := processor.ProcessEvent(ctx, event, hooks)

	// Assert
	assert.NoError(t, err)
	mockDeduplicationManager.AssertExpectations(t)
	mockKagentClient.AssertExpectations(t)
	mockStatusManager.AssertExpectations(t)
}

func TestProcessor_ProcessEvent_NoMatchingHooks(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data - hook that doesn't match the event type
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{
			EventType: "oom-kill",
			AgentRef: v1alpha2.ObjectReference{
				Name: "test-agent",
			},
			Prompt: "Handle OOM kill",
		},
	})

	event := createTestEvent("pod-restart", "test-pod", "default")
	hooks := []*v1alpha2.Hook{hook}

	ctx := context.Background()

	// Execute
	err := processor.ProcessEvent(ctx, event, hooks)

	// Assert - should succeed but not call any services
	assert.NoError(t, err)
	mockDeduplicationManager.AssertNotCalled(t, "ShouldProcessEvent")
	mockKagentClient.AssertNotCalled(t, "CallAgent")
	mockStatusManager.AssertNotCalled(t, "RecordEventFiring")
}

func TestProcessor_ExpandPromptTemplate(t *testing.T) {
	processor := &Processor{}

	event := interfaces.Event{
		Type:         "pod-restart",
		ResourceName: "test-pod",
		Namespace:    "default",
		Reason:       "BackOff",
		Message:      "Container failed to start",
		Timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	template := "Event {{.EventType}} occurred for {{.ResourceName}} in {{.Namespace}} at {{.Timestamp}}"
	expected := "Event pod-restart occurred for test-pod in default at 2023-01-01T12:00:00Z"

	result := processor.expandPromptTemplate(template, event)
	assert.Equal(t, expected, result)
}

func TestProcessor_UpdateHookStatuses(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{EventType: "pod-restart", AgentRef: v1alpha2.ObjectReference{Name: "agent1"}, Prompt: "prompt1"},
	})
	hooks := []*v1alpha2.Hook{hook}

	activeEvents := []interfaces.ActiveEvent{
		{
			EventType:    "pod-restart",
			ResourceName: "test-pod",
			FirstSeen:    time.Now(),
			LastSeen:     time.Now(),
			Status:       "firing",
		},
	}

	ctx := context.Background()

	// Setup expectations
	mockDeduplicationManager.On("GetActiveEventsWithStatus", "default/test-hook").Return(activeEvents)
	mockStatusManager.On("UpdateHookStatus", ctx, hook, activeEvents).Return(nil)

	// Execute
	err := processor.UpdateHookStatuses(ctx, hooks)

	// Assert
	assert.NoError(t, err)
	mockDeduplicationManager.AssertExpectations(t)
	mockStatusManager.AssertExpectations(t)
}

func TestProcessor_CleanupExpiredEvents(t *testing.T) {
	// Setup mocks
	mockEventWatcher := &MockEventWatcher{}
	mockDeduplicationManager := &MockDeduplicationManager{}
	mockKagentClient := &MockKagentClient{}
	mockStatusManager := &MockStatusManager{}

	processor := NewProcessor(mockEventWatcher, mockDeduplicationManager, mockKagentClient, mockStatusManager)

	// Create test data
	hook := createTestHook("test-hook", "default", []v1alpha2.EventConfiguration{
		{EventType: "pod-restart", AgentRef: v1alpha2.ObjectReference{Name: "agent1"}, Prompt: "prompt1"},
	})
	hooks := []*v1alpha2.Hook{hook}

	ctx := context.Background()

	// Setup expectations
	mockDeduplicationManager.On("CleanupExpiredEvents", "default/test-hook").Return(nil)

	// Execute
	err := processor.CleanupExpiredEvents(ctx, hooks)

	// Assert
	assert.NoError(t, err)
	mockDeduplicationManager.AssertExpectations(t)
}
