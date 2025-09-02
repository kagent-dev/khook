package client

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestNewClientFromEnv(t *testing.T) {
	logger := log.Log.WithName("test")

	t.Run("with all environment variables", func(t *testing.T) {
		// Set environment variables
		os.Setenv("KAGENT_API_URL", "https://custom.api.com")
		os.Setenv("KAGENT_USER_ID", "test-user")
		os.Setenv("KAGENT_API_TIMEOUT", "45s")
		defer func() {
			os.Unsetenv("KAGENT_API_URL")
			os.Unsetenv("KAGENT_USER_ID")
			os.Unsetenv("KAGENT_API_TIMEOUT")
		}()

		client, err := NewClientFromEnv(logger)
		require.NoError(t, err)

		assert.Equal(t, "https://custom.api.com", client.config.BaseURL)
		assert.Equal(t, "test-user", client.config.UserID)
		assert.Equal(t, 45*time.Second, client.config.Timeout)
	})

	t.Run("with minimal environment variables", func(t *testing.T) {
		// No environment variables set, should use defaults
		client, err := NewClientFromEnv(logger)
		require.NoError(t, err)

		// Should use defaults for all values
		assert.Equal(t, "http://kagent-controller.kagent.svc.local:8083", client.config.BaseURL)
		assert.Equal(t, "admin@kagent.dev", client.config.UserID)
		assert.Equal(t, 120*time.Second, client.config.Timeout)
	})

	t.Run("with legacy environment variable", func(t *testing.T) {
		// Test backward compatibility with old environment variable name
		os.Setenv("KAGENT_API_BASE_URL", "https://legacy.api.com")
		defer os.Unsetenv("KAGENT_API_BASE_URL")

		client, err := NewClientFromEnv(logger)
		require.NoError(t, err)

		assert.Equal(t, "https://legacy.api.com", client.config.BaseURL)
	})

	t.Run("new env var takes precedence over legacy", func(t *testing.T) {
		// Both environment variables set, new one should take precedence
		os.Setenv("KAGENT_API_URL", "https://new.api.com")
		os.Setenv("KAGENT_API_BASE_URL", "https://legacy.api.com")
		defer func() {
			os.Unsetenv("KAGENT_API_URL")
			os.Unsetenv("KAGENT_API_BASE_URL")
		}()

		client, err := NewClientFromEnv(logger)
		require.NoError(t, err)

		assert.Equal(t, "https://new.api.com", client.config.BaseURL)
	})

	t.Run("invalid timeout format", func(t *testing.T) {
		os.Setenv("KAGENT_API_TIMEOUT", "invalid")
		defer os.Unsetenv("KAGENT_API_TIMEOUT")

		_, err := NewClientFromEnv(logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid KAGENT_API_TIMEOUT format")
	})
}

func TestValidateConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://api.kagent.dev",
			UserID:  "test-user",
			Timeout: 30 * time.Second,
		}

		err := ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("nil config", func(t *testing.T) {
		err := ValidateConfig(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("empty base URL", func(t *testing.T) {
		config := &Config{
			BaseURL: "",
			UserID:  "test-user",
			Timeout: 30 * time.Second,
		}

		err := ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "BaseURL cannot be empty")
	})

	t.Run("empty user ID", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://api.kagent.dev",
			UserID:  "",
			Timeout: 30 * time.Second,
		}

		err := ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "UserID cannot be empty")
	})

	t.Run("zero timeout", func(t *testing.T) {
		config := &Config{
			BaseURL: "https://api.kagent.dev",
			UserID:  "test-user",
			Timeout: 0,
		}

		err := ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Timeout must be positive")
	})
}
