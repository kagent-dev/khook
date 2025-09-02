# Hook Configuration Examples

This directory contains example Hook configurations for common use cases.

## Available Examples

### Basic Examples
- [`basic-pod-monitoring.yaml`](basic-pod-monitoring.yaml) - Simple pod restart and OOM monitoring
- [`development-monitoring.yaml`](development-monitoring.yaml) - Lightweight monitoring for dev environments

### Production Examples  
- [`production-monitoring.yaml`](production-monitoring.yaml) - Comprehensive production monitoring
- [`multi-namespace-monitoring.yaml`](multi-namespace-monitoring.yaml) - Cross-namespace monitoring setup

### Specialized Examples
- [`security-monitoring.yaml`](security-monitoring.yaml) - Security-focused event monitoring
- [`performance-monitoring.yaml`](performance-monitoring.yaml) - Performance and resource monitoring
- [`ci-cd-monitoring.yaml`](ci-cd-monitoring.yaml) - CI/CD pipeline monitoring

## Quick Start

1. **Choose an example** that matches your use case
2. **Customize the configuration:**
   - Update `agentId` fields with your Kagent agent IDs
   - Modify prompts to match your requirements
   - Adjust namespaces as needed
3. **Apply the configuration:**
   ```bash
   kubectl apply -f examples/basic-pod-monitoring.yaml
   ```
4. **Verify the hook is working:**
   ```bash
   kubectl get hooks
   kubectl describe hook your-hook-name
   ```

## Configuration Tips

### Agent Selection
- Use specialized agents for different event types
- Consider creating dedicated agents for different environments
- Ensure agents have appropriate Kubernetes knowledge

### Prompt Design
- Include relevant context variables: `{{.ResourceName}}`, `{{.EventTime}}`
- Be specific about the type of analysis needed
- Include priority levels for production environments
- Add troubleshooting checklists for common issues

### Namespace Strategy
- Create separate hooks for different environments
- Use labels to organize and filter hooks
- Consider cluster-wide vs namespace-specific monitoring

## Testing Your Configuration

1. **Validate the YAML:**
   ```bash
   kubectl apply --dry-run=client -f your-hook.yaml
   ```

2. **Create a test event:**
   ```bash
   # Create a pod that will restart
   kubectl run test-pod --image=busybox --restart=Always -- sh -c 'sleep 10; exit 1'
   ```

3. **Monitor hook status:**
   ```bash
   kubectl get hook your-hook-name -w
   ```

4. **Check controller logs:**
   ```bash
   kubectl logs -n kagent deployment/kagent-hook-controller -f
   ```