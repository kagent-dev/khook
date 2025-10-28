# Flexible Event Sources - Design Document

## Overview

KHook has been enhanced to support diverse event sources beyond Kubernetes, including Redis, gRPC streams, IoT sensors, financial transactions, CI/CD pipelines, and more. This document describes the changes made to support this flexibility.

## Problem Statement

The original implementation assumed all events would be Kubernetes-related and required fields like `ResourceName`, `Namespace`, and `Reason`. This made it difficult to integrate events from other sources such as:
- Business application events (e.g., order processing, payment failures)
- IoT sensor data (e.g., temperature alerts, equipment failures)
- Financial transactions (e.g., fraud detection, payment processing)
- CI/CD pipeline events (e.g., build failures, deployments)
- Custom application events

## Solution

### 1. Flexible Event Structure

**Changes to `Event` struct** (`internal/interfaces/controller.go`):

```go
// Event represents an event from any source with relevant metadata
type Event struct {
    Type         string                 `json:"type"`
    ResourceName string                 `json:"resourceName"` // Optional: may be empty for non-resource events
    Timestamp    time.Time              `json:"timestamp"`
    Namespace    string                 `json:"namespace"`   // Optional: may be empty for non-Kubernetes events
    Reason       string                 `json:"reason"`      // Optional: may be empty
    Message      string                 `json:"message"`
    UID          string                 `json:"uid"`         // Optional: may be empty
    Metadata     map[string]interface{} `json:"metadata,omitempty"` // Flexible metadata for any event source
}
```

**Key Changes:**
- `Metadata` changed from `map[string]string` to `map[string]interface{}` to support any JSON-serializable data type
- Added documentation clarifying that `ResourceName`, `Namespace`, `Reason`, and `UID` are optional
- `Metadata` can now contain nested objects, arrays, numbers, booleans, etc.

### 2. Smart Event Deduplication

**Changes to deduplication manager** (`internal/deduplication/manager.go`):

The `eventKey` function now uses a smart fallback strategy:

```go
// Priority:
// 1. If UID is present, use Type:UID (best for unique identification)
// 2. Otherwise use Type:Namespace:ResourceName (for Kubernetes events)
// 3. If no ResourceName, use Type:Namespace (for namespace-scoped events)
// 4. If no Namespace, use just Type (for global events)
```

**Benefits:**
- Events with UIDs get perfect deduplication
- Kubernetes events without UIDs use the traditional namespace:resource key
- Non-Kubernetes events can rely on UID or event type alone
- Flexible enough for any event source

### 3. Flexible Template System

**Enhanced template expansion** (`internal/pipeline/processor.go`):

Templates now have access to:
- All standard event fields (Type, Message, Timestamp, etc.)
- Full metadata map: `{{.Metadata.customField}}`
- Individual metadata fields are also promoted to top-level template variables
- Nested metadata structures via dot notation

**Example:**
```yaml
prompt: |
  Transaction Alert!
  
  ID: {{.Metadata.transactionId}}
  Amount: ${{.Metadata.amount}}
  {{if .Metadata.riskScore}}
  Risk Score: {{.Metadata.riskScore}}/100
  {{end}}
```

### 4. Context Enrichment

**Agent request context** (`internal/pipeline/processor.go`):

All metadata fields are now passed to agents both:
- In the `metadata` map for structured access
- As top-level context fields for convenient access

This allows agents to easily access custom event data.

### 5. Logging Improvements

**Optional field logging** (across multiple files):

Logging now gracefully handles optional fields:
- Only logs `resourceName` if present
- Includes `uid` in logs when available
- Generic event messages that work for any event type

## Migration Guide

### For Kubernetes Events

No changes required! Kubernetes events work exactly as before:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: pod-monitor
spec:
  eventConfigurations:
  - eventType: "pod-restart"
    agentRef:
      name: diagnostic-agent
    prompt: "Pod {{.ResourceName}} restarted in {{.Namespace}}"
```

### For Custom Event Sources

#### 1. Define Your Event Source

Create an `ExternalEventSource` that sends events with custom metadata:

```go
event := interfaces.Event{
    Type:      "order-placed",
    Message:   "New order received",
    Timestamp: time.Now(),
    UID:       orderID, // Unique identifier for deduplication
    Metadata: map[string]interface{}{
        "orderId":      "ORD-12345",
        "customerId":   "CUST-789",
        "orderTotal":   249.99,
        "itemCount":    3,
        "paymentMethod": "credit_card",
        "items": []map[string]interface{}{
            {"sku": "PROD-1", "quantity": 2},
            {"sku": "PROD-2", "quantity": 1},
        },
    },
}
```

#### 2. Create Your Hook

Reference the metadata fields in your prompts:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: order-processor
spec:
  eventConfigurations:
  - source:
      name: order-events
      events:
      - eventType: "order-placed"
        agentRef:
          name: fulfillment-agent
        prompt: |
          New Order: {{.Metadata.orderId}}
          Customer: {{.Metadata.customerId}}
          Total: ${{.Metadata.orderTotal}}
          Items: {{.Metadata.itemCount}}
          
          {{range .Metadata.items}}
          - SKU: {{.sku}}, Qty: {{.quantity}}
          {{end}}
```

## Best Practices

### 1. Always Include a UID for Custom Events

For proper deduplication, include a unique identifier:

```go
event := interfaces.Event{
    Type: "transaction-alert",
    UID:  transactionID, // Critical for deduplication!
    Metadata: map[string]interface{}{
        "transactionId": transactionID,
        // ... other fields
    },
}
```

### 2. Use Descriptive Metadata Keys

Use clear, consistent naming:

```go
// Good
Metadata: map[string]interface{}{
    "customerId":    "CUST-123",
    "orderTotal":    99.99,
    "paymentMethod": "visa",
}

// Less clear
Metadata: map[string]interface{}{
    "cid":  "CUST-123",
    "amt":  99.99,
    "pm":   "visa",
}
```

### 3. Include Context in Metadata

Provide enough information for agents to make decisions:

```go
Metadata: map[string]interface{}{
    // Core data
    "errorCode":    "DB_TIMEOUT",
    "errorMessage": "Connection timeout after 30s",
    
    // Context
    "database":      "customers-db",
    "host":          "db-primary-1",
    "attemptNumber": 3,
    "maxRetries":    5,
    
    // Impact
    "affectedUsers":  127,
    "severity":       "high",
}
```

### 4. Use Appropriate Data Types

Leverage the type flexibility:

```go
Metadata: map[string]interface{}{
    "count":    42,                           // number
    "enabled":  true,                         // boolean
    "tags":     []string{"urgent", "prod"},   // array
    "metrics": map[string]float64{            // nested object
        "cpu":    85.5,
        "memory": 72.3,
    },
}
```

## Files Modified

### Core Interfaces
- `internal/interfaces/controller.go` - Event and ActiveEvent structs

### Deduplication
- `internal/deduplication/manager.go` - Smart event key generation
- `internal/deduplication/manager_test.go` - Test fixes

### Pipeline Processing
- `internal/pipeline/processor.go` - Template expansion and context enrichment
- `internal/pipeline/processor_test.go` - Test updates
- `internal/pipeline/integration_test.go` - Test updates

### Status Management
- `internal/status/manager.go` - Flexible logging and event recording

### Event Sources
- `internal/event/watcher.go` - Kubernetes event mapping
- `internal/event/grpc_client.go` - gRPC metadata conversion

### Documentation
- `docs/event-templating.md` - Comprehensive templating guide
- `docs/flexible-event-sources.md` - This document

### Examples
- `examples/custom-event-metadata.yaml` - Real-world examples

## Testing

All existing tests pass, and new tests verify:
- Flexible metadata types
- Smart deduplication with and without UIDs
- Template expansion with custom metadata
- Optional field handling in logging

## Backward Compatibility

✅ **Fully backward compatible**

All existing Kubernetes-focused hooks continue to work without any changes. The enhancements are purely additive.

## Future Enhancements

Potential future improvements:
1. Schema validation for metadata
2. Metadata transformation rules
3. Event correlation across sources
4. Metadata-based routing and filtering
5. Custom deduplication strategies per event source

## Summary

These changes transform KHook from a Kubernetes-specific event processor into a universal event handling platform capable of processing events from any source while maintaining full backward compatibility with existing Kubernetes-focused deployments.

