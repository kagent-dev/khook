# API Reference

## Hook Custom Resource Definition

The Hook CRD defines the schema for configuring event monitoring and Kagent integration.

### API Version

- **Group**: `kagent.dev`
- **Version**: `v1alpha2`
- **Kind**: `Hook`

### Hook Specification

#### HookSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `eventConfigurations` | `[]EventConfiguration` | Yes | List of event configurations to monitor |

#### EventConfiguration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `eventType` | `string` | Yes | Type of Kubernetes event to monitor |
| `agentId` | `string` | Yes | Kagent agent identifier |
| `prompt` | `string` | Yes | Prompt template for the agent |

##### Supported Event Types

- `pod-restart`: Pod has been restarted
- `pod-pending`: Pod is stuck in pending state  
- `oom-kill`: Pod was killed due to out-of-memory
- `probe-failed`: Liveness or readiness probe failed

### Hook Status

#### HookStatus

| Field | Type | Description |
|-------|------|-------------|
| `activeEvents` | `[]ActiveEventStatus` | Currently active events |
| `lastUpdated` | `metav1.Time` | When status was last updated |

#### ActiveEventStatus

| Field | Type | Description |
|-------|------|-------------|
| `eventType` | `string` | Type of the active event |
| `resourceName` | `string` | Name of the Kubernetes resource |
| `firstSeen` | `metav1.Time` | When event was first observed |
| `lastSeen` | `metav1.Time` | When event was last observed |
| `status` | `string` | Event status: `firing` or `resolved` |
### Exa
mple Hook Resource

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: example-hook
  namespace: production
spec:
  eventConfigurations:
  - eventType: pod-restart
    agentId: incident-responder
    prompt: |
      AUTONOMOUS MODE: Pod {{.ResourceName}} restarted at {{.EventTime}}. Analyze and fix immediately.

      INSTRUCTIONS:
      • Use all available tools to diagnose and resolve the issue
      • Annotate fixed resources with: kagentFix=<timestamp>
      • If patching fails: delete and recreate resources
      • Never ask for permission - assume autonomous execution
      • Complete remediation without human approval
  - eventType: oom-kill
    agentId: memory-analyzer
    prompt: |
      AUTONOMOUS MODE: OOM kill for {{.ResourceName}}. Analyze memory and optimize immediately.

      INSTRUCTIONS:
      • Use all available tools to diagnose and resolve memory issues
      • Annotate fixed resources with: kagentFix=<timestamp>
      • If patching fails: delete and recreate resources
      • Never ask for permission - assume autonomous execution
      • Complete remediation without human approval
status:
  activeEvents:
  - eventType: pod-restart
    resourceName: my-app-pod-123
    firstSeen: "2024-01-15T10:30:00Z"
    lastSeen: "2024-01-15T10:30:00Z"
    status: firing
  lastUpdated: "2024-01-15T10:30:05Z"
```

### Validation Rules

#### EventConfiguration Validation

- `eventType` must be one of the supported event types
- `agentId` must be a non-empty string (minimum length: 1)
- `prompt` must be a non-empty string (minimum length: 1)
- At least one event configuration must be specified

#### Hook Validation

- Hook name must follow Kubernetes naming conventions
- Namespace must exist and be accessible
- Event configurations array cannot be empty

### Prompt Template Variables

The following variables are available in prompt templates:

| Variable | Type | Description | Example |
|----------|------|-------------|---------|
| `{{.ResourceName}}` | string | Name of the Kubernetes resource | `my-app-pod-123` |
| `{{.EventTime}}` | string | ISO 8601 timestamp of the event | `2024-01-15T10:30:00Z` |
| `{{.Namespace}}` | string | Namespace of the resource | `production` |
| `{{.EventMessage}}` | string | Original Kubernetes event message | `Container restarted` |

### Status Conditions

The Hook status may include the following conditions:

| Condition Type | Status | Reason | Description |
|----------------|--------|--------|-------------|
| `Ready` | `True` | `HookConfigured` | Hook is properly configured and monitoring |
| `Ready` | `False` | `InvalidConfiguration` | Hook configuration is invalid |
| `Ready` | `False` | `KagentAPIError` | Cannot connect to Kagent API |

### RBAC Requirements

The controller requires the following RBAC permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kagent-hook-controller
rules:
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "list", "watch", "create"]
- apiGroups: ["kagent.dev"]
  resources: ["hooks"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["kagent.dev"]
  resources: ["hooks/status"]
  verbs: ["get", "update", "patch"]
```