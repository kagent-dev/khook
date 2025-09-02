package client

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/antweiss/khook/internal/interfaces"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Config holds the configuration for the Kagent API client
type Config struct {
	BaseURL string
	UserID  string
	Timeout time.Duration
}

// Validate validates the client configuration
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate BaseURL
	if c.BaseURL == "" {
		return fmt.Errorf("BaseURL cannot be empty")
	}

	if len(c.BaseURL) > 2048 {
		return fmt.Errorf("BaseURL too long: %d characters (max 2048)", len(c.BaseURL))
	}

	// Basic URL validation
	if !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
		return fmt.Errorf("BaseURL must start with http:// or https://")
	}

	// Validate UserID
	if c.UserID == "" {
		return fmt.Errorf("UserID cannot be empty")
	}

	if len(c.UserID) > 100 {
		return fmt.Errorf("UserID too long: %d characters (max 100)", len(c.UserID))
	}

	// Validate UserID format (basic email or identifier format)
	if strings.Contains(c.UserID, "@") {
		// If it looks like an email, validate email format
		if !strings.Contains(c.UserID, ".") || len(strings.Split(c.UserID, "@")) != 2 {
			return fmt.Errorf("UserID appears to be an email but has invalid format")
		}
	} else {
		// For non-email user IDs, allow alphanumeric, hyphens, underscores, dots
		for _, r := range c.UserID {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
				return fmt.Errorf("UserID contains invalid character '%c', only alphanumeric, hyphens, underscores, and dots allowed", r)
			}
		}
	}

	// Validate Timeout
	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be positive, got %v", c.Timeout)
	}

	if c.Timeout > 300*time.Second {
		return fmt.Errorf("Timeout too long: %v (max 300s)", c.Timeout)
	}

	return nil
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
	res, err := a2a.SendMessage(sendCtx, protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      protocol.MessageRoleUser,
			ContextID: &sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(text)},
		},
	})
	if err != nil {
		c.logger.Error(err, "Failed to send message to agent",
			"agentId", request.AgentId,
			"sessionId", sessionResp.Data.ID)
		return nil, fmt.Errorf("failed to send A2A message: %w", err)
	}

	// Best-effort check whether a Task was returned (per A2A Life of a Task)
	isTask := false
	if res != nil {
		rv := reflect.ValueOf(res)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.IsValid() {
			if f := rv.FieldByName("Task"); f.IsValid() && !f.IsZero() {
				isTask = true
			}
		}
	}

	c.logger.Info("Agent accepted message via A2A",
		"agentId", request.AgentId,
		"sessionId", sessionID,
		"taskReturned", isTask)

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
