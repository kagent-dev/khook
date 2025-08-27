# Kagent Client

This package provides a client for interacting with the Kagent API platform using the official Kagent Go client library.

## Features

- **Official Client**: Uses the official Kagent Go client from `github.com/kagent-dev/kagent/go/pkg/client`
- **Session Management**: Creates sessions for agent interactions
- **Health Checks**: Verifies connectivity with the Kagent platform
- **Retry Logic**: Built-in retry logic from the official client
- **Error Handling**: Comprehensive error handling with proper HTTP status code handling
- **Configuration**: Flexible configuration via environment variables or direct config
- **Logging**: Structured logging using controller-runtime's logr interface
- **Testing**: Comprehensive unit tests

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/kagent/hook-controller/internal/client"
    "github.com/kagent/hook-controller/internal/interfaces"
    "github.com/kagent/hook-controller/internal/logging"
)

func main() {
    logger := logging.NewLogger("main")
    
    // Create client with custom configuration
    config := &client.Config{
        BaseURL: "https://api.kagent.dev",
        UserID:  "hook-controller",
        Timeout: 30 * time.Second,
    }
    
    client := client.NewClient(config, logger)
    
    // Test connectivity with the API
    if err := client.Authenticate(); err != nil {
        log.Fatal("Authentication failed:", err)
    }
    
    // Execute an agent by creating a session
    request := interfaces.AgentRequest{
        AgentId:      "my-agent-id",
        Prompt:       "Analyze this Kubernetes event",
        EventName:    "pod-restart",
        EventTime:    time.Now(),
        ResourceName: "my-pod",
        Context: map[string]interface{}{
            "namespace": "default",
            "reason":    "CrashLoopBackOff",
        },
    }
    
    response, err := client.CallAgent(context.Background(), request)
    if err != nil {
        log.Fatal("Agent call failed:", err)
    }
    
    log.Printf("Agent response: %+v", response)
}
```

### Environment Variable Configuration

```go
// Create client from environment variables
client, err := client.NewClientFromEnv(logger)
if err != nil {
    log.Fatal("Failed to create client:", err)
}
```

### Environment Variables

- `KAGENT_API_BASE_URL`: Base URL for the Kagent API (default: "https://api.kagent.dev")
- `KAGENT_USER_ID`: User ID for API requests (default: "hook-controller")
- `KAGENT_API_TIMEOUT`: Request timeout duration (default: "30s")

## API Integration

The client uses the official Kagent client library and interacts with:

- **Health endpoint**: For connectivity verification
- **Session management**: Creates sessions for agent interactions
- **Agent execution**: Through session creation with agent references

## Error Handling

The client provides comprehensive error handling:

- **Network errors**: Connection failures, timeouts
- **Server errors**: 5xx responses with automatic retry from the official client
- **Client errors**: 4xx responses (no retry)
- **Session creation errors**: Proper error propagation from the Kagent API

## Testing

Run the test suite:

```bash
go test ./internal/client/...
```

## Configuration Validation

The client validates configuration before use:

```go
config := &client.Config{
    BaseURL: "https://api.kagent.dev",
    UserID:  "hook-controller",
    Timeout: 30 * time.Second,
}

if err := client.ValidateConfig(config); err != nil {
    log.Fatal("Invalid configuration:", err)
}
```

## Official Client Integration

This implementation leverages the official Kagent client library:

```go
import kagentclient "github.com/kagent-dev/kagent/go/pkg/client"

// The client uses the official ClientSet
clientSet := kagentclient.New(baseURL, kagentclient.WithUserID(userID))
```

This ensures compatibility with the latest Kagent API and provides access to all official client features including proper error handling, retry logic, and API versioning.