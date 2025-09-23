# Plugin Deployment Guide

This guide covers deploying KHook plugins in various environments and configurations.

## Table of Contents

- [Deployment Methods](#deployment-methods)
- [Built-in Plugin Deployment](#built-in-plugin-deployment)
- [Configuration Management](#configuration-management)
- [Environment-Specific Deployments](#environment-specific-deployments)
- [Monitoring and Observability](#monitoring-and-observability)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)

## Deployment Methods

### Built-in Plugins

Built-in plugins are compiled directly into the KHook binary and deployed with the main controller.

**Advantages:**
- No additional deployment complexity
- Better performance (no dynamic loading overhead)
- Easier dependency management
- More secure (no external plugin files)

**Disadvantages:**
- Requires recompilation for plugin updates
- Larger binary size
- Less flexibility for custom plugins

### Dynamic Plugins (Future)

Dynamic plugins will be loaded at runtime from `.so` files.

**Advantages:**
- Hot-swappable plugins
- Smaller core binary
- Easy plugin distribution
- Third-party plugin support

**Disadvantages:**
- Additional security considerations
- Runtime loading complexity
- Dependency management challenges

## Built-in Plugin Deployment

### 1. Register Plugin in Code

Add your plugin to the workflow manager:

```go
// internal/workflow/plugin_workflow_manager.go
func (pwm *PluginWorkflowManager) registerBuiltinPlugins(ctx context.Context) error {
    // Register Kubernetes plugin
    if err := pwm.registerKubernetesPlugin(ctx); err != nil {
        return err
    }

    // Register your custom plugin
    if err := pwm.registerYourPlugin(ctx); err != nil {
        return err
    }

    return nil
}

func (pwm *PluginWorkflowManager) registerYourPlugin(ctx context.Context) error {
    source := yourplugin.NewYourEventSource()
    metadata := &plugin.PluginMetadata{
        Name:        source.Name(),
        Version:     source.Version(),
        EventTypes:  source.SupportedEventTypes(),
        Description: "Your custom plugin description",
        Path:        "built-in",
    }

    loadedPlugin := &plugin.LoadedPlugin{
        Metadata:    metadata,
        EventSource: source,
        Plugin:      nil,
        Active:      false,
    }

    return pwm.pluginManager.RegisterBuiltinPlugin("your-plugin", loadedPlugin)
}
```

### 2. Build and Deploy

```bash
# Build the updated binary
make docker-build

# Deploy to Kubernetes
make helm-upgrade
```

### 3. Verify Deployment

```bash
# Check plugin registration in logs
kubectl logs -n kagent deployment/khook | grep "Successfully registered.*plugin"

# Verify plugin is active
kubectl logs -n kagent deployment/khook | grep "your-plugin"
```

## Configuration Management

### ConfigMaps for Plugin Configuration

```yaml
# config-plugin-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: plugin-config
  namespace: kagent
data:
  plugins.yaml: |
    plugins:
      - name: kubernetes
        enabled: true
        config:
          namespace: "production"
      
      - name: webhook
        enabled: true
        config:
          port: 8090
          path: "/webhook"
          timeout: "30s"
      
      - name: your-plugin
        enabled: true
        config:
          endpoint: "https://api.example.com"
          apiKey: "${API_KEY}"
          retries: 3
```

### Event Mappings Configuration

```yaml
# config-event-mappings.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: event-mappings
  namespace: kagent
data:
  event-mappings.yaml: |
    mappings:
      - eventSource: your-plugin
        eventType: your-event
        internalType: YourEvent
        description: "Your plugin event"
        severity: warning
        tags:
          category: your-plugin
          impact: notification
        enabled: true
```

### Secrets for Sensitive Configuration

```yaml
# plugin-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: plugin-secrets
  namespace: kagent
type: Opaque
data:
  api-key: <base64-encoded-api-key>
  webhook-token: <base64-encoded-webhook-token>
  cert-file: <base64-encoded-certificate>
```

### Helm Chart Integration

Update the Helm chart to include plugin configurations:

```yaml
# helm/khook/templates/configmap-plugins.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "khook.fullname" . }}-plugin-config
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "khook.labels" . | nindent 4 }}
data:
  plugins.yaml: |
    {{- toYaml .Values.plugins | nindent 4 }}
```

```yaml
# helm/khook/values.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "{{ .Values.watchNamespace | default "default" }}"
  
  - name: webhook
    enabled: "{{ .Values.webhook.enabled | default false }}"
    config:
      port: "{{ .Values.webhook.port | default 8090 }}"
      path: "{{ .Values.webhook.path | default "/webhook" }}"

webhook:
  enabled: false
  port: 8090
  path: "/webhook"
```

## Environment-Specific Deployments

### Development Environment

```yaml
# values-dev.yaml
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

eventMappings:
  - eventSource: kubernetes
    eventType: pod-restart
    severity: info  # Lower severity in dev
    enabled: true

logLevel: debug
```

```bash
# Deploy to development
helm upgrade khook ./helm/khook \
  --namespace kagent-dev \
  --create-namespace \
  --values values-dev.yaml
```

### Staging Environment

```yaml
# values-staging.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "staging"
      retryInterval: "10s"
  
  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/staging-webhook"
      rateLimitRPS: 10

eventMappings:
  - eventSource: kubernetes
    eventType: pod-restart
    severity: warning
    enabled: true

resources:
  limits:
    memory: "256Mi"
    cpu: "250m"
  requests:
    memory: "128Mi"
    cpu: "100m"
```

### Production Environment

```yaml
# values-prod.yaml
plugins:
  - name: kubernetes
    enabled: true
    config:
      namespace: "production"
      retryInterval: "5s"
      maxRetries: 5
      healthCheckInterval: "30s"
  
  - name: webhook
    enabled: true
    config:
      port: 8090
      path: "/webhook"
      enableTLS: true
      certFile: "/etc/certs/tls.crt"
      keyFile: "/etc/certs/tls.key"
      rateLimitRPS: 100
      maxConcurrentRequests: 200

eventMappings:
  - eventSource: kubernetes
    eventType: oom-kill
    severity: critical
    enabled: true
  
  - eventSource: kubernetes
    eventType: pod-restart
    severity: error
    enabled: true

resources:
  limits:
    memory: "1Gi"
    cpu: "1000m"
  requests:
    memory: "512Mi"
    cpu: "500m"

replicaCount: 2

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

## Monitoring and Observability

### Plugin Metrics

```yaml
# ServiceMonitor for Prometheus
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: khook-plugins
  namespace: kagent
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: khook
  endpoints:
  - port: metrics
    path: /metrics
    interval: 30s
```

### Plugin-Specific Alerts

```yaml
# PrometheusRule for plugin alerts
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: khook-plugin-alerts
  namespace: kagent
spec:
  groups:
  - name: khook-plugins
    rules:
    - alert: PluginDown
      expr: khook_plugin_active == 0
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "KHook plugin {{ $labels.plugin }} is down"
        description: "Plugin {{ $labels.plugin }} has been inactive for more than 5 minutes"
    
    - alert: PluginHighErrorRate
      expr: rate(khook_plugin_errors_total[5m]) > 0.1
      for: 2m
      labels:
        severity: warning
      annotations:
        summary: "High error rate for plugin {{ $labels.plugin }}"
        description: "Plugin {{ $labels.plugin }} error rate is {{ $value }} errors/sec"
    
    - alert: PluginEventProcessingLag
      expr: khook_plugin_event_processing_duration_seconds > 30
      for: 1m
      labels:
        severity: warning
      annotations:
        summary: "Plugin {{ $labels.plugin }} processing lag"
        description: "Plugin {{ $labels.plugin }} is taking {{ $value }}s to process events"
```

### Logging Configuration

```yaml
# Fluent Bit configuration for plugin logs
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluent-bit-config
  namespace: kagent
data:
  fluent-bit.conf: |
    [INPUT]
        Name tail
        Path /var/log/containers/khook-*.log
        Parser docker
        Tag khook.*
        Refresh_Interval 5
    
    [FILTER]
        Name grep
        Match khook.*
        Regex log plugin
    
    [OUTPUT]
        Name elasticsearch
        Match khook.*
        Host elasticsearch.logging.svc.cluster.local
        Port 9200
        Index khook-plugins
```

## Troubleshooting

### Common Deployment Issues

#### Plugin Not Loading

**Symptoms:**
```
kubectl logs -n kagent deployment/khook | grep "plugin.*not found"
```

**Solutions:**
1. Verify plugin registration in code
2. Check build process includes plugin
3. Verify plugin dependencies

#### Configuration Errors

**Symptoms:**
```
kubectl logs -n kagent deployment/khook | grep "failed to initialize plugin"
```

**Solutions:**
1. Validate ConfigMap syntax
2. Check required configuration parameters
3. Verify secret references

#### Resource Constraints

**Symptoms:**
```
kubectl describe pod -n kagent khook-xxx | grep -i "oom\|memory"
```

**Solutions:**
1. Increase memory limits
2. Optimize plugin memory usage
3. Add resource monitoring

### Debug Commands

```bash
# Check plugin status
kubectl logs -n kagent deployment/khook | grep -i plugin

# Verify configuration
kubectl get configmap plugin-config -n kagent -o yaml

# Check secrets
kubectl get secret plugin-secrets -n kagent -o yaml

# Monitor resource usage
kubectl top pods -n kagent

# Check events
kubectl get events -n kagent --sort-by='.lastTimestamp'

# Port forward for metrics
kubectl port-forward -n kagent deployment/khook 8080:8080
curl http://localhost:8080/metrics | grep plugin
```

## Best Practices

### Security

1. **Use Secrets for Sensitive Data**
   ```yaml
   env:
   - name: API_KEY
     valueFrom:
       secretKeyRef:
         name: plugin-secrets
         key: api-key
   ```

2. **Network Policies**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: khook-plugin-network-policy
   spec:
     podSelector:
       matchLabels:
         app.kubernetes.io/name: khook
     policyTypes:
     - Egress
     egress:
     - to: []
       ports:
       - protocol: TCP
         port: 443  # HTTPS only
   ```

3. **RBAC Restrictions**
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: Role
   metadata:
     name: khook-plugin-role
   rules:
   - apiGroups: [""]
     resources: ["configmaps"]
     verbs: ["get", "list"]
     resourceNames: ["plugin-config", "event-mappings"]
   ```

### Performance

1. **Resource Limits**
   ```yaml
   resources:
     limits:
       memory: "512Mi"
       cpu: "500m"
     requests:
       memory: "256Mi"
       cpu: "250m"
   ```

2. **Horizontal Pod Autoscaling**
   ```yaml
   apiVersion: autoscaling/v2
   kind: HorizontalPodAutoscaler
   metadata:
     name: khook-hpa
   spec:
     scaleTargetRef:
       apiVersion: apps/v1
       kind: Deployment
       name: khook
     minReplicas: 2
     maxReplicas: 10
     metrics:
     - type: Resource
       resource:
         name: cpu
         target:
           type: Utilization
           averageUtilization: 70
   ```

### Reliability

1. **Health Checks**
   ```yaml
   livenessProbe:
     httpGet:
       path: /healthz
       port: 8081
     initialDelaySeconds: 30
     periodSeconds: 10
   
   readinessProbe:
     httpGet:
       path: /readyz
       port: 8081
     initialDelaySeconds: 5
     periodSeconds: 5
   ```

2. **Pod Disruption Budget**
   ```yaml
   apiVersion: policy/v1
   kind: PodDisruptionBudget
   metadata:
     name: khook-pdb
   spec:
     minAvailable: 1
     selector:
       matchLabels:
         app.kubernetes.io/name: khook
   ```

### Observability

1. **Structured Logging**
   ```yaml
   env:
   - name: LOG_FORMAT
     value: "json"
   - name: LOG_LEVEL
     value: "info"
   ```

2. **Metrics Collection**
   ```yaml
   annotations:
     prometheus.io/scrape: "true"
     prometheus.io/port: "8080"
     prometheus.io/path: "/metrics"
   ```

3. **Distributed Tracing**
   ```yaml
   env:
   - name: JAEGER_ENDPOINT
     value: "http://jaeger-collector:14268/api/traces"
   - name: JAEGER_SERVICE_NAME
     value: "khook"
   ```

## Deployment Checklist

Before deploying plugins to production:

- [ ] Plugin code reviewed and tested
- [ ] Configuration validated in staging
- [ ] Security scan completed
- [ ] Performance testing done
- [ ] Monitoring and alerting configured
- [ ] Documentation updated
- [ ] Rollback plan prepared
- [ ] Team trained on new plugin
- [ ] Incident response procedures updated

## Rollback Procedures

### Quick Rollback

```bash
# Disable problematic plugin
kubectl patch configmap plugin-config -n kagent --type merge -p '{"data":{"plugins.yaml":"plugins:\n- name: problematic-plugin\n  enabled: false"}}'

# Restart deployment
kubectl rollout restart deployment/khook -n kagent
```

### Full Rollback

```bash
# Rollback to previous Helm release
helm rollback khook -n kagent

# Or rollback Kubernetes deployment
kubectl rollout undo deployment/khook -n kagent
```

### Emergency Disable

```bash
# Scale down to stop all processing
kubectl scale deployment khook --replicas=0 -n kagent

# Fix configuration
# ...

# Scale back up
kubectl scale deployment khook --replicas=2 -n kagent
```
