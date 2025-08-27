# Kagent API Integration

This document describes how the KAgent Hook Controller integrates with the Kagent platform, including authentication, API requirements, and configuration.

## Overview

The controller communicates with the Kagent platform via REST API to trigger agent executions when Kubernetes events occur. Each event configuration in a Hook resource specifies which Kagent agent to call and what prompt to send.

## Authentication

### API Key Authentication

The controller uses API key authentication to communicate with the Kagent platform.

#### Setting up API Keys

1. **Obtain API Key from Kagent Platform:**
   - Log into your Kagent dashboard
   - Navigate to Settings > API Keys
   - Create a new API key with appropriate permissions
   - Copy the generated key

2. **Configure in Kubernetes:**
   ```bash
   kubectl create secret generic kagent-credentials \
     --from-literal=api-key=your-kagent-api-key \
     --from-literal=base-url=https://api.kagent.dev \
     --namespace=kagent-system
   ```

3. **Environment Variables (Alternative):**
   ```bash
   export KAGENT_API_KEY=your-kagent-api-key
   export KAGENT_BASE_URL=https://api.kagent.dev
   ```

### Required Permissions

The API key must have the following permissions:
- `agents:execute` - Execute agent requests
- `agents:read` - Read agent configurations (for validation)

## API Endpoints

### Base URL Configuration

Configure the base URL for your Kagent instance:

- **SaaS**: `https://api.kagent.dev`
- **Self-hosted**: `https://your-kagent-instance.com/api`
- **Development**: `https://dev.kagent.dev`

### Agent Execution Endpoint

**Endpoint**: `POST /v1/agents/{agentId}/execute`

**Headers:**
```
Authorization: Bearer {api-key}
Content-Type: application/json
```

**Request Body:**
```json
{
  "prompt": "Your prompt template with context",
  "context": {
    "eventName": "pod-restart",
    "eventTime": "2024-01-15T10:30:00Z",
    "resourceName": "my-app-pod-123",
    "namespace": "production",
    "eventMessage": "Container my-app restarted"
  },
  "metadata": {
    "source": "kagent-hook-controller",
    "hookName": "production-monitoring",
    "kubernetesCluster": "prod-cluster-1"
  }
}
```

**Response:**
```json
{
  "success": true,
  "executionId": "exec-123456",
  "message": "Agent execution started successfully",
  "estimatedDuration": "30s"
}
```

## Agent Configuration

### Agent Requirements

Agents used with the Hook Controller should:

1. **Accept Context Parameters:**
   - Be configured to handle event context in prompts
   - Support template variables like `{{.ResourceName}}`

2. **Handle Kubernetes Context:**
   - Understand Kubernetes terminology
   - Be trained on common Kubernetes issues

3. **Provide Actionable Responses:**
   - Give specific troubleshooting steps
   - Include relevant kubectl commands
   - Suggest preventive measures

### Recommended Agent Types

#### Incident Response Agent
```yaml
# Example agent configuration in Kagent platform
name: incident-responder
description: Analyzes Kubernetes incidents and provides response plans
capabilities:
  - kubernetes-troubleshooting
  - incident-analysis
  - root-cause-analysis
prompt_template: |
  You are a Kubernetes incident response specialist. 
  Analyze the following event and provide a structured response plan.
```

#### Memory Analysis Agent
```yaml
name: memory-analyzer
description: Analyzes memory-related issues and optimization
capabilities:
  - memory-profiling
  - performance-optimization
  - resource-planning
prompt_template: |
  You are a memory optimization expert for Kubernetes workloads.
  Analyze the OOM event and provide optimization recommendations.
```

## Error Handling

### Retry Logic

The controller implements exponential backoff for failed API calls:

```
Attempt 1: Immediate
Attempt 2: 2 seconds delay
Attempt 3: 4 seconds delay
Max attempts: 3
```

### Error Types

#### Authentication Errors (401)
```json
{
  "error": "unauthorized",
  "message": "Invalid API key",
  "code": 401
}
```

**Resolution:**
- Verify API key is correct
- Check API key permissions
- Ensure API key hasn't expired

#### Agent Not Found (404)
```json
{
  "error": "agent_not_found",
  "message": "Agent 'incident-responder' not found",
  "code": 404
}
```

**Resolution:**
- Verify agent ID in hook configuration
- Check agent exists in Kagent platform
- Ensure agent is active and deployed

#### Rate Limiting (429)
```json
{
  "error": "rate_limited",
  "message": "Too many requests",
  "code": 429,
  "retry_after": 60
}
```

**Resolution:**
- Controller automatically retries after specified delay
- Consider reducing hook frequency
- Upgrade Kagent plan if needed

#### Server Errors (5xx)
```json
{
  "error": "internal_server_error",
  "message": "Temporary service unavailable",
  "code": 500
}
```

**Resolution:**
- Controller automatically retries
- Check Kagent platform status
- Contact Kagent support if persistent

## Configuration Examples

### Production Configuration
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kagent-credentials
  namespace: kagent-system
type: Opaque
stringData:
  api-key: "prod-key-abc123..."
  base-url: "https://api.kagent.dev"
  timeout: "30s"
  max-retries: "3"
```

### Development Configuration
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kagent-credentials
  namespace: kagent-system
type: Opaque
stringData:
  api-key: "dev-key-xyz789..."
  base-url: "https://dev.kagent.dev"
  timeout: "10s"
  max-retries: "1"
```

## Monitoring Integration

### API Call Metrics

The controller exposes metrics for monitoring API interactions:

- `kagent_api_calls_total{status="success|error", agent_id="..."}`
- `kagent_api_call_duration_seconds{agent_id="..."}`
- `kagent_api_errors_total{error_type="auth|not_found|rate_limit|server"}`

### Health Checks

Verify Kagent API connectivity:

```bash
# Test API connectivity
kubectl exec -n kagent-system deployment/kagent-hook-controller -- \
  curl -H "Authorization: Bearer $KAGENT_API_KEY" \
  $KAGENT_BASE_URL/health

# Check controller health
curl http://localhost:8081/healthz
```

## Security Considerations

### API Key Management

1. **Rotation:**
   - Rotate API keys regularly (recommended: every 90 days)
   - Use Kubernetes secrets for secure storage
   - Never commit API keys to version control

2. **Least Privilege:**
   - Use API keys with minimal required permissions
   - Create separate keys for different environments
   - Monitor API key usage

3. **Network Security:**
   - Use HTTPS for all API communications
   - Implement network policies if required
   - Consider VPN or private networking for sensitive environments

### Data Privacy

The controller sends the following data to Kagent:
- Event metadata (timestamps, resource names)
- Kubernetes event messages
- Configured prompt templates
- Cluster identification (if configured)

Ensure this complies with your data governance policies.

## Troubleshooting API Integration

### Common Issues

1. **Connection Timeouts:**
   ```bash
   # Increase timeout in configuration
   kubectl patch secret kagent-credentials -p '{"stringData":{"timeout":"60s"}}'
   ```

2. **SSL Certificate Issues:**
   ```bash
   # For self-hosted instances with custom certificates
   kubectl create configmap kagent-ca-cert --from-file=ca.crt=your-ca.crt
   ```

3. **Proxy Configuration:**
   ```bash
   # Configure HTTP proxy if needed
   kubectl set env deployment/kagent-hook-controller HTTP_PROXY=http://proxy:8080
   ```

### Debug API Calls

Enable API debug logging:

```bash
kubectl set env deployment/kagent-hook-controller LOG_LEVEL=debug
kubectl logs -n kagent-system deployment/kagent-hook-controller | grep "kagent-api"
```

## Support

For Kagent API integration issues:

1. **Check API Status**: [status.kagent.dev](https://status.kagent.dev)
2. **API Documentation**: [docs.kagent.dev/api](https://docs.kagent.dev/api)
3. **Support**: [support.kagent.dev](https://support.kagent.dev)