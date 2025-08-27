# Troubleshooting Guide

This guide helps you diagnose and resolve common issues with the KAgent Hook Controller.

## Quick Diagnostics

### Check Controller Status

```bash
# Check if controller is running
kubectl get pods -n kagent-system -l app=kagent-hook-controller

# Check controller logs
kubectl logs -n kagent-system deployment/kagent-hook-controller --tail=100

# Check hook resources
kubectl get hooks -A
```

### Verify Configuration

```bash
# Check Kagent API credentials
kubectl get secret kagent-credentials -o yaml

# Verify CRD installation
kubectl get crd hooks.kagent.dev

# Check RBAC permissions
kubectl auth can-i get events --as=system:serviceaccount:kagent-system:kagent-hook-controller
```

## Common Issues

### 1. Hook Not Processing Events

**Symptoms:**
- Hook is created successfully
- Events are occurring in the cluster
- No Kagent API calls are being made
- Hook status shows no active events

**Diagnostic Steps:**

```bash
# Check if events are being generated
kubectl get events --field-selector involvedObject.kind=Pod --sort-by='.lastTimestamp'

# Verify hook configuration
kubectl describe hook your-hook-name

# Check controller logs for event processing
kubectl logs -n kagent-system deployment/kagent-hook-controller | grep "event-processing"
```

**Common Causes & Solutions:**

1. **Controller not watching the namespace:**
   ```bash
   # Check if controller has RBAC permissions for the namespace
   kubectl auth can-i get events --namespace=your-namespace --as=system:serviceaccount:kagent-system:kagent-hook-controller
   ```

2. **Event type mismatch:**
   - Verify that the `eventType` in your hook matches actual Kubernetes events
   - Check event reasons: `kubectl get events --field-selector reason=Killing,reason=Failed`

3. **Hook in wrong namespace:**
   - Ensure hook is in the same namespace as the pods you want to monitor
   - Or use cluster-wide monitoring if configured

### 2. Kagent API Connection Failures

**Symptoms:**
- Events are being detected
- Controller logs show API connection errors
- Hook status shows failed API calls

**Diagnostic Steps:**

```bash
# Check API credentials
kubectl get secret kagent-credentials -o jsonpath='{.data.api-key}' | base64 -d

# Test API connectivity
kubectl exec -n kagent-system deployment/kagent-hook-controller -- \
  curl -v -H "Authorization: Bearer $KAGENT_API_KEY" $KAGENT_BASE_URL/health

# Check controller logs for API errors
kubectl logs -n kagent-system deployment/kagent-hook-controller | grep "kagent-api"
```**Common 
Causes & Solutions:**

1. **Invalid API credentials:**
   ```bash
   # Update credentials
   kubectl create secret generic kagent-credentials \
     --from-literal=api-key=your-correct-key \
     --from-literal=base-url=https://correct-url.com \
     --dry-run=client -o yaml | kubectl apply -f -
   
   # Restart controller to pick up new credentials
   kubectl rollout restart deployment/kagent-hook-controller -n kagent-system
   ```

2. **Network connectivity issues:**
   ```bash
   # Check DNS resolution
   kubectl exec -n kagent-system deployment/kagent-hook-controller -- nslookup api.kagent.dev
   
   # Check firewall/network policies
   kubectl get networkpolicies -A
   ```

3. **API endpoint unreachable:**
   - Verify the `KAGENT_BASE_URL` is correct
   - Check if the Kagent service is running
   - Validate SSL certificates if using HTTPS

### 3. Events Not Being Deduplicated

**Symptoms:**
- Same event triggers multiple Kagent calls within 10 minutes
- Hook status shows duplicate active events
- Excessive API calls in logs

**Diagnostic Steps:**

```bash
# Check active events in hook status
kubectl get hook your-hook-name -o jsonpath='{.status.activeEvents}' | jq .

# Check controller restart count
kubectl get pods -n kagent-system -l app=kagent-hook-controller

# Verify leader election is working
kubectl logs -n kagent-system deployment/kagent-hook-controller | grep "leader"
```

**Common Causes & Solutions:**

1. **Controller restarts causing memory loss:**
   ```bash
   # Check for frequent restarts
   kubectl describe pod -n kagent-system -l app=kagent-hook-controller
   
   # Increase memory limits if needed
   kubectl patch deployment kagent-hook-controller -n kagent-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","resources":{"limits":{"memory":"512Mi"}}}]}}}}'
   ```

2. **Multiple controller instances without leader election:**
   ```bash
   # Ensure only one controller is leader
   kubectl logs -n kagent-system deployment/kagent-hook-controller | grep "successfully acquired lease"
   
   # Check replica count
   kubectl get deployment kagent-hook-controller -n kagent-system
   ```

3. **Clock skew issues:**
   ```bash
   # Check system time on controller
   kubectl exec -n kagent-system deployment/kagent-hook-controller -- date
   
   # Compare with cluster time
   kubectl get nodes -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].lastTransitionTime}'
   ```

### 4. High Memory Usage

**Symptoms:**
- Controller pod consuming excessive memory
- OOMKilled events for controller pod
- Slow event processing

**Diagnostic Steps:**

```bash
# Monitor memory usage
kubectl top pod -n kagent-system -l app=kagent-hook-controller

# Check active events across all hooks
kubectl get hooks -A -o jsonpath='{range .items[*]}{.metadata.name}: {.status.activeEvents}{"\n"}{end}'

# Check for memory leaks in logs
kubectl logs -n kagent-system deployment/kagent-hook-controller | grep -i "memory\|leak\|gc"
```

**Solutions:**

1. **Increase resource limits:**
   ```bash
   kubectl patch deployment kagent-hook-controller -n kagent-system -p '{
     "spec": {
       "template": {
         "spec": {
           "containers": [{
             "name": "manager",
             "resources": {
               "limits": {"memory": "1Gi", "cpu": "500m"},
               "requests": {"memory": "256Mi", "cpu": "100m"}
             }
           }]
         }
       }
     }
   }'
   ```

2. **Clean up stale events:**
   ```bash
   # Restart controller to clean up memory
   kubectl rollout restart deployment/kagent-hook-controller -n kagent-system
   ```

### 5. Permission Denied Errors

**Symptoms:**
- Controller logs show RBAC permission errors
- Cannot watch events or update hook status
- "Forbidden" errors in logs

**Diagnostic Steps:**

```bash
# Check current permissions
kubectl auth can-i get events --as=system:serviceaccount:kagent-system:kagent-hook-controller
kubectl auth can-i update hooks --as=system:serviceaccount:kagent-system:kagent-hook-controller

# Verify ClusterRole and ClusterRoleBinding
kubectl get clusterrole kagent-hook-controller -o yaml
kubectl get clusterrolebinding kagent-hook-controller -o yaml
```

**Solutions:**

1. **Apply correct RBAC:**
   ```bash
   kubectl apply -f config/rbac/
   ```

2. **Verify service account:**
   ```bash
   kubectl get serviceaccount kagent-hook-controller -n kagent-system
   ```

## Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
# Enable debug logging
kubectl set env deployment/kagent-hook-controller -n kagent-system LOG_LEVEL=debug

# Watch debug logs
kubectl logs -n kagent-system deployment/kagent-hook-controller -f | grep DEBUG
```

## Performance Issues

### Slow Event Processing

**Symptoms:**
- Long delays between event occurrence and Kagent API calls
- High CPU usage on controller

**Solutions:**

1. **Increase controller resources:**
   ```bash
   kubectl patch deployment kagent-hook-controller -n kagent-system -p '{
     "spec": {
       "template": {
         "spec": {
           "containers": [{
             "name": "manager",
             "resources": {
               "limits": {"cpu": "1000m"},
               "requests": {"cpu": "200m"}
             }
           }]
         }
       }
     }
   }'
   ```

2. **Optimize hook configurations:**
   - Reduce number of event types per hook
   - Use more specific event filtering
   - Minimize prompt template complexity

### High API Call Volume

**Symptoms:**
- Kagent API rate limiting
- High network usage
- API timeout errors

**Solutions:**

1. **Implement backoff strategies:**
   - Controller automatically implements exponential backoff
   - Check logs for retry attempts

2. **Optimize hook configurations:**
   - Consolidate similar hooks
   - Use appropriate deduplication timeouts
   - Review event type selections

## Getting Help

### Log Collection

Collect comprehensive logs for support:

```bash
# Controller logs
kubectl logs -n kagent-system deployment/kagent-hook-controller --previous > controller-logs.txt

# Hook status
kubectl get hooks -A -o yaml > hooks-status.yaml

# Events
kubectl get events -A --sort-by='.lastTimestamp' > cluster-events.txt

# System info
kubectl version > cluster-info.txt
kubectl get nodes -o wide >> cluster-info.txt
```

### Support Channels

1. **GitHub Issues**: [kagent-hook-controller/issues](https://github.com/kagent-dev/kagent-hook-controller/issues)
2. **Community Forum**: [community.kagent.dev](https://community.kagent.dev)
3. **Documentation**: [docs.kagent.dev](https://docs.kagent.dev)

### Before Reporting Issues

Please include:
- Controller version and Kubernetes version
- Hook configuration (sanitized)
- Controller logs (last 100 lines)
- Steps to reproduce the issue
- Expected vs actual behavior