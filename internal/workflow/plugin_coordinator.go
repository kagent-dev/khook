package workflow

import (
	"context"
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

// PluginCoordinator orchestrates the complete workflow lifecycle using the plugin system
type PluginCoordinator struct {
	hookDiscovery         *HookDiscoveryService
	pluginWorkflowManager *PluginWorkflowManager
	logger                logr.Logger

	// namespaceStates tracks active workflows per namespace
	namespaceStates map[string]*NamespaceState
}

// NewPluginCoordinator creates a new plugin-aware workflow coordinator
func NewPluginCoordinator(
	k8sClient kubernetes.Interface,
	ctrlClient client.Client,
	kagentClient interfaces.KagentClient,
	eventRecorder interfaces.EventRecorder,
) *PluginCoordinator {
	dedupManager := deduplication.NewManager()
	statusManager := status.NewManager(ctrlClient, eventRecorder)

	hookDiscovery := NewHookDiscoveryService(ctrlClient)

	config := PluginWorkflowManagerConfig{
		K8sClient:     k8sClient,
		CtrlClient:    ctrlClient,
		DedupManager:  dedupManager,
		KagentClient:  kagentClient,
		StatusManager: statusManager,
		EventRecorder: eventRecorder,
		// MappingFilePath will use default if empty
	}

	pluginWorkflowManager := NewPluginWorkflowManager(config)

	return &PluginCoordinator{
		hookDiscovery:         hookDiscovery,
		pluginWorkflowManager: pluginWorkflowManager,
		logger:                log.Log.WithName("plugin-coordinator"),
		namespaceStates:       make(map[string]*NamespaceState),
	}
}

// Start begins the plugin-based workflow coordination process
func (pc *PluginCoordinator) Start(ctx context.Context) error {
	pc.logger.Info("Starting plugin-based workflow coordinator")

	// Initialize the plugin system
	if err := pc.pluginWorkflowManager.Initialize(ctx); err != nil {
		pc.logger.Error(err, "Failed to initialize plugin workflow manager")
		return err
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Ensure graceful shutdown of plugin system
	defer func() {
		if err := pc.pluginWorkflowManager.Shutdown(); err != nil {
			pc.logger.Error(err, "Failed to shutdown plugin workflow manager")
		}
	}()

	// Initial sync
	if err := pc.sync(ctx); err != nil {
		pc.logger.Error(err, "Initial sync failed")
	}

	for {
		select {
		case <-ctx.Done():
			pc.logger.Info("Stopping plugin-based workflow coordinator")
			pc.stopAllWorkflows()
			return ctx.Err()

		case <-ticker.C:
			if err := pc.sync(ctx); err != nil {
				pc.logger.Error(err, "Sync failed")
			}
		}
	}
}

// sync synchronizes workflows with current hook state
func (pc *PluginCoordinator) sync(ctx context.Context) error {
	pc.logger.V(1).Info("Starting plugin-based workflow sync")

	hooksByNamespace, err := pc.hookDiscovery.DiscoverHooks(ctx)
	if err != nil {
		return err
	}

	hookCount := pc.hookDiscovery.GetHookCount(hooksByNamespace)
	pc.logger.Info("Discovered hooks for plugin processing", "totalHooks", hookCount)

	// Start new workflows and restart changed ones
	for namespace, hooks := range hooksByNamespace {
		pc.manageNamespaceWorkflow(ctx, namespace, hooks)
	}

	// Stop workflows for namespaces that no longer have hooks
	pc.cleanupOrphanedWorkflows(hooksByNamespace)

	if len(hooksByNamespace) == 0 {
		pc.logger.Info("No hooks found; all plugin workflows stopped")
	}

	return nil
}

// manageNamespaceWorkflow ensures the correct plugin workflow is running for a namespace
func (pc *PluginCoordinator) manageNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
) {
	signature := pc.pluginWorkflowManager.CalculateSignature(hooks)

	if state, exists := pc.namespaceStates[namespace]; exists {
		if state.Signature == signature {
			pc.logger.V(1).Info("No changes in hooks; keeping plugin workflow running", "namespace", namespace)
			return
		}

		pc.logger.Info("Restarting plugin namespace workflow due to hook changes", "namespace", namespace)
		pc.pluginWorkflowManager.StopNamespaceWorkflow(namespace, state)
		delete(pc.namespaceStates, namespace)
	}

	// Start new plugin workflow
	state, err := pc.pluginWorkflowManager.StartNamespaceWorkflow(ctx, namespace, hooks, signature)
	if err != nil {
		pc.logger.Error(err, "Failed to start plugin namespace workflow", "namespace", namespace)
		return
	}

	pc.namespaceStates[namespace] = state
	pc.logger.Info("Started plugin namespace workflow", "namespace", namespace, "hookCount", len(hooks))
}

// cleanupOrphanedWorkflows stops workflows for namespaces that no longer have hooks
func (pc *PluginCoordinator) cleanupOrphanedWorkflows(hooksByNamespace map[string][]*kagentv1alpha2.Hook) {
	for namespace, state := range pc.namespaceStates {
		if _, exists := hooksByNamespace[namespace]; !exists {
			pc.logger.Info("Stopping orphaned plugin namespace workflow", "namespace", namespace)
			pc.pluginWorkflowManager.StopNamespaceWorkflow(namespace, state)
			delete(pc.namespaceStates, namespace)
		}
	}
}

// stopAllWorkflows stops all running plugin workflows
func (pc *PluginCoordinator) stopAllWorkflows() {
	pc.logger.Info("Stopping all plugin workflows", "namespaceCount", len(pc.namespaceStates))

	for namespace, state := range pc.namespaceStates {
		pc.logger.Info("Stopping plugin namespace workflow", "namespace", namespace)
		pc.pluginWorkflowManager.StopNamespaceWorkflow(namespace, state)
	}

	pc.namespaceStates = make(map[string]*NamespaceState)
}
