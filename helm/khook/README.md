# Kagent Hook Controller Helm Chart

This Helm chart deploys the Kagent Hook Controller, a Kubernetes controller that enables automated responses to Kubernetes events by integrating with the Kagent platform.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- Kagent reachable in-cluster (no API token required)

## Installation

### Install from Local Chart (Repository Path)

```bash
# Clone the repository and install from local chart
git clone https://github.com/kagent-dev/khook.git
cd khook
helm install khook ./helm/khook \
  --namespace kagent \
  --create-namespace \
  # no API token required
```

### Install with Custom Values

```bash
helm install khook ./helm/khook \
  --namespace kagent \
  --create-namespace \
  --values custom-values.yaml
```

## Configuration

The following table lists the configurable parameters and their default values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of controller replicas | `1` |
| `image.repository` | Controller image repository | `otomato/khook` |
| `image.tag` | Controller image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `kagent.apiUrl` | Kagent API URL | `http://kagent-controller.kagent.svc.cluster.local:8083` |
| `kagent.userId` | User identity for requests | `admin@kagent.dev` |
| `kagent.timeout` | API request timeout | `30s` |
| `kagent.retryAttempts` | Number of retry attempts | `3` |
| `controller.logLevel` | Log level (debug, info, warn, error) | `info` |
| `controller.logFormat` | Log format (json, text) | `json` |
| `controller.leaderElection.enabled` | Enable leader election | `true` |
| `controller.deduplication.timeoutMinutes` | Event deduplication timeout | `10` |
| `serviceAccount.create` | Create service account | `true` |
| `rbac.create` | Create RBAC resources | `true` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus | `false` |
| `namespace.create` | Create namespace | `true` |
| `namespace.name` | Namespace name | `kagent` |

## Examples

### Basic Installation

```bash
helm install khook ./helm/khook
```

### Production Installation with Monitoring

```yaml
# production-values.yaml
replicaCount: 2

resources:
  limits:
    cpu: 1000m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

controller:
  logLevel: "warn"
  leaderElection:
    enabled: true

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    namespace: monitoring
    labels:
      release: prometheus

kagent:
  timeout: "60s"
  retryAttempts: 5

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - khook
        topologyKey: kubernetes.io/hostname
```

```bash
helm install khook ./helm/khook \
  --namespace kagent \
  --create-namespace \
  --values production-values.yaml
```

## Usage

After installation, create Hook resources to define event monitoring:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: pod-monitoring-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: "pod-restart"
    agentId: "kagent/pod-restart-agent"
    prompt: |
      AUTONOMOUS MODE: A pod has restarted. Analyze the cause and implement fixes immediately.

      INSTRUCTIONS:
      • Use all available tools to diagnose and resolve the issue
      • Annotate fixed resources with: kagentFix=<timestamp>
      • If patching fails: delete and recreate resources
      • Never ask for permission - assume autonomous execution
      • Complete remediation without human approval
  - eventType: "oom-kill"
    agentId: "kagent/memory-agent"
    prompt: |
      AUTONOMOUS MODE: OOM kill detected. Analyze memory usage and optimize immediately.

      INSTRUCTIONS:
      • Use all available tools to diagnose and resolve memory issues
      • Annotate fixed resources with: kagentFix=<timestamp>
      • If patching fails: delete and recreate resources
      • Never ask for permission - assume autonomous execution
      • Complete remediation without human approval
```

## Monitoring

The controller exposes metrics on port 8080 at `/metrics` endpoint. Key metrics include:

- `khook_controller_events_processed_total` - Total number of events processed
- `khook_controller_api_calls_total` - Total number of Kagent API calls
- `khook_controller_active_hooks` - Number of active hooks

## Troubleshooting

### Check Controller Status

```bash
kubectl get pods -n kagent -l app.kubernetes.io/name=khook
kubectl logs -n kagent -l app.kubernetes.io/name=khook
```

### Verify Hook Resources

```bash
kubectl get hooks -A
kubectl describe hook <hook-name> -n <namespace>
```

### Check Events

```bash
kubectl get events -n kagent --field-selector involvedObject.kind=Hook
```

## Uninstallation

```bash
helm uninstall khook -n kagent
kubectl delete namespace kagent
```

## Contributing

Please see the main repository for contribution guidelines.

## License

This project is licensed under the MIT License - see the LICENSE file for details.