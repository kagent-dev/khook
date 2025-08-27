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

	return NewClient(config, logger), nil
}

// ValidateConfig validates the client configuration
func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if config.BaseURL == "" {
		return fmt.Errorf("BaseURL cannot be empty")
	}

	if config.UserID == "" {
		return fmt.Errorf("UserID cannot be empty")
	}

	if config.Timeout <= 0 {
		return fmt.Errorf("Timeout must be positive")
	}

	return nil
}
