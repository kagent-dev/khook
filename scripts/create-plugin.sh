#!/bin/bash

# Plugin Scaffolding Script for KHook
# Usage: ./scripts/create-plugin.sh <plugin-name>

set -e

PLUGIN_NAME="$1"
if [ -z "$PLUGIN_NAME" ]; then
    echo "Usage: $0 <plugin-name>"
    echo "Example: $0 webhook"
    exit 1
fi

# Validate plugin name
if [[ ! "$PLUGIN_NAME" =~ ^[a-z][a-z0-9-]*$ ]]; then
    echo "Error: Plugin name must start with a letter and contain only lowercase letters, numbers, and hyphens"
    exit 1
fi

PLUGIN_DIR="internal/plugin/$PLUGIN_NAME"
PLUGIN_PACKAGE="$(echo $PLUGIN_NAME | tr '-' '_')"
# Convert plugin name to PascalCase (compatible with older bash versions)
PLUGIN_STRUCT="$(echo $PLUGIN_NAME | awk -F'-' '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)} 1' OFS='')EventSource"
PLUGIN_CAPITALIZED="$(echo $PLUGIN_NAME | awk -F'-' '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)} 1' OFS='')"

echo "Creating plugin: $PLUGIN_NAME"
echo "Directory: $PLUGIN_DIR"
echo "Package: $PLUGIN_PACKAGE"
echo "Struct: $PLUGIN_STRUCT"

# Create plugin directory
mkdir -p "$PLUGIN_DIR"

# Create main plugin file
cat > "$PLUGIN_DIR/${PLUGIN_NAME}.go" << EOF
package $PLUGIN_PACKAGE

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/internal/plugin"
)

const (
	PluginName    = "$PLUGIN_NAME"
	PluginVersion = "1.0.0"
)

// $PLUGIN_STRUCT implements the plugin.EventSource interface
type $PLUGIN_STRUCT struct {
	logger logr.Logger
	stopCh chan struct{}
	
	// Add your plugin-specific fields here
	// Example:
	// client   SomeClient
	// config   PluginConfig
}

// New$PLUGIN_STRUCT creates a new $PLUGIN_NAME event source
func New$PLUGIN_STRUCT() *$PLUGIN_STRUCT {
	return &$PLUGIN_STRUCT{
		logger: log.Log.WithName("$PLUGIN_NAME-event-source"),
		stopCh: make(chan struct{}),
	}
}

// Name returns the unique name of the event source
func (p *$PLUGIN_STRUCT) Name() string {
	return PluginName
}

// Version returns the version of the event source plugin
func (p *$PLUGIN_STRUCT) Version() string {
	return PluginVersion
}

// SupportedEventTypes returns a list of event types that this source can emit
func (p *$PLUGIN_STRUCT) SupportedEventTypes() []string {
	return []string{
		"$PLUGIN_NAME-event",
		"$PLUGIN_NAME-error",
		// Add your event types here
	}
}

// Initialize sets up the event source with its configuration
func (p *$PLUGIN_STRUCT) Initialize(ctx context.Context, config map[string]interface{}) error {
	p.logger.Info("Initializing $PLUGIN_NAME event source", "config", config)

	// TODO: Extract and validate configuration
	// Example:
	// if endpoint, ok := config["endpoint"]; ok {
	//     if endpointStr, ok := endpoint.(string); ok {
	//         p.endpoint = endpointStr
	//     } else {
	//         return fmt.Errorf("endpoint must be a string")
	//     }
	// } else {
	//     return fmt.Errorf("endpoint configuration is required")
	// }

	p.logger.Info("$PLUGIN_STRUCT initialized successfully")
	return nil
}

// Start begins watching for events and sends them to the provided channel
func (p *$PLUGIN_STRUCT) Start(ctx context.Context, eventCh chan<- plugin.Event) error {
	p.logger.Info("Starting $PLUGIN_NAME event source")

	// TODO: Implement your event watching logic
	// This is typically done in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error(fmt.Errorf("plugin panic: %v", r), "Plugin panicked")
			}
		}()

		// Example event loop
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				p.logger.Info("Context cancelled, stopping $PLUGIN_NAME event source")
				return
			case <-p.stopCh:
				p.logger.Info("Stop signal received, stopping $PLUGIN_NAME event source")
				return
			case <-ticker.C:
				// TODO: Replace with your actual event detection logic
				p.logger.V(1).Info("Checking for $PLUGIN_NAME events")
				
				// Example: Create and send an event
				if p.shouldCreateEvent() {
					event := plugin.NewEvent(
						"$PLUGIN_NAME-event",
						"example-resource",
						"",
						"ExampleReason",
						"Example event from $PLUGIN_NAME plugin",
						PluginName,
					).WithMetadata("timestamp", time.Now()).
						WithTag("source", "$PLUGIN_NAME")

					select {
					case eventCh <- *event:
						p.logger.Info("Event sent", "type", event.Type, "resource", event.ResourceName)
					case <-ctx.Done():
						return
					case <-p.stopCh:
						return
					default:
						p.logger.Error(nil, "Event channel full, dropping event")
					}
				}
			}
		}
	}()

	return nil
}

// Stop gracefully shuts down the event source
func (p *$PLUGIN_STRUCT) Stop() error {
	p.logger.Info("Stopping $PLUGIN_NAME event source")
	close(p.stopCh)

	// TODO: Clean up resources
	// Example:
	// if p.client != nil {
	//     p.client.Close()
	// }

	return nil
}

// shouldCreateEvent is an example method - replace with your logic
func (p *$PLUGIN_STRUCT) shouldCreateEvent() bool {
	// TODO: Implement your event detection logic
	// This is just an example that creates events occasionally
	return time.Now().Unix()%10 == 0
}

// TODO: Add any additional helper methods your plugin needs
EOF

# Create test file
cat > "$PLUGIN_DIR/${PLUGIN_NAME}_test.go" << EOF
package $PLUGIN_PACKAGE

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/khook/internal/plugin"
)

func TestNew$PLUGIN_STRUCT(t *testing.T) {
	source := New$PLUGIN_STRUCT()
	require.NotNil(t, source)
	assert.Equal(t, PluginName, source.Name())
	assert.Equal(t, PluginVersion, source.Version())
	assert.NotNil(t, source.stopCh)
}

func Test${PLUGIN_STRUCT}SupportedEventTypes(t *testing.T) {
	source := New$PLUGIN_STRUCT()
	expectedTypes := []string{"$PLUGIN_NAME-event", "$PLUGIN_NAME-error"}
	assert.ElementsMatch(t, expectedTypes, source.SupportedEventTypes())
}

func Test${PLUGIN_STRUCT}Initialize(t *testing.T) {
	source := New$PLUGIN_STRUCT()
	ctx := context.Background()

	tests := []struct {
		name      string
		config    map[string]interface{}
		expectErr bool
	}{
		{
			name:      "empty config",
			config:    map[string]interface{}{},
			expectErr: false, // Change to true if configuration is required
		},
		{
			name: "valid config",
			config: map[string]interface{}{
				// Add your configuration parameters here
				// "endpoint": "http://example.com",
			},
			expectErr: false,
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := source.Initialize(ctx, tt.config)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test${PLUGIN_STRUCT}StartStop(t *testing.T) {
	source := New$PLUGIN_STRUCT()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eventCh := make(chan plugin.Event, 10)

	// Initialize
	err := source.Initialize(ctx, map[string]interface{}{})
	require.NoError(t, err)

	// Start
	err = source.Start(ctx, eventCh)
	require.NoError(t, err)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop
	err = source.Stop()
	assert.NoError(t, err)

	// Verify stop channel is closed
	select {
	case _, ok := <-source.stopCh:
		assert.False(t, ok, "stopCh should be closed")
	default:
		t.Fatal("stopCh should be closed")
	}
}

func Test${PLUGIN_STRUCT}EventGeneration(t *testing.T) {
	source := New$PLUGIN_STRUCT()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh := make(chan plugin.Event, 10)

	// Initialize and start
	err := source.Initialize(ctx, map[string]interface{}{})
	require.NoError(t, err)

	err = source.Start(ctx, eventCh)
	require.NoError(t, err)

	// TODO: Trigger event generation or wait for events
	// This depends on your plugin's event generation logic

	// Wait for events (adjust timeout as needed)
	select {
	case event := <-eventCh:
		assert.Equal(t, PluginName, event.Source)
		assert.True(t, event.IsValid())
		t.Logf("Received event: %+v", event)
	case <-time.After(1 * time.Second):
		t.Log("No events received (this might be expected depending on your plugin)")
	}

	// Stop
	err = source.Stop()
	assert.NoError(t, err)
}

// TODO: Add more specific tests for your plugin's functionality
EOF

# Create example configuration files
mkdir -p "examples/$PLUGIN_NAME"

cat > "examples/$PLUGIN_NAME/event-mappings.yaml" << EOF
# Event mappings for $PLUGIN_NAME plugin
mappings:
  - eventSource: $PLUGIN_NAME
    eventType: $PLUGIN_NAME-event
    internalType: ${PLUGIN_CAPITALIZED}Event
    description: "Event from $PLUGIN_NAME plugin"
    severity: info
    tags:
      category: $PLUGIN_NAME
      impact: notification
    enabled: true

  - eventSource: $PLUGIN_NAME
    eventType: $PLUGIN_NAME-error
    internalType: ${PLUGIN_CAPITALIZED}Error
    description: "Error from $PLUGIN_NAME plugin"
    severity: error
    tags:
      category: $PLUGIN_NAME
      impact: system-error
    enabled: true
EOF

cat > "examples/$PLUGIN_NAME/plugins.yaml" << EOF
# Plugin configuration for $PLUGIN_NAME
plugins:
  - name: $PLUGIN_NAME
    enabled: true
    config:
      # Add your plugin-specific configuration here
      # endpoint: "http://example.com"
      # timeout: "30s"
      # retries: 3
EOF

cat > "examples/$PLUGIN_NAME/hook.yaml" << EOF
# Example Hook configuration for $PLUGIN_NAME plugin
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: $PLUGIN_NAME-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: "${PLUGIN_CAPITALIZED}Event"
    agentRef:
      name: "$PLUGIN_NAME-handler"
    prompt: |
      Event received from $PLUGIN_NAME plugin:
      
      Resource: {{.ResourceName}}
      Message: {{.Message}}
      Timestamp: {{.Timestamp}}
      
      Please process this $PLUGIN_NAME event and take appropriate action.
      
      Metadata: {{range \$key, \$value := .Metadata}}
      - {{\$key}}: {{\$value}}{{end}}

  - eventType: "${PLUGIN_CAPITALIZED}Error"
    agentRef:
      name: "$PLUGIN_NAME-error-handler"
    prompt: |
      ERROR: $PLUGIN_NAME plugin encountered an error:
      
      Resource: {{.ResourceName}}
      Error: {{.Message}}
      Timestamp: {{.Timestamp}}
      
      Please investigate and resolve this $PLUGIN_NAME error.
EOF

# Create README for the plugin
cat > "$PLUGIN_DIR/README.md" << EOF
# $PLUGIN_NAME Plugin

This plugin provides event source integration for $PLUGIN_NAME.

## Overview

TODO: Describe what this plugin does and what events it monitors.

## Configuration

### Plugin Configuration

\`\`\`yaml
plugins:
  - name: $PLUGIN_NAME
    enabled: true
    config:
      # Add configuration options here
\`\`\`

### Event Mappings

\`\`\`yaml
mappings:
  - eventSource: $PLUGIN_NAME
    eventType: $PLUGIN_NAME-event
    internalType: ${PLUGIN_CAPITALIZED}Event
    severity: info
    enabled: true
\`\`\`

## Supported Event Types

- \`$PLUGIN_NAME-event\`: TODO: Describe this event type
- \`$PLUGIN_NAME-error\`: TODO: Describe this event type

## Development

### Running Tests

\`\`\`bash
go test ./internal/plugin/$PLUGIN_NAME/... -v
\`\`\`

### Integration Testing

\`\`\`bash
# TODO: Add integration test instructions
\`\`\`

## Examples

See the \`examples/$PLUGIN_NAME/\` directory for configuration examples.

## TODO

- [ ] Implement actual event detection logic
- [ ] Add comprehensive error handling
- [ ] Add configuration validation
- [ ] Add integration tests
- [ ] Add performance optimizations
- [ ] Add documentation
EOF

echo ""
echo "✅ Plugin scaffolding created successfully!"
echo ""
echo "📁 Files created:"
echo "   - $PLUGIN_DIR/${PLUGIN_NAME}.go (main plugin implementation)"
echo "   - $PLUGIN_DIR/${PLUGIN_NAME}_test.go (unit tests)"
echo "   - $PLUGIN_DIR/README.md (plugin documentation)"
echo "   - examples/$PLUGIN_NAME/event-mappings.yaml (event mapping example)"
echo "   - examples/$PLUGIN_NAME/plugins.yaml (plugin config example)"
echo "   - examples/$PLUGIN_NAME/hook.yaml (Hook resource example)"
echo ""
echo "🔧 Next steps:"
echo "   1. Edit $PLUGIN_DIR/${PLUGIN_NAME}.go to implement your plugin logic"
echo "   2. Update the configuration handling in Initialize()"
echo "   3. Implement event detection logic in Start()"
echo "   4. Add proper resource cleanup in Stop()"
echo "   5. Update the unit tests"
echo "   6. Test your plugin: go test ./internal/plugin/$PLUGIN_NAME/... -v"
echo "   7. Register your plugin in internal/workflow/plugin_workflow_manager.go"
echo ""
echo "📖 For detailed guidance, see: docs/plugin-development.md"