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

EventConfiguration supports two forms:

1) Single built-in event (Kubernetes events watcher):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `eventType` | `string` | Yes | Type of event to monitor |
| `agentRef` | `ObjectReference` | Yes | Agent to call on event |
| `prompt` | `string` | Yes | Prompt template for the agent |

2) Grouped external source events:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source.name` | `string` | Yes | ExternalEventSource name |
| `source.namespace` | `string` | No | Namespace (defaults to Hook namespace) |
| `source.events[]` | `[]SourceEvent` | Yes | List of event entries |

`SourceEvent` fields:

| Field | Type | Required |
|-------|------|----------|
| `eventType` | `string` | Yes |
| `agentRef` | `ObjectReference` | Yes |
| `prompt` | `string` | Yes |

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
### Example Hook Resource

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: example-hook
  namespace: production
spec:
  eventConfigurations:
  - eventType: pod-restart
    agentRef: { name: kagent/incident-responder }
    prompt: |
      Pod {{.ResourceName}} restarted at {{.EventTime}}.
  - source:
      name: my-external-source
      events:
      - eventType: probe-failed
        agentRef: { name: kagent/health-checker }
        prompt: |
          Probe failed on {{.ResourceName}}.
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