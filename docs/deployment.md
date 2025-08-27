# Deployment Guide

This guide covers different deployment methods for the Kagent Hook Controller.

## Prerequisites

- Kubernetes cluster (version 1.19+)
- kubectl configured to access your cluster
- Kagent API token (obtain from Kagent platform)
- (Optional) Helm 3.0+ for Helm-based deployment

## Deployment Methods

### Method 1: Helm Deployment (Recommended)

This method uses Kubernetes native Kustomize for deployment.

#### 1. Clone the Repository

```bash
git clone https://github.com/antweiss/khook.git
cd khook
```

#### 2. Install the Controller with Helm

```bash
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent-system \
  --create-namespace \
  --set kagent.apiToken="your-kagent-api-token"
```

#### 3. Verify Deployment

```bash
helm status kagent-hook-controller -n kagent-system
kubectl get pods -n kagent-system
```

### Method 2: Kustomize Deployment

Edit the secret configuration:

```bash
# Create base64 encoded token
echo -n "your-kagent-api-token" | base64

# Edit the secret file
vim config/default/secret.yaml
```

Update the `api-token` field with your base64 encoded token.

#### 3. Deploy the Controller

```bash
# Install CRDs first
make install

# Deploy the controller
make deploy
```

#### 4. Verify Deployment

```bash
kubectl get pods -n kagent-system
kubectl logs -n kagent-system -l app.kubernetes.io/name=kagent-hook-controller
```

### Method 3: Manual Deployment

### Method 3: Manual Deployment

#### 1. Create Namespace

```bash
kubectl create namespace kagent-system
```

#### 2. Install CRDs

```bash
kubectl apply -f config/crd/bases/kagent.dev_hooks.yaml
```

#### 3. Create RBAC Resources

```bash
kubectl apply -f config/rbac/
```

#### 4. Create ConfigMap and Secret

```bash
# Create ConfigMap
kubectl apply -f config/default/configmap.yaml

# Create Secret with your API token
kubectl create secret generic kagent-credentials \
  --from-literal=api-token="your-kagent-api-token" \
  -n kagent-system
```

#### 5. Deploy Controller

```bash
kubectl apply -f config/manager/manager.yaml
```

## Configuration Options

### Environment Variables

The controller supports the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `KAGENT_API_URL` | Kagent API endpoint | `https://api.kagent.dev` |
| `KAGENT_API_TOKEN` | Kagent API authentication token | Required |
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | `info` |
| `METRICS_BIND_ADDRESS` | Metrics server bind address | `:8080` |
| `HEALTH_PROBE_BIND_ADDRESS` | Health probe bind address | `:8081` |

### ConfigMap Configuration

The controller uses a ConfigMap for configuration. Key settings include:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: manager-config
  namespace: kagent-system
data:
  kagent-api-url: "https://api.kagent.dev"
  log-level: "info"
  deduplication-timeout-minutes: "10"
  cleanup-interval-minutes: "5"
  retry-attempts: "3"
  retry-backoff: "1s"
  api-timeout: "30s"
```

## Production Deployment Considerations

### High Availability

For production deployments, consider:

1. **Multiple Replicas**: Deploy multiple controller replicas with leader election
2. **Resource Limits**: Set appropriate CPU and memory limits
3. **Node Affinity**: Spread replicas across different nodes

```yaml
spec:
  replicas: 2
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/name: kagent-hook-controller
              topologyKey: kubernetes.io/hostname
```

### Security

1. **RBAC**: Use minimal required permissions (already configured)
2. **Security Context**: Run as non-root user (already configured)
3. **Network Policies**: Implement network policies if required
4. **Secret Management**: Use external secret management systems

### Monitoring

1. **Metrics**: Enable Prometheus metrics collection
2. **Logging**: Configure structured logging
3. **Health Checks**: Monitor liveness and readiness probes
4. **Alerts**: Set up alerts for controller failures

```yaml
# ServiceMonitor for Prometheus
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kagent-hook-controller
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: kagent-hook-controller
  endpoints:
  - port: metrics
```

## Upgrading

### Kustomize Upgrade

```bash
# Pull latest changes
git pull origin main

# Apply updates
make deploy
```

### Helm Upgrade

```bash
# Update chart
git pull origin main

# Upgrade release
helm upgrade kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent-system
```

## Troubleshooting

### Common Issues

1. **Controller Not Starting**
   - Check RBAC permissions
   - Verify API token is correct
   - Check resource limits

2. **Events Not Being Processed**
   - Verify Hook resources are created
   - Check controller logs
   - Ensure Kagent API is accessible

3. **High Memory Usage**
   - Check for event processing bottlenecks
   - Adjust deduplication settings
   - Monitor active events

### Debug Commands

```bash
# Check controller status
kubectl get pods -n kagent-system
kubectl describe pod <controller-pod> -n kagent-system

# View logs
kubectl logs -n kagent-system -l app.kubernetes.io/name=kagent-hook-controller -f

# Check Hook resources
kubectl get hooks -A
kubectl describe hook <hook-name> -n <namespace>

# Check events
kubectl get events -n kagent-system --sort-by='.lastTimestamp'
```

## Uninstalling

### Kustomize Uninstall

```bash
make undeploy
make uninstall
```

### Helm Uninstall

```bash
helm uninstall kagent-hook-controller -n kagent-system
kubectl delete namespace kagent-system
```

### Manual Uninstall

```bash
kubectl delete -f config/manager/
kubectl delete -f config/rbac/
kubectl delete -f config/crd/bases/
kubectl delete namespace kagent-system
```