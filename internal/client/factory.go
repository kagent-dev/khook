package client

import (
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
)

// NewClientFromEnv creates a new Kagent client using environment variables
func NewClientFromEnv(logger logr.Logger) (*Client, error) {
	config := DefaultConfig()

	// Override with environment variables if present
	if baseURL := os.Getenv("KAGENT_API_URL"); baseURL != "" {
		config.BaseURL = baseURL
	} else if legacyBaseURL := os.Getenv("KAGENT_API_BASE_URL"); legacyBaseURL != "" { // legacy fallback
		config.BaseURL = legacyBaseURL
	}

	if userID := os.Getenv("KAGENT_USER_ID"); userID != "" {
		config.UserID = userID
	}

	if timeoutStr := os.Getenv("KAGENT_API_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid KAGENT_API_TIMEOUT format: %w", err)
		}
		config.Timeout = timeout
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid client configuration: %w", err)
	}

	return NewClient(config, logger), nil
}

// ValidateConfig validates the client configuration (deprecated: use config.Validate() instead)
func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	return config.Validate()
}
