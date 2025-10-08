package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/deduplication"
	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/kagent-dev/khook/internal/status"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Coordinator orchestrates the complete workflow lifecycle
type Coordinator struct {
	hookDiscovery   *HookDiscoveryService
	workflowManager *WorkflowManager
	logger          logr.Logger

	// namespaceStates tracks active workflows per namespace
	namespaceStates map[string]*NamespaceState
}

// NewCoordinator creates a new workflow coordinator
func NewCoordinator(
	k8sClient kubernetes.Interface,
	ctrlClient client.Client,
	kagentClient interfaces.KagentClient,
	eventRecorder interfaces.EventRecorder,
	sreServer interface{},
) *Coordinator {
	dedupManager := deduplication.NewManager()
	statusManager := status.NewManager(ctrlClient, eventRecorder)

	hookDiscovery := NewHookDiscoveryService(ctrlClient)
	workflowManager := NewWorkflowManager(
		k8sClient,
		ctrlClient,
		dedupManager,
		kagentClient,
		statusManager,
		eventRecorder,
		sreServer,
	)

	return &Coordinator{
		hookDiscovery:   hookDiscovery,
		workflowManager: workflowManager,
		logger:          log.Log.WithName("workflow-coordinator"),
		namespaceStates: make(map[string]*NamespaceState),
	}
}

// Start begins the workflow coordination process
func (c *Coordinator) Start(ctx context.Context) error {
	c.logger.Info("Starting workflow coordinator")

	// Load existing events into SRE server if available
	c.logger.Info("Attempting to load existing events", "sreServerType", fmt.Sprintf("%T", c.workflowManager.sreServer))
	if sreServer, ok := c.workflowManager.sreServer.(interface{ LoadExistingEvents(context.Context) }); ok {
		c.logger.Info("Loading existing events into SRE server")
		sreServer.LoadExistingEvents(ctx)
	} else {
		c.logger.Info("SRE server does not support LoadExistingEvents method")
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial sync
	if err := c.sync(ctx); err != nil {
		c.logger.Error(err, "Initial sync failed")
	}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Stopping workflow coordinator")
			c.stopAllWorkflows()
			return ctx.Err()

		case <-ticker.C:
			if err := c.sync(ctx); err != nil {
				c.logger.Error(err, "Sync failed")
			}
		}
	}
}

// sync synchronizes workflows with current hook state
func (c *Coordinator) sync(ctx context.Context) error {
	c.logger.V(1).Info("Starting workflow sync")

	hooksByNamespace, err := c.hookDiscovery.DiscoverHooks(ctx)
	if err != nil {
		return err
	}

	hookCount := c.hookDiscovery.GetHookCount(hooksByNamespace)
	c.logger.Info("Discovered hooks", "totalHooks", hookCount)

	// Start new workflows and restart changed ones
	for namespace, hooks := range hooksByNamespace {
		c.manageNamespaceWorkflow(ctx, namespace, hooks)
	}

	// Stop workflows for namespaces that no longer have hooks
	c.cleanupOrphanedWorkflows(hooksByNamespace)

	if len(hooksByNamespace) == 0 {
		c.logger.Info("No hooks found; all workflows stopped")
	}

	return nil
}

// manageNamespaceWorkflow ensures the correct workflow is running for a namespace
func (c *Coordinator) manageNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
) {
	signature := c.workflowManager.CalculateSignature(hooks)

	if state, exists := c.namespaceStates[namespace]; exists {
		if state.Signature == signature {
			c.logger.V(1).Info("No changes in hooks; keeping workflow running", "namespace", namespace)
			return
		}

		c.logger.Info("Restarting namespace workflow due to hook changes", "namespace", namespace)
		c.workflowManager.StopNamespaceWorkflow(namespace, state)
		delete(c.namespaceStates, namespace)
	}

	// Start new workflow
	state, err := c.workflowManager.StartNamespaceWorkflow(ctx, namespace, hooks, signature)
	if err != nil {
		c.logger.Error(err, "Failed to start namespace workflow", "namespace", namespace)
		return
	}

	c.namespaceStates[namespace] = state
	c.logger.Info("Started namespace workflow", "namespace", namespace, "hookCount", len(hooks))
}

// cleanupOrphanedWorkflows stops workflows for namespaces that no longer have hooks
func (c *Coordinator) cleanupOrphanedWorkflows(hooksByNamespace map[string][]*kagentv1alpha2.Hook) {
	for namespace, state := range c.namespaceStates {
		if _, exists := hooksByNamespace[namespace]; !exists {
			c.logger.Info("Stopping orphaned namespace workflow", "namespace", namespace)
			c.workflowManager.StopNamespaceWorkflow(namespace, state)
			delete(c.namespaceStates, namespace)
		}
	}
}

// stopAllWorkflows stops all running workflows
func (c *Coordinator) stopAllWorkflows() {
	c.logger.Info("Stopping all workflows", "namespaceCount", len(c.namespaceStates))

	for namespace, state := range c.namespaceStates {
		c.logger.Info("Stopping namespace workflow", "namespace", namespace)
		c.workflowManager.StopNamespaceWorkflow(namespace, state)
	}

	c.namespaceStates = make(map[string]*NamespaceState)
}
