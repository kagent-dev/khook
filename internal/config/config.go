package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// Config holds the configuration for the hook controller
type Config struct {
	// Kagent holds the Kagent API configuration
	Kagent KagentConfig `yaml:"kagent"`

	// Controller holds controller-specific configuration
	Controller ControllerConfig `yaml:"controller"`

	// Logging holds logging configuration
	Logging LoggingConfig `yaml:"logging"`
}

// KagentConfig holds Kagent API configuration
type KagentConfig struct {
	// BaseURL is the base URL for the Kagent API
	BaseURL string `yaml:"baseUrl"`

	// APIKey is the API key for authentication
	APIKey string `yaml:"apiKey"`

	// Timeout is the timeout for API calls
	Timeout time.Duration `yaml:"timeout"`

	// RetryAttempts is the number of retry attempts for failed API calls
	RetryAttempts int `yaml:"retryAttempts"`
}

// ControllerConfig holds controller-specific configuration
type ControllerConfig struct {
	// EventDeduplicationTimeout is the timeout for event deduplication
	EventDeduplicationTimeout time.Duration `yaml:"eventDeduplicationTimeout"`

	// EventCleanupInterval is the interval for cleaning up expired events
	EventCleanupInterval time.Duration `yaml:"eventCleanupInterval"`

	// MaxConcurrentReconciles is the maximum number of concurrent reconciles
	MaxConcurrentReconciles int `yaml:"maxConcurrentReconciles"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	// Level is the logging level
	Level string `yaml:"level"`

	// Format is the logging format (json or text)
	Format string `yaml:"format"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Kagent: KagentConfig{
			BaseURL:       "https://api.kagent.dev",
			Timeout:       30 * time.Second,
			RetryAttempts: 3,
		},
		Controller: ControllerConfig{
			EventDeduplicationTimeout: 10 * time.Minute,
			EventCleanupInterval:      5 * time.Minute,
			MaxConcurrentReconciles:   1,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// validateConfigPath validates and sanitizes the config file path to prevent path traversal
func validateConfigPath(configFile string) (string, error) {
	if configFile == "" {
		return "", nil
	}

	// Clean the path to remove any .. or . components
	cleanPath := filepath.Clean(configFile)

	// Check for path traversal attempts
	if strings.Contains(cleanPath, "..") {
		return "", fmt.Errorf("path traversal detected in config file path: %s", configFile)
	}

	// Ensure the path doesn't start with suspicious characters
	if strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
		// Allow absolute paths but validate them
		if !filepath.IsAbs(cleanPath) {
			return "", fmt.Errorf("invalid absolute path: %s", configFile)
		}
	}

	// Additional validation: check for suspicious patterns
	suspiciousPatterns := []string{"../", "..\\", "/..", "\\.."}
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(cleanPath, pattern) {
			return "", fmt.Errorf("suspicious path pattern detected: %s", pattern)
		}
	}

	return cleanPath, nil
}

// Load loads configuration from file or returns default configuration
func Load(configFile string) (*Config, error) {
	config := DefaultConfig()

	// Override with environment variables
	if baseURL := os.Getenv("KAGENT_API_URL"); baseURL != "" {
		config.Kagent.BaseURL = baseURL
	}
	// Also support legacy KAGENT_BASE_URL for backward compatibility
	if baseURL := os.Getenv("KAGENT_BASE_URL"); baseURL != "" {
		config.Kagent.BaseURL = baseURL
	}
	if apiKey := os.Getenv("KAGENT_API_KEY"); apiKey != "" {
		config.Kagent.APIKey = apiKey
	}

	// Load from file if specified
	if configFile != "" {
		// Validate and sanitize the config file path
		safePath, err := validateConfigPath(configFile)
		if err != nil {
			return nil, fmt.Errorf("invalid config file path: %w", err)
		}

		// #nosec G304 - Path is validated above to prevent path traversal
		data, err := os.ReadFile(safePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Kagent.BaseURL == "" {
		return fmt.Errorf("kagent.baseUrl is required")
	}

	if c.Kagent.APIKey == "" {
		return fmt.Errorf("kagent.apiKey is required")
	}

	if c.Controller.EventDeduplicationTimeout <= 0 {
		return fmt.Errorf("controller.eventDeduplicationTimeout must be positive")
	}

	if c.Controller.EventCleanupInterval <= 0 {
		return fmt.Errorf("controller.eventCleanupInterval must be positive")
	}

	return nil
}
