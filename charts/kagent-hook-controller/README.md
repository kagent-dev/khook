# Kagent Hook Controller Helm Chart

This Helm chart deploys the Kagent Hook Controller, a Kubernetes controller that enables automated responses to Kubernetes events by integrating with the Kagent platform.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- Access to Kagent API (API token required)

## Installation

### Add the Helm Repository

```bash
# If using a Helm repository (replace with actual repository URL)
helm repo add kagent https://charts.kagent.dev
helm repo update
```

### Install from Local Chart

```bash
# Clone the repository and install from local chart
git clone https://github.com/kagent-dev/kagent-hook-controller.git
cd kagent-hook-controller
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent-system \
  --create-namespace \
  --set kagent.apiToken="your-kagent-api-token"
```

### Install with Custom Values

```bash
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent-system \
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
| `kagent.apiUrl` | Kagent API URL | `https://api.kagent.dev` |
| `kagent.apiToken` | Kagent API token (required) | `""` |
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
| `namespace.name` | Namespace name | `kagent-system` |

## Examples

### Basic Installation

```bash
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --set kagent.apiToken="your-api-token"
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
  apiToken: "your-production-api-token"
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
            - kagent-hook-controller
        topologyKey: kubernetes.io/hostname
```

```bash
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent-system \
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
    agentId: "pod-restart-agent"
    prompt: "A pod has restarted. Please analyze the restart reason and provide recommendations."
  - eventType: "oom-kill"
    agentId: "memory-agent"
    prompt: "A pod was killed due to out of memory. Please analyze memory usage and provide optimization recommendations."
```

## Monitoring

The controller exposes metrics on port 8080 at `/metrics` endpoint. Key metrics include:

- `kagent_hook_controller_events_processed_total` - Total number of events processed
- `kagent_hook_controller_api_calls_total` - Total number of Kagent API calls
- `kagent_hook_controller_active_hooks` - Number of active hooks

## Troubleshooting

### Check Controller Status

```bash
kubectl get pods -n kagent-system -l app.kubernetes.io/name=kagent-hook-controller
kubectl logs -n kagent-system -l app.kubernetes.io/name=kagent-hook-controller
```

### Verify Hook Resources

```bash
kubectl get hooks -A
kubectl describe hook <hook-name> -n <namespace>
```

### Check Events

```bash
kubectl get events -n kagent-system --field-selector involvedObject.kind=Hook
```

## Uninstallation

```bash
helm uninstall kagent-hook-controller -n kagent-system
kubectl delete namespace kagent-system
```

## Contributing

Please see the main repository for contribution guidelines.

## License

This project is licensed under the MIT License - see the LICENSE file for details.