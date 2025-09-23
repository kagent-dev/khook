# Plugin Troubleshooting Guide

This guide helps diagnose and resolve common issues with KHook plugins.

## Table of Contents

- [Diagnostic Tools](#diagnostic-tools)
- [Common Issues](#common-issues)
- [Plugin-Specific Issues](#plugin-specific-issues)
- [Performance Issues](#performance-issues)
- [Configuration Issues](#configuration-issues)
- [Debug Mode](#debug-mode)
- [Monitoring and Metrics](#monitoring-and-metrics)

## Diagnostic Tools

### Check Plugin Status

```bash
# Check controller logs
kubectl logs -n kagent deployment/khook --tail=50

# Check plugin registration
kubectl logs -n kagent deployment/khook | grep -i "plugin"

# Check plugin manager status
kubectl logs -n kagent deployment/khook | grep -i "plugin-manager"
```

### Verify Configuration

```bash
# Check ConfigMaps
kubectl get configmaps -n kagent

# View event mappings
kubectl get configmap event-mappings -n kagent -o yaml

# View plugin configuration
kubectl get configmap plugin-config -n kagent -o yaml
```

### Test Event Processing

```bash
# Create a test pod to trigger events
kubectl run test-pod --image=nginx --restart=Never
kubectl delete pod test-pod

# Check for events
kubectl get events --sort-by='.lastTimestamp'
```

## Common Issues

### Plugin Not Loading

**Symptoms:**
- Plugin not mentioned in startup logs
- No events from plugin source
- Plugin not listed in metrics

**Diagnostic Steps:**

1. **Check Plugin Registration:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep "Successfully registered.*plugin"
   ```

2. **Verify Plugin Configuration:**
   ```bash
   kubectl get configmap plugin-config -n kagent -o yaml
   ```

3. **Check for Initialization Errors:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep -i "failed to.*plugin"
   ```

**Solutions:**

1. **Missing Plugin Registration:**
   ```go
   // Ensure plugin is registered in workflow manager
   func (pwm *PluginWorkflowManager) registerBuiltinPlugins(ctx context.Context) error {
       // Add your plugin registration here
       return pwm.registerYourPlugin(ctx)
   }
   ```

2. **Invalid Configuration:**
   ```yaml
   # Fix plugin configuration
   plugins:
     - name: your-plugin
       enabled: true  # Ensure enabled is true
       config:
         # Valid configuration parameters
   ```

3. **Plugin Interface Issues:**
   ```go
   // Ensure all interface methods are implemented
   func (p *YourPlugin) Name() string { return "your-plugin" }
   func (p *YourPlugin) Version() string { return "1.0.0" }
   // ... other required methods
   ```

### Events Not Being Processed

**Symptoms:**
- Plugin loads successfully
- Events are generated but not processed
- No Hook triggers

**Diagnostic Steps:**

1. **Check Event Mappings:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep -i "mapping"
   ```

2. **Verify Hook Configuration:**
   ```bash
   kubectl get hooks -A
   kubectl describe hook your-hook -n your-namespace
   ```

3. **Check Event Validation:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep -i "invalid event"
   ```

**Solutions:**

1. **Missing Event Mappings:**
   ```yaml
   # Add event mappings
   mappings:
     - eventSource: your-plugin
       eventType: your-event-type
       internalType: YourEventType
       enabled: true
   ```

2. **Hook Configuration Mismatch:**
   ```yaml
   # Ensure Hook eventType matches mapping internalType
   spec:
     eventConfigurations:
     - eventType: "YourEventType"  # Must match mapping internalType
   ```

3. **Event Validation Failures:**
   ```go
   // Ensure events have required fields
   event := plugin.NewEvent(
       "your-event-type",  // Must not be empty
       "resource-name",    // Must not be empty
       "namespace",
       "reason",
       "message",          // Must not be empty
       "your-plugin",      // Must not be empty
   )
   ```

### Plugin Crashes or Stops

**Symptoms:**
- Plugin starts but stops working
- Panic messages in logs
- Plugin restarts frequently

**Diagnostic Steps:**

1. **Check for Panics:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep -i "panic\|fatal"
   ```

2. **Monitor Resource Usage:**
   ```bash
   kubectl top pods -n kagent
   ```

3. **Check Plugin Health:**
   ```bash
   kubectl logs -n kagent deployment/khook | grep "plugin.*stopped\|plugin.*failed"
   ```

**Solutions:**

1. **Handle Panics Gracefully:**
   ```go
   func (p *YourPlugin) Start(ctx context.Context, eventCh chan<- plugin.Event) error {
       defer func() {
           if r := recover(); r != nil {
               p.logger.Error(fmt.Errorf("plugin panic: %v", r), "Plugin panicked")
           }
       }()
       // Plugin logic here
   }
   ```

2. **Implement Proper Resource Cleanup:**
   ```go
   func (p *YourPlugin) Stop() error {
       // Close connections
       if p.connection != nil {
           p.connection.Close()
       }
       // Stop goroutines
       close(p.stopCh)
       // Clean up resources
       return nil
   }
   ```

3. **Add Health Checks:**
   ```go
   func (p *YourPlugin) healthCheck() {
       ticker := time.NewTicker(30 * time.Second)
       defer ticker.Stop()
       
       for {
           select {
           case <-ticker.C:
               if !p.isHealthy() {
                   p.logger.Error(nil, "Plugin health check failed")
                   // Attempt recovery
               }
           case <-p.stopCh:
               return
           }
       }
   }
   ```

## Plugin-Specific Issues

### Kubernetes Plugin Issues

**Issue: No Kubernetes Events Detected**

```bash
# Check RBAC permissions
kubectl auth can-i list events --as=system:serviceaccount:kagent:khook

# Check event watcher
kubectl logs -n kagent deployment/khook | grep -i "kubernetes.*watcher"

# Verify events exist
kubectl get events -n your-namespace
```

**Solution:**
```yaml
# Ensure proper RBAC
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: khook-events-reader
rules:
- apiGroups: ["events.k8s.io"]
  resources: ["events"]
  verbs: ["get", "list", "watch"]
```

### Webhook Plugin Issues

**Issue: Webhook Server Not Responding**

```bash
# Test webhook endpoint
curl -X POST http://localhost:8090/webhook \
  -H "Content-Type: application/json" \
  -d '{"test": "data"}'

# Check port binding
kubectl logs -n kagent deployment/khook | grep -i "webhook.*server"
```

**Solution:**
```go
// Ensure proper server configuration
func (w *WebhookEventSource) Start(ctx context.Context, eventCh chan<- plugin.Event) error {
    w.server = &http.Server{
        Addr:         fmt.Sprintf(":%d", w.port),
        Handler:      w.handler,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
    }
    
    go func() {
        if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            w.logger.Error(err, "Webhook server failed")
        }
    }()
    
    return nil
}
```

## Performance Issues

### High CPU Usage

**Symptoms:**
- Controller pod using excessive CPU
- Slow event processing
- Timeouts in logs

**Diagnostic Steps:**

```bash
# Check CPU usage
kubectl top pods -n kagent

# Profile the application
kubectl exec -n kagent deployment/khook -- go tool pprof http://localhost:6060/debug/pprof/profile
```

**Solutions:**

1. **Optimize Event Processing:**
   ```go
   // Use buffered channels
   eventCh := make(chan plugin.Event, 1000)
   
   // Implement batching
   func (p *YourPlugin) processBatch(events []plugin.Event) {
       // Process events in batches
   }
   ```

2. **Add Rate Limiting:**
   ```go
   import "golang.org/x/time/rate"
   
   limiter := rate.NewLimiter(rate.Limit(100), 10) // 100 events/sec, burst of 10
   
   func (p *YourPlugin) sendEvent(event plugin.Event) {
       if limiter.Allow() {
           p.eventCh <- event
       }
   }
   ```

### High Memory Usage

**Symptoms:**
- Pod memory usage growing over time
- OOMKilled events
- Memory leaks

**Diagnostic Steps:**

```bash
# Check memory usage
kubectl top pods -n kagent

# Get memory profile
kubectl exec -n kagent deployment/khook -- go tool pprof http://localhost:6060/debug/pprof/heap
```

**Solutions:**

1. **Fix Memory Leaks:**
   ```go
   // Properly close resources
   func (p *YourPlugin) Stop() error {
       // Close all connections
       for _, conn := range p.connections {
           conn.Close()
       }
       // Clear maps/slices
       p.cache = make(map[string]interface{})
       return nil
   }
   ```

2. **Implement Memory Limits:**
   ```go
   // Limit cache size
   const maxCacheSize = 1000
   
   func (p *YourPlugin) addToCache(key string, value interface{}) {
       if len(p.cache) >= maxCacheSize {
           // Remove oldest entries
           p.evictOldest()
       }
       p.cache[key] = value
   }
   ```

## Configuration Issues

### Invalid Configuration Format

**Symptoms:**
- Plugin fails to initialize
- Configuration parsing errors
- Default values used unexpectedly

**Diagnostic Steps:**

```bash
# Validate YAML syntax
kubectl get configmap plugin-config -n kagent -o yaml | yq eval '.'

# Check configuration loading
kubectl logs -n kagent deployment/khook | grep -i "config"
```

**Solutions:**

1. **Validate Configuration Schema:**
   ```go
   type PluginConfig struct {
       Port     int    `yaml:"port" validate:"min=1,max=65535"`
       Path     string `yaml:"path" validate:"required"`
       Timeout  string `yaml:"timeout" validate:"required"`
   }
   
   func (p *YourPlugin) validateConfig(config map[string]interface{}) error {
       // Implement validation logic
       return nil
   }
   ```

2. **Provide Clear Error Messages:**
   ```go
   func (p *YourPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
       port, ok := config["port"]
       if !ok {
           return fmt.Errorf("required configuration 'port' is missing")
       }
       
       portInt, ok := port.(int)
       if !ok {
           return fmt.Errorf("configuration 'port' must be an integer, got %T", port)
       }
       
       if portInt < 1 || portInt > 65535 {
           return fmt.Errorf("configuration 'port' must be between 1 and 65535, got %d", portInt)
       }
       
       return nil
   }
   ```

### Environment Variable Issues

**Symptoms:**
- Configuration values not resolved
- Empty or default values used
- Environment-specific behavior not working

**Solutions:**

1. **Check Environment Variables:**
   ```bash
   kubectl exec -n kagent deployment/khook -- env | grep -i plugin
   ```

2. **Use Proper Environment Variable Expansion:**
   ```yaml
   # In ConfigMap
   config:
     port: "${PLUGIN_PORT:-8090}"
     token: "${PLUGIN_TOKEN}"
   ```

3. **Validate Environment Variables:**
   ```go
   func getEnvOrDefault(key, defaultValue string) string {
       if value := os.Getenv(key); value != "" {
           return value
       }
       return defaultValue
   }
   ```

## Debug Mode

### Enable Debug Logging

```bash
# Set debug log level
kubectl set env deployment/khook -n kagent LOG_LEVEL=debug

# Check debug logs
kubectl logs -n kagent deployment/khook | grep -i debug
```

### Plugin-Specific Debug Mode

```go
type YourPlugin struct {
    debug  bool
    logger logr.Logger
}

func (p *YourPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
    if debug, ok := config["debug"].(bool); ok {
        p.debug = debug
    }
    
    if p.debug {
        p.logger.Info("Debug mode enabled for plugin")
    }
    
    return nil
}

func (p *YourPlugin) debugLog(message string, keysAndValues ...interface{}) {
    if p.debug {
        p.logger.Info("[DEBUG] "+message, keysAndValues...)
    }
}
```

## Monitoring and Metrics

### Plugin Metrics

```bash
# Access metrics endpoint
kubectl port-forward -n kagent deployment/khook 8080:8080
curl http://localhost:8080/metrics | grep plugin
```

### Custom Plugin Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    pluginEventsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "khook_plugin_events_total",
            Help: "Total number of events processed by plugin",
        },
        []string{"plugin", "event_type"},
    )
    
    pluginErrorsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "khook_plugin_errors_total",
            Help: "Total number of plugin errors",
        },
        []string{"plugin", "error_type"},
    )
)

func (p *YourPlugin) recordEvent(eventType string) {
    pluginEventsTotal.WithLabelValues(p.Name(), eventType).Inc()
}

func (p *YourPlugin) recordError(errorType string) {
    pluginErrorsTotal.WithLabelValues(p.Name(), errorType).Inc()
}
```

### Health Checks

```bash
# Check plugin health
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
```

## Getting Help

If you're still experiencing issues:

1. **Check Documentation**: Review the [Plugin Development Guide](plugin-development.md)
2. **Search Issues**: Look for similar issues on [GitHub](https://github.com/kagent-dev/khook/issues)
3. **Enable Debug Mode**: Collect detailed logs with debug mode enabled
4. **Create Issue**: Report the issue with:
   - Plugin configuration
   - Error logs
   - Steps to reproduce
   - Environment details

## Prevention Best Practices

1. **Comprehensive Testing**: Test plugins thoroughly before deployment
2. **Gradual Rollout**: Deploy plugins to staging environments first
3. **Monitoring**: Set up proper monitoring and alerting
4. **Documentation**: Document plugin configuration and behavior
5. **Version Control**: Track plugin and configuration changes
