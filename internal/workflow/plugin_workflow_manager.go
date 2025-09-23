package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/event"
	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/kagent-dev/khook/internal/pipeline"
	"github.com/kagent-dev/khook/internal/plugin"
	k8splugin "github.com/kagent-dev/khook/internal/plugin/kubernetes"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PluginWorkflowManagerConfig holds configuration for the PluginWorkflowManager
type PluginWorkflowManagerConfig struct {
	K8sClient       kubernetes.Interface
	CtrlClient      client.Client
	DedupManager    interfaces.DeduplicationManager
	KagentClient    interfaces.KagentClient
	StatusManager   interfaces.StatusManager
	EventRecorder   interfaces.EventRecorder
	MappingFilePath string
}

// PluginWorkflowManager manages per-namespace event processing workflows using the plugin system
type PluginWorkflowManager struct {
	config PluginWorkflowManagerConfig
	logger logr.Logger

	// Plugin system components
	pluginManager *plugin.Manager
	mappingLoader *event.MappingLoader
}

// NewPluginWorkflowManager creates a new plugin-aware workflow manager
func NewPluginWorkflowManager(config PluginWorkflowManagerConfig) *PluginWorkflowManager {
	logger := log.Log.WithName("plugin-workflow-manager")

	// Set default mapping file path if not provided
	if config.MappingFilePath == "" {
		config.MappingFilePath = filepath.Join("config", "event-mappings.yaml")
	}

	// Initialize plugin manager
	pluginManager := plugin.NewManager(logger.WithName("plugin-manager"), []string{})

	// Initialize event mapping loader
	mappingLoader := event.NewMappingLoader(logger.WithName("mapping-loader"))

	return &PluginWorkflowManager{
		config:        config,
		logger:        logger,
		pluginManager: pluginManager,
		mappingLoader: mappingLoader,
	}
}

// Initialize sets up the plugin system with configuration
func (pwm *PluginWorkflowManager) Initialize(ctx context.Context) error {
	pwm.logger.Info("Initializing plugin workflow manager")

	// Load event mappings
	if err := pwm.mappingLoader.LoadMappings(pwm.config.MappingFilePath); err != nil {
		pwm.logger.Info("Event mappings file not found, using default mappings", "file", pwm.config.MappingFilePath, "error", err.Error())
		// For testing or when config file is missing, create default Kubernetes mappings
		if err := pwm.createDefaultMappings(); err != nil {
			return fmt.Errorf("failed to create default mappings: %w", err)
		}
	}

	// Register built-in Kubernetes plugin
	if err := pwm.registerKubernetesPlugin(ctx); err != nil {
		return fmt.Errorf("failed to register Kubernetes plugin: %w", err)
	}

	pwm.logger.Info("Plugin workflow manager initialized successfully")
	return nil
}

// registerKubernetesPlugin registers the built-in Kubernetes event source
func (pwm *PluginWorkflowManager) registerKubernetesPlugin(ctx context.Context) error {
	pwm.logger.Info("Registering built-in Kubernetes plugin")

	// Create Kubernetes event source
	k8sEventSource := k8splugin.NewKubernetesEventSource()

	// Create plugin metadata
	metadata := &plugin.PluginMetadata{
		Name:        k8sEventSource.Name(),
		Version:     k8sEventSource.Version(),
		EventTypes:  k8sEventSource.SupportedEventTypes(),
		Description: "Built-in Kubernetes event source plugin",
		Path:        "built-in",
	}

	// Create loaded plugin
	loadedPlugin := &plugin.LoadedPlugin{
		Metadata:    metadata,
		EventSource: k8sEventSource,
		Plugin:      nil, // Built-in plugins don't have a .so file
		Active:      false,
	}

	// Register the plugin manually (since it's built-in)
	pwm.pluginManager.RegisterBuiltinPlugin("kubernetes", loadedPlugin)

	pwm.logger.Info("Successfully registered Kubernetes plugin",
		"name", metadata.Name,
		"version", metadata.Version,
		"eventTypes", metadata.EventTypes)

	return nil
}

// StartNamespaceWorkflow starts a workflow for a specific namespace using plugins
func (pwm *PluginWorkflowManager) StartNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
	signature string,
) (*NamespaceState, error) {

	ctxNS, cancel := context.WithCancel(ctx)
	state := &NamespaceState{
		Cancel:    cancel,
		Signature: signature,
	}

	eventTypes := pwm.uniqueEventTypes(hooks)
	pwm.logger.Info("Starting plugin-based namespace workflow",
		"namespace", namespace,
		"hookCount", len(hooks),
		"eventTypes", eventTypes)

	go pwm.runPluginNamespaceWorkflow(ctxNS, namespace, hooks, eventTypes)

	return state, nil
}

// StopNamespaceWorkflow stops a namespace workflow
func (pwm *PluginWorkflowManager) StopNamespaceWorkflow(namespace string, state *NamespaceState) {
	pwm.logger.Info("Stopping plugin-based namespace workflow", "namespace", namespace)
	state.Cancel()
}

// runPluginNamespaceWorkflow runs the actual workflow for a namespace using the plugin system
func (pwm *PluginWorkflowManager) runPluginNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
	eventTypes []string,
) {
	defer func() {
		if r := recover(); r != nil {
			pwm.logger.Error(fmt.Errorf("namespace workflow panic: %v", r),
				"plugin namespace workflow panicked", "namespace", namespace)
		}
	}()

	pwm.logger.Info("Plugin-based namespace workflow started", "namespace", namespace)

	// Initialize Kubernetes plugin with namespace configuration
	pluginConfig := map[string]interface{}{
		"client":    pwm.config.K8sClient,
		"namespace": namespace,
	}

	if err := pwm.pluginManager.InitializePlugin("kubernetes", pluginConfig); err != nil {
		pwm.logger.Error(err, "Failed to initialize Kubernetes plugin", "namespace", namespace)
		return
	}

	// Create plugin-aware processor
	processor := pipeline.NewPluginProcessor(
		pwm.pluginManager,
		pwm.mappingLoader,
		pwm.config.DedupManager,
		pwm.config.KagentClient,
		pwm.config.StatusManager,
	)

	// Start event processing
	if err := processor.StartEventProcessing(ctx, hooks); err != nil {
		pwm.logger.Error(err, "Plugin-based namespace workflow exited with error", "namespace", namespace)
	} else {
		pwm.logger.Info("Plugin-based namespace workflow finished", "namespace", namespace)
	}

	// Stop the processor
	if err := processor.Stop(); err != nil {
		pwm.logger.Error(err, "Failed to stop plugin processor", "namespace", namespace)
	}
}

// uniqueEventTypes extracts unique event types from hooks
func (pwm *PluginWorkflowManager) uniqueEventTypes(hooks []*kagentv1alpha2.Hook) []string {
	set := map[string]struct{}{}
	for _, h := range hooks {
		for _, ec := range h.Spec.EventConfigurations {
			set[ec.EventType] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}

// CalculateSignature creates a signature for hook changes detection
func (pwm *PluginWorkflowManager) CalculateSignature(hooks []*kagentv1alpha2.Hook) string {
	parts := make([]string, 0, len(hooks))
	for _, h := range hooks {
		cfgs := make([]string, 0, len(h.Spec.EventConfigurations))
		for _, ec := range h.Spec.EventConfigurations {
			cfgs = append(cfgs, ec.EventType+"|"+ec.AgentRef.Name+"|"+ec.Prompt)
		}
		parts = append(parts, h.Namespace+"/"+h.Name+"@"+strings.Join(cfgs, ";"))
	}
	return strings.Join(parts, ",")
}

// createDefaultMappings creates default event mappings for Kubernetes events
func (pwm *PluginWorkflowManager) createDefaultMappings() error {
	pwm.logger.Info("Creating default event mappings for Kubernetes")

	// Create default mappings for Kubernetes events
	defaultMappings := []*event.EventMapping{
		{
			EventSource:  "kubernetes",
			EventType:    "pod-restart",
			InternalType: "PodRestart",
			Description:  "Pod container restart detected",
			Severity:     "warning",
			Enabled:      true,
		},
		{
			EventSource:  "kubernetes",
			EventType:    "oom-kill",
			InternalType: "OOMKill",
			Description:  "Pod killed due to out of memory",
			Severity:     "error",
			Enabled:      true,
		},
		{
			EventSource:  "kubernetes",
			EventType:    "pod-pending",
			InternalType: "PodPending",
			Description:  "Pod stuck in pending state",
			Severity:     "warning",
			Enabled:      true,
		},
		{
			EventSource:  "kubernetes",
			EventType:    "probe-failed",
			InternalType: "ProbeFailed",
			Description:  "Health probe failure detected",
			Severity:     "warning",
			Enabled:      true,
		},
	}

	// Manually add mappings to the loader
	for _, mapping := range defaultMappings {
		key := fmt.Sprintf("%s:%s", mapping.EventSource, mapping.EventType)
		pwm.mappingLoader.AddMapping(key, mapping)
	}

	pwm.logger.Info("Successfully created default event mappings", "count", len(defaultMappings))
	return nil
}

// Shutdown gracefully shuts down the plugin system
func (pwm *PluginWorkflowManager) Shutdown() error {
	pwm.logger.Info("Shutting down plugin workflow manager")
	return pwm.pluginManager.Shutdown()
}
