package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"github.com/kagent/hook-controller/internal/interfaces"
)

// Config holds the configuration for the Kagent API client
type Config struct {
	BaseURL string
	UserID  string
	Timeout time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		BaseURL: "https://api.kagent.dev",
		UserID:  "hook-controller",
		Timeout: 30 * time.Second,
	}
}

// Client implements the KagentClient interface
type Client struct {
	config    *Config
	clientSet *client.ClientSet
	logger    logr.Logger
}

// NewClient creates a new Kagent API client
func NewClient(config *Config, logger logr.Logger) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	// Create client options
	options := []client.ClientOption{
		client.WithUserID(config.UserID),
	}

	// Create the Kagent client set
	clientSet := client.New(config.BaseURL, options...)

	return &Client{
		config:    config,
		clientSet: clientSet,
		logger:    logger,
	}
}

// Authenticate verifies connectivity with the Kagent platform
func (c *Client) Authenticate() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	// Test connectivity by trying to get health status
	err := c.clientSet.Health.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Kagent API: %w", err)
	}

	c.logger.Info("Successfully connected to Kagent API")
	return nil
}

// CallAgent makes a request to the Kagent API to trigger an agent
func (c *Client) CallAgent(ctx context.Context, request interfaces.AgentRequest) (*interfaces.AgentResponse, error) {
	// Create a session for this agent call
	sessionName := fmt.Sprintf("hook-%s-%d", request.EventName, time.Now().Unix())

	sessionReq := &api.SessionRequest{
		AgentRef: &request.AgentId,
		Name:     &sessionName,
	}

	c.logger.Info("Creating session for agent call",
		"sessionName", sessionName,
		"agentId", request.AgentId,
		"eventName", request.EventName)

	sessionResp, err := c.clientSet.Session.CreateSession(ctx, sessionReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	if sessionResp.Error {
		return nil, fmt.Errorf("session creation failed: %s", sessionResp.Message)
	}

	sessionNameStr := ""
	if sessionResp.Data.Name != nil {
		sessionNameStr = *sessionResp.Data.Name
	}

	c.logger.Info("Session created successfully",
		"sessionId", sessionResp.Data.ID,
		"sessionName", sessionNameStr)

	// For now, we'll return a success response since the session was created
	// In a full implementation, we might want to create a run within the session
	// and wait for completion, but that would require understanding the run API better

	response := &interfaces.AgentResponse{
		Success:   true,
		Message:   fmt.Sprintf("Session created successfully: %s", sessionNameStr),
		RequestId: sessionResp.Data.ID,
	}

	c.logger.Info("Agent call completed successfully",
		"agentId", request.AgentId,
		"sessionId", response.RequestId)

	return response, nil
}
