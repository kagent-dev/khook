package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"github.com/kagent/hook-controller/internal/interfaces"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
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
		BaseURL: "http://kagent-controller.kagent.svc.local:8083",
		UserID:  "admin@kagent.dev",
		Timeout: 120 * time.Second,
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

	// Compose message from prompt and event context
	text := request.Prompt
	if request.Context != nil {
		if ns, ok := request.Context["namespace"].(string); ok && ns != "" {
			text += fmt.Sprintf("\nNamespace: %s", ns)
		}
		if reason, ok := request.Context["reason"].(string); ok && reason != "" {
			text += fmt.Sprintf("\nReason: %s", reason)
		}
		if msg, ok := request.Context["message"].(string); ok && msg != "" {
			text += fmt.Sprintf("\nMessage: %s", msg)
		}
	}

	// Use A2A SendMessage (POST). Provide a clean base URL with trailing slash; no query params.
	a2aURL := fmt.Sprintf("%s/api/a2a/%s/", c.config.BaseURL, request.AgentId)
	a2a, err := a2aclient.NewA2AClient(a2aURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create A2A client: %w", err)
	}

	sendCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	sessionID := sessionResp.Data.ID

	// Retry SendMessage with exponential backoff
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, lastErr = a2a.SendMessage(sendCtx, protocol.SendMessageParams{
			Message: protocol.Message{
				Role:      protocol.MessageRoleUser,
				ContextID: &sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(text)},
			},
		})
		if lastErr == nil {
			break
		}
		delay := time.Duration(1<<attempt) * time.Second
		c.logger.V(1).Info("A2A SendMessage failed, will retry",
			"attempt", attempt+1,
			"delay", delay,
			"error", lastErr.Error())
		time.Sleep(delay)
	}
	if lastErr != nil {
		c.logger.Error(lastErr, "Failed to send message to agent",
			"agentId", request.AgentId,
			"sessionId", sessionResp.Data.ID)
		return nil, fmt.Errorf("failed to send A2A message after retries: %w", lastErr)
	}

	c.logger.Info("Agent accepted message via A2A",
		"agentId", request.AgentId,
		"sessionId", sessionID)

	response := &interfaces.AgentResponse{
		Success:   true,
		Message:   fmt.Sprintf("Session created successfully: %s", sessionNameStr),
		RequestId: sessionID,
	}

	c.logger.Info("Agent call completed successfully",
		"agentId", request.AgentId,
		"sessionId", response.RequestId)

	return response, nil
}
