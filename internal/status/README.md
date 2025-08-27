# Status Manager

The Status Manager package provides comprehensive status management and reporting functionality for the KAgent Hook Controller. It handles updating Hook CRD status, emitting Kubernetes events for audit trails, and providing structured logging for all controller operations.

## Features

- **Hook Status Updates**: Updates Hook CRD status with active events and timestamps
- **Event Recording**: Emits Kubernetes events for audit trails and monitoring
- **Structured Logging**: Provides consistent, structured logging across all operations
- **Error Handling**: Comprehensive error recording and reporting
- **Event Lifecycle Tracking**: Tracks events from firing to resolution

## Usage

### Creating a Status Manager

```go
import (
    "k8s.io/client-go/tools/record"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "github.com/kagent/hook-controller/internal/status"
)

// Create a new status manager
manager := status.NewManager(kubernetesClient, eventRecorder)
```

### Updating Hook Status

```go
// Update hook status with active events
activeEvents := []interfaces.ActiveEvent{
    {
        EventType:    "pod-restart",
        ResourceName: "my-pod",
        FirstSeen:    time.Now().Add(-5 * time.Minute),
        LastSeen:     time.Now(),
        Status:       "firing",
    },
}

err := manager.UpdateHookStatus(ctx, hook, activeEvents)
if err != nil {
    log.Error(err, "Failed to update hook status")
}
```

### Recording Events

#### Event Firing
```go
err := manager.RecordEventFiring(ctx, hook, event, "agent-id")
```

#### Event Resolution
```go
err := manager.RecordEventResolved(ctx, hook, "pod-restart", "my-pod")
```

#### Agent Call Success
```go
err := manager.RecordAgentCallSuccess(ctx, hook, event, "agent-id", "request-123")
```

#### Agent Call Failure
```go
err := manager.RecordAgentCallFailure(ctx, hook, event, "agent-id", err)
```

#### Processing Errors
```go
err := manager.RecordError(ctx, hook, event, processingError, "agent-id")
```

#### Duplicate Events
```go
err := manager.RecordDuplicateEvent(ctx, hook, event)
```

### Controller Lifecycle Logging

```go
// Log controller startup
config := map[string]interface{}{
    "logLevel": "info",
    "port":     8080,
}
manager.LogControllerStartup(ctx, "v1.0.0", config)

// Log controller shutdown
manager.LogControllerShutdown(ctx, "graceful shutdown")
```

## Event Types

The Status Manager emits the following Kubernetes event types:

### Normal Events
- `EventFiring`: When an event starts firing
- `EventResolved`: When an event is resolved after timeout
- `AgentCallSuccess`: When an agent call succeeds
- `DuplicateEventIgnored`: When a duplicate event is ignored

### Warning Events
- `EventProcessingError`: When event processing fails
- `AgentCallFailure`: When an agent call fails

## Logging

The Status Manager uses structured logging with the following fields:

- `hook`: Hook name
- `namespace`: Hook namespace
- `eventType`: Type of Kubernetes event
- `resourceName`: Name of the affected resource
- `agentId`: ID of the called agent
- `requestId`: Request ID for agent calls
- `eventTimestamp`: Timestamp of the original event

## Error Handling

All methods return errors that can be handled by the caller. The Status Manager also:

- Logs all errors with appropriate context
- Emits Kubernetes warning events for failures
- Continues processing other operations if one fails
- Provides detailed error messages for troubleshooting

## Testing

The package includes comprehensive unit tests covering:

- Status updates with various event configurations
- Event recording for all event types
- Error scenarios and edge cases
- Kubernetes client interactions
- Event recorder functionality

Run tests with:
```bash
go test ./internal/status/... -v
```