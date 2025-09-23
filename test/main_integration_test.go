package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/fake"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/kagent-dev/khook/internal/workflow"
)

// MockKagentClient implements interfaces.KagentClient for testing
type MockKagentClient struct{}

func (m *MockKagentClient) CallAgent(ctx context.Context, request interfaces.AgentRequest) (*interfaces.AgentResponse, error) {
	return &interfaces.AgentResponse{
		Success:   true,
		Message:   "Mock response",
		RequestId: "mock-request-id",
	}, nil
}

func (m *MockKagentClient) Authenticate() error {
	return nil
}

// MockEventRecorder implements interfaces.EventRecorder for testing
type MockEventRecorder struct{}

func (m *MockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {}

func (m *MockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}

func (m *MockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}

// TestPluginCoordinatorIntegration tests the plugin coordinator with the main controller workflow
func TestPluginCoordinatorIntegration(t *testing.T) {
	// Create fake Kubernetes client
	k8sClient := fake.NewSimpleClientset()

	// Create fake controller-runtime client with scheme
	scheme := runtime.NewScheme()
	utilruntime.Must(kagentv1alpha2.AddToScheme(scheme))
	ctrlClient := fakeclient.NewClientBuilder().WithScheme(scheme).Build()

	// Create mock clients
	kagentClient := &MockKagentClient{}
	eventRecorder := &MockEventRecorder{}

	// Create plugin coordinator
	coordinator := workflow.NewPluginCoordinator(k8sClient, ctrlClient, kagentClient, eventRecorder)

	// Test initialization
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start coordinator in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- coordinator.Start(ctx)
	}()

	// Give it a moment to initialize
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop coordinator
	cancel()

	// Wait for coordinator to stop
	select {
	case err := <-errCh:
		// Should return context.Canceled
		assert.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Coordinator did not stop within timeout")
	}
}

// TestLegacyCoordinatorStillWorks ensures backward compatibility
func TestLegacyCoordinatorStillWorks(t *testing.T) {
	// Create fake Kubernetes client
	k8sClient := fake.NewSimpleClientset()

	// Create fake controller-runtime client with scheme
	scheme := runtime.NewScheme()
	utilruntime.Must(kagentv1alpha2.AddToScheme(scheme))
	ctrlClient := fakeclient.NewClientBuilder().WithScheme(scheme).Build()

	// Create mock clients
	kagentClient := &MockKagentClient{}
	eventRecorder := &MockEventRecorder{}

	// Create legacy coordinator
	coordinator := workflow.NewCoordinator(k8sClient, ctrlClient, kagentClient, eventRecorder)

	// Test initialization
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start coordinator in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- coordinator.Start(ctx)
	}()

	// Give it a moment to initialize
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop coordinator
	cancel()

	// Wait for coordinator to stop
	select {
	case err := <-errCh:
		// Should return context.Canceled
		assert.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Legacy coordinator did not stop within timeout")
	}
}
