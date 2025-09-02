# Installation Guide

This guide provides detailed instructions for installing and configuring khook.

## Prerequisites

- Kubernetes cluster (v1.20 or later)
- kubectl configured to access your cluster
- Cluster admin permissions for CRD installation
- Kagent reachable in-cluster or via network

## Installation Methods

### Method 1: Helm Chart (Recommended)

Install using the Helm charts from this repository (install CRDs first, then controller):

```bash
# Clone the repository
git clone https://github.com/antweiss/khook.git
cd khook

# Create namespace (recommended to pre-create to avoid Helm ownership issues)
kubectl create namespace kagent --dry-run=client -o yaml | kubectl apply -f -

# Install CRDs
helm install khook-crds ./charts/kagent-hook-crds \
  --namespace kagent \
  
# Install controller with default values
helm install khook ./charts/khook-controller \
  --namespace kagent \
  --create-namespace

# Optional: customize API URL and other values
helm install khook ./charts/khook-controller \
  --namespace kagent \
  --create-namespace \
  --set kagent.apiUrl="https://api.kagent.dev"

# Verify installation
kubectl get pods -n kagent
```

Chart location: charts/khook-controller (see repo tree).

#### One-liner install

```bash
TMP_DIR="$(mktemp -d)" && \
  git clone --depth 1 https://github.com/antweiss/khook.git "$TMP_DIR/khook" && \
  helm install khook-crds "$TMP_DIR/khook/charts/khook-crds" \
    --namespace kagent \
    --create-namespace && \
  helm install khook "$TMP_DIR/khook/charts/khook-controller" \
    --namespace kagent \
    --create-namespace && \
  rm -rf "$TMP_DIR"
```

### Method 2: Manual Installation

For custom deployments or development:

```bash
# Clone the repository
git clone https://github.com/antweiss/khook.git
cd khook

# Install CRDs
kubectl apply -f config/crd/bases/

# Install RBAC
kubectl apply -f config/rbac/

# Install controller
kubectl apply -f config/manager/
```

## Configuration

### 1. Kagent API

No API key is required. Configure base URL and user ID via Helm values or env vars.

### 2. Controller Configuration

Configure the controller using environment variables or ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: khook-config
  namespace: kagent
data:
  LOG_LEVEL: "info"
  METRICS_PORT: "8080"
  HEALTH_PORT: "8081"
  LEADER_ELECTION: "true"
  EVENT_RESYNC_PERIOD: "10m"
  DEDUPLICATION_TIMEOUT: "10m"
```

### 3. Resource Limits

Configure appropriate resource limits:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: khook
  namespace: kagent
spec:
  template:
    spec:
      containers:
      - name: manager
        resources:
          limits:
            cpu: 500m
            memory: 512Mi
          requests:
            cpu: 100m
            memory: 128Mi
```

## Verification

### 1. Check Controller Status

```bash
# Verify controller is running
kubectl get pods -n kagent -l app=khook

# Check controller logs
kubectl logs -n kagent deployment/khook
```

### 2. Verify CRD Installation

```bash
# Check if Hook CRD is installed
kubectl get crd hooks.kagent.dev

# Verify CRD schema
kubectl describe crd hooks.kagent.dev
```

### 3. Test Basic Functionality

Create a test hook:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kagent.dev/v1alpha2
kind: Hook
metadata:
  name: test-hook
  namespace: default
spec:
  eventConfigurations:
  - eventType: pod-restart
    agentId: test-agent
    prompt: "Test hook is working"
EOF
```

Verify the hook was created:

```bash
kubectl get hooks
kubectl describe hook test-hook
```

## Production Configuration

### High Availability

For production deployments, configure high availability:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: khook
  namespace: kagent
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: LEADER_ELECTION
          value: "true"
        - name: LEADER_ELECTION_NAMESPACE
          value: "kagent"
```

### Security Hardening

1. **Use dedicated service account:**
   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: khook
     namespace: kagent
   ```

2. **Apply security context:**
   ```yaml
   securityContext:
     runAsNonRoot: true
     runAsUser: 65532
     fsGroup: 65532
     seccompProfile:
       type: RuntimeDefault
   ```

3. **Network policies:**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: khook
     namespace: kagent
   spec:
     podSelector:
       matchLabels:
         app: khook
     policyTypes:
     - Ingress
     - Egress
     egress:
     - to: []
       ports:
       - protocol: TCP
         port: 443  # HTTPS to Kagent API
     - to:
       - namespaceSelector: {}
       ports:
       - protocol: TCP
         port: 6443  # Kubernetes API
   ```

### Monitoring Setup

1. **Enable metrics:**
   ```yaml
   - name: METRICS_ENABLED
     value: "true"
   - name: METRICS_PORT
     value: "8080"
   ```

2. **Configure ServiceMonitor for Prometheus:**
   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: khook
     namespace: kagent
   spec:
     selector:
       matchLabels:
         app: khook
     endpoints:
     - port: metrics
       interval: 30s
       path: /metrics
   ```

## Troubleshooting Installation

### Common Issues

1. **CRD Installation Fails:**
   ```bash
   # Check cluster admin permissions
   kubectl auth can-i create customresourcedefinitions
   
   # Manually install CRDs
   kubectl apply -f config/crd/bases/ --validate=false
   ```

2. **Controller Won't Start:**
   ```bash
   # Check RBAC permissions
   kubectl auth can-i get events --as=system:serviceaccount:kagent:khook
   
   # Check resource constraints
   kubectl describe pod -n kagent -l app=khook
   ```

3. **API Connection Issues:**
   ```bash
   # Verify credentials
   kubectl get secret kagent-credentials -n kagent -o yaml
   
   # Test connectivity
   kubectl exec -n kagent deployment/khook -- \
     curl -v https://api.kagent.dev/health
   ```

### Debug Mode

Enable debug logging for troubleshooting:

```bash
kubectl set env deployment/khook -n kagent LOG_LEVEL=debug
```

## Upgrading

### Helm Upgrade

```bash
# From the cloned repository root
helm upgrade khook ./charts/khook-controller \
  --namespace kagent
```

### Manual Upgrade

```bash
# Update CRDs first
kubectl apply -f https://github.com/antweiss/khook/releases/latest/download/crds.yaml

# Update controller
kubectl apply -f https://github.com/antweiss/khook/releases/latest/download/install.yaml
```

## Uninstallation

### Complete Removal

```bash
# Remove hooks (this will stop monitoring)
kubectl delete hooks --all -A

# Remove controller installed via Helm
helm uninstall khook -n kagent

# Remove CRDs (optional - this will delete all hook resources)
kubectl delete crd hooks.kagent.dev
```

### Helm Uninstall

```bash
helm uninstall khook --namespace kagent
```

## Next Steps

After installation:

1. **Configure your first hook** using the [examples](../examples/)
2. **Set up monitoring** following the [monitoring guide](monitoring.md)
3. **Review security** with the [security guide](security.md)
4. **Read troubleshooting** in the [troubleshooting guide](troubleshooting.md)