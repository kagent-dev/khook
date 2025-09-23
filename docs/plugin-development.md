# Plugin Development Guide

This guide provides comprehensive instructions for developing custom event source plugins for KHook.

## Table of Contents

- [Overview](#overview)
- [Plugin Interface](#plugin-interface)
- [Development Setup](#development-setup)
- [Step-by-Step Tutorial](#step-by-step-tutorial)
- [Configuration](#configuration)
- [Testing](#testing)
- [Best Practices](#best-practices)
- [Deployment](#deployment)
- [Troubleshooting](#troubleshooting)

## Overview

KHook's plugin architecture allows you to create custom event sources that integrate seamlessly with the existing event processing pipeline. Plugins implement the `EventSource` interface and can be either:

- **Built-in plugins**: Compiled directly into the KHook binary
- **Dynamic plugins**: Loaded at runtime from `.so` files (future feature)

## Plugin Interface

All event source plugins must implement the `EventSource` interface:

```go
type EventSource interface {
    // Name returns the unique name of the event source
    Name() string

    // Version returns the version of the event source plugin
    Version() string

    // SupportedEventTypes returns a list of event types that this source can emit
    SupportedEventTypes() []string

    // Initialize sets up the event source with its configuration
    Initialize(ctx context.Context, config map[string]interface{}) error

    // Start begins watching for events and sends them to the provided channel
    Start(ctx context.Context, eventCh chan<- Event) error

    // Stop gracefully shuts down the event source
    Stop() error
}
```

### Event Structure

Events must conform to the unified `Event` structure:

```go
type Event struct {
    Type         string                 `json:"type"`         // Event type (e.g., "webhook-received")
    ResourceName string                 `json:"resourceName"` // Resource identifier
    Timestamp    time.Time              `json:"timestamp"`    // When the event occurred
    Namespace    string                 `json:"namespace,omitempty"` // Namespace (if applicable)
    Reason       string                 `json:"reason,omitempty"`    // Additional context
    Message      string                 `json:"message"`      // Detailed event message
    Source       string                 `json:"source"`       // Plugin name
    Metadata     map[string]interface{} `json:"metadata,omitempty"`  // Source-specific data
    Tags         map[string]string      `json:"tags,omitempty"`      // Categorization tags
}
```

## Development Setup

1. **Clone the KHook repository**:
   ```bash
   git clone https://github.com/kagent-dev/khook.git
   cd khook
   ```

2. **Set up Go development environment**:
   ```bash
   go mod tidy
   ```

3. **Run existing tests to verify setup**:
   ```bash
   go test ./internal/plugin/... -v
   ```

## Step-by-Step Tutorial

Let's create a simple webhook event source plugin as an example.

### Step 1: Create Plugin Directory

```bash
mkdir -p internal/plugin/webhook
```

### Step 2: Implement the EventSource Interface

Create `internal/plugin/webhook/webhook.go`:

```go
package webhook

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/go-logr/logr"
    "sigs.k8s.io/controller-runtime/pkg/log"

    "github.com/kagent-dev/khook/internal/plugin"
)

const (
    PluginName    = "webhook"
    PluginVersion = "1.0.0"
)

// WebhookEventSource implements the plugin.EventSource interface
type WebhookEventSource struct {
    server   *http.Server
    logger   logr.Logger
    eventCh  chan<- plugin.Event
    stopCh   chan struct{}
    port     int
    path     string
}

// NewWebhookEventSource creates a new webhook event source
func NewWebhookEventSource() *WebhookEventSource {
    return &WebhookEventSource{
        logger: log.Log.WithName("webhook-event-source"),
        stopCh: make(chan struct{}),
    }
}

// Name returns the unique name of the event source
func (w *WebhookEventSource) Name() string {
    return PluginName
}

// Version returns the version of the event source plugin
func (w *WebhookEventSource) Version() string {
    return PluginVersion
}

// SupportedEventTypes returns a list of event types that this source can emit
func (w *WebhookEventSource) SupportedEventTypes() []string {
    return []string{
        "webhook-received",
        "webhook-error",
    }
}

// Initialize sets up the event source with its configuration
func (w *WebhookEventSource) Initialize(ctx context.Context, config map[string]interface{}) error {
    w.logger.Info("Initializing webhook event source", "config", config)

    // Extract port from config
    if portVal, ok := config["port"]; ok {
        if port, ok := portVal.(int); ok {
            w.port = port
        } else {
            return fmt.Errorf("port must be an integer")
        }
    } else {
        w.port = 8090 // Default port
    }

    // Extract path from config
    if pathVal, ok := config["path"]; ok {
        if path, ok := pathVal.(string); ok {
            w.path = path
        } else {
            return fmt.Errorf("path must be a string")
        }
    } else {
        w.path = "/webhook" // Default path
    }

    w.logger.Info("Webhook event source initialized", "port", w.port, "path", w.path)
    return nil
}

// Start begins watching for events and sends them to the provided channel
func (w *WebhookEventSource) Start(ctx context.Context, eventCh chan<- plugin.Event) error {
    w.logger.Info("Starting webhook event source")
    w.eventCh = eventCh

    // Set up HTTP server
    mux := http.NewServeMux()
    mux.HandleFunc(w.path, w.handleWebhook)

    w.server = &http.Server{
        Addr:    fmt.Sprintf(":%d", w.port),
        Handler: mux,
    }

    // Start server in goroutine
    go func() {
        if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            w.logger.Error(err, "Webhook server failed")
            // Send error event
            errorEvent := plugin.NewEvent(
                "webhook-error",
                "webhook-server",
                "",
                "ServerError",
                fmt.Sprintf("Webhook server failed: %v", err),
                PluginName,
            )
            select {
            case w.eventCh <- *errorEvent:
            case <-ctx.Done():
            case <-w.stopCh:
            }
        }
    }()

    w.logger.Info("Webhook server started", "address", w.server.Addr, "path", w.path)
    return nil
}

// Stop gracefully shuts down the event source
func (w *WebhookEventSource) Stop() error {
    w.logger.Info("Stopping webhook event source")
    close(w.stopCh)

    if w.server != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        return w.server.Shutdown(ctx)
    }
    return nil
}

// handleWebhook processes incoming webhook requests
func (w *WebhookEventSource) handleWebhook(rw http.ResponseWriter, req *http.Request) {
    if req.Method != http.MethodPost {
        http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Parse request body
    var payload map[string]interface{}
    if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
        w.logger.Error(err, "Failed to parse webhook payload")
        http.Error(rw, "Invalid JSON", http.StatusBadRequest)
        return
    }

    // Extract resource name from payload
    resourceName := "unknown"
    if name, ok := payload["resource"]; ok {
        if nameStr, ok := name.(string); ok {
            resourceName = nameStr
        }
    }

    // Extract message from payload
    message := "Webhook received"
    if msg, ok := payload["message"]; ok {
        if msgStr, ok := msg.(string); ok {
            message = msgStr
        }
    }

    // Create event
    event := plugin.NewEvent(
        "webhook-received",
        resourceName,
        "",
        "WebhookReceived",
        message,
        PluginName,
    ).WithMetadata("headers", req.Header).
        WithMetadata("payload", payload).
        WithTag("source", "webhook").
        WithTag("method", req.Method)

    // Send event
    select {
    case w.eventCh <- *event:
        w.logger.Info("Webhook event sent", "resource", resourceName)
        rw.WriteHeader(http.StatusOK)
        json.NewEncoder(rw).Encode(map[string]string{"status": "received"})
    case <-w.stopCh:
        http.Error(rw, "Service shutting down", http.StatusServiceUnavailable)
    default:
        w.logger.Error(nil, "Event channel full, dropping webhook event")
        http.Error(rw, "Event processing overloaded", http.StatusServiceUnavailable)
    }
}
```

### Step 3: Create Unit Tests

Create `internal/plugin/webhook/webhook_test.go`:

```go
package webhook

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/kagent-dev/khook/internal/plugin"
)

func TestNewWebhookEventSource(t *testing.T) {
    source := NewWebhookEventSource()
    require.NotNil(t, source)
    assert.Equal(t, PluginName, source.Name())
    assert.Equal(t, PluginVersion, source.Version())
    assert.NotNil(t, source.stopCh)
}

func TestWebhookEventSourceSupportedEventTypes(t *testing.T) {
    source := NewWebhookEventSource()
    expectedTypes := []string{"webhook-received", "webhook-error"}
    assert.ElementsMatch(t, expectedTypes, source.SupportedEventTypes())
}

func TestWebhookEventSourceInitialize(t *testing.T) {
    source := NewWebhookEventSource()
    ctx := context.Background()

    tests := []struct {
        name      string
        config    map[string]interface{}
        expectErr bool
        expectedPort int
        expectedPath string
    }{
        {
            name: "valid config",
            config: map[string]interface{}{
                "port": 8091,
                "path": "/test-webhook",
            },
            expectErr: false,
            expectedPort: 8091,
            expectedPath: "/test-webhook",
        },
        {
            name: "default values",
            config: map[string]interface{}{},
            expectErr: false,
            expectedPort: 8090,
            expectedPath: "/webhook",
        },
        {
            name: "invalid port type",
            config: map[string]interface{}{
                "port": "invalid",
            },
            expectErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := source.Initialize(ctx, tt.config)
            if tt.expectErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expectedPort, source.port)
                assert.Equal(t, tt.expectedPath, source.path)
            }
        })
    }
}

func TestWebhookEventSourceStartStop(t *testing.T) {
    source := NewWebhookEventSource()
    ctx := context.Background()
    eventCh := make(chan plugin.Event, 10)

    // Initialize
    config := map[string]interface{}{
        "port": 8092,
        "path": "/test",
    }
    err := source.Initialize(ctx, config)
    require.NoError(t, err)

    // Start
    err = source.Start(ctx, eventCh)
    require.NoError(t, err)

    // Give server time to start
    time.Sleep(100 * time.Millisecond)

    // Test webhook endpoint
    payload := map[string]interface{}{
        "resource": "test-resource",
        "message":  "test message",
    }
    payloadBytes, _ := json.Marshal(payload)

    resp, err := http.Post("http://localhost:8092/test", "application/json", bytes.NewBuffer(payloadBytes))
    require.NoError(t, err)
    assert.Equal(t, http.StatusOK, resp.StatusCode)
    resp.Body.Close()

    // Check event was received
    select {
    case event := <-eventCh:
        assert.Equal(t, "webhook-received", event.Type)
        assert.Equal(t, "test-resource", event.ResourceName)
        assert.Equal(t, "test message", event.Message)
        assert.Equal(t, PluginName, event.Source)
    case <-time.After(1 * time.Second):
        t.Fatal("Expected event not received")
    }

    // Stop
    err = source.Stop()
    assert.NoError(t, err)
}
```

### Step 4: Register the Plugin

Add the plugin to the main controller by updating `internal/workflow/plugin_workflow_manager.go`:

```go
// Add import
import (
    webhookplugin "github.com/kagent-dev/khook/internal/plugin/webhook"
)

// In registerBuiltinPlugins method, add:
func (pwm *PluginWorkflowManager) registerBuiltinPlugins(ctx context.Context) error {
    // ... existing Kubernetes plugin registration ...

    // Register webhook plugin
    webhookSource := webhookplugin.NewWebhookEventSource()
    webhookMetadata := &plugin.PluginMetadata{
        Name:        webhookSource.Name(),
        Version:     webhookSource.Version(),
        EventTypes:  webhookSource.SupportedEventTypes(),
        Description: "Built-in webhook event source plugin",
        Path:        "built-in",
    }

    webhookLoadedPlugin := &plugin.LoadedPlugin{
        Metadata:    webhookMetadata,
        EventSource: webhookSource,
        Plugin:      nil,
        Active:      false,
    }

    if err := pwm.pluginManager.RegisterBuiltinPlugin("webhook", webhookLoadedPlugin); err != nil {
        return fmt.Errorf("failed to register webhook plugin: %w", err)
    }

    return nil
}
```

## Configuration

### Event Mappings

Add event mappings for your plugin in `config/event-mappings.yaml`:

```yaml
mappings:
  - eventSource: webhook
    eventType: webhook-received
    internalType: WebhookReceived
    description: "Webhook event received"
    severity: info
    tags:
      category: webhook
      impact: notification
    enabled: true

  - eventSource: webhook
    eventType: webhook-error
    internalType: WebhookError
    description: "Webhook processing error"
    severity: error
    tags:
      category: webhook
      impact: system-error
    enabled: true
```

### Plugin Configuration

Configure your plugin in `config/plugins.yaml`:

```yaml
plugins:
  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/webhook"
```

## Testing

### Unit Tests

Run plugin-specific tests:

```bash
go test ./internal/plugin/webhook/... -v
```

### Integration Tests

Create integration tests in `test/webhook_integration_test.go`:

```go
package test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/kagent-dev/khook/internal/plugin"
    webhookplugin "github.com/kagent-dev/khook/internal/plugin/webhook"
)

func TestWebhookPluginIntegration(t *testing.T) {
    // Create plugin manager
    eventCh := make(chan plugin.Event, 10)
    manager := plugin.NewManager(logr.Discard(), eventCh)

    // Register webhook plugin
    source := webhookplugin.NewWebhookEventSource()
    metadata := &plugin.PluginMetadata{
        Name:       source.Name(),
        Version:    source.Version(),
        EventTypes: source.SupportedEventTypes(),
    }

    err := manager.AddPlugin(metadata, source)
    require.NoError(t, err)

    // Initialize and start
    ctx := context.Background()
    config := map[string]interface{}{
        "port": 8093,
        "path": "/integration-test",
    }

    err = manager.InitializePlugin(ctx, "webhook", config)
    require.NoError(t, err)

    err = manager.StartPlugin(ctx, "webhook")
    require.NoError(t, err)

    // Test webhook functionality
    // ... (similar to unit test)

    // Cleanup
    err = manager.StopPlugin("webhook")
    assert.NoError(t, err)
}
```

### End-to-End Testing

Test with the full KHook system:

```bash
# Build and deploy
make build-deploy

# Create a Hook that uses webhook events
kubectl apply -f - <<EOF
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: webhook-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: "webhook-received"
    agentRef:
      name: "webhook-handler"
    prompt: |
      A webhook event was received: {{.Message}}
      Resource: {{.ResourceName}}
      Please process this webhook event.
EOF

# Send test webhook
curl -X POST http://localhost:8090/webhook \
  -H "Content-Type: application/json" \
  -d '{"resource": "test-resource", "message": "Hello from webhook!"}'
```

## Best Practices

### Error Handling

1. **Graceful Degradation**: Handle errors without crashing the entire system
2. **Logging**: Use structured logging with appropriate log levels
3. **Timeouts**: Implement reasonable timeouts for external operations
4. **Resource Cleanup**: Always clean up resources in the `Stop()` method

### Performance

1. **Non-blocking Operations**: Use goroutines for I/O operations
2. **Channel Management**: Don't block on event channel writes
3. **Resource Limits**: Implement connection pooling and rate limiting
4. **Memory Management**: Avoid memory leaks in long-running operations

### Security

1. **Input Validation**: Validate all configuration and input data
2. **Authentication**: Implement proper authentication for external sources
3. **TLS/SSL**: Use secure connections when possible
4. **Secrets Management**: Use Kubernetes secrets for sensitive data

### Configuration

1. **Validation**: Validate configuration during initialization
2. **Defaults**: Provide sensible defaults for optional parameters
3. **Documentation**: Document all configuration options
4. **Backward Compatibility**: Maintain compatibility when adding new options

## Deployment

### Built-in Plugins

Built-in plugins are compiled into the KHook binary and deployed automatically.

### Dynamic Plugins (Future)

Dynamic plugins will be loaded from `.so` files:

1. **Build Plugin**:
   ```bash
   go build -buildmode=plugin -o webhook.so ./internal/plugin/webhook/
   ```

2. **Deploy Plugin**:
   ```bash
   kubectl create configmap webhook-plugin --from-file=webhook.so
   ```

3. **Configure Plugin Loading**:
   ```yaml
   plugins:
     - name: webhook
       enabled: true
       path: "/plugins/webhook.so"
   ```

## Troubleshooting

### Common Issues

#### Plugin Not Loading

**Symptoms**: Plugin not appearing in logs or not processing events

**Solutions**:
1. Check plugin registration in workflow manager
2. Verify plugin implements all interface methods
3. Check for initialization errors in logs
4. Validate plugin configuration

#### Events Not Being Processed

**Symptoms**: Plugin receives events but they don't trigger hooks

**Solutions**:
1. Verify event mappings in `config/event-mappings.yaml`
2. Check event type matches Hook configuration
3. Ensure events are valid (use `Event.IsValid()`)
4. Check deduplication settings

#### Performance Issues

**Symptoms**: High CPU/memory usage, slow event processing

**Solutions**:
1. Implement connection pooling
2. Add rate limiting
3. Use buffered channels appropriately
4. Profile plugin performance

### Debug Mode

Enable debug logging:

```bash
kubectl set env deployment/khook -n kagent LOG_LEVEL=debug
```

### Plugin Status

Check plugin status via metrics endpoint:

```bash
kubectl port-forward -n kagent deployment/khook 8080:8080
curl http://localhost:8080/metrics | grep plugin
```

## Support

For plugin development support:

1. **Documentation**: Check the [API Reference](../README.md#api-reference)
2. **Examples**: See `internal/plugin/kubernetes/` for a complete example
3. **Issues**: Report bugs on [GitHub Issues](https://github.com/kagent-dev/khook/issues)
4. **Community**: Join discussions in [GitHub Discussions](https://github.com/kagent-dev/khook/discussions)

## Next Steps

After completing your plugin:

1. **Testing**: Run comprehensive tests
2. **Documentation**: Update plugin-specific documentation
3. **Examples**: Create usage examples
4. **Contributing**: Consider contributing back to the project

For more advanced topics, see:
- [Plugin Security Guidelines](plugin-security.md)
- [Performance Optimization](plugin-performance.md)
- [Dynamic Plugin Loading](dynamic-plugins.md)
