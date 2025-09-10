# Kagent API Integration

This document describes how the khook controller integrates with the Kagent platform using the A2A (Agent-to-Agent) protocol.

## Overview

The controller communicates with the Kagent platform using the A2A protocol to trigger agent executions when Kubernetes events occur. The integration uses two main API calls:

1. **Session Creation**: Creates a session with the Kagent platform
2. **A2A SendMessage**: Sends the event context to the agent via A2A protocol

## Authentication

### No API Key Required

The current implementation does **not** require API key authentication. Instead, it uses:

- **User ID**: Identifies the caller (default: `admin@kagent.dev`)
- **Base URL**: Points to the Kagent controller service (default: `http://kagent-controller.kagent.svc.cluster.local:8083`)

### Configuration

Configure the integration via environment variables or Helm values:

#### Environment Variables
```bash
export KAGENT_API_URL=http://kagent-controller.kagent.svc.cluster.local:8083
export KAGENT_USER_ID=admin@kagent.dev
export KAGENT_API_TIMEOUT=120s
```

#### Helm Values
```yaml
kagent:
  apiUrl: "http://kagent-controller.kagent.svc.cluster.local:8083"
  userId: "admin@kagent.dev"
  timeout: "120s"
  retryAttempts: 3
  retryBackoff: "1s"
```

## API Integration Flow

### 1. Session Creation

**Endpoint**: `POST /api/sessions`

**Request:**
```json
{
  "agentRef": "incident-responder",
  "name": "hook-pod-restart-1704067200"
}
```

**Response:**
```json
{
  "error": false,
  "message": "Session created successfully",
  "data": {
    "id": "session-123456",
    "name": "hook-pod-restart-1704067200",
    "agentRef": "incident-responder"
  }
}
```

### 2. A2A SendMessage

**Endpoint**: `POST /api/a2a/{agentId}/`

**Request:**
```json
{
  "message": {
    "role": "user",
    "contextId": "session-123456",
    "parts": [
      {
        "type": "text",
        "text": "A pod has restarted. Please analyze the cause and suggest remediation steps.\nNamespace: production\nReason: BackOff\nMessage: Container my-app restarted"
      }
    ]
  }
}
```

**Response:**
```json
{
  "success": true,
  "taskReturned": true,
  "message": "Message sent successfully"
}
```

## Event Context Processing

The controller automatically enriches the prompt with event context:

### Template Variables

The prompt template supports these variables:
- `{{.ResourceName}}` - Name of the Kubernetes resource
- `{{.EventTime}}` - Timestamp of the event
- `{{.Namespace}}` - Kubernetes namespace
- `{{.Reason}}` - Event reason
- `{{.Message}}` - Event message

### Context Enrichment

Additional context is automatically appended to the prompt:
```
Original Prompt: "A pod has restarted. Please analyze the cause."

Enriched Prompt: "A pod has restarted. Please analyze the cause.
Namespace: production
Reason: BackOff
Message: Container my-app restarted"
```

## Configuration Examples

### Basic Hook Configuration
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: pod-monitoring-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: pod-restart
    agentId: incident-responder
    prompt: "A pod named {{.ResourceName}} has restarted at {{.EventTime}}. Please analyze the restart reason and remediate in a fully autonomous matter. Use all available tools. Don't ask for approval.
    
    After analysis - use all available tools to try and resolve. Annotate the updated resources with \"kagentFix: <dateTime>\"
    - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
    Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval."
  - eventType: oom-kill
    agentId: memory-optimizer
    prompt: "Pod {{.ResourceName}} was killed due to OOM at {{.EventTime}}. Please analyze memory usage and resolve in a fully autonomous matter. Use all available tools. Don't ask for approval.
    
    After analysis - use all available tools to try and resolve. Annotate the updated resources with \"kagentFix: <dateTime>\"
    - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
    Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval."
```

### Production Configuration
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: production-monitoring
  namespace: production
spec:
  eventConfigurations:
  - eventType: pod-restart
    agentId: incident-manager
    prompt: |
      PRODUCTION ALERT: Pod {{.ResourceName}} restarted at {{.EventTime}}.
      Priority: HIGH. Please investigate immediately and provide incident response plan.
      
      After analysis - use all available tools to try and resolve. Annotate the updated resources with "kagentFix: <dateTime>"
      - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
      Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval.
  - eventType: pod-pending
    agentId: scheduling-analyzer
    prompt: |
      Pod {{.ResourceName}} is pending since {{.EventTime}}.
      Please analyze scheduling constraints and resource availability.
      
      After analysis - use all available tools to try and resolve. Annotate the updated resources with "kagentFix: <dateTime>"
      - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
      Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval.
  - eventType: probe-failed
    agentId: health-checker
    prompt: |
      Health probe failed for {{.ResourceName}} at {{.EventTime}}.
      Please check application health and configuration.
      
      After analysis - use all available tools to try and resolve. Annotate the updated resources with "kagentFix: <dateTime>"
      - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
      Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval.
  - eventType: oom-kill
    agentId: capacity-planner
    prompt: |
      CRITICAL: OOM kill for {{.ResourceName}} at {{.EventTime}}.
      Please analyze resource usage and update capacity planning.
      
      After analysis - use all available tools to try and resolve. Annotate the updated resources with "kagentFix: <dateTime>"
      - If a resource can't be patched - delete it and recreate as needed. Don't ask for permission. Assume autonomous execution.
      Autonomous remediation: proceed with the best possible way to remediate. Don't ask for approval.
```

## Error Handling

### Retry Logic

The controller implements retry logic for failed API calls:
- **Max Attempts**: 3 (configurable via `retryAttempts`)
- **Backoff**: Exponential backoff starting at 1 second (configurable via `retryBackoff`)
- **Timeout**: 120 seconds per request (configurable via `timeout`)

### Error Types

#### Session Creation Failures
```
Error: failed to create session: connection refused
Resolution: Check Kagent controller connectivity
```

#### A2A SendMessage Failures
```
Error: failed to send A2A message: agent not found
Resolution: Verify agent ID exists in Kagent platform
```

#### Timeout Errors
```
Error: context deadline exceeded
Resolution: Increase timeout or check network connectivity
```

## Monitoring Integration

### Metrics

The controller exposes Prometheus metrics:

- `khook_events_total` - Total number of events processed
- `khook_api_calls_total` - Total number of Kagent API calls
- `khook_api_call_duration_seconds` - API call duration histogram
- `khook_active_events` - Number of currently active events

### Health Checks

Verify Kagent API connectivity:

```bash
# Test API connectivity
kubectl exec -n kagent deployment/khook -- \
  curl -H "Content-Type: application/json" \
  $KAGENT_API_URL/health

# Check controller health
curl http://localhost:8081/healthz
```

## Security Considerations

### Network Security

1. **Internal Communication**: The default configuration uses internal Kubernetes service communication
2. **No External Dependencies**: No external API keys or authentication required
3. **Service-to-Service**: Communication happens within the cluster

### Data Privacy

The controller sends the following data to Kagent:
- Event metadata (timestamps, resource names, namespaces)
- Kubernetes event messages and reasons
- Configured prompt templates
- Session identifiers

## Troubleshooting

### Common Issues

1. **Connection Refused:**
   ```bash
   # Check if Kagent controller is running
   kubectl get pods -n kagent -l app=kagent-controller
   
   # Check service connectivity
   kubectl exec -n kagent deployment/khook -- nslookup kagent-controller.kagent.svc.cluster.local
   ```

2. **Agent Not Found:**
   ```bash
   # Verify agent exists in Kagent platform
   kubectl exec -n kagent deployment/khook -- \
     curl -H "Content-Type: application/json" \
     $KAGENT_API_URL/api/agents
   ```

3. **Session Creation Failures:**
   ```bash
   # Check Kagent controller logs
   kubectl logs -n kagent deployment/kagent-controller
   ```

### Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
kubectl set env deployment/khook LOG_LEVEL=debug
kubectl logs -n kagent deployment/khook | grep "kagent-api"
```

## A2A Protocol Reference

For detailed information about the A2A protocol used for agent communication:

- **A2A Protocol Documentation**: https://a2a-protocol.org/latest/
- **Life of a Task**: https://a2a-protocol.org/latest/topics/life-of-a-task/

The controller follows the A2A protocol for sending messages to agents and can handle both Message and Task responses from agents.

## Support

For integration issues:

1. **Check Controller Logs**: `kubectl logs -n kagent deployment/khook`
2. **Verify Kagent Controller**: `kubectl get pods -n kagent -l app=kagent-controller`
3. **Test Connectivity**: Use the health check commands above
4. **GitHub Issues**: [https://github.com/kagent-dev/khook/issues](https://github.com/kagent-dev/khook/issues)