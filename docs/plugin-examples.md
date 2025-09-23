# Plugin Configuration Examples

This document provides practical examples of plugin configurations for various use cases.

## Table of Contents

- [Basic Configurations](#basic-configurations)
- [Production Configurations](#production-configurations)
- [Multi-Source Monitoring](#multi-source-monitoring)
- [Event Mapping Examples](#event-mapping-examples)
- [Hook Integration Examples](#hook-integration-examples)

## Basic Configurations

### Kubernetes Plugin (Default)

```yaml
# config/plugins.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "default"  # Watch specific namespace, or omit for all namespaces
```

```yaml
# config/event-mappings.yaml
mappings:
  - eventSource: kubernetes
    eventType: pod-restart
    internalType: PodRestart
    description: "Pod container restart detected"
    severity: warning
    tags:
      category: kubernetes
      impact: service-disruption
    enabled: true

  - eventSource: kubernetes
    eventType: oom-kill
    internalType: OOMKill
    description: "Pod killed due to out of memory"
    severity: error
    tags:
      category: kubernetes
      impact: resource-exhaustion
    enabled: true
```

### Webhook Plugin

```yaml
# config/plugins.yaml
plugins:
  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/webhook"
      timeout: "30s"
      maxBodySize: "1MB"
```

```yaml
# config/event-mappings.yaml
mappings:
  - eventSource: webhook
    eventType: webhook-received
    internalType: WebhookReceived
    description: "External webhook event received"
    severity: info
    tags:
      category: webhook
      impact: notification
    enabled: true
```

## Production Configurations

### High Availability Setup

```yaml
# config/plugins.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "production"
      retryInterval: "5s"
      maxRetries: 3
      healthCheckInterval: "30s"

  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/webhook"
      timeout: "10s"
      maxConcurrentRequests: 100
      rateLimitRPS: 50
      enableTLS: true
      certFile: "/etc/certs/tls.crt"
      keyFile: "/etc/certs/tls.key"
```

### Resource Monitoring Focus

```yaml
# config/event-mappings.yaml
mappings:
  # Critical resource events
  - eventSource: kubernetes
    eventType: oom-kill
    internalType: OOMKill
    description: "Critical: Pod OOM killed"
    severity: critical
    tags:
      category: kubernetes
      impact: resource-exhaustion
      priority: high
    enabled: true

  - eventSource: kubernetes
    eventType: pod-pending
    internalType: PodPending
    description: "Pod scheduling failure"
    severity: warning
    tags:
      category: kubernetes
      impact: deployment-failure
      priority: medium
    enabled: true

  # Disable less critical events in production
  - eventSource: kubernetes
    eventType: probe-failed
    internalType: ProbeFailed
    description: "Health probe failure"
    severity: warning
    tags:
      category: kubernetes
      impact: health-check
      priority: low
    enabled: false  # Disabled to reduce noise
```

## Multi-Source Monitoring

### Kubernetes + Webhook Integration

```yaml
# config/plugins.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "production"

  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/alerts"
      authentication:
        type: "bearer"
        token: "${WEBHOOK_TOKEN}"  # From environment variable
```

```yaml
# config/event-mappings.yaml
mappings:
  # Kubernetes events
  - eventSource: kubernetes
    eventType: pod-restart
    internalType: PodRestart
    description: "Kubernetes pod restart"
    severity: warning
    tags:
      category: kubernetes
      source: cluster
    enabled: true

  # External monitoring webhooks
  - eventSource: webhook
    eventType: webhook-received
    internalType: ExternalAlert
    description: "External monitoring alert"
    severity: error
    tags:
      category: external
      source: monitoring
    enabled: true
```

### Development Environment

```yaml
# config/plugins.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "development"
      verboseLogging: true

  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/dev-webhook"
      enableDebug: true
      logPayloads: true  # Log full payloads for debugging
```

```yaml
# config/event-mappings.yaml
mappings:
  # Enable all event types for development
  - eventSource: kubernetes
    eventType: pod-restart
    internalType: PodRestart
    severity: info  # Lower severity in dev
    enabled: true

  - eventSource: kubernetes
    eventType: pod-pending
    internalType: PodPending
    severity: info
    enabled: true

  - eventSource: webhook
    eventType: webhook-received
    internalType: WebhookReceived
    severity: info
    enabled: true
```

## Event Mapping Examples

### Severity-Based Routing

```yaml
# config/event-mappings.yaml
mappings:
  # Critical events - immediate attention
  - eventSource: kubernetes
    eventType: oom-kill
    internalType: OOMKill
    severity: critical
    tags:
      priority: p0
      escalation: immediate
    enabled: true

  # Error events - urgent attention
  - eventSource: kubernetes
    eventType: probe-failed
    internalType: ProbeFailed
    severity: error
    tags:
      priority: p1
      escalation: urgent
    enabled: true

  # Warning events - normal attention
  - eventSource: kubernetes
    eventType: pod-restart
    internalType: PodRestart
    severity: warning
    tags:
      priority: p2
      escalation: normal
    enabled: true

  # Info events - monitoring only
  - eventSource: webhook
    eventType: webhook-received
    internalType: WebhookReceived
    severity: info
    tags:
      priority: p3
      escalation: none
    enabled: true
```

### Tag-Based Categorization

```yaml
# config/event-mappings.yaml
mappings:
  # Infrastructure events
  - eventSource: kubernetes
    eventType: pod-restart
    internalType: PodRestart
    severity: warning
    tags:
      category: infrastructure
      component: compute
      team: platform
    enabled: true

  - eventSource: kubernetes
    eventType: oom-kill
    internalType: OOMKill
    severity: error
    tags:
      category: infrastructure
      component: memory
      team: platform
    enabled: true

  # Application events
  - eventSource: webhook
    eventType: webhook-received
    internalType: ApplicationAlert
    severity: warning
    tags:
      category: application
      component: service
      team: development
    enabled: true
```

## Hook Integration Examples

### Basic Hook Configuration

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: kubernetes-monitoring
  namespace: production
spec:
  eventConfigurations:
  - eventType: "PodRestart"
    agentRef:
      name: "kubernetes-troubleshooter"
    prompt: |
      KUBERNETES INCIDENT: Pod restart detected
      
      Pod: {{.ResourceName}}
      Namespace: {{.Namespace}}
      Reason: {{.Reason}}
      Message: {{.Message}}
      
      Please investigate the cause of this pod restart and take corrective action.
      
      Available context:
      - Pod logs
      - Resource usage metrics
      - Recent deployments
      - Node status

  - eventType: "OOMKill"
    agentRef:
      name: "resource-optimizer"
    prompt: |
      CRITICAL: Out of Memory Kill detected
      
      Pod: {{.ResourceName}}
      Namespace: {{.Namespace}}
      
      This pod was killed due to memory exhaustion. Please:
      1. Analyze memory usage patterns
      2. Check for memory leaks
      3. Adjust resource limits if needed
      4. Scale resources if required
```

### Multi-Source Hook

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: comprehensive-monitoring
  namespace: production
spec:
  eventConfigurations:
  # Kubernetes events
  - eventType: "PodRestart"
    agentRef:
      name: "kubernetes-agent"
    prompt: |
      Kubernetes pod restart: {{.ResourceName}} in {{.Namespace}}
      Reason: {{.Reason}}
      
  # External webhook events
  - eventType: "ExternalAlert"
    agentRef:
      name: "external-alert-handler"
    prompt: |
      External alert received: {{.Message}}
      Source: {{.Source}}
      Resource: {{.ResourceName}}
      
      Please correlate with Kubernetes events and take appropriate action.
```

### Environment-Specific Hooks

```yaml
# Development environment
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: dev-monitoring
  namespace: development
spec:
  eventConfigurations:
  - eventType: "PodRestart"
    agentRef:
      name: "dev-assistant"
    prompt: |
      Development pod restart: {{.ResourceName}}
      
      This is a development environment event. Please:
      1. Log the incident for analysis
      2. Check if this is expected behavior
      3. Update development documentation if needed

---
# Production environment
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: prod-monitoring
  namespace: production
spec:
  eventConfigurations:
  - eventType: "PodRestart"
    agentRef:
      name: "production-sre"
    prompt: |
      PRODUCTION ALERT: Pod restart detected
      
      Pod: {{.ResourceName}}
      Namespace: {{.Namespace}}
      Time: {{.Timestamp}}
      
      IMMEDIATE ACTION REQUIRED:
      1. Assess service impact
      2. Check dependent services
      3. Investigate root cause
      4. Implement fix or rollback
      5. Update incident documentation
```

### Conditional Processing

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: conditional-monitoring
  namespace: production
spec:
  eventConfigurations:
  - eventType: "PodRestart"
    agentRef:
      name: "smart-responder"
    prompt: |
      Pod restart detected: {{.ResourceName}}
      
      {{if eq .Namespace "critical-services"}}
      CRITICAL SERVICE AFFECTED - IMMEDIATE RESPONSE REQUIRED
      {{else if eq .Namespace "production"}}
      Production service affected - urgent response needed
      {{else}}
      Non-critical service affected - standard response
      {{end}}
      
      Metadata: {{range $key, $value := .Metadata}}
      - {{$key}}: {{$value}}{{end}}
      
      Tags: {{range $key, $value := .Tags}}
      - {{$key}}: {{$value}}{{end}}
```

## Advanced Configuration Patterns

### Plugin Chaining

```yaml
# config/plugins.yaml
plugins:
  # Primary event source
  - name: kubernetes
    enabled: true
    config:
      namespace: "production"

  # Secondary enrichment source
  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/enrich"
      mode: "enrichment"  # Custom mode for event enrichment
```

### Environment-Based Configuration

```yaml
# config/plugins.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "${WATCH_NAMESPACE:-default}"
      logLevel: "${LOG_LEVEL:-info}"
      
  - name: webhook
    enabled: "${WEBHOOK_ENABLED:-false}"
    config:
      port: "${WEBHOOK_PORT:-8090}"
      path: "${WEBHOOK_PATH:-/webhook}"
      authentication:
        type: "${AUTH_TYPE:-none}"
        token: "${AUTH_TOKEN}"
```

### Custom Event Types

```yaml
# config/event-mappings.yaml
mappings:
  # Custom business events
  - eventSource: webhook
    eventType: deployment-started
    internalType: DeploymentStarted
    description: "Application deployment initiated"
    severity: info
    tags:
      category: deployment
      phase: start
    enabled: true

  - eventSource: webhook
    eventType: deployment-failed
    internalType: DeploymentFailed
    description: "Application deployment failed"
    severity: error
    tags:
      category: deployment
      phase: failure
    enabled: true

  - eventSource: webhook
    eventType: deployment-completed
    internalType: DeploymentCompleted
    description: "Application deployment completed successfully"
    severity: info
    tags:
      category: deployment
      phase: success
    enabled: true
```

These examples provide a foundation for configuring KHook plugins in various scenarios. Adapt them to your specific requirements and environment needs.
