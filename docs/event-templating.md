# Event Prompt Templating

KHook provides flexible templating for event prompts, allowing you to customize agent instructions based on event data from any source.

## Overview

Event prompts support Go's `text/template` syntax, giving you access to all event fields and metadata. This works for events from Kubernetes, Redis, custom gRPC sources, or any other event source.

## Available Template Variables

### Standard Event Fields

These fields are available for all events:

- `{{.Type}}` or `{{.EventType}}` - The event type
- `{{.Message}}` or `{{.EventMessage}}` - The event message
- `{{.Timestamp}}` or `{{.EventTime}}` - Event timestamp (RFC3339 format)
- `{{.UID}}` - Unique event identifier

### Kubernetes-Specific Fields

These fields may be empty for non-Kubernetes events:

- `{{.ResourceName}}` - Name of the resource (e.g., pod name)
- `{{.Namespace}}` - Kubernetes namespace
- `{{.Reason}}` - Event reason (e.g., "Unhealthy", "Failed")

### Metadata Access

The `Metadata` map contains source-specific data and can hold any type of value:

- `{{.Metadata.fieldName}}` - Access a specific metadata field
- `{{.Metadata}}` - Access the entire metadata map

### Full Event Access

For advanced use cases:

- `{{.Event}}` - Access the complete event object

## Examples

### Basic Kubernetes Event

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: pod-restart-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: "pod-restart"
    agentRef:
      name: diagnostic-agent
    prompt: |
      A pod has restarted in namespace {{.Namespace}}.
      
      Pod: {{.ResourceName}}
      Reason: {{.Reason}}
      Message: {{.Message}}
      Time: {{.Timestamp}}
      
      Kubernetes Metadata:
      - Kind: {{.Metadata.kind}}
      - API Version: {{.Metadata.apiVersion}}
      - Event Count: {{.Metadata.count}}
      
      Please diagnose the issue and implement a fix.
```

### Custom Event Source (Redis/Jira Example)

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: jira-issue-hook
  namespace: default
spec:
  eventConfigurations:
  - source:
      name: redis-events
      events:
      - eventType: "create-issue"
        agentRef:
          name: jira-agent
        prompt: |
          Create a Jira issue with the following details:
          
          Issue Type: {{.Metadata.issueType}}
          Priority: {{.Metadata.priority}}
          Project: {{.Metadata.project}}
          Summary: {{.Metadata.summary}}
          Description: {{.Metadata.description}}
          
          {{if .Metadata.assignee}}
          Assign to: {{.Metadata.assignee}}
          {{end}}
          
          {{if .Metadata.labels}}
          Labels: {{.Metadata.labels}}
          {{end}}
          
          Event received at: {{.Timestamp}}
```

### Financial Transaction Event

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: transaction-monitor
  namespace: default
spec:
  eventConfigurations:
  - source:
      name: transaction-stream
      events:
      - eventType: "suspicious-transaction"
        agentRef:
          name: fraud-detection-agent
        prompt: |
          ALERT: Suspicious transaction detected
          
          Transaction Details:
          - ID: {{.Metadata.transactionId}}
          - Amount: ${{.Metadata.amount}}
          - Currency: {{.Metadata.currency}}
          - Merchant: {{.Metadata.merchantName}}
          - Card Last 4: {{.Metadata.cardLast4}}
          - Location: {{.Metadata.location}}
          - Risk Score: {{.Metadata.riskScore}}
          
          {{if .Metadata.previousTransactions}}
          Recent transaction count: {{.Metadata.previousTransactions}}
          {{end}}
          
          Please analyze this transaction and take appropriate action.
```

### IoT Sensor Event

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: temperature-alert
  namespace: default
spec:
  eventConfigurations:
  - source:
      name: iot-sensors
      events:
      - eventType: "temperature-threshold-exceeded"
        agentRef:
          name: facility-management-agent
        prompt: |
          Temperature threshold exceeded!
          
          Sensor: {{.Metadata.sensorId}}
          Location: {{.Metadata.location}}
          Current Temperature: {{.Metadata.currentTemp}}°C
          Threshold: {{.Metadata.threshold}}°C
          Duration: {{.Metadata.durationMinutes}} minutes
          
          Facility: {{.Metadata.facility}}
          Zone: {{.Metadata.zone}}
          
          {{if .Metadata.hvacStatus}}
          HVAC Status: {{.Metadata.hvacStatus}}
          {{end}}
          
          Please investigate and take corrective action immediately.
```

## Template Functions

Go templates support several built-in functions:

### Conditionals

```yaml
{{if .Metadata.urgent}}
URGENT: {{.Message}}
{{else}}
INFO: {{.Message}}
{{end}}
```

### Ranges (for arrays)

```yaml
{{range .Metadata.affectedServices}}
- Service: {{.}}
{{end}}
```

### Comparisons

```yaml
{{if gt .Metadata.errorCount 10}}
Critical: High error count detected
{{end}}
```

## Security Features

### Allowed Template Constructs

- Variable access: `{{.FieldName}}`
- Conditionals: `{{if}}, {{else}}, {{end}}`
- Ranges: `{{range}}, {{end}}`
- Comparisons: `{{eq}}, {{ne}}, {{lt}}, {{gt}}, etc.`

### Blocked Template Constructs (for security)

The following are blocked to prevent code injection:

- `{{define}}` - Template definitions
- `{{template}}` - Template calls
- `{{call}}` - Function calls
- `{{print}}`, `{{printf}}`, `{{println}}` - Print functions

### Validation

- Maximum template length: 10,000 characters
- Bracket matching validation
- Dangerous construct detection

## Best Practices

### 1. Use Descriptive Field Names in Metadata

When creating custom event sources, use clear, descriptive field names:

```go
// Good
metadata := map[string]interface{}{
    "orderId": "ORD-12345",
    "customerId": "CUST-789",
    "orderTotal": 249.99,
    "currency": "USD",
}

// Less clear
metadata := map[string]interface{}{
    "id": "ORD-12345",
    "cid": "CUST-789",
    "amt": 249.99,
    "cur": "USD",
}
```

### 2. Include Context in Metadata

Provide enough context for the agent to make informed decisions:

```go
metadata := map[string]interface{}{
    "errorCode": "DB_CONN_TIMEOUT",
    "errorMessage": "Connection timeout after 30s",
    "database": "customers-db",
    "host": "db-primary-1",
    "attemptNumber": 3,
    "maxRetries": 5,
    "queryType": "SELECT",
    "affectedUsers": 127,
}
```

### 3. Use Appropriate Data Types

Metadata supports any JSON-serializable type:

```go
metadata := map[string]interface{}{
    "count": 42,                    // number
    "enabled": true,                // boolean
    "tags": []string{"urgent", "production"}, // array
    "details": map[string]interface{}{ // nested object
        "subsystem": "auth",
        "component": "oauth",
    },
}
```

### 4. Handle Missing Fields Gracefully

Use conditionals to check for optional fields:

```yaml
prompt: |
  Processing {{.Type}} event
  
  {{if .ResourceName}}
  Resource: {{.ResourceName}}
  {{end}}
  
  {{if .Metadata.customField}}
  Custom Data: {{.Metadata.customField}}
  {{end}}
```

## Accessing Event Data in Custom Sources

When creating custom event sources (gRPC, Redis, etc.), populate the Metadata field with your custom data:

### Example: Redis Event Source

```go
// In your custom event handler
event := interfaces.Event{
    Type:      "order-placed",
    Message:   "New order received",
    Timestamp: time.Now(),
    Metadata: map[string]interface{}{
        "orderId":      data["order_id"],
        "customerId":   data["customer_id"],
        "orderTotal":   data["total"],
        "itemCount":    data["item_count"],
        "paymentMethod": data["payment_method"],
        "shippingAddress": data["address"],
    },
}
```

### Example: gRPC Event Source

Your gRPC event should populate the metadata map in the proto:

```protobuf
message Event {
    string type = 1;
    string message = 2;
    int64 timestamp_unix = 3;
    map<string, string> metadata = 4;  // Convert to interface{} internally
}
```

## Debugging Templates

Enable verbose logging to see template expansion:

```bash
# Set log level to see template debugging
kubectl set env deployment/khook -n kagent LOG_LEVEL=2
```

This will show logs like:

```
Advanced template expansion completed
  originalLength: 245
  expandedLength: 312
```

## Migration from Simple Placeholders

If you were using simple placeholder syntax, it's still supported for backward compatibility:

```yaml
# Old style (still works)
prompt: "Event {{.EventType}} on {{.ResourceName}}"

# New style (more powerful)
prompt: |
  Event: {{.Type}}
  {{if .ResourceName}}
  Resource: {{.ResourceName}}
  {{end}}
  Metadata: {{.Metadata}}
```

## Summary

The flexible metadata system allows KHook to work with events from any source while providing powerful templating capabilities. By leveraging Go's text/template syntax, you can create dynamic, context-aware prompts that adapt to your specific use case—whether you're monitoring Kubernetes, processing business events, handling IoT data, or integrating with external systems.

