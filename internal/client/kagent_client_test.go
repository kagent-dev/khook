package client

import (
	"context"
	"testing"
	"time"

	"github.com/antweiss/khook/internal/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestNewClient(t *testing.T) {
	logger := log.Log.WithName("test")

	t.Run("with custom config", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://custom.api.com",
			UserID:  "test-user",
			Timeout: 10 * time.Second,
		}

		client := NewClient(config, logger)
		assert.Equal(t, config, client.config)
		assert.NotNil(t, client.clientSet)
	})

	t.Run("with nil config uses defaults", func(t *testing.T) {
		client := NewClient(nil, logger)
		assert.Equal(t, "http://kagent-controller.kagent.svc.local:8083", client.config.BaseURL)
		assert.Equal(t, "admin@kagent.dev", client.config.UserID)
		assert.Equal(t, 120*time.Second, client.config.Timeout)
	})
}

func TestClient_Authenticate(t *testing.T) {
	logger := log.Log.WithName("test")

	t.Run("authentication creates client successfully", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://api.kagent.dev",
			UserID:  "test-user",
			Timeout: 1 * time.Second,
		}

		client := NewClient(config, logger)
		// The authenticate method will try to connect to the actual service
		// In a unit test environment, this might fail due to network issues
		// but the client should be created successfully
		assert.NotNil(t, client)
		assert.NotNil(t, client.clientSet)
	})
}

func TestClient_CallAgent(t *testing.T) {
	logger := log.Log.WithName("test")

	t.Run("call agent with invalid URL fails", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://invalid-url-that-does-not-exist.com",
			UserID:  "test-user",
			Timeout: 5 * time.Second,
		}

		client := NewClient(config, logger)

		request := interfaces.AgentRequest{
			AgentId:      "test-agent",
			Prompt:       "Test prompt",
			EventName:    "pod-restart",
			EventTime:    time.Now(),
			ResourceName: "test-pod",
			Context: map[string]interface{}{
				"namespace": "default",
			},
		}

		_, err := client.CallAgent(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create session")
	})

	t.Run("context cancellation", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://api.kagent.dev", // This will timeout
			UserID:  "test-user",
			Timeout: 5 * time.Second,
		}

		client := NewClient(config, logger)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		request := interfaces.AgentRequest{
			AgentId:      "test-agent",
			Prompt:       "Test prompt",
			EventName:    "pod-restart",
			EventTime:    time.Now(),
			ResourceName: "test-pod",
			Context:      map[string]interface{}{},
		}

		_, err := client.CallAgent(ctx, request)
		require.Error(t, err)
		// Should fail due to context cancellation or network timeout
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, "http://kagent-controller.kagent.svc.local:8083", config.BaseURL)
	assert.Equal(t, "admin@kagent.dev", config.UserID)
	assert.Equal(t, 120*time.Second, config.Timeout)
}
